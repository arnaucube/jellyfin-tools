// Package source reads immutable history exports and Jellyfin API metadata.
package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reelreview/internal/model"
)

var ErrDenied = errors.New("authentication denied")
var ErrUnavailable = errors.New("Jellyfin unavailable")

type Jellyfin struct {
	URL  string
	HTTP *http.Client
}
type Account struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Policy struct {
		Disabled bool `json:"IsDisabled"`
	} `json:"Policy"`
}

func NewJellyfin(raw string) (*Jellyfin, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid JELLYFIN_URL")
	}
	return &Jellyfin{URL: strings.TrimRight(raw, "/"), HTTP: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Call never returns server bodies or transport URLs, which may contain sensitive data.
func (j *Jellyfin) Call(ctx context.Context, method, path, token, device string, body, out any) error {
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, j.URL+path, &b)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	authorization := fmt.Sprintf(`MediaBrowser Client="Reel Review", Device="Review browser", DeviceId="%s", Version="1.0.0"`, url.QueryEscape(device))
	if token != "" {
		authorization += fmt.Sprintf(`, Token="%s"`, url.QueryEscape(token))
	}
	req.Header.Set("Authorization", authorization)
	res, err := j.HTTP.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return ErrDenied
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ErrUnavailable
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(out); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}
func (j *Jellyfin) Me(ctx context.Context, token, device string) (Account, error) {
	var a Account
	err := j.Call(ctx, "GET", "/Users/Me", token, device, nil, &a)
	if err != nil {
		return a, err
	}
	id, err := model.ID(a.ID)
	if err != nil || a.Policy.Disabled {
		return a, ErrDenied
	}
	a.ID = id
	return a, nil
}
func (j *Jellyfin) Login(ctx context.Context, name, password, device string) (Account, string, error) {
	var r struct {
		AccessToken string
		User        Account
	}
	err := j.Call(ctx, "POST", "/Users/AuthenticateByName", "", device, map[string]string{"Username": name, "Pw": password}, &r)
	if err != nil {
		return Account{}, "", err
	}
	if r.AccessToken == "" {
		return Account{}, "", ErrDenied
	}
	a, err := j.Me(ctx, r.AccessToken, device)
	if err != nil {
		_ = j.Call(ctx, "POST", "/Sessions/Logout", r.AccessToken, device, nil, nil)
		return Account{}, "", err
	}
	return a, r.AccessToken, nil
}

// Metadata takes a bounded inventory once; API responses become the generation snapshot.
func (j *Jellyfin) Metadata(ctx context.Context, key string) ([]model.User, []model.Item, error) {
	var users []Account
	if err := j.Call(ctx, "GET", "/Users", key, "generator", nil, &users); err != nil {
		return nil, nil, err
	}
	result := []model.User{}
	for _, u := range users {
		id, err := model.ID(u.ID)
		if err != nil {
			return nil, nil, err
		}
		if !u.Policy.Disabled {
			result = append(result, model.User{ID: id, Name: u.Name})
		}
	}
	items := []model.Item{}
	for start := 0; start < 1000000; start += 500 {
		var page struct {
			Items []struct {
				ID                               string `json:"Id"`
				Name, Type, SeriesID, SeriesName string
				ProductionYear                   int
				RunTimeTicks                     int64
				Genres                           []string
				ImageTags                        map[string]string
				ProviderIDs                      map[string]string
			}
			TotalRecordCount int
		}
		path := fmt.Sprintf("/Items?Recursive=true&IncludeItemTypes=Movie,Episode,Series&Fields=Genres,ProviderIds&EnableImages=true&StartIndex=%d&Limit=500", start)
		if err := j.Call(ctx, "GET", path, key, "generator", nil, &page); err != nil {
			return nil, nil, err
		}
		for _, i := range page.Items {
			items = append(items, model.Item{ID: i.ID, Title: i.Name, Type: i.Type, SeriesID: i.SeriesID, SeriesTitle: i.SeriesName, Year: i.ProductionYear, Runtime: float64(i.RunTimeTicks) / 1e7, Genres: i.Genres, ImageTag: i.ImageTags["Primary"], ProviderIDs: i.ProviderIDs})
		}
		if start+len(page.Items) >= page.TotalRecordCount {
			return result, items, nil
		}
		if len(page.Items) == 0 {
			return nil, nil, fmt.Errorf("incomplete Jellyfin inventory")
		}
	}
	return nil, nil, fmt.Errorf("Jellyfin inventory exceeds limit")
}
