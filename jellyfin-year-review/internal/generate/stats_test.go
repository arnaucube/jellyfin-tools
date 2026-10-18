package generate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"reelreview/internal/model"
)

const alice = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const bob = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func instant(s string) time.Time {
	t, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return t
}
func number(n float64) *float64 { return &n }
func fixture() model.Snapshot {
	return model.Snapshot{Version: 1, CapturedAt: instant("2026-01-01T00:00:00Z"), Source: "synthetic exact intervals", Coverage: "2025 full synthetic fixture", Users: []model.User{{ID: alice, Name: "Alice"}}, Items: []model.Item{{ID: "film", Title: "A Film", Type: "Movie", Runtime: 7200, Genres: []string{"Drama", "Adventure"}}}}
}
func play(id, start, end string) model.Playback {
	a, b := instant(start), instant(end)
	return model.Playback{ID: id, UserID: alice, ItemID: "film", Start: a, End: b, Exact: true, Seconds: b.Sub(a).Seconds(), PositionStart: number(0), PositionEnd: number(b.Sub(a).Seconds())}
}
func compute(t *testing.T, s model.Snapshot, c Config) []model.Review {
	t.Helper()
	r, e := Compute(s, c)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestResumedCoverageAndDuplicate(t *testing.T) {
	s := fixture()
	a := play("a", "2025-01-02T12:00:00Z", "2025-01-02T13:00:00Z")
	b := play("b", "2025-01-03T12:00:00Z", "2025-01-03T13:00:00Z")
	b.PositionStart = number(3600)
	b.PositionEnd = number(7200)
	s.Playback = []model.Playback{a, a, b}
	r := compute(t, s, DefaultConfig(2025))[0]
	if r.Seconds != 7200 || r.Records != 2 || len(r.Abandoned) != 0 || *r.Movies[0].Completion != 100 {
		t.Fatalf("resumed/duplicate result: %+v", r)
	}
	b.PositionStart = number(0)
	b.PositionEnd = number(3600)
	s.Playback = []model.Playback{a, b}
	r = compute(t, s, DefaultConfig(2025))[0]
	if len(r.Abandoned) != 1 || *r.Abandoned[0].Completion != 50 {
		t.Fatal("repeated positions inflated completion")
	}
	b.ID = a.ID
	s.Playback = []model.Playback{a, b}
	if _, e := Compute(s, DefaultConfig(2025)); e == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}
func TestYearMidnightAndDST(t *testing.T) {
	tests := []struct {
		name, zone, start, end string
		seconds                float64
		hours                  map[int]float64
		days                   int
	}{
		{"new year", "UTC", "2024-12-31T23:30:00Z", "2025-01-01T00:30:00Z", 1800, map[int]float64{0: 30}, 1},
		{"local boundary", "America/New_York", "2025-01-01T04:30:00Z", "2025-01-01T05:30:00Z", 1800, map[int]float64{0: 30}, 1},
		{"spring", "America/New_York", "2025-03-09T06:30:00Z", "2025-03-09T07:30:00Z", 3600, map[int]float64{1: 30, 2: 0, 3: 30}, 1},
		{"fall", "America/New_York", "2025-11-02T05:30:00Z", "2025-11-02T06:30:00Z", 3600, map[int]float64{1: 60}, 1},
		{"midnight", "UTC", "2025-04-01T23:30:00Z", "2025-04-02T00:30:00Z", 3600, map[int]float64{23: 30, 0: 30}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fixture()
			s.Playback = []model.Playback{play("a", tt.start, tt.end)}
			c := DefaultConfig(2025)
			c.Timezone = tt.zone
			r := compute(t, s, c)[0]
			if r.Seconds != tt.seconds || r.ActiveDays != tt.days {
				t.Fatalf("bad boundary: %f, %d", r.Seconds, r.ActiveDays)
			}
			for h, want := range tt.hours {
				if r.Hourly[h] != want {
					t.Fatalf("hour %d got %v want %v", h, r.Hourly[h], want)
				}
			}
		})
	}
}
func TestMarathonOverlapGapAndTie(t *testing.T) {
	v := []span{{instant("2025-01-01T00:00:00Z"), instant("2025-01-01T02:00:00Z")}, {instant("2025-01-01T01:00:00Z"), instant("2025-01-01T03:00:00Z")}, {instant("2025-01-01T03:45:00Z"), instant("2025-01-01T04:45:00Z")}, {instant("2025-01-02T00:00:00Z"), instant("2025-01-02T04:00:00Z")}}
	r := marathon(v, 45*time.Minute)
	if r.Watched != 14400 || r.Elapsed != 17100 || r.Records != 3 || !r.Start.Equal(v[0].start) {
		t.Fatalf("overlap/gap/tie: %+v", r)
	}
	r = marathon(v, 44*time.Minute)
	if r.Records != 1 || !r.Start.Equal(v[3].start) {
		t.Fatal("gap was not enforced")
	}
}
func TestRankingTiesAndSmallCohort(t *testing.T) {
	rs := []model.Review{{Seconds: 7200, Availability: map[string]model.Availability{}}, {Seconds: 7200, Availability: map[string]model.Availability{}}, {Seconds: 3600, Availability: map[string]model.Availability{}}, {Seconds: 100, Availability: map[string]model.Availability{}}}
	rank(rs)
	if rs[0].Ranking.Rank != 1 || rs[1].Ranking.Rank != 1 || rs[2].Ranking.Rank != 3 || rs[0].Ranking.Percentile != 50 || rs[3].Ranking != nil {
		t.Fatal("ranking ties wrong")
	}
	rank(rs[:1])
	if rs[0].Ranking.Percentile != 0 || rs[0].Ranking.Cohort != 1 {
		t.Fatal("solo cohort")
	}
}
func TestMissingMetadataAndAggregateAvailability(t *testing.T) {
	s := fixture()
	s.Playback = []model.Playback{{ID: "aggregate", UserID: alice, ItemID: "film", ObservedAt: instant("2025-02-03T19:00:00Z"), Seconds: 3600}, {ID: "deleted", UserID: alice, ItemID: "deleted", ObservedAt: instant("2025-01-01T00:00:00Z"), Seconds: 900}}
	r := compute(t, s, DefaultConfig(2025))[0]
	if r.Seconds != 3600 || r.Hourly[19] != 1 || r.Marathon != nil || r.Personality != nil || len(r.Abandoned) != 0 || r.Availability["completion"].Available {
		t.Fatalf("invented unavailable metrics: %+v", r)
	}
	s.Playback = nil
	r = compute(t, s, DefaultConfig(2025))[0]
	if r.Seconds != 0 || r.Records != 0 || r.Ranking != nil || r.Marathon != nil {
		t.Fatal("empty history")
	}
	s.Items[0].Runtime = 0
	s.Playback = []model.Playback{play("a", "2025-01-02T12:00:00Z", "2025-01-02T13:00:00Z")}
	r = compute(t, s, DefaultConfig(2025))[0]
	if r.Movies[0].Completion != nil {
		t.Fatal("missing runtime fabricated completion")
	}
}
func TestPersonalityGateAndPriority(t *testing.T) {
	r := model.Review{Records: 10, Seconds: 36000, MovieSeconds: 36000}
	r.Hourly[0] = 600
	r.Weekdays[0] = 600
	p := personality(r, true)
	if p == nil || p.Name != "Night Owl" {
		t.Fatal("deterministic first-match priority")
	}
	if personality(r, false) != nil {
		t.Fatal("aggregate personality invented")
	}
	r.Records = 9
	if personality(r, true) != nil {
		t.Fatal("minimum data gate")
	}
}
func TestDeterministicAndAtomicPublication(t *testing.T) {
	s := fixture()
	s.Playback = []model.Playback{play("a", "2025-01-02T12:00:00Z", "2025-01-02T13:00:00Z")}
	c := DefaultConfig(2025)
	a := compute(t, s, c)
	b := compute(t, s, c)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if !reflect.DeepEqual(ja, jb) {
		t.Fatal("nondeterministic computation")
	}
	root := t.TempDir()
	if e := Publish(root, a, nil); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(root, "CURRENT"))
	if e := Publish(root, a, map[string][]byte{"../unsafe": {1}}); e == nil {
		t.Fatal("unsafe asset accepted")
	}
	after, _ := os.ReadFile(filepath.Join(root, "CURRENT"))
	if string(before) != string(after) {
		t.Fatal("failed generation changed pointer")
	}
	if e := Publish(root, a, nil); e != nil {
		t.Fatal(e)
	}
	after, _ = os.ReadFile(filepath.Join(root, "CURRENT"))
	if string(before) != string(after) {
		t.Fatal("non deterministic publication")
	}
	c.Year = 2024
	if e := Publish(root, compute(t, s, c), nil); e != nil {
		t.Fatal(e)
	}
	m, _, e := Manifest(root)
	if e != nil || !reflect.DeepEqual(m.Users[alice], []int{2024, 2025}) {
		t.Fatal("year was lost")
	}
	if e := os.WriteFile(filepath.Join(root, ".generate.lock"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e := Publish(root, a, nil); e == nil {
		t.Fatal("concurrent writer allowed")
	}
}

func TestPerUserTimezoneThresholdAndGenreWeight(t *testing.T) {
	s := fixture()
	s.Users = append(s.Users, model.User{ID: bob, Name: "Bob"})
	s.Items[0].Genres = []string{"Drama", "drama", "Adventure"}
	a := play("a", "2025-01-01T00:00:00Z", "2025-01-01T01:00:00Z")
	b := a
	b.UserID = bob
	tiny := play("tiny", "2025-01-02T12:00:00Z", "2025-01-02T12:00:30Z")
	s.Playback = []model.Playback{a, b, tiny}
	c := DefaultConfig(2025)
	c.UserTimezones = map[string]string{bob: "America/New_York"}
	rs := compute(t, s, c)
	if rs[0].Seconds != 3600 || rs[0].Records != 1 || rs[0].Genres[0].Seconds != 1800 || rs[0].Genres[1].Seconds != 1800 {
		t.Fatal("minimum threshold or distinct genre weighting")
	}
	if rs[1].Seconds != 0 || rs[1].Timezone != "America/New_York" {
		t.Fatal("per-user local year not honored")
	}
}

func TestAllFourteenPersonalityRulesReachable(t *testing.T) {
	base := func(seconds float64) model.Review {
		r := model.Review{Seconds: seconds, MovieSeconds: seconds / 2, TVSeconds: seconds / 2, Records: 100, UniqueTitles: 100, ActiveDays: 100}
		for i := range r.Hourly {
			r.Hourly[i] = seconds / 60 / 24
		}
		for i := range r.Weekdays {
			r.Weekdays[i] = seconds / 60 / 7
		}
		for i := range r.Monthly {
			r.Monthly[i] = seconds / 60 / 12
		}
		return r
	}
	cases := []struct {
		name   string
		change func(*model.Review)
	}{
		{"Night Owl", func(r *model.Review) { r.Hourly = [24]float64{0: r.Seconds / 60} }},
		{"Early Bird", func(r *model.Review) { r.Hourly = [24]float64{6: r.Seconds / 60} }},
		{"Weekend Warrior", func(r *model.Review) { r.Weekdays = [7]float64{0: r.Seconds / 60} }},
		{"Marathoner", func(r *model.Review) { r.Marathon = &model.Marathon{Watched: 21600} }},
		{"Binge Watcher", func(r *model.Review) {
			r.TVSeconds = r.Seconds * .7
			r.MovieSeconds = r.Seconds * .3
			r.Marathon = &model.Marathon{Watched: 10800, Records: 3}
		}},
		{"Movie Buff", func(r *model.Review) { r.MovieSeconds = r.Seconds * .8 }},
		{"Series Devotee", func(r *model.Review) { r.TVSeconds = r.Seconds * .8 }},
		{"Comfort Rewatcher", func(r *model.Review) { r.UniqueTitles = 40 }},
		{"Genre Explorer", func(r *model.Review) {
			for _, n := range []string{"A", "B", "C", "D", "E", "F"} {
				r.Genres = append(r.Genres, model.Named{Name: n, Seconds: r.Seconds * .05})
			}
		}},
		{"Prime-Time Regular", func(r *model.Review) { r.Hourly = [24]float64{20: r.Seconds / 60} }},
		{"Daily Viewer", func(r *model.Review) { r.ActiveDays = 180 }},
		{"Seasonal Viewer", func(r *model.Review) { r.Monthly = [12]float64{0: r.Seconds / 60} }},
		{"Short-Session Viewer", func(r *model.Review) { *r = base(60000) }},
		{"Balanced Viewer", func(r *model.Review) {}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := base(360000)
			tt.change(&r)
			p := personality(r, true)
			if p == nil || p.Name != tt.name {
				t.Fatalf("got %+v", p)
			}
		})
	}
}

func TestContentViewingMonths(t *testing.T) {
	cases := []struct {
		name, zone string
		playback   []model.Playback
		months     []int
	}{
		{"month boundary", "UTC", []model.Playback{play("a", "2025-01-31T23:30:00Z", "2025-02-01T00:30:00Z")}, []int{1, 2}},
		{"exclusive end", "UTC", []model.Playback{play("a", "2025-01-31T23:00:00Z", "2025-02-01T00:00:00Z")}, []int{1}},
		{"local month", "America/New_York", []model.Playback{play("a", "2025-02-01T01:00:00Z", "2025-02-01T02:00:00Z")}, []int{1}},
		{"year clipping", "UTC", []model.Playback{play("a", "2024-12-31T23:30:00Z", "2025-01-01T00:30:00Z"), play("b", "2025-12-31T23:30:00Z", "2026-01-01T00:30:00Z")}, []int{1, 12}},
		{"resumed and repeated", "UTC", []model.Playback{play("c", "2025-03-01T12:00:00Z", "2025-03-01T13:00:00Z"), play("a", "2025-01-01T12:00:00Z", "2025-01-01T13:00:00Z"), play("b", "2025-01-02T12:00:00Z", "2025-01-02T13:00:00Z")}, []int{1, 3}},
		{"aggregate observed month", "America/New_York", []model.Playback{{ID: "a", UserID: alice, ItemID: "film", Seconds: 3600, ObservedAt: instant("2025-03-01T02:00:00Z")}}, []int{2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			s.Playback = tc.playback
			c := DefaultConfig(2025)
			c.Timezone = tc.zone
			r := compute(t, s, c)[0]
			if len(r.Movies) != 1 || !reflect.DeepEqual(r.Movies[0].Months, tc.months) {
				t.Fatalf("unexpected movie months: %+v", r.Movies)
			}
		})
	}
}

func TestSeriesViewingMonthsCombineEpisodes(t *testing.T) {
	s := fixture()
	s.Items = append(s.Items,
		model.Item{ID: "series", Title: "A Series", Type: "Series"},
		model.Item{ID: "episode1", Title: "Episode One", Type: "Episode", SeriesID: "series"},
		model.Item{ID: "episode2", Title: "Episode Two", Type: "Episode", SeriesID: "series"})
	for i, date := range []string{"2025-01-01", "2025-01-02", "2025-03-01"} {
		p := play(date, date+"T12:00:00Z", date+"T13:00:00Z")
		p.ItemID = "episode1"
		if i > 0 {
			p.ItemID = "episode2"
		}
		s.Playback = append(s.Playback, p)
	}
	r := compute(t, s, DefaultConfig(2025))[0]
	if len(r.Shows) != 1 || r.Shows[0].ID != "series" || r.Shows[0].Records != 3 || !reflect.DeepEqual(r.Shows[0].Months, []int{1, 3}) {
		t.Fatalf("episodes did not combine into monthly series entries: %+v", r.Shows)
	}
}
