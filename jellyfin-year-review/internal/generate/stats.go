// Package generate computes all owner-visible statistics before publishing a dataset.
package generate

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"reelreview/internal/model"
	"reelreview/internal/source"
)

type Config struct {
	Year           int
	Timezone       string
	UserTimezones  map[string]string
	MinimumSeconds float64
	Completion     float64
	MarathonGap    time.Duration
}

func DefaultConfig(year int) Config {
	return Config{Year: year, Timezone: "UTC", MinimumSeconds: 60, Completion: 90, MarathonGap: 45 * time.Minute}
}
func (c Config) Validate() error {
	if c.Year < 1970 || c.Year > 9998 || c.MinimumSeconds < 0 || math.IsNaN(c.MinimumSeconds) || math.IsInf(c.MinimumSeconds, 0) || math.IsNaN(c.Completion) || c.Completion <= 0 || c.Completion > 100 || c.MarathonGap < 0 || c.MarathonGap > 24*time.Hour {
		return fmt.Errorf("invalid generation thresholds or year")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("invalid default timezone")
	}
	for id, z := range c.UserTimezones {
		n, err := model.ID(id)
		if err != nil || n != id {
			return fmt.Errorf("timezone keys must be normalized UUIDs")
		}
		if _, err := time.LoadLocation(z); err != nil {
			return fmt.Errorf("invalid user timezone")
		}
	}
	return nil
}

type span struct{ start, end time.Time }
type mediaSpan struct{ start, end float64 }

func normalizeName(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "Unknown"
	}
	return s
}
func named(m map[string]*model.Named) []model.Named {
	out := []model.Named{}
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seconds != out[j].Seconds {
			return out[i].Seconds > out[j].Seconds
		}
		return out[i].Name < out[j].Name
	})
	return out
}
func add(m map[string]*model.Named, name string, seconds float64) {
	name = normalizeName(name)
	key := strings.ToLower(name)
	v := m[key]
	if v == nil {
		v = &model.Named{Name: name}
		m[key] = v
	}
	if name < v.Name {
		v.Name = name
	}
	v.Seconds += seconds
	v.Records++
}
func content(m map[string]*model.Content) []model.Content {
	out := []model.Content{}
	for _, v := range m {
		sort.Ints(v.Months)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seconds != out[j].Seconds {
			return out[i].Seconds > out[j].Seconds
		}
		if out[i].Records != out[j].Records {
			return out[i].Records > out[j].Records
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Compute clips exact intervals at local year boundaries. Aggregate-only records
// are explicitly attributed to their observed end year; no history is inferred.
func Compute(s model.Snapshot, c Config) ([]model.Review, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := source.Validate(&s); err != nil {
		return nil, err
	}
	items := map[string]model.Item{}
	for _, i := range s.Items {
		items[i.ID] = i
	}
	out := []model.Review{}
	for _, u := range s.Users {
		zone := c.Timezone
		if z := c.UserTimezones[u.ID]; z != "" {
			zone = z
		}
		loc, _ := time.LoadLocation(zone)
		begin := time.Date(c.Year, 1, 1, 0, 0, 0, 0, loc)
		end := begin.AddDate(1, 0, 0)
		r := model.Review{Rules: model.Rules{MinimumSeconds: c.MinimumSeconds, CompletionPercent: c.Completion, MarathonGapSeconds: c.MarathonGap.Seconds()}, Version: 1, User: u, Year: c.Year, Timezone: zone, GeneratedAt: s.CapturedAt, Source: s.Source, Coverage: s.Coverage, Availability: map[string]model.Availability{}, Warnings: []string{}, Movies: []model.Content{}, Shows: []model.Content{}, Abandoned: []model.Content{}, DistributionMetric: "watched minutes"}
		movies, shows := map[string]*model.Content{}, map[string]*model.Content{}
		genres, devices, clients := map[string]*model.Named{}, map[string]*model.Named{}, map[string]*model.Named{}
		positions := map[string][]mediaSpan{}
		positionUnknown := map[string]bool{}
		unique := map[string]bool{}
		days := map[string]bool{}
		intervals := []span{}
		exact := true
		completionKnown := true
		deleted := 0
		aggregate := 0
		for _, p := range s.Playback {
			if p.UserID != u.ID || p.Seconds < c.MinimumSeconds {
				continue
			}
			a, b := p.Start, p.End
			seconds := p.Seconds
			if p.Exact {
				if !b.After(begin) || !a.Before(end) {
					continue
				}
				if a.Before(begin) {
					a = begin
				}
				if b.After(end) {
					b = end
				}
				seconds = b.Sub(a).Seconds()
			} else {
				if p.ObservedAt.Before(begin) || !p.ObservedAt.Before(end) {
					continue
				}
			}
			item, ok := items[p.ItemID]
			if !ok {
				deleted++
				continue
			}
			if item.Type != "Movie" && item.Type != "Episode" {
				continue
			}
			if !p.Exact {
				aggregate++
				exact = false
			}
			r.Seconds += seconds
			r.Records++
			unique[item.ID] = true
			key, title, poster := item.ID, item.Title, item.Poster
			m := movies
			if item.Type == "Episode" {
				r.TVSeconds += seconds
				m = shows
				if item.SeriesID != "" {
					key = item.SeriesID
					title = item.SeriesTitle
					if series, ok := items[key]; ok {
						title = series.Title
						poster = series.Poster
						if len(item.Genres) == 0 {
							item.Genres = series.Genres
						}
					}
				}
			} else {
				r.MovieSeconds += seconds
			}
			if title == "" {
				title = "Unknown title"
			}
			v := m[key]
			if v == nil {
				v = &model.Content{ID: key, Title: title, Poster: poster}
				m[key] = v
			}
			v.Seconds += seconds
			v.Records++
			if p.Exact {
				// Include every local month touched by the clipped half-open interval.
				lastMonth := int(b.Add(-time.Nanosecond).In(loc).Month())
				for month := int(a.In(loc).Month()); month <= lastMonth; month++ {
					addContentMonth(v, month)
				}
			} else {
				addContentMonth(v, int(p.ObservedAt.In(loc).Month()))
			}
			gs := map[string]string{}
			for _, g := range item.Genres {
				name := normalizeName(g)
				key := strings.ToLower(name)
				if previous, ok := gs[key]; !ok || name < previous {
					gs[key] = name
				}
			}
			if len(gs) == 0 {
				gs["unknown"] = "Unknown"
			}
			for _, g := range gs {
				add(genres, g, seconds/float64(len(gs)))
			}
			add(devices, p.Device, seconds)
			add(clients, p.Client, seconds)
			if item.Type == "Movie" {
				if item.Runtime <= 0 || p.PositionStart == nil || !p.Exact || !p.Start.Equal(a) || !p.End.Equal(b) {
					positionUnknown[item.ID] = true
					completionKnown = false
				} else {
					positions[item.ID] = append(positions[item.ID], mediaSpan{math.Min(*p.PositionStart, item.Runtime), math.Min(*p.PositionEnd, item.Runtime)})
				}
			}
			if p.Exact {
				intervals = append(intervals, span{a, b})
				allocate(&r, a, b, loc, days)
			} else {
				t := p.ObservedAt.In(loc)
				r.Hourly[t.Hour()]++
				r.Weekdays[t.Weekday()]++
				r.Monthly[int(t.Month())-1]++
				days[t.Format("2006-01-02")] = true
			}
		}
		// Mixed precision uses end-record counts for all distributions rather than mixing units.
		if !exact {
			r.Hourly = [24]float64{}
			r.Weekdays = [7]float64{}
			r.Monthly = [12]float64{}
			days = map[string]bool{}
			for _, p := range s.Playback {
				i, ok := items[p.ItemID]
				if !ok || p.UserID != u.ID || p.Seconds < c.MinimumSeconds || (i.Type != "Movie" && i.Type != "Episode") {
					continue
				}
				t := p.ObservedAt
				if p.Exact {
					if !p.End.After(begin) || !p.Start.Before(end) {
						continue
					}
					t = p.End.Add(-time.Nanosecond)
					if !t.Before(end) {
						t = end.Add(-time.Nanosecond)
					}
				}
				if t.Before(begin) || !t.Before(end) {
					continue
				}
				t = t.In(loc)
				r.Hourly[t.Hour()]++
				r.Weekdays[t.Weekday()]++
				r.Monthly[int(t.Month())-1]++
				days[t.Format("2006-01-02")] = true
			}
			r.DistributionMetric = "activity record ends (counts)"
			r.Warnings = append(r.Warnings, "Aggregate history is assigned to the record end year; exact yearly watch time, play counts, and timing cannot be recovered.")
		}
		r.UniqueTitles = len(unique)
		r.ActiveDays = len(days)
		r.Movies = content(movies)
		r.Shows = content(shows)
		r.Genres = named(genres)
		r.Devices = named(devices)
		r.Clients = named(clients)
		r.Platforms = platforms(r.Movies, r.Shows, items)
		for i := range r.Movies {
			v := &r.Movies[i]
			if positionUnknown[v.ID] {
				continue
			}
			ranges := positions[v.ID]
			if len(ranges) == 0 {
				continue
			}
			sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
			covered, finish := 0.0, 0.0
			for _, p := range ranges {
				if p.end > finish {
					covered += p.end - math.Max(p.start, finish)
					finish = p.end
				}
			}
			pct := math.Min(100, 100*covered/items[v.ID].Runtime)
			v.Completion = &pct
			if pct < c.Completion {
				r.Abandoned = append(r.Abandoned, *v)
			}
		}
		if exact {
			r.Marathon = marathon(intervals, c.MarathonGap)
		}
		r.Availability["exactWatchTime"] = model.Availability{Available: exact, Reason: reason(exact, "Jellystat end-attributed totals are estimates of selected-year viewing.")}
		r.Availability["marathon"] = model.Availability{Available: exact && r.Records > 0, Reason: reason(exact && r.Records > 0, "Exact uninterrupted intervals are required.")}
		r.Availability["completion"] = model.Availability{Available: completionKnown, Reason: reason(completionKnown, "Some movies lack runtime or actual media-position ranges; they are omitted from abandonment results.")}
		if deleted > 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%d records excluded because media metadata was deleted or unavailable.", deleted))
		}
		if aggregate > 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%d aggregate activity records; records may combine resumed viewing.", aggregate))
		}
		r.Personality = personality(r, exact)
		r.Availability["personality"] = model.Availability{Available: r.Personality != nil, Reason: reason(r.Personality != nil, "At least 10 exact records and 5 watched hours are required.")}
		out = append(out, r)
	}
	rank(out)
	return out, nil
}

// addContentMonth deduplicates resumed movies and episodes of the same series.
func addContentMonth(v *model.Content, month int) {
	for _, existing := range v.Months {
		if existing == month {
			return
		}
	}
	v.Months = append(v.Months, month)
}

func reason(ok bool, s string) string {
	if ok {
		return ""
	}
	return s
}

// allocate walks real UTC minute boundaries, preserving repeated/skipped DST hours.
func allocate(r *model.Review, a, b time.Time, loc *time.Location, days map[string]bool) {
	for a.Before(b) {
		next := a.Truncate(time.Minute).Add(time.Minute)
		if next.After(b) {
			next = b
		}
		n := next.Sub(a).Minutes()
		t := a.In(loc)
		r.Hourly[t.Hour()] += n
		r.Weekdays[t.Weekday()] += n
		r.Monthly[int(t.Month())-1] += n
		days[t.Format("2006-01-02")] = true
		a = next
	}
}
func marathon(v []span, gap time.Duration) *model.Marathon {
	if len(v) == 0 {
		return nil
	}
	sort.Slice(v, func(i, j int) bool {
		if v[i].start.Equal(v[j].start) {
			return v[i].end.Before(v[j].end)
		}
		return v[i].start.Before(v[j].start)
	})
	var best *model.Marathon
	var run model.Marathon
	save := func() {
		run.Elapsed = run.End.Sub(run.Start).Seconds()
		if best == nil || run.Watched > best.Watched || (run.Watched == best.Watched && run.Start.Before(best.Start)) {
			copy := run
			best = &copy
		}
	}
	for _, s := range v {
		if run.Records == 0 || s.start.After(run.End.Add(gap)) {
			if run.Records > 0 {
				save()
			}
			run = model.Marathon{Start: s.start, End: s.end, Watched: s.end.Sub(s.start).Seconds(), Records: 1}
			continue
		}
		run.Records++
		if s.end.After(run.End) {
			a := s.start
			if a.Before(run.End) {
				a = run.End
			}
			run.Watched += s.end.Sub(a).Seconds()
			run.End = s.end
		}
	}
	save()
	return best
}
func rank(rs []model.Review) {
	eligible := []int{}
	total := 0.0
	for i, r := range rs {
		if r.Seconds >= 3600 {
			eligible = append(eligible, i)
			total += r.Seconds
		}
	}
	for _, i := range eligible {
		higher, lower := 0, 0
		for _, j := range eligible {
			if rs[j].Seconds > rs[i].Seconds {
				higher++
			}
			if rs[j].Seconds < rs[i].Seconds {
				lower++
			}
		}
		pct := 0.0
		if len(eligible) > 1 {
			pct = 100 * float64(lower) / float64(len(eligible)-1)
		}
		rs[i].Ranking = &model.Ranking{Rank: higher + 1, Cohort: len(eligible), Percentile: pct, Average: total / float64(len(eligible))}
	}
	for i := range rs {
		rs[i].Availability["ranking"] = model.Availability{Available: rs[i].Ranking != nil, Reason: reason(rs[i].Ranking != nil, "At least one watched hour is required for cohort eligibility.")}
	}
}
