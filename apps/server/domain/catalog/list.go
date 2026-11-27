package catalog

import (
	"sort"
	"strings"
)

// Summary is the light list projection of a type for browsing: enough to draw a
// card (icon, color, category, aliases, parent) without the property payload.
type Summary struct {
	Name        string         `json:"name"`
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Parent      string         `json:"parent,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	UI          map[string]any `json:"ui,omitempty"`
}

// CategoryCount is one browse facet.
type CategoryCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// ListFilter narrows a browse listing. Zero values mean "no filter", with the
// usual default/cap applied to Limit.
type ListFilter struct {
	Query    string
	Category string
	Parent   string
	Limit    int
	Offset   int
}

// ListResult is one browse page: the filtered types plus the full category
// facets and the unpaginated match count.
type ListResult struct {
	Version    string          `json:"version"`
	Total      int             `json:"total"`
	Limit      int             `json:"limit"`
	Offset     int             `json:"offset"`
	Categories []CategoryCount `json:"categories"`
	Types      []Summary       `json:"types"`
}

const (
	defaultListLimit = 200
	maxListLimit     = 1000
)

// List filters the catalog for browsing. Category facets are computed over the
// whole catalog so the UI can offer every category even while a filter is
// active; Types is the filtered, paginated set in catalog order.
func List(cat *Catalog, f ListFilter) ListResult {
	var res ListResult
	if cat == nil {
		return res
	}
	res.Version = cat.Version
	res.Limit = f.Limit
	if res.Limit <= 0 {
		res.Limit = defaultListLimit
	}
	if res.Limit > maxListLimit {
		res.Limit = maxListLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	res.Offset = f.Offset

	counts := make(map[string]int)
	for _, t := range cat.Types {
		if t.Category != "" {
			counts[t.Category]++
		}
	}
	res.Categories = make([]CategoryCount, 0, len(counts))
	for k, n := range counts {
		res.Categories = append(res.Categories, CategoryCount{Key: k, Count: n})
	}
	sort.Slice(res.Categories, func(i, j int) bool { return res.Categories[i].Key < res.Categories[j].Key })

	q := strings.ToLower(strings.TrimSpace(f.Query))
	category := strings.TrimSpace(f.Category)
	parent := strings.TrimSpace(f.Parent)

	var matched []Summary
	for _, t := range cat.Types {
		if category != "" && t.Category != category {
			continue
		}
		if parent != "" && t.Parent != parent {
			continue
		}
		if q != "" && !typeMatchesQuery(t, q) {
			continue
		}
		matched = append(matched, summarize(t))
	}
	res.Total = len(matched)

	if f.Offset >= len(matched) {
		return res
	}
	end := f.Offset + res.Limit
	if end > len(matched) {
		end = len(matched)
	}
	res.Types = matched[f.Offset:end]
	return res
}

func summarize(t Type) Summary {
	return Summary{
		Name:        t.Name,
		Label:       t.Label,
		Description: t.Description,
		Category:    t.Category,
		Parent:      t.Parent,
		Labels:      t.Labels,
		UI:          t.UI,
	}
}

func typeMatchesQuery(t Type, q string) bool {
	if strings.Contains(strings.ToLower(t.Name), q) ||
		strings.Contains(strings.ToLower(t.Label), q) ||
		strings.Contains(strings.ToLower(t.Description), q) ||
		strings.Contains(strings.ToLower(t.Category), q) ||
		strings.Contains(strings.ToLower(t.Parent), q) {
		return true
	}
	for _, l := range t.Labels {
		if strings.Contains(strings.ToLower(l), q) {
			return true
		}
	}
	return false
}
