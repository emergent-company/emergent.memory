package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// consistency_ratchet_test.go implements a test-only consistency ratchet over the
// authored gateway templates. It changes no rendered output: both gates are
// compile-time (go test) consistency checks that fail the build when authored
// markup drifts away from the shared-component / design-token canon.
//
// Two gates:
//
//  1. Adoption ratchet (TestAdoptionConsistencyRatchet): the generated
//     adoptionFindings — component-owned markup re-declared inline in an authored
//     .templ file — must match a declared budget exactly. A new finding fails
//     until an author either renders the shared component instead, or adds a
//     budget entry carrying a justification note.
//
//  2. Pattern budgets (TestTemplPatternConsistencyRatchet): authored top-level *.templ files
//     must not grow new instances of legacy/drifting class tokens beyond the
//     declared budget. Commented-out markup is stripped before matching so a
//     disabled snippet does not count.

// --- adoption ratchet ---

type adoptionKey struct {
	file   string
	marker string
}

// adoptionBudgetEntry declares the tolerated count of inline reuse of one
// component-owned marker in one authored file, plus the justification for
// tolerating it (mirrors adoptionAllowlist in devgallery_adoption.go).
type adoptionBudgetEntry struct {
	file   string
	marker string
	count  int
	note   string
}

func TestAdoptionConsistencyRatchet(t *testing.T) {
	actual := map[adoptionKey]int{}
	examples := map[adoptionKey]int{} // first finding line per key
	for _, f := range adoptionFindings {
		k := adoptionKey{file: f.File, marker: f.Marker}
		actual[k]++
		if _, ok := examples[k]; !ok {
			examples[k] = f.Line
		}
	}

	budget := map[adoptionKey]adoptionBudgetEntry{}
	for _, b := range adoptionBudget() {
		budget[adoptionKey{file: b.file, marker: b.marker}] = b
	}

	for _, k := range sortedAdoptionKeys(actual, budget) {
		a, aOK := actual[k]
		b, bOK := budget[k]
		switch {
		case !bOK:
			t.Errorf("adoption ratchet: new raw markup %s in %s:%d (%d occurrence%s) — adopt a shared component, or add a budget entry with a justification Note",
				k.marker, k.file, examples[k], a, pluralS(a))
		case !aOK:
			t.Errorf("adoption ratchet: budget entry %s in %s (%d) no longer matches the tree (actual 0) — tighten/adjust the budget in this same change",
				k.marker, k.file, b.count)
		case a > b.count:
			t.Errorf("adoption ratchet: %s in %s grew to %d (budget %d) — adopt a shared component, or lower the budget in this change; the ratchet only shrinks (e.g. %s:%d)",
				k.marker, k.file, a, b.count, k.file, examples[k])
		case a < b.count:
			t.Errorf("adoption ratchet: %s in %s dropped to %d (budget %d) — tighten/adjust the budget in this same change (e.g. %s:%d)",
				k.marker, k.file, a, b.count, k.file, examples[k])
		}
	}
}

func sortedAdoptionKeys(actual map[adoptionKey]int, budget map[adoptionKey]adoptionBudgetEntry) []adoptionKey {
	keys := slices.Collect(maps.Keys(actual))
	seen := map[adoptionKey]bool{}
	for k := range actual {
		seen[k] = true
	}
	for k := range budget {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b adoptionKey) int {
		if c := strings.Compare(a.file, b.file); c != 0 {
			return c
		}
		return strings.Compare(a.marker, b.marker)
	})
	return keys
}

// --- pattern budgets ---

// patternID names the three drift patterns the ratchet tracks.
const (
	patternLegacyBordered   = "legacy-bordered"
	patternSurfaceOpacity   = "surface-opacity-drift"
	patternBorderTokenDrift = "border-token-drift"
)

type patternKey struct {
	pattern string
	file    string
}

// patternBudgetEntry declares the tolerated count of one drift token in one
// authored file, plus the canonical token to normalise toward.
type patternBudgetEntry struct {
	pattern string
	file    string
	count   int
	note    string
}

var (
	// legacyBorderedRE matches a daisyUI legacy border class token. The class
	// names are complete tokens, so word boundaries keep the match exact.
	legacyBorderedRE = regexp.MustCompile(`\b(?:input|select|textarea|checkbox|toggle|radio)-bordered\b`)
	// surfaceOpacityRE matches a bg-base-200/<N> opacity token. Canonical is
	// bg-base-200/40; any other N is a surface-opacity drift.
	surfaceOpacityRE = regexp.MustCompile(`bg-base-200/(\d+)`)
	// borderBaseColorRE matches the non-canonical flat border colour tokens.
	borderBaseColorRE = regexp.MustCompile(`border-base-(?:100|200|300)`)
	// borderBaseContentRE matches border-base-content/<N>. Canonical is
	// border-base-content/10; any other N is a drift.
	borderBaseContentRE = regexp.MustCompile(`border-base-content/(\d+)`)
)

func TestTemplPatternConsistencyRatchet(t *testing.T) {
	hits, err := scanPatternHits()
	if err != nil {
		t.Fatalf("scan templ patterns: %v", err)
	}

	actual := map[patternKey]int{}
	examples := map[patternKey]int{} // first hit line per key
	for pattern, files := range hits {
		for file, lines := range files {
			k := patternKey{pattern: pattern, file: file}
			actual[k] = len(lines)
			examples[k] = lines[0]
		}
	}

	budget := map[patternKey]patternBudgetEntry{}
	for _, b := range patternBudget() {
		budget[patternKey{pattern: b.pattern, file: b.file}] = b
	}

	for _, k := range sortedPatternKeys(actual, budget) {
		a, aOK := actual[k]
		b, bOK := budget[k]
		switch {
		case !bOK:
			t.Errorf("%s: new drift token in %s:%d (%d occurrence%s) — normalise to the canonical token, or add a budget entry with a justification note",
				k.pattern, k.file, examples[k], a, pluralS(a))
		case !aOK:
			t.Errorf("%s: budget entry in %s (%d) no longer matches the tree (actual 0) — tighten/adjust the budget in this same change",
				k.pattern, k.file, b.count)
		case a > b.count:
			t.Errorf("%s: %s grew to %d (budget %d) — normalise to the canonical token, or lower the budget in this change; the ratchet only shrinks (e.g. %s:%d)",
				k.pattern, k.file, a, b.count, k.file, examples[k])
		case a < b.count:
			t.Errorf("%s: %s dropped to %d (budget %d) — tighten/adjust the budget in this same change (e.g. %s:%d)",
				k.pattern, k.file, a, b.count, k.file, examples[k])
		}
	}
}

func sortedPatternKeys(actual map[patternKey]int, budget map[patternKey]patternBudgetEntry) []patternKey {
	keys := slices.Collect(maps.Keys(actual))
	seen := map[patternKey]bool{}
	for k := range actual {
		seen[k] = true
	}
	for k := range budget {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b patternKey) int {
		if c := strings.Compare(a.pattern, b.pattern); c != 0 {
			return c
		}
		return strings.Compare(a.file, b.file)
	})
	return keys
}

// scanPatternHits scans the authored top-level .templ sources (non-recursive,
// mirroring the componentgraph generator, so the components/ package is
// naturally excluded) and records, per pattern, the file and every line that
// carries a drift token. Commented-out markup is stripped first so a disabled
// snippet does not count.
func scanPatternHits() (map[string]map[string][]int, error) {
	matches, err := filepath.Glob(filepath.Join(".", "*.templ"))
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]int{}
	for _, m := range matches {
		if skipPatternFile(filepath.Base(m)) {
			continue
		}
		if err := scanPatternFile(m, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// skipPatternFile reports whether a .templ basename is excluded from the scan,
// matching scanAdoption's skip rule: the gallery's own files, test fixtures, and
// the components-package fixtures.
func skipPatternFile(base string) bool {
	return strings.HasPrefix(base, "devgallery") ||
		strings.HasSuffix(base, "_test.templ") ||
		base == "fixtures_test.templ"
}

func scanPatternFile(path string, out map[string]map[string][]int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	base := filepath.Base(path)
	lines := strings.Split(string(data), "\n")

	var st templCommentState
	for i, line := range lines {
		stripped := stripTemplComments(line, &st)
		if stripped == "" {
			continue
		}
		lineNo := i + 1

		for range legacyBorderedRE.FindAllStringIndex(stripped, -1) {
			addPatternHit(out, patternLegacyBordered, base, lineNo)
		}
		for _, m := range surfaceOpacityRE.FindAllStringSubmatchIndex(stripped, -1) {
			if n := stripped[m[2]:m[3]]; n != "40" {
				addPatternHit(out, patternSurfaceOpacity, base, lineNo)
			}
		}
		for range borderBaseColorRE.FindAllStringIndex(stripped, -1) {
			addPatternHit(out, patternBorderTokenDrift, base, lineNo)
		}
		for _, m := range borderBaseContentRE.FindAllStringSubmatchIndex(stripped, -1) {
			if n := stripped[m[2]:m[3]]; n != "10" {
				addPatternHit(out, patternBorderTokenDrift, base, lineNo)
			}
		}
	}
	return nil
}

func addPatternHit(out map[string]map[string][]int, pattern, file string, line int) {
	if out[pattern] == nil {
		out[pattern] = map[string][]int{}
	}
	out[pattern][file] = append(out[pattern][file], line)
}

// --- comment stripping (ported from cmd/componentgraph/main.go) ---

// templCommentState tracks comment spans that outlive a single line so a token
// inside a multi-line /* ... */ or <!-- ... --> comment is not mistaken for
// authored markup. It is carried across the lines of one file by
// stripTemplComments.
type templCommentState struct {
	block bool // inside a /* ... */ comment
	html  bool // inside a <!-- ... --> comment
}

// stripTemplComments removes Go line comments (//, respecting double-quoted
// strings), block comments (/* ... */), and HTML comments (<!-- ... -->) from a
// .templ source line, advancing st across multi-line spans. It is a best-effort
// lexical strip, not a full parser — the same posture as the componentgraph
// scanner.
func stripTemplComments(line string, st *templCommentState) string {
	if st.block {
		end := strings.Index(line, "*/")
		if end < 0 {
			return ""
		}
		line = line[end+2:]
		st.block = false
	}
	if st.html {
		end := strings.Index(line, "-->")
		if end < 0 {
			return ""
		}
		line = line[end+3:]
		st.html = false
	}
	line = stripLineComment(line)
	for {
		start := strings.Index(line, "/*")
		if start < 0 {
			break
		}
		end := strings.Index(line[start+2:], "*/")
		if end < 0 {
			st.block = true
			line = line[:start]
			break
		}
		line = line[:start] + line[start+2+end+2:]
	}
	for {
		start := strings.Index(line, "<!--")
		if start < 0 {
			break
		}
		end := strings.Index(line[start+4:], "-->")
		if end < 0 {
			st.html = true
			line = line[:start]
			break
		}
		line = line[:start] + line[start+4+end+3:]
	}
	return line
}

// stripLineComment removes a // line comment from s, respecting double-quoted
// string literals so a // inside a URL or other string is not treated as a
// comment.
func stripLineComment(s string) string {
	inString := false
	for i := 0; i+1 < len(s); i++ {
		switch s[i] {
		case '"':
			inString = !inString
		case '/':
			if !inString && s[i+1] == '/' {
				return s[:i]
			}
		}
	}
	return s
}

// pluralS returns "" for count 1 and "s" otherwise, for error-message grammar.
// (Named pluralS because package main already declares plural in ui.go.)
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
