package artwork

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"reelreview/internal/model"
)

type tvmazeClient struct {
	http     *http.Client
	nextCall time.Time
	wait     func(context.Context, time.Duration) error
}

type tvmazeShow struct {
	ID        int
	Name      string
	Premiered string
	Image     *struct{ Medium, Original string }
}

type tvmazePoster struct {
	ShowID int    `json:"showId"`
	JPEG   []byte `json:"jpeg"`
}

func newTVmaze() *tvmazeClient {
	t := &tvmazeClient{wait: waitRetry}
	t.http = &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		if req.URL.Host == "api.tvmaze.com" {
			if e := t.pace(req.Context()); e != nil {
				return e
			}
		}
		return nil
	}}
	return t
}

// pace includes lookup redirects in the API request rate limit.
func (t *tvmazeClient) pace(ctx context.Context) error {
	if delay := time.Until(t.nextCall); delay > 0 {
		if e := t.wait(ctx, delay); e != nil {
			return e
		}
	}
	t.nextCall = time.Now().Add(500 * time.Millisecond)
	return nil
}

// get uses a separate, credential-free client and only TVmaze's API/image hosts.
func (t *tvmazeClient) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	u, e := url.Parse(target)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Host != "api.tvmaze.com" && u.Host != "static.tvmaze.com") {
		return nil, fmt.Errorf("invalid TVmaze URL")
	}
	for attempt := 0; attempt < 3; attempt++ {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if u.Host == "api.tvmaze.com" {
			if e = t.pace(ctx); e != nil {
				return nil, e
			}
		}
		req, e := http.NewRequestWithContext(ctx, "GET", target, nil)
		if e != nil {
			return nil, fmt.Errorf("invalid TVmaze request")
		}
		req.Header.Set("User-Agent", "Jellyfin-Year-Review/1.0")
		res, e := t.http.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("TVmaze unavailable")
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, limit+1))
		res.Body.Close()
		if readErr != nil || int64(len(body)) > limit {
			return nil, fmt.Errorf("invalid TVmaze response")
		}
		if res.StatusCode == http.StatusOK {
			return body, nil
		}
		if (res.StatusCode != http.StatusTooManyRequests && res.StatusCode < 500) || attempt == 2 {
			return nil, fmt.Errorf("TVmaze unavailable (%d)", res.StatusCode)
		}
		if e = t.wait(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); e != nil {
			return nil, e
		}
	}
	return nil, fmt.Errorf("TVmaze retries exhausted")
}

// titleKey tolerates punctuation/case differences without accepting fuzzy matches.
func titleKey(title string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, title)
}

// match prefers provider IDs, then an unambiguous title/alias and premiere year.
// TVmaze contains television series, so movies are deliberately never matched.
func (t *tvmazeClient) match(ctx context.Context, item model.Item) (tvmazeShow, error) {
	for _, provider := range []string{"Tvdb", "Imdb"} {
		for name, id := range item.ProviderIDs {
			if !strings.EqualFold(name, provider) || id == "" {
				continue
			}
			parameter := "imdb"
			if provider == "Tvdb" {
				parameter = "thetvdb"
			}
			body, e := t.get(ctx, "https://api.tvmaze.com/lookup/shows?"+parameter+"="+url.QueryEscape(id), 1<<20)
			var show tvmazeShow
			if e == nil && json.Unmarshal(body, &show) == nil && show.ID > 0 && show.Image != nil {
				return show, nil
			}
		}
	}
	body, e := t.get(ctx, "https://api.tvmaze.com/search/shows?q="+url.QueryEscape(item.Title), 1<<20)
	if e != nil {
		return tvmazeShow{}, e
	}
	var hits []struct{ Show tvmazeShow }
	if json.Unmarshal(body, &hits) != nil || len(hits) > 100 {
		return tvmazeShow{}, fmt.Errorf("invalid TVmaze search response")
	}
	eligible := []tvmazeShow{}
	exact := []tvmazeShow{}
	key := titleKey(item.Title)
	for _, hit := range hits {
		show := hit.Show
		if show.ID < 1 || show.Image == nil || (item.Year != 0 && !strings.HasPrefix(show.Premiered, strconv.Itoa(item.Year)+"-")) {
			continue
		}
		eligible = append(eligible, show)
		if titleKey(show.Name) == key {
			exact = append(exact, show)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return tvmazeShow{}, fmt.Errorf("ambiguous TVmaze title")
	}
	// Check a few alternate/localized titles rather than accepting a fuzzy hit.
	aliases := []tvmazeShow{}
	for i, show := range eligible {
		if i == 3 {
			break
		}
		body, e := t.get(ctx, fmt.Sprintf("https://api.tvmaze.com/shows/%d/akas", show.ID), 1<<20)
		var names []struct{ Name string }
		if e != nil || json.Unmarshal(body, &names) != nil {
			continue
		}
		for _, name := range names {
			if titleKey(name.Name) == key {
				aliases = append(aliases, show)
				break
			}
		}
	}
	if len(aliases) == 1 {
		return aliases[0], nil
	}
	return tvmazeShow{}, fmt.Errorf("no unambiguous TVmaze match")
}

// fallback caches successful TVmaze posters and attribution for offline publishing.
func (c *Client) fallback(ctx context.Context, item model.Item) ([]byte, string, error) {
	if item.Type != "Series" || titleKey(item.Title) == "" {
		return nil, "", fmt.Errorf("TVmaze fallback requires a series title")
	}
	identity, _ := json.Marshal(struct {
		Title     string
		Year      int
		Providers map[string]string
	}{item.Title, item.Year, item.ProviderIDs})
	file := filepath.Join(c.Cache, digest(append([]byte("tvmaze-poster-v1\x00"), identity...)))
	if f, e := os.Open(file); e == nil {
		body, readErr := io.ReadAll(io.LimitReader(f, 8<<20))
		f.Close()
		var cached tvmazePoster
		if readErr == nil && json.Unmarshal(body, &cached) == nil && cached.ShowID > 0 && validJPEG(cached.JPEG) {
			return cached.JPEG, fmt.Sprintf("https://www.tvmaze.com/shows/%d", cached.ShowID), nil
		}
	}
	show, e := c.tvmaze.match(ctx, item)
	if e != nil {
		return nil, "", e
	}
	target := show.Image.Medium
	if target == "" {
		target = show.Image.Original
	}
	imageURL, e := url.Parse(target)
	if e != nil || imageURL.Host != "static.tvmaze.com" {
		return nil, "", fmt.Errorf("invalid TVmaze image host")
	}
	body, e := c.tvmaze.get(ctx, target, maxImageBytes)
	if e != nil {
		return nil, "", e
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(body))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return nil, "", fmt.Errorf("invalid TVmaze image")
	}
	img, _, e := image.Decode(bytes.NewReader(body))
	if e != nil {
		return nil, "", fmt.Errorf("invalid TVmaze image")
	}
	var jpegBytes bytes.Buffer
	if jpeg.Encode(&jpegBytes, img, &jpeg.Options{Quality: 85}) != nil || !validJPEG(jpegBytes.Bytes()) {
		return nil, "", fmt.Errorf("invalid TVmaze image")
	}
	cached, _ := json.Marshal(tvmazePoster{ShowID: show.ID, JPEG: jpegBytes.Bytes()})
	if os.MkdirAll(c.Cache, 0700) != nil || c.cache(file, cached) != nil {
		return nil, "", fmt.Errorf("cannot write artwork cache")
	}
	return jpegBytes.Bytes(), fmt.Sprintf("https://www.tvmaze.com/shows/%d", show.ID), nil
}
