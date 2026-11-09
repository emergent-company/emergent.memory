// Package typehygiene canonicalizes discovered object type names.
//
// Discovery is an LLM pass and is not stable across runs: the same real-world
// entity arrives as "Organization" one run and "Organisation" or
// "Organizations" the next. Appending a new schema version per run therefore
// fragments the graph instead of converging it. This package provides the
// name-only half of the fix: a normalized key (case, punctuation, plurals and a
// small, explicit synonym map) plus a resolver that prefers names that already
// exist over names a new run invents.
//
// Property/schema merging lives in the discovery domain, which owns the type
// payload shape. Keeping this package pure means both discovery and any future
// schema-lint check can share exactly one notion of "same type".
package typehygiene

import (
	"strings"
	"unicode"
)

// irregularPlurals maps plurals that the suffix rules below cannot derive.
// Deliberately small: an aggressive stemmer would merge unrelated types.
var irregularPlurals = map[string]string{
	"people":     "person",
	"children":   "child",
	"men":        "man",
	"women":      "woman",
	"mice":       "mouse",
	"geese":      "goose",
	"feet":       "foot",
	"teeth":      "tooth",
	"statuses":   "status",
	"analyses":   "analysis",
	"crises":     "crisis",
	"criteria":   "criterion",
	"indices":    "index",
	"matrices":   "matrix",
	"vertices":   "vertex",
	"appendixes": "appendix",
}

// synonyms maps known spelling/word variants to one canonical key. Only pairs
// that describe the same entity belong here; it is intentionally conservative
// because a wrong synonym merges two real types.
var synonyms = map[string]string{
	"organisation": "organization",
	"place":        "location",
}

// NormalizeKey returns the comparison key for a discovered type name.
// "Organization", "organisations" and "Organisation" all normalize to
// "organization"; "Place" and "Locations" normalize to "location".
func NormalizeKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	singular := singularize(b.String())
	if canonical, ok := synonyms[singular]; ok {
		return canonical
	}
	return singular
}

// DisplaySingular returns a name suitable for a brand-new discovered type:
// trimmed, with a simple plural on the final word singularized but the original
// casing preserved. It does not apply synonyms; an existing type is matched by
// the resolver first, and only genuinely new types reach this function.
func DisplaySingular(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return trimmed
	}
	idx := strings.LastIndexAny(trimmed, " \t-_")
	head, word := trimmed[:idx+1], trimmed[idx+1:]
	singular := singularize(strings.ToLower(word))
	if canonical, ok := synonyms[singular]; ok {
		singular = canonical
	}
	if singular == strings.ToLower(word) {
		return trimmed
	}
	if word != "" {
		r := []rune(singular)
		if unicode.IsUpper([]rune(word)[0]) {
			r[0] = unicode.ToUpper(r[0])
		}
		return head + string(r)
	}
	return head + singular
}

// Resolver maps discovered names onto names the project already uses.
// Existing names are added in preference order (registered types before a
// previous discovered pack), so a synonym or plural never replaces a reviewed
// name with a newly invented one.
type Resolver struct {
	canonical map[string]string
}

// NewResolver builds a resolver from names in preference order.
func NewResolver(existing ...string) *Resolver {
	r := &Resolver{canonical: make(map[string]string, len(existing))}
	for _, name := range existing {
		r.Add(name)
	}
	return r
}

// Add registers name when its normalized key is new and returns the canonical
// name for that key. It reports whether the name was newly added.
func (r *Resolver) Add(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	key := NormalizeKey(name)
	if canonical, ok := r.canonical[key]; ok {
		return canonical, false
	}
	r.canonical[key] = name
	return name, true
}

// Resolve returns the canonical name already registered for name.
func (r *Resolver) Resolve(name string) (string, bool) {
	canonical, ok := r.canonical[NormalizeKey(name)]
	return canonical, ok
}

// ResolveOrAdd returns the canonical name for name, registering a singularized
// display form when no existing name matches.
func (r *Resolver) ResolveOrAdd(name string) string {
	if canonical, ok := r.Resolve(name); ok {
		return canonical
	}
	canonical, _ := r.Add(DisplaySingular(name))
	return canonical
}

// singularize applies suffix rules to a lowercase alphanumeric word.
func singularize(s string) string {
	if s == "" {
		return s
	}
	if singular, ok := irregularPlurals[s]; ok {
		return singular
	}
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "sses"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "shes"), strings.HasSuffix(s, "ches"),
		strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "zes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "ses") && len(s) > 3:
		// statuses -> status is handled above; this covers buses -> bus,
		// addresses -> address (sses above), while leaving "cases" -> "case".
		if strings.HasSuffix(s, "sses") {
			return s[:len(s)-2]
		}
		return s[:len(s)-1]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") &&
		!strings.HasSuffix(s, "us") && !strings.HasSuffix(s, "is") && len(s) > 2:
		return s[:len(s)-1]
	}
	return s
}
