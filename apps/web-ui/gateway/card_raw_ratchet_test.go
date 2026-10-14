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

// card_raw_ratchet_test.go implements the page-vs-component ui.CardRaw check:
// a page surface must render components.PanelCard / components.TableCard instead
// of invoking the raw ui.CardRaw shell directly.
//
// ui.CardRaw cannot be banned wholesale (unlike form.FormField, which is covered
// by the deprecated-primitive check): the 5 uses inside components/ —
// components.PanelCard (panel.templ), components.TableCard (table.templ),
// components.SecretRevealPanel (secret.templ), components.ApprovalCard
// (approval_card.templ), and components.QuestionCard (question_card.templ) — are
// the owning card shells themselves, not page-level adopters. This check
// therefore scans only page-level root *.templ (the components/ package is out
// of scope by construction), so it sees the adopters and never the owners.

// pageCardRawBudgetEntry declares the tolerated count of direct ui.CardRaw calls
// in one page-level authored .templ file.
type pageCardRawBudgetEntry struct {
	file  string
	count int
}

// pageCardRawBudget is seeded at the 8 pre-adoption page sites. Every site
// migrated to components.PanelCard / TableCard (task 1.1), so the budget is now
// empty: no page-level authored .templ opens the raw ui.CardRaw shell directly.
func pageCardRawBudget() []pageCardRawBudgetEntry {
	return []pageCardRawBudgetEntry{}
}

// uiCardRawRE matches a direct ui.CardRaw invocation (word-bounded, so
// ui.CardRawProps-like symbols cannot false-positive).
var uiCardRawRE = regexp.MustCompile(`\bui\.CardRaw\b`)

// checkPageCardRaw asserts that the number of direct ui.CardRaw call sites in
// each page-level authored .templ file matches the budget exactly, in both
// directions.
func checkPageCardRaw(t *testing.T) {
	actual := map[string]int{}
	examples := map[string]int{} // first hit line per file

	for _, file := range pageCardRawSources() {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		base := filepath.Base(file)
		for i, line := range strings.Split(string(data), "\n") {
			if uiCardRawRE.MatchString(line) {
				actual[base]++
				if _, ok := examples[base]; !ok {
					examples[base] = i + 1
				}
			}
		}
	}

	budget := map[string]pageCardRawBudgetEntry{}
	for _, b := range pageCardRawBudget() {
		budget[b.file] = b
	}

	for _, file := range sortedPageCardRawFiles(actual, budget) {
		a, aOK := actual[file]
		b, bOK := budget[file]
		switch {
		case !bOK:
			t.Errorf("page-card-raw: page %s opens the raw ui.CardRaw shell at line %d (%d occurrence%s) — render components.PanelCard / components.TableCard, or budget the site with a justification",
				file, examples[file], a, pluralS(a))
		case !aOK:
			t.Errorf("page-card-raw: budget entry %s (%d) no longer matches the tree (actual 0) — tighten/adjust the budget in this same change",
				file, b.count)
		case a > b.count:
			t.Errorf("page-card-raw: %s grew to %d (budget %d) — render components.PanelCard / components.TableCard, or lower the budget in this change (e.g. %s:%d)",
				file, a, b.count, file, examples[file])
		case a < b.count:
			t.Errorf("page-card-raw: %s dropped to %d (budget %d) — tighten/adjust the budget in this same change (e.g. %s:%d)",
				file, a, b.count, file, examples[file])
		}
	}
}

func sortedPageCardRawFiles(actual map[string]int, budget map[string]pageCardRawBudgetEntry) []string {
	files := slices.Collect(maps.Keys(actual))
	seen := map[string]bool{}
	for f := range actual {
		seen[f] = true
	}
	for f := range budget {
		if !seen[f] {
			files = append(files, f)
		}
	}
	slices.Sort(files)
	return files
}

// pageCardRawSources returns the page-level authored .templ files scanned by the
// check: the gateway root (non-recursive, skip set applied). components/ is
// deliberately excluded — those are the owning shells, not page adopters — and
// root *.go is excluded so the gallery fixture (devgallery_fixtures.go) that
// demonstrates ui.CardRaw is not mistaken for a page adopter.
func pageCardRawSources() []string {
	var files []string
	for _, m := range globOrEmpty("*.templ") {
		if skipPatternFile(filepath.Base(m)) {
			continue
		}
		files = append(files, m)
	}
	slices.Sort(files)
	return files
}

// TestPageCardRawConsistencyRatchet is the page-vs-component ui.CardRaw gate.
func TestPageCardRawConsistencyRatchet(t *testing.T) {
	t.Run("page-card-raw", checkPageCardRaw)
}
