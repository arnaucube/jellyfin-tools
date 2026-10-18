package artwork

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"reelreview/internal/model"
)

type tvmazeTransport func(*http.Request) (*http.Response, error)

func (f tvmazeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Keep synthetic tests on a private transport while validating real API hostnames.
func fakeTVmaze(t *testing.T, c *Client, handler http.HandlerFunc) {
	t.Helper()
	c.tvmaze.wait = func(context.Context, time.Duration) error { return nil }
	c.tvmaze.http.Transport = tvmazeTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || strings.Contains(r.URL.String(), c.Key) {
			t.Error("private credentials reached TVmaze")
		}
		if r.Header.Get("User-Agent") != "Jellyfin-Year-Review/1.0" {
			t.Error("missing application identity")
		}
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Result(), nil
	})
}

func TestTVmazeFallbackProviderLookupCacheAndJellyfinPriority(t *testing.T) {
	jellyfinRecovered := false
	jellyfinImage := picture(t, 25)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if jellyfinRecovered {
			w.Write(jellyfinImage)
		} else {
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	c := New(jellyfinClient(t, api, ""), "secret-jellyfin-key", t.TempDir())
	calls := []string{}
	fakeTVmaze(t, c, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Host+r.URL.Path)
		switch r.URL.Path {
		case "/lookup/shows":
			if r.URL.Query().Get("thetvdb") != "123" {
				t.Error("missing Jellyfin provider ID")
			}
			w.Header().Set("Location", "https://api.tvmaze.com/shows/42")
			w.WriteHeader(302)
		case "/shows/42":
			w.Write([]byte(`{"id":42,"name":"Frieren","image":{"medium":"https://static.tvmaze.com/poster.png"}}`))
		case "/poster.png":
			png.Encode(w, image.NewRGBA(image.Rect(0, 0, 2, 3)))
		default:
			t.Error("unexpected TVmaze request")
			w.WriteHeader(404)
		}
	})
	items := []model.Item{{ID: seriesID, Title: "Frieren", Type: "Series", ProviderIDs: map[string]string{"Tvdb": "123"}}, {ID: movieID, Title: "Frieren", Type: "Movie"}}
	reviews := []model.Review{{Shows: []model.Content{{ID: seriesID, Title: "Frieren"}}, Movies: []model.Content{{ID: movieID, Title: "Frieren"}}}}
	assets, warnings := c.Resolve(context.Background(), items, reviews)
	show := reviews[0].Shows[0]
	if len(calls) != 3 || len(assets) != 1 || len(warnings) != 1 || show.PosterSource != "https://www.tvmaze.com/shows/42" || !strings.HasPrefix(show.Poster, "/art/") || reviews[0].Movies[0].Poster != "" {
		t.Fatalf("fallback not published correctly: calls=%d assets=%d warnings=%v", len(calls), len(assets), warnings)
	}
	for _, b := range assets {
		if !validJPEG(b) {
			t.Error("TVmaze PNG was not converted to a valid local JPEG")
		}
	}
	cached, _ := c.Resolve(context.Background(), items, reviews)
	if len(calls) != 3 || !reflect.DeepEqual(assets, cached) {
		t.Fatal("successful fallback was not cached")
	}
	jellyfinRecovered = true
	_, warnings = c.Resolve(context.Background(), items, reviews)
	if len(calls) != 3 || len(warnings) != 0 || reviews[0].Shows[0].PosterSource != "" || reviews[0].Shows[0].Poster != "/art/"+digest(jellyfinImage)+".jpg" {
		t.Fatal("Jellyfin did not take priority over cached fallback")
	}
}

func TestTVmazeTitleAliasAndYearMatching(t *testing.T) {
	cases := []struct {
		name, title, results, aliases string
		year, want                    int
	}{
		{"title", "A Show!", `[{"show":{"id":1,"name":"a show","premiered":"2023-01-01","image":{"medium":"https://static.tvmaze.com/1.jpg"}}}]`, `[]`, 2023, 1},
		{"alias", "Frieren: Beyond Journey's End", `[{"show":{"id":2,"name":"Sousou no Frieren","premiered":"2023-09-29","image":{"medium":"https://static.tvmaze.com/2.jpg"}}}]`, `[{"name":"Frieren: Beyond Journey's End"}]`, 2023, 2},
		{"wrong year", "A Show", `[{"show":{"id":1,"name":"A Show","premiered":"2000-01-01","image":{"medium":"https://static.tvmaze.com/1.jpg"}}}]`, `[]`, 2023, 0},
		{"ambiguous", "A Show", `[{"show":{"id":1,"name":"A Show","image":{}}},{"show":{"id":2,"name":"A Show","image":{}}}]`, `[]`, 0, 0},
		{"no image", "A Show", `[{"show":{"id":1,"name":"A Show","image":null}}]`, `[]`, 0, 0},
		{"no match", "A Show", `[]`, `[]`, 0, 0},
		{"fuzzy mismatch", "A Show", `[{"show":{"id":1,"name":"A Different Show","image":{}}}]`, `[{"name":"Another Show"}]`, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(nil, "private-key", t.TempDir())
			fakeTVmaze(t, c, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/search/shows" {
					if r.URL.Query().Get("q") != tc.title {
						t.Error("incorrect title search")
					}
					w.Write([]byte(tc.results))
				} else {
					w.Write([]byte(tc.aliases))
				}
			})
			show, e := c.tvmaze.match(context.Background(), model.Item{Title: tc.title, Year: tc.year})
			if tc.want == 0 && e == nil || tc.want != 0 && (e != nil || show.ID != tc.want) {
				t.Fatalf("unexpected match: id=%d error=%v", show.ID, e)
			}
		})
	}
}

func TestTVmazeRejectsUnsafeImagesAndInvalidResponses(t *testing.T) {
	for _, target := range []string{"https://private.invalid/secret", "http://static.tvmaze.com/image", "https://static.tvmaze.com/image"} {
		t.Run(target, func(t *testing.T) {
			c := New(nil, "private-key", t.TempDir())
			fakeTVmaze(t, c, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/search/shows" {
					json.NewEncoder(w).Encode([]any{map[string]any{"show": map[string]any{"id": 1, "name": "Show", "image": map[string]string{"medium": target}}}})
				} else if r.URL.Host == "static.tvmaze.com" {
					w.Write([]byte("private upstream error"))
				} else {
					t.Error("unexpected request to foreign host")
				}
			})
			_, _, e := c.fallback(context.Background(), model.Item{Title: "Show", Type: "Series"})
			if e == nil || strings.Contains(e.Error(), "private upstream") || strings.Contains(e.Error(), "private-key") {
				t.Fatal("unsafe or invalid fallback accepted")
			}
		})
	}
}

func TestTVmazeRetryRedirectBoundsAndCancellation(t *testing.T) {
	c := New(nil, "private-key", t.TempDir())
	calls := 0
	fakeTVmaze(t, c, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(429)
	})
	_, e := c.tvmaze.get(context.Background(), "https://api.tvmaze.com/search/shows?q=Show", 1<<20)
	if e == nil || calls != 3 {
		t.Fatal("TVmaze retries were not bounded")
	}
	calls = 0
	fakeTVmaze(t, c, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "https://private.invalid/secret")
		w.WriteHeader(302)
	})
	_, e = c.tvmaze.get(context.Background(), "https://api.tvmaze.com/lookup/shows?thetvdb=1", 1<<20)
	if e == nil || calls != 1 {
		t.Fatal("unsafe redirect followed")
	}
	_, e = c.tvmaze.get(canceledContext(), "https://api.tvmaze.com/search/shows?q=Show", 1<<20)
	if e == nil || calls != 1 {
		t.Fatal("canceled request reached TVmaze")
	}
	fakeTVmaze(t, c, func(w http.ResponseWriter, r *http.Request) { w.Write(bytes.Repeat([]byte("x"), 20)) })
	if _, e := c.tvmaze.get(context.Background(), "https://api.tvmaze.com/search/shows?q=Show", 10); e == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestTVmazeFailuresKeepPlaceholderAndAreNotCached(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer api.Close()
	c := New(jellyfinClient(t, api, ""), "private-key", t.TempDir())
	available := false
	searches := 0
	fakeTVmaze(t, c, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/shows" {
			searches++
			if available {
				w.Write([]byte(`[{"show":{"id":42,"name":"Show","image":{"medium":"https://static.tvmaze.com/image.jpg"}}}]`))
			} else {
				w.Write([]byte(`[]`))
			}
		} else {
			w.Write(picture(t, 10))
		}
	})
	items := []model.Item{{ID: seriesID, Title: "Show", Type: "Series"}}
	reviews := []model.Review{{Shows: []model.Content{{ID: seriesID, Title: "Show", Poster: "https://untrusted.invalid", PosterSource: "https://untrusted.invalid"}}}}
	assets, warnings := c.Resolve(context.Background(), items, reviews)
	if len(assets) != 0 || len(warnings) != 1 || reviews[0].Shows[0].Poster != "" || reviews[0].Shows[0].PosterSource != "" {
		t.Fatal("failed fallback did not clear unsafe references")
	}
	available = true
	assets, warnings = c.Resolve(context.Background(), items, reviews)
	if searches != 2 || len(assets) != 1 || len(warnings) != 0 || reviews[0].Shows[0].PosterSource != "https://www.tvmaze.com/shows/42" {
		t.Fatal("failed fallback was permanently cached")
	}
}
