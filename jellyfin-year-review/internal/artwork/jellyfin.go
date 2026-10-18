// Package artwork caches Jellyfin and fallback posters for offline serving.
package artwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"reelreview/internal/model"
	"reelreview/internal/source"
)

const maxImageBytes = 5 << 20

type Client struct {
	Jellyfin   *source.Jellyfin
	Key, Cache string
	wait       func(context.Context, time.Duration) error
	tvmaze     *tvmazeClient
}

func New(j *source.Jellyfin, key, cache string) *Client {
	return &Client{Jellyfin: j, Key: key, Cache: cache, wait: waitRetry, tvmaze: newTVmaze()}
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// validJPEG verifies both allocation bounds and the full image before publication.
func validJPEG(b []byte) bool {
	if len(b) > maxImageBytes {
		return false
	}
	cfg, e := jpeg.DecodeConfig(bytes.NewReader(b))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return false
	}
	_, e = jpeg.Decode(bytes.NewReader(b))
	return e == nil
}
func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func retryDelay(header string, attempt int) time.Duration {
	delay := time.Duration(attempt+1) * time.Second
	if n, e := strconv.Atoi(header); e == nil && n > 0 {
		if n > 3 {
			n = 3
		}
		delay = time.Duration(n) * time.Second
	} else if date, e := http.ParseTime(header); e == nil {
		if delta := time.Until(date); delta > 0 {
			delay = delta
		}
	}
	if delay > 3*time.Second {
		delay = 3 * time.Second
	}
	return delay
}

// fetch accepts only UUID item paths on the configured Jellyfin server. Image
// tags invalidate successful cache entries; untagged images are always refreshed.
func (c *Client) fetch(ctx context.Context, id, tag string) ([]byte, error) {
	normalized, e := model.ID(id)
	if e != nil {
		return nil, fmt.Errorf("invalid Jellyfin artwork ID")
	}
	if c.Jellyfin == nil || c.Key == "" {
		return nil, fmt.Errorf("Jellyfin artwork is not configured")
	}
	query := url.Values{"format": {"Jpg"}, "maxWidth": {"500"}, "maxHeight": {"750"}, "quality": {"85"}}
	if tag != "" {
		query.Set("tag", tag)
	}
	target := c.Jellyfin.URL + "/Items/" + normalized + "/Images/Primary?" + query.Encode()
	if e = os.MkdirAll(c.Cache, 0700); e != nil {
		return nil, fmt.Errorf("cannot create artwork cache")
	}
	file := filepath.Join(c.Cache, digest([]byte("jellyfin-primary-v1\x00"+target)))
	if tag != "" {
		if f, e := os.Open(file); e == nil {
			b, readErr := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
			f.Close()
			if readErr == nil && validJPEG(b) {
				return b, nil
			}
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		req, e := http.NewRequestWithContext(ctx, "GET", target, nil)
		if e != nil {
			return nil, fmt.Errorf("invalid artwork request")
		}
		req.Header.Set("Accept", "image/jpeg")
		req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Client="Reel Review", Device="Review generator", DeviceId="generator", Version="1.0.0", Token="%s"`, url.QueryEscape(c.Key)))
		res, e := c.Jellyfin.HTTP.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == 2 {
				return nil, fmt.Errorf("Jellyfin artwork unavailable")
			}
		} else {
			b, readErr := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
			res.Body.Close()
			if readErr != nil || len(b) > maxImageBytes {
				return nil, fmt.Errorf("invalid artwork response")
			}
			if res.StatusCode == http.StatusOK {
				if !validJPEG(b) {
					return nil, fmt.Errorf("invalid JPEG artwork")
				}
				if e = c.cache(file, b); e != nil {
					return nil, e
				}
				return b, nil
			}
			if res.StatusCode != http.StatusTooManyRequests && res.StatusCode < 500 {
				return nil, fmt.Errorf("Jellyfin artwork unavailable (%d)", res.StatusCode)
			}
		}
		if attempt < 2 {
			header := ""
			if res != nil {
				header = res.Header.Get("Retry-After")
			}
			if e = c.wait(ctx, retryDelay(header, attempt)); e != nil {
				return nil, e
			}
		}
	}
	return nil, fmt.Errorf("Jellyfin artwork retries exhausted")
}

// cache atomically replaces validated artwork/cache records without exposing bodies.
func (c *Client) cache(path string, b []byte) error {
	f, e := os.CreateTemp(c.Cache, ".poster-")
	if e != nil {
		return fmt.Errorf("cannot write artwork cache")
	}
	temp := f.Name()
	defer os.Remove(temp)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(temp, path)
	}
	if e != nil {
		return fmt.Errorf("cannot write artwork cache")
	}
	return nil
}

// Resolve downloads only content present in generated reviews, trying Jellyfin
// before TVmaze for series. Every poster reference points to a local JPEG.
func (c *Client) Resolve(ctx context.Context, items []model.Item, reviews []model.Review) (map[string][]byte, []string) {
	assets := map[string][]byte{}
	warnings := []string{}
	needed := map[string]bool{}
	index := map[string]model.Item{}
	posters := map[string]string{}
	sources := map[string]string{}
	series := map[string]model.Item{}
	for _, item := range items {
		index[source.ItemKey(item.ID)] = item
	}
	for _, r := range reviews {
		for _, v := range r.Shows {
			series[v.ID] = model.Item{ID: v.ID, Title: v.Title, Type: "Series"}
		}
		for _, list := range [][]model.Content{r.Movies, r.Shows, r.Abandoned} {
			for _, v := range list {
				needed[v.ID] = true
			}
		}
	}
	// Discard any poster references supplied by a canonical input snapshot.
	assign := func() {
		for i := range reviews {
			for _, list := range [][]model.Content{reviews[i].Movies, reviews[i].Shows, reviews[i].Abandoned} {
				for j := range list {
					list[j].Poster = posters[list[j].ID]
					list[j].PosterSource = sources[list[j].ID]
				}
			}
		}
	}
	if len(needed) == 0 {
		assign()
		return assets, warnings
	}
	if c.Jellyfin == nil || c.Key == "" {
		assign()
		return assets, []string{"Jellyfin artwork needs JELLYFIN_URL and JELLYFIN_API_KEY during generation; poster placeholders are used."}
	}
	ids := make([]string, 0, len(needed))
	for id := range needed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	failed := 0
	for _, id := range ids {
		item, ok := index[id]
		if !ok {
			item = series[id]
		}
		if item.Type != "Movie" && item.Type != "Series" {
			failed++
			continue
		}
		b, e := c.fetch(ctx, id, item.ImageTag)
		if e != nil {
			var sourceURL string
			b, sourceURL, e = c.fallback(ctx, item)
			if e != nil {
				failed++
				continue
			}
			sources[id] = sourceURL
		}
		name := digest(b) + ".jpg"
		assets[name] = b
		posters[id] = "/art/" + name
	}
	assign()
	if failed > 0 {
		warnings = append(warnings, fmt.Sprintf("Posters unavailable for %d titles after Jellyfin and TVmaze fallback; poster placeholders are used.", failed))
	}
	return assets, warnings
}
