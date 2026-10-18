package generate

import (
	"reflect"
	"testing"

	"reelreview/internal/model"
)

func TestPlatformsCountDistinctEligibleTitles(t *testing.T) {
	s := fixture()
	s.Users = append(s.Users, model.User{ID: bob, Name: "Bob"})
	s.Items[0].Studios = []string{"Netflix", "NETFLIX", "Unrelated studio"}
	s.Items = append(s.Items,
		model.Item{ID: "series", Title: "Show", Type: "Series", Studios: []string{"HBO Max", "HBO"}},
		model.Item{ID: "e1", Type: "Episode", SeriesID: "series"},
		model.Item{ID: "e2", Type: "Episode", SeriesID: "series"},
		model.Item{ID: "apple", Type: "Movie", Studios: []string{"Apple Original Films"}},
		model.Item{ID: "unknown", Type: "Movie", Studios: []string{"Warner Bros. Pictures"}},
		model.Item{ID: "shared", Type: "Movie", Platforms: []string{" Netflix ", "netflix", "AppleTV", "Apple TV+"}},
		model.Item{ID: "excluded", Type: "Movie", Platforms: []string{"Hulu"}},
	)
	for i, item := range []string{"film", "film", "e1", "e2", "e1", "apple", "unknown", "shared", "excluded"} {
		p := play(item+string(rune('a'+i)), "2025-03-01T12:00:00Z", "2025-03-01T13:00:00Z")
		p.ItemID = item
		if item == "excluded" {
			p.UserID = bob
		}
		s.Playback = append(s.Playback, p)
	}
	outside := play("outside", "2024-03-01T12:00:00Z", "2024-03-01T13:00:00Z")
	outside.ItemID = "excluded"
	short := play("short", "2025-03-01T12:00:00Z", "2025-03-01T12:00:30Z")
	short.ItemID = "excluded"
	s.Playback = append(s.Playback, outside, short)
	want := []model.Platform{{Name: "Apple TV+", Movies: 2}, {Name: "Netflix", Movies: 2}, {Name: "HBO", Shows: 1}, {Name: "Other", Movies: 1}}
	if got := compute(t, s, DefaultConfig(2025))[0].Platforms; !reflect.DeepEqual(got, want) {
		t.Fatalf("platforms = %+v, want %+v", got, want)
	}
}

func TestPlatformMetadataAndEmptyHistory(t *testing.T) {
	for _, tc := range []struct {
		name string
		item model.Item
		want []string
	}{
		{"explicit overrides studios", model.Item{Platforms: []string{" Custom Service ", "custom service"}, Studios: []string{"Netflix"}}, []string{"Custom Service"}},
		{"blank explicit uses studios", model.Item{Platforms: []string{" "}, Studios: []string{"amazon prime video"}}, []string{"Prime Video"}},
		{"missing metadata", model.Item{}, []string{}},
		{"no guessed Disney availability", model.Item{Studios: []string{"Walt Disney Pictures"}}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := itemPlatforms(tc.item); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("platforms = %v, want %v", got, tc.want)
			}
		})
	}
	if got := compute(t, fixture(), DefaultConfig(2025))[0].Platforms; len(got) != 0 {
		t.Fatalf("empty history produced counts: %+v", got)
	}
}

// Other stays last even when most watched titles lack platform metadata.
func TestPlatformsKeepOtherLast(t *testing.T) {
	movies := []model.Content{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "netflix"}}
	items := map[string]model.Item{"netflix": {Studios: []string{"Netflix"}}}
	want := []model.Platform{{Name: "Netflix", Movies: 1}, {Name: "Other", Movies: 3}}
	if got := platforms(movies, nil, items); !reflect.DeepEqual(got, want) {
		t.Fatalf("platforms = %+v, want %+v", got, want)
	}
}
