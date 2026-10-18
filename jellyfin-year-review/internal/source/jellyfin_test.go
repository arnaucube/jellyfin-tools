package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetadataIncludesPrimaryImageTags(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), `Token="operator-key"`) {
			t.Error("missing operator credential")
		}
		switch r.URL.Path {
		case "/jellyfin/Users":
			w.Write([]byte(`[{"Id":"AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA","Name":"Viewer"}]`))
		case "/jellyfin/Items":
			if r.URL.Query().Get("EnableImages") != "true" || !strings.Contains(r.URL.Query().Get("Fields"), "ProviderIds") || !strings.Contains(r.URL.Query().Get("Fields"), "Studios") {
				t.Error("image metadata not requested")
			}
			w.Write([]byte(`{"Items":[{"Id":"11111111111111111111111111111111","Name":"Film","Type":"Movie","RunTimeTicks":72000000000,"Genres":["Drama"],"Studios":[{"Name":"Netflix"}],"ImageTags":{"Primary":"image-version"},"ProviderIds":{"Tvdb":"123","Imdb":"tt123"}}],"TotalRecordCount":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	j, e := NewJellyfin(api.URL + "/jellyfin")
	if e != nil {
		t.Fatal(e)
	}
	users, items, e := j.Metadata(context.Background(), "operator-key")
	if e != nil || len(users) != 1 || len(items) != 1 || items[0].ProviderIDs["Tvdb"] != "123" || items[0].ProviderIDs["Imdb"] != "tt123" || items[0].ImageTag != "image-version" || items[0].Runtime != 7200 || items[0].Genres[0] != "Drama" || len(items[0].Studios) != 1 || items[0].Studios[0] != "Netflix" {
		t.Fatal("incomplete Jellyfin metadata", e)
	}
}
