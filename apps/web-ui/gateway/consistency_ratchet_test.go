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
//  2. Pattern budgets (TestTemplPatternConsistencyRatchet): authored top-level
//     *.templ files must not grow new instances of legacy/drifting class tokens,
//     nor new raw component-consolidation surfaces (empty-note, dialog-title,
//     soft-box, clipboard), beyond the declared budget. The components/ package
//     is scanned for the consolidation surfaces too (the shared components must
//     adopt the canonical primitives), but not for the design-token drift
//     patterns — those tokens are owned by the shared components themselves.
//     Commented-out markup is stripped before matching so a disabled snippet does
//     not count.

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

// patternID names the drift/consolidation patterns the ratchet tracks. The
// first three are design-token drift; the last four are the component-
// consolidation surfaces (each seeded at its pre-adoption count and lowered as
// the matching adoption lands — see consistency_ratchet_budget_test.go).
const (
	patternLegacyBordered     = "legacy-bordered"
	patternSurfaceOpacity     = "surface-opacity-drift"
	patternBorderTokenDrift   = "border-token-drift"
	patternEmptyNote          = "empty-note"
	patternDialogTitle        = "dialog-title"
	patternDialogTitleSection = "dialog-title-section"
	patternSoftBoxSurface     = "soft-box-surface"
	patternClipboard          = "clipboard"
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

	// --- component-consolidation surface patterns (seeded at pre-adoption) ---

	// noteMutedTokens / noteSizeTokens are the note predicate's token sets:
	// a note site is a <p> whose class contains one of each (token-exact, order-
	// independent). text-muted-bright is deliberately absent — it is the
	// brighter step, not the muted note tier ui.EmptyNote replaces.
	noteMutedTokens = []string{"text-muted", "text-muted-faint", "text-muted-strong", "text-base-content/40", "text-base-content/50", "text-base-content/60"}
	noteSizeTokens  = []string{"text-sm", "text-xs", "text-[11px]"}
	// noteClassAttrRE extracts a static class="…" attribute from a <p> tag.
	noteClassAttrRE = regexp.MustCompile(`<p\b[^>]*class="([^"]*)"`)
	// dialogTitleH3TagRE matches an <h3> opening tag — the DialogTitle dialog
	// heading. dialogTitleH2TagRE matches an <h2>: the same class shape, but a
	// section/card heading rather than a dialog title, so it is budgeted as a
	// separate residual (ui.DialogTitle has no bare-section-heading primitive).
	dialogTitleH3TagRE = regexp.MustCompile(`<h3\b`)
	dialogTitleH2TagRE = regexp.MustCompile(`<h2\b`)
	// softBoxSurfaceFillRE matches a static canonical bg-base-200/40 fill. The
	// [^\w:-] prefix excludes the hover:/conditional variants (hover:bg-base-200/40
	// and bg-primary/… style prefixes), so a hover-only or conditional fill is not
	// mistaken for a soft-box surface.
	softBoxSurfaceFillRE = regexp.MustCompile(`(?:^|[^\w:-])bg-base-200/40`)
	// clipboardTokenRE matches the hand-rolled copy affordance: the data-copy-*
	// wiring attributes, the page-local copy-script identifiers, and the copy JS
	// body (navigator.clipboard). Any of them re-introduces the surface
	// form.ClipboardCopy retires.
	clipboardTokenRE = regexp.MustCompile(`data-copy-target|data-copy-link|data-reveal-url|apiTokenCopyScript|shareManageScriptOnce|navigator\.clipboard`)
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

// scanPatternHits scans the authored templates and records, per pattern, the
// file and every line that carries a drift token. Root top-level *.templ files
// are scanned for every pattern; the components/ package is scanned for the
// component-consolidation surfaces only (design-token drift is owned by the
// shared components, not authored per-page). Commented-out markup is stripped
// first so a disabled snippet does not count.
func scanPatternHits() (map[string]map[string][]int, error) {
	out := map[string]map[string][]int{}

	rootMatches, err := filepath.Glob(filepath.Join(".", "*.templ"))
	if err != nil {
		return nil, err
	}
	for _, m := range rootMatches {
		if skipPatternFile(filepath.Base(m)) {
			continue
		}
		if err := scanPatternFile(m, out, true); err != nil {
			return nil, err
		}
	}

	compMatches, err := filepath.Glob(filepath.Join("components", "*.templ"))
	if err != nil {
		return nil, err
	}
	for _, m := range compMatches {
		if skipPatternFile(filepath.Base(m)) {
			continue
		}
		if err := scanPatternFile(m, out, false); err != nil {
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

func scanPatternFile(path string, out map[string]map[string][]int, scanDrift bool) error {
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

		if scanDrift {
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

		// empty-note: a <p> note site (token-exact, order-independent muted+size
		// predicate, minus the non-note markers).
		for _, m := range noteClassAttrRE.FindAllStringSubmatch(stripped, -1) {
			if isNoteSite(m[1]) {
				addPatternHit(out, patternEmptyNote, base, lineNo)
				break
			}
		}
		// dialog-title: an h3 in the text-lg font-semibold dialog-heading shape.
		if dialogTitleH3TagRE.MatchString(stripped) && strings.Contains(stripped, "text-lg") && strings.Contains(stripped, "font-semibold") {
			addPatternHit(out, patternDialogTitle, base, lineNo)
		}
		// dialog-title-section: an h2 in the text-lg font-semibold shape — a
		// section/card heading, not a dialog title, so it is a recorded residual.
		if dialogTitleH2TagRE.MatchString(stripped) && strings.Contains(stripped, "text-lg") && strings.Contains(stripped, "font-semibold") {
			addPatternHit(out, patternDialogTitleSection, base, lineNo)
		}
		// soft-box-surface: rounded-box plus a static canonical /40 fill — the
		// canonical surface the drift tokens alone never flag.
		if strings.Contains(stripped, "rounded-box") && softBoxSurfaceFillRE.MatchString(stripped) {
			addPatternHit(out, patternSoftBoxSurface, base, lineNo)
		}
		// clipboard: hand-rolled copy markup/wiring/script.
		if clipboardTokenRE.MatchString(stripped) {
			addPatternHit(out, patternClipboard, base, lineNo)
		}
	}
	return nil
}

// isNoteSite reports whether a <p> class attribute is a note site: it carries
// both a muted-foreground token and a size token (token-exact, order-
// independent) and none of the non-note markers (font-mono code, uppercase/
// tracking eyebrow-or-label copy, or hidden).
func isNoteSite(class string) bool {
	var muted, size bool
	for _, t := range strings.Fields(class) {
		if !muted && slices.Contains(noteMutedTokens, t) {
			muted = true
		}
		if !size && slices.Contains(noteSizeTokens, t) {
			size = true
		}
		if t == "font-mono" || t == "uppercase" || t == "hidden" || strings.HasPrefix(t, "tracking") {
			return false
		}
	}
	return muted && size
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
