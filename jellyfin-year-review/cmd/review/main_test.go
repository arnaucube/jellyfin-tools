package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reelreview/internal/generate"
	"reelreview/internal/model"
)

// Test the actual CLI generation path against source-shaped Jellyfin and native
// Jellystat fixtures, including parent-series artwork and deterministic caching.
func TestGenerateDownloadsJellyfinPosters(t *testing.T) {
	const user = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const movie = "11111111111111111111111111111111"
	const series = "22222222222222222222222222222222"
	const episode = "33333333333333333333333333333333"
	const key = "synthetic-operator-key"
	var jpegBytes bytes.Buffer
	if e := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); e != nil {
		t.Fatal(e)
	}
	images := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Jellyfin 12 accepts credentials in Authorization, without legacy headers.
		if r.Header.Get("X-Emby-Token") != "" {
			t.Error("unexpected legacy authentication header")
		}
		if !strings.Contains(r.Header.Get("Authorization"), `Token="`+key+`"`) {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/jellyfin/Users":
			json.NewEncoder(w).Encode([]map[string]any{{"Id": user, "Name": "Viewer"}})
		case "/jellyfin/Items":
			json.NewEncoder(w).Encode(map[string]any{"TotalRecordCount": 3, "Items": []map[string]any{
				{"Id": movie, "Name": "Film", "Type": "Movie", "ImageTags": map[string]string{"Primary": "film-v1"}},
				{"Id": series, "Name": "Show", "Type": "Series", "ImageTags": map[string]string{"Primary": "show-v1"}},
				{"Id": episode, "Name": "Episode", "Type": "Episode", "SeriesId": series, "SeriesName": "Show"},
			}})
		case "/jellyfin/Items/" + movie + "/Images/Primary", "/jellyfin/Items/" + series + "/Images/Primary":
			images++
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(jpegBytes.Bytes())
		default:
			t.Error("unexpected generation request")
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	t.Setenv("JELLYFIN_URL", api.URL+"/jellyfin")
	t.Setenv("JELLYFIN_API_KEY", key)
	root := t.TempDir()
	history := filepath.Join(root, "history.jsonl")
	f, e := os.Create(history)
	if e != nil {
		t.Fatal(e)
	}
	enc := json.NewEncoder(f)
	for _, row := range []map[string]any{
		{"Id": "movie-row", "UserId": user, "NowPlayingItemId": movie, "PlaybackDuration": 3600, "ActivityDateInserted": "2025-02-03T19:00:00Z"},
		{"Id": "episode-row", "UserId": user, "NowPlayingItemId": series, "EpisodeId": episode, "PlaybackDuration": 3600, "ActivityDateInserted": "2025-02-03T20:00:00Z"},
	} {
		if e = enc.Encode(row); e != nil {
			t.Fatal(e)
		}
	}
	f.Close()
	output := filepath.Join(root, "data")
	args := []string{"generate", "--year", "2025", "--history", history, "--captured-at", "2026-01-01T00:00:00Z", "--coverage", "synthetic native fixture", "--output", output, "--cache", filepath.Join(root, "cache")}
	if e = run(args); e != nil {
		t.Fatal(e)
	}
	before, e := generate.Current(output)
	if e != nil {
		t.Fatal(e)
	}
	if e = run(args); e != nil {
		t.Fatal(e)
	}
	after, e := generate.Current(output)
	if e != nil || before != after || images != 2 {
		t.Fatal("generation was not deterministic or cache was not reused")
	}
	api.Close() // The published files must remain usable without their source.
	b, e := os.ReadFile(filepath.Join(after, user, "2025.json"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), key) || strings.Contains(string(b), api.URL) {
		t.Fatal("source credentials or remote image URL entered review JSON")
	}
	var review model.Review
	if e = json.Unmarshal(b, &review); e != nil {
		t.Fatal(e)
	}
	if len(review.Movies) != 1 || len(review.Shows) != 1 || review.Seconds != 7200 {
		t.Fatal("incorrect generated source statistics")
	}
	for _, poster := range []string{review.Movies[0].Poster, review.Shows[0].Poster} {
		if !strings.HasPrefix(poster, "/art/") || !generate.SafeAsset(strings.TrimPrefix(poster, "/art/")) {
			t.Fatal("nonlocal generated poster")
		}
		stored, e := os.ReadFile(filepath.Join(after, strings.TrimPrefix(poster, "/")))
		if e != nil || !bytes.Equal(stored, jpegBytes.Bytes()) {
			t.Fatal("local poster absent after source shutdown")
		}
	}
}
