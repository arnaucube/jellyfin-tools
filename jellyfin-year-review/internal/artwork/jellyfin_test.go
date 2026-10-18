package artwork

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reelreview/internal/model"
	"reelreview/internal/source"
)

const movieID = "11111111111111111111111111111111"
const seriesID = "22222222222222222222222222222222"
const unusedID = "33333333333333333333333333333333"

func picture(t *testing.T, red uint8) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.RGBA{R: red, A: 255})
		}
	}
	if e := jpeg.Encode(&b, img, nil); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func jellyfinClient(t *testing.T, server *httptest.Server, base string) *source.Jellyfin {
	t.Helper()
	j, e := source.NewJellyfin(server.URL + base)
	if e != nil {
		t.Fatal(e)
	}
	return j
}
func TestJellyfinPostersCacheAndOnlyReviewedTitles(t *testing.T) {
	imageBytes := picture(t, 180)
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.Header.Get("Authorization"), `Token="operator-only-key"`) || !strings.Contains(r.Header.Get("Authorization"), `DeviceId="generator"`) {
			t.Error("missing generator credential/identity")
		}
		if r.URL.Query().Get("format") != "Jpg" || r.URL.Query().Get("maxWidth") != "500" || r.URL.Query().Get("maxHeight") != "750" || strings.Contains(r.URL.RawQuery, "operator-only-key") {
			t.Error("unsafe artwork query")
		}
		if r.URL.Path != "/jellyfin/Items/"+movieID+"/Images/Primary" && r.URL.Path != "/jellyfin/Items/"+seriesID+"/Images/Primary" {
			t.Error("unnecessary or incorrect poster request")
		}
		w.Write(imageBytes)
	}))
	defer api.Close()
	c := New(jellyfinClient(t, api, "/jellyfin"), "operator-only-key", t.TempDir())
	items := []model.Item{{ID: movieID, Type: "Movie", ImageTag: "movie-v1"}, {ID: seriesID, Type: "Series", ImageTag: "show-v1"}, {ID: unusedID, Type: "Movie", ImageTag: "unwatched"}}
	reviews := []model.Review{{Movies: []model.Content{{ID: movieID, Title: "Film", Poster: "https://untrusted.invalid/image"}}, Shows: []model.Content{{ID: seriesID, Title: "Show"}}, Abandoned: []model.Content{{ID: movieID}}}, {Movies: []model.Content{{ID: movieID}}}}
	assets, warnings := c.Resolve(context.Background(), items, reviews)
	want := "/art/" + digest(imageBytes) + ".jpg"
	if calls != 2 || len(assets) != 1 || len(warnings) != 0 || reviews[0].Movies[0].Poster != want || reviews[0].Shows[0].Poster != want || reviews[0].Abandoned[0].Poster != want || reviews[1].Movies[0].Poster != want {
		t.Fatalf("poster resolution/cache failed: %d calls, %d assets, %v", calls, len(assets), warnings)
	}
	api.Close()
	// A fixed-tag cached image remains available when the source is unreachable.
	cached, warnings := c.Resolve(context.Background(), items, reviews)
	if !reflect.DeepEqual(cached, assets) || len(warnings) != 0 {
		t.Fatal("successful posters were not cached")
	}
}
func TestImageTagChangesUntaggedRefreshAndCorruptCache(t *testing.T) {
	calls := 0
	red := uint8(0)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write(picture(t, red)) }))
	defer api.Close()
	cache := t.TempDir()
	c := New(jellyfinClient(t, api, ""), "key", cache)
	first, e := c.fetch(context.Background(), movieID, "v1")
	if e != nil {
		t.Fatal(e)
	}
	red = 255
	second, e := c.fetch(context.Background(), movieID, "v2")
	if e != nil || bytes.Equal(first, second) || calls != 2 {
		t.Fatal("changed image tag retained old artwork")
	}
	for i := 0; i < 2; i++ {
		if _, e = c.fetch(context.Background(), movieID, ""); e != nil {
			t.Fatal(e)
		}
	}
	if calls != 4 {
		t.Fatal("untagged image was cached indefinitely")
	}
	files, e := os.ReadDir(cache)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if e = os.WriteFile(filepath.Join(cache, f.Name()), []byte("corrupt"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = c.fetch(context.Background(), movieID, "v2"); e != nil || calls != 5 {
		t.Fatal("corrupt cache was not recovered")
	}
}
func TestMissingInvalidAndUnconfiguredPosters(t *testing.T) {
	for _, status := range []int{404, 401, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				w.Write([]byte("private upstream error"))
			}))
			defer api.Close()
			c := New(jellyfinClient(t, api, ""), "key", t.TempDir())
			reviews := []model.Review{{Movies: []model.Content{{ID: movieID, Poster: "/old.jpg"}}}}
			assets, warnings := c.Resolve(context.Background(), []model.Item{{ID: movieID, Type: "Movie"}}, reviews)
			if calls != 1 || len(assets) != 0 || len(warnings) != 1 || reviews[0].Movies[0].Poster != "" || strings.Contains(warnings[0], "private upstream") {
				t.Fatal("failed artwork leaked upstream details or retained old reference")
			}
		})
	}
	reviews := []model.Review{{Movies: []model.Content{{ID: movieID, Poster: "https://untrusted.invalid"}}}}
	assets, warnings := New(nil, "", t.TempDir()).Resolve(context.Background(), nil, reviews)
	if len(assets) != 0 || len(warnings) != 1 || reviews[0].Movies[0].Poster != "" {
		t.Fatal("unconfigured artwork did not use placeholders")
	}
	if _, warnings = New(nil, "", t.TempDir()).Resolve(context.Background(), nil, nil); len(warnings) != 0 {
		t.Fatal("empty history needs no artwork")
	}
}
func TestRejectsUnsafeIDsRedirectsAndOversizedImages(t *testing.T) {
	foreignCalls := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls++; w.Write(picture(t, 10)) }))
	defer foreign.Close()
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Redirect(w, r, foreign.URL+"/poster", 302) }))
	defer api.Close()
	c := New(jellyfinClient(t, api, ""), "operator-key", t.TempDir())
	if _, e := c.fetch(context.Background(), "../Users/Me", ""); e == nil || calls != 0 {
		t.Fatal("unsafe item path reached Jellyfin")
	}
	if _, e := c.fetch(context.Background(), movieID, ""); e == nil || calls != 1 || foreignCalls != 0 {
		t.Fatal("redirect followed with operator credentials")
	}
	var large bytes.Buffer
	if e := jpeg.Encode(&large, image.NewGray(image.Rect(0, 0, 4097, 1)), nil); e != nil {
		t.Fatal(e)
	}
	if validJPEG(large.Bytes()) || validJPEG(bytes.Repeat([]byte{0}, maxImageBytes+1)) {
		t.Fatal("image bounds not enforced")
	}
	valid := picture(t, 10)
	if validJPEG(valid[:len(valid)-10]) {
		t.Fatal("truncated JPEG accepted")
	}
}
func TestRetriesBoundedAndCancellation(t *testing.T) {
	for _, status := range []int{429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "600")
				w.WriteHeader(status)
			}))
			defer api.Close()
			c := New(jellyfinClient(t, api, ""), "key", t.TempDir())
			delays := []time.Duration{}
			c.wait = func(ctx context.Context, d time.Duration) error { delays = append(delays, d); return nil }
			if _, e := c.fetch(context.Background(), movieID, ""); e == nil || calls != 3 || !reflect.DeepEqual(delays, []time.Duration{3 * time.Second, 3 * time.Second}) {
				t.Fatal("unbounded retry behavior")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, e := c.fetch(ctx, movieID, ""); e == nil || calls != 3 {
				t.Fatal("canceled request reached source")
			}
		})
	}
	if e := waitRetry(canceledContext(), time.Hour); e == nil {
		t.Fatal("backoff ignored cancellation")
	}
}
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
