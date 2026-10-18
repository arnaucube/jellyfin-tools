// This controlled test service binds loopback only and uses disposable synthetic data.
// Production has no demo login or test-service mode.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reelreview/internal/artwork"
	"reelreview/internal/auth"
	"strings"

	"reelreview/internal/generate"
	"reelreview/internal/server"
	"reelreview/internal/source"
)

func main() {
	root, e := os.MkdirTemp("", "reel-e2e-")
	if e != nil {
		panic(e)
	}
	defer os.RemoveAll(root)
	data := filepath.Join(root, "data")
	snapshot, e := source.Load("testdata/snapshot.json")
	if e != nil {
		panic(e)
	}
	// Give the synthetic snapshot real UUID-shaped media IDs and fetch posters
	// from a controlled generation-only source that closes before serving starts.
	ids := map[string]string{}
	for i, item := range snapshot.Items {
		ids[item.ID] = fmt.Sprintf("%032x", i+1)
	}
	for i := range snapshot.Items {
		item := &snapshot.Items[i]
		item.ID = ids[item.ID]
		item.SeriesID = ids[item.SeriesID]
		item.ImageTag = "fixture-image-v1"
	}
	for i := range snapshot.Playback {
		snapshot.Playback[i].ItemID = ids[snapshot.Playback[i].ItemID]
	}
	var jpegBytes bytes.Buffer
	if e = jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 2, 3)), nil); e != nil {
		panic(e)
	}
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), `Token="fixture-art-key"`) {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes.Bytes())
	}))
	imageSource, e := source.NewJellyfin(images.URL)
	if e != nil {
		panic(e)
	}
	posters := artwork.New(imageSource, "fixture-art-key", filepath.Join(root, "cache"))

	for _, year := range []int{2024, 2025} {
		c := generate.DefaultConfig(year)
		rs, e := generate.Compute(snapshot, c)
		if e != nil {
			panic(e)
		}
		assets, warnings := posters.Resolve(context.Background(), snapshot.Items, rs)
		if len(warnings) > 0 {
			panic("synthetic posters unavailable")
		}
		if e = generate.Publish(data, rs, assets); e != nil {
			panic(e)
		}
	}
	images.Close()
	if os.Getenv("REEL_E2E_AUTH_MODE") == "offline" {
		b, e := os.ReadFile("testdata/offline-auth.json")
		if e != nil {
			panic(e)
		}
		path := filepath.Join(root, "users.json")
		if e = os.WriteFile(path, b, 0600); e != nil {
			panic(e)
		}
		p, e := auth.NewOffline(path)
		if e != nil {
			panic(e)
		}
		s, e := server.NewWithAuth(data, "web/build", "http://127.0.0.1:18180", p)
		if e != nil {
			panic(e)
		}
		defer s.Close()
		fmt.Println("SYNTHETIC OFFLINE TEST SERVICE: no Jellyfin client or server")
		if e = server.HTTPServer("127.0.0.1:18180", s).ListenAndServe(); e != nil {
			panic(e)
		}
		return
	}

	revoked := false
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users/AuthenticateByName":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["Username"] != "fixture" || b["Pw"] != "fixture-only" {
				w.WriteHeader(401)
				return
			}
			revoked = false
			json.NewEncoder(w).Encode(map[string]string{"AccessToken": "test-service-token"})
		case "/Users/Me":
			if revoked || !strings.Contains(r.Header.Get("Authorization"), `Token="test-service-token"`) {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"Id": snapshot.Users[0].ID, "Name": "Fixture Viewer"})
		case "/Sessions/Logout":
			revoked = true
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer jf.Close()
	j, e := source.NewJellyfin(jf.URL)
	if e != nil {
		panic(e)
	}
	s, e := server.New(data, "web/build", "http://127.0.0.1:18180", j)
	if e != nil {
		panic(e)
	}
	defer s.Close()
	fmt.Println("SYNTHETIC TEST SERVICE on loopback :18180")
	h := server.HTTPServer("127.0.0.1:18180", s)
	if e = h.ListenAndServe(); e != nil {
		panic(e)
	}
}
