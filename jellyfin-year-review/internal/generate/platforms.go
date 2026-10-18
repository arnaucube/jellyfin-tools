package generate

import (
	"sort"
	"strings"

	"reelreview/internal/model"
)

// platformName folds common service aliases without treating production studios
// as evidence of streaming availability.
func platformName(name string) string {
	switch strings.ToLower(strings.Join(strings.Fields(name), " ")) {
	case "netflix", "netflix studios":
		return "Netflix"
	case "hbo", "hbo max", "max", "home box office", "hbo films":
		return "HBO"
	case "apple tv", "apple tv+", "appletv", "appletv+", "apple tv plus", "apple original films", "apple studios":
		return "Apple TV+"
	case "amazon", "amazon prime", "amazon prime video", "prime video", "amazon studios", "amazon mgm studios":
		return "Prime Video"
	case "disney+", "disney plus":
		return "Disney+"
	case "hulu":
		return "Hulu"
	case "paramount+", "paramount plus", "cbs all access":
		return "Paramount+"
	case "peacock":
		return "Peacock"
	case "showtime":
		return "Showtime"
	case "starz":
		return "Starz"
	case "discovery+", "discovery plus":
		return "Discovery+"
	case "amc+", "amc plus":
		return "AMC+"
	case "crunchyroll":
		return "Crunchyroll"
	}
	return ""
}

// itemPlatforms prefers explicit snapshot associations; only recognized service
// names are inferred from Jellyfin studios. Unknown studios remain unclassified.
func itemPlatforms(item model.Item) []string {
	seen := map[string]string{}
	for _, name := range item.Platforms {
		name = strings.Join(strings.Fields(name), " ")
		if canonical := platformName(name); canonical != "" {
			name = canonical
		}
		if name != "" {
			key := strings.ToLower(name)
			if previous, ok := seen[key]; !ok || name < previous {
				seen[key] = name
			}
		}
	}
	if len(seen) == 0 {
		for _, studio := range item.Studios {
			if name := platformName(studio); name != "" {
				seen[strings.ToLower(name)] = name
			}
		}
	}
	names := []string{}
	for _, name := range seen {
		names = append(names, name)
	}
	return names
}

// platforms consumes the already filtered distinct titles, so episodes, resumes,
// and repeat viewings never multiply a title's contribution to a service.
func platforms(movies, shows []model.Content, items map[string]model.Item) []model.Platform {
	counts := map[string]*model.Platform{}
	for kind, titles := range [][]model.Content{movies, shows} {
		for _, title := range titles {
			names := itemPlatforms(items[title.ID])
			if len(names) == 0 {
				names = []string{"Other"}
			}
			for _, name := range names {
				key := strings.ToLower(name)
				row := counts[key]
				if row == nil {
					row = &model.Platform{Name: name}
					counts[key] = row
				}
				if name < row.Name {
					row.Name = name
				}
				if kind == 0 {
					row.Movies++
				} else {
					row.Shows++
				}
			}
		}
	}
	out := []model.Platform{}
	for _, row := range counts {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Name == "Other") != (b.Name == "Other") {
			return b.Name == "Other"
		}
		if a.Movies+a.Shows != b.Movies+b.Shows {
			return a.Movies+a.Shows > b.Movies+b.Shows
		}
		return a.Name < b.Name
	})
	return out
}
