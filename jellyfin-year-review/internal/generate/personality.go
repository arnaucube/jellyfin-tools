package generate

import "reelreview/internal/model"

// personality uses ordered, evidence-based gates; the first match wins. The final
// balanced label applies only after the same minimum-data gate as every other type.
func personality(r model.Review, exact bool) *model.Personality {
	if !exact || r.Records < 10 || r.Seconds < 18000 {
		return nil
	}
	sum := func(a, b int) float64 {
		n := 0.0
		for i := a; i < b; i++ {
			n += r.Hourly[i]
		}
		return n
	}
	minutes := r.Seconds / 60
	night := (sum(0, 5) + sum(23, 24)) / minutes
	early := sum(5, 10) / minutes
	weekend := (r.Weekdays[0] + r.Weekdays[6]) / minutes
	repeated := float64(r.Records-r.UniqueTitles) / float64(r.Records)
	maxMonth := 0.0
	for _, v := range r.Monthly {
		if v > maxMonth {
			maxMonth = v
		}
	}
	rules := []struct {
		name, why string
		match     bool
	}{
		{"Night Owl", "At least 40% of your watch time falls between 23:00 and 05:00.", night >= .4},
		{"Early Bird", "At least 40% of your watch time falls between 05:00 and 10:00.", early >= .4},
		{"Weekend Warrior", "At least 60% of your watch time falls on weekends.", weekend >= .6},
		{"Marathoner", "Your longest run includes at least six watched hours.", r.Marathon != nil && r.Marathon.Watched >= 21600},
		{"Binge Watcher", "TV dominates your viewing and your longest run spans at least three records and three watched hours.", r.TVSeconds/r.Seconds >= .7 && r.Marathon != nil && r.Marathon.Records >= 3 && r.Marathon.Watched >= 10800},
		{"Movie Buff", "Movies account for at least 80% of your watch time.", r.MovieSeconds/r.Seconds >= .8},
		{"Series Devotee", "TV accounts for at least 80% of your watch time.", r.TVSeconds/r.Seconds >= .8},
		{"Comfort Rewatcher", "At least half your records revisit a previously seen title or episode.", repeated >= .5},
		{"Genre Explorer", "You watched at least six known genres with meaningful watch time.", knownGenres(r) >= 6},
		{"Prime-Time Regular", "At least half your watch time falls between 19:00 and 23:00.", sum(19, 23)/minutes >= .5},
		{"Daily Viewer", "You watched on at least 180 different dates.", r.ActiveDays >= 180},
		{"Seasonal Viewer", "At least 40% of your watch time falls in one month.", maxMonth/minutes >= .4},
		{"Short-Session Viewer", "Your average uninterrupted record lasts at most 25 minutes.", r.Seconds/float64(r.Records) <= 1500},
		{"Balanced Viewer", "Your viewing spans several habits without one of the stronger patterns dominating.", true},
	}
	for _, v := range rules {
		if v.match {
			return &model.Personality{Name: v.name, Explanation: v.why}
		}
	}
	return nil
}
func knownGenres(r model.Review) int {
	n := 0
	for _, g := range r.Genres {
		if g.Name != "Unknown" && g.Seconds >= r.Seconds*.03 {
			n++
		}
	}
	return n
}
