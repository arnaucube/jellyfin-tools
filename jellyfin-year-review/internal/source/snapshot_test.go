package source

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reelreview/internal/model"
)

func TestJellystatRawRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.jsonl")
	row := `{"Id":"record1","UserId":"AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA","NowPlayingItemId":"series","EpisodeId":"episode","PlaybackDuration":"1800","ActivityDateInserted":"2025-05-01T19:00:00Z","DeviceName":"TV","Client":"Jellyfin"}`
	if e := os.WriteFile(path, []byte(row+"\n"+row+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := Jellystat(path, []model.User{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, nil, time.Now(), "test coverage")
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Playback) != 1 || s.Playback[0].ItemID != "episode" || s.Playback[0].Exact || s.Playback[0].Seconds != 1800 {
		t.Fatalf("incorrect raw source semantics %+v", s)
	}
	if e := os.WriteFile(path, []byte(`{"results":[{}],"TotalDuration":1800}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Jellystat(path, nil, nil, time.Now(), "test"); e == nil {
		t.Fatal("grouped API result accepted")
	}
}
func TestInvalidSource(t *testing.T) {
	s := model.Snapshot{Version: 1, CapturedAt: time.Now(), Source: "test", Coverage: "test", Users: []model.User{{ID: "../escape"}}}
	if Validate(&s) == nil {
		t.Fatal("invalid user ID")
	}
	s.Users = nil
	s.Playback = []model.Playback{{ID: "x", UserID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ItemID: "x", Seconds: -1}}
	if Validate(&s) == nil {
		t.Fatal("negative seconds")
	}
}

func TestPlaybackReportingImportsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.jsonl")
	row := `{"Id":"record1","UserId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","NowPlayingItemId":"film","PlaybackDuration":1800,"ActivityDateInserted":"2025-05-01T19:00:00Z","imported":true}`
	if e := os.WriteFile(path, []byte(row), 0600); e != nil {
		t.Fatal(e)
	}
	if _, err := Jellystat(path, nil, nil, time.Now(), "test"); err == nil {
		t.Fatal("prohibited plugin import accepted")
	}
}
