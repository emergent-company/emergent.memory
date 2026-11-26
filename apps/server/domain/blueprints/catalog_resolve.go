package blueprints

import (
	"sort"
	"strings"
	"unicode"
)

// CatalogTypeMatch is one ranked result of resolving a free-text query against
// the catalog's types. Score is 0..1 (exact name/label at the top); Matched
// names the fields that contributed, so a caller can explain the hit.
type CatalogTypeMatch struct {
	Name     string   `json:"name"`
	Label    string   `json:"label,omitempty"`
	Category string   `json:"category,omitempty"`
	Parent   string   `json:"parent,omitempty"`
	Score    float64  `json:"score"`
	Matched  []string `json:"matched,omitempty"`
}

const (
	defaultResolveLimit = 10
	maxResolveLimit     = 50
)

// ResolveCatalogTypes ranks catalog object types against a free-text query using
// deterministic lexical signals: exact name, alias/label, normalized tokens
// across name/label/category/parent/aliases/description, and a whole-string
// substring bonus. It is the precision leg of type resolution; a semantic
// embedding leg can be added later without changing this contract.
func ResolveCatalogTypes(cat *TaxonomyCatalog, query string, limit int) []CatalogTypeMatch {
	if cat == nil || len(cat.Types) == 0 {
		return nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultResolveLimit
	}
	if limit > maxResolveLimit {
		limit = maxResolveLimit
	}

	qTokens := tokenize(q)
	if len(qTokens) == 0 {
		return nil
	}
	qJoined := strings.Join(qTokens, " ")
	qLower := strings.ToLower(q)

	matches := make([]CatalogTypeMatch, 0, len(cat.Types))
	for _, t := range cat.Types {
		score, matched := scoreCatalogType(t, qLower, qJoined, qTokens)
		if score <= 0 {
			continue
		}
		matches = append(matches, CatalogTypeMatch{
			Name:     t.Name,
			Label:    t.Label,
			Category: t.Category,
			Parent:   t.Parent,
			Score:    round3(score),
			Matched:  matched,
		})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Name < matches[j].Name
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func scoreCatalogType(t ObjectTypeDef, qLower, qJoined string, qTokens []string) (float64, []string) {
	nameTokens := tokenize(t.Name)
	nameJoined := strings.Join(nameTokens, " ")
	labelTokens := tokenize(t.Label)
	labelJoined := strings.Join(labelTokens, " ")
	categoryTokens := tokenize(t.Category)
	parentTokens := tokenize(t.Parent)

	var aliasTokens [][]string
	for _, l := range t.Labels {
		aliasTokens = append(aliasTokens, tokenize(l))
	}
	descTokens := tokenize(t.Description)

	var score float64
	var matched []string

	add := func(field string, v float64) {
		if v <= 0 {
			return
		}
		score += v
		if !containsString(matched, field) {
			matched = append(matched, field)
		}
	}

	// Tier 1: exact identifiers.
	if nameJoined != "" && nameJoined == qJoined {
		add("name", 1.0)
	} else if labelJoined != "" && labelJoined == qJoined {
		add("label", 0.85)
	} else {
		for _, at := range aliasTokens {
			if aj := strings.Join(at, " "); aj != "" && aj == qJoined {
				add("alias", 0.8)
				break
			}
		}
	}

	// Tier 2: token overlap across the searchable fields.
	add("name", 0.45*overlapRatio(qTokens, nameTokens))
	add("label", 0.2*overlapRatio(qTokens, labelTokens))
	for _, at := range aliasTokens {
		if at != nil {
			add("alias", 0.25*overlapRatio(qTokens, at))
			break // best alias only; aliases are a flat synonym list
		}
	}
	add("category", 0.1*equalTokenRatio(qTokens, categoryTokens))
	add("parent", 0.1*equalTokenRatio(qTokens, parentTokens))
	add("description", 0.1*overlapRatio(qTokens, descTokens))

	// Tier 3: whole-string substring (e.g. "documentar" -> documentary work).
	if !strings.Contains(qJoined, " ") && strings.Contains(strings.ToLower(nameJoined), qLower) {
		add("name", 0.15)
	}

	if score > 1 {
		score = 1
	}
	return score, matched
}

// tokenize lowercases and splits on non-alphanumerics, camelCase boundaries and
// letter/digit boundaries, dropping one-character noise and duplicates.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		switch {
		case unicode.IsUpper(r) && i > 0:
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			prevLowerOrDigit := unicode.IsLower(prev) || unicode.IsDigit(prev)
			prevUpper := unicode.IsUpper(prev)
			if prevLowerOrDigit || (prevUpper && nextLower) {
				b.WriteByte(' ')
			}
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte(' ')
		}
	}
	seen := make(map[string]bool)
	out := make([]string, 0, 8)
	for _, tok := range strings.Fields(b.String()) {
		if len(tok) < 2 || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// overlapRatio is the fraction of query tokens present in the target tokens.
func overlapRatio(query, target []string) float64 {
	if len(query) == 0 || len(target) == 0 {
		return 0
	}
	set := make(map[string]bool, len(target))
	for _, t := range target {
		set[t] = true
	}
	hits := 0
	for _, q := range query {
		if set[q] {
			hits++
		}
	}
	return float64(hits) / float64(len(query))
}

// equalTokenRatio is like overlapRatio but normalizes by target size, used for
// short category/parent hints where one match should not be diluted by a long
// query.
func equalTokenRatio(query, target []string) float64 {
	if len(query) == 0 || len(target) == 0 {
		return 0
	}
	set := make(map[string]bool, len(target))
	for _, t := range target {
		set[t] = true
	}
	hits := 0
	for _, q := range query {
		if set[q] {
			hits++
		}
	}
	return float64(hits) / float64(len(target))
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}

// ResolveCatalogTypes runs resolution against the bundled catalog without
// requiring a project. limit<=0 uses the default.
func (s *Service) ResolveCatalogTypes(query string, limit int) ([]CatalogTypeMatch, error) {
	cat, err := ListBuiltinTaxonomy()
	if err != nil {
		return nil, err
	}
	return ResolveCatalogTypes(cat, query, limit), nil
}
