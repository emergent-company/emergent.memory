package catalog

import (
	"sort"
	"strings"
	"unicode"
)

// TypeMatch is one ranked result of resolving a free-text query. Score is
// 0..1 (exact name/label at the top); Matched names the fields that
// contributed.
type TypeMatch struct {
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

// ResolveTypes ranks catalog types against a free-text query using
// deterministic lexical signals: exact name, label, alias, normalized token
// overlap across searchable fields, and a whole-string substring bonus.
func ResolveTypes(cat *Catalog, query string, limit int) []TypeMatch {
	if cat == nil || len(cat.Types) == 0 {
		return nil
	}
	return ResolveTypesAgainst(cat.Types, query, limit)
}

// ResolveTypesAgainst ranks an arbitrary type list against a free-text query
// with the same deterministic lexical signals as ResolveTypes. It exists so a
// caller can resolve against a merged vocabulary — the catalog plus the
// remaining domain taxonomy packs — instead of a *Catalog alone. Discovery uses
// it to narrow its candidate set before the classifier.
func ResolveTypesAgainst(types []Type, query string, limit int) []TypeMatch {
	if len(types) == 0 {
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

	matches := make([]TypeMatch, 0, len(types))
	for _, t := range types {
		score, matched := scoreType(t, qLower, qJoined, qTokens)
		if score <= 0 {
			continue
		}
		matches = append(matches, TypeMatch{
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

func scoreType(t Type, qLower, qJoined string, qTokens []string) (float64, []string) {
	nameTokens := tokenize(t.Name)
	nameJoined := strings.Join(nameTokens, " ")
	labelTokens := tokenize(t.Label)
	labelJoined := strings.Join(labelTokens, " ")

	var aliasTokens [][]string
	for _, l := range t.Labels {
		aliasTokens = append(aliasTokens, tokenize(l))
	}

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

	add("name", 0.45*overlapRatio(qTokens, nameTokens))
	add("label", 0.2*overlapRatio(qTokens, labelTokens))
	for _, at := range aliasTokens {
		if at != nil {
			add("alias", 0.25*overlapRatio(qTokens, at))
			break
		}
	}
	add("category", 0.1*equalTokenRatio(qTokens, tokenize(t.Category)))
	add("parent", 0.1*equalTokenRatio(qTokens, tokenize(t.Parent)))
	add("description", 0.1*overlapRatio(qTokens, tokenize(t.Description)))

	if !strings.Contains(qJoined, " ") && strings.Contains(strings.ToLower(nameJoined), qLower) {
		add("name", 0.15)
	}
	if score > 1 {
		score = 1
	}
	return score, matched
}

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
