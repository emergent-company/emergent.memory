package main

import (
	"fmt"
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
// by the deprecated-primitive check): the 4 uses inside components/ —
// components.PanelCard (panel.templ), components.TableCard (table.templ),
// components.ApprovalCard (approval_card.templ), and components.QuestionCard
// (question_card.templ) — are the owning card shells themselves, not page-level
// adopters. This check therefore scans only page-level root *.templ (the
// components/ package is out of scope by construction), so it sees the adopters
// and never the owners.

// pageCardRawBudgetEntry declares the tolerated count of direct ui.CardRaw calls
// in one page-level authored .templ file, plus the justification for tolerating
// it. The note is required (an empty note fails the ratchet, the way the
// adoption and pattern budgets require theirs): a budget entry is a claim that
// the pinned go-daisy cannot express the site, and a claim with no stated gap
// cannot be re-checked when the pin moves.
type pageCardRawBudgetEntry struct {
	file  string
	count int
	note  string
}

// pageCardRawBudget is seeded at the 8 pre-adoption page sites. Every site has
// been migrated to components.PanelCard / TableCard (task 1.1), so the budget is
// empty: components.PanelCard now passes Border and Surface through
// (components.PanelCardOpt), which was the last gap that kept a page on the raw
// shell. The list is kept (rather than deleted) because it is the only place a
// page-level ui.CardRaw site can be declared, and an empty budget is the goal
// state the ratchet defends.
func pageCardRawBudget() []pageCardRawBudgetEntry {
	return []pageCardRawBudgetEntry{}
}

// uiCardRawRE matches a direct ui.CardRaw invocation (word-bounded, so
// ui.CardRawProps-like symbols cannot false-positive).
var uiCardRawRE = regexp.MustCompile(`\bui\.CardRaw\b`)

// checkPageCardRaw asserts that the number of direct ui.CardRaw call sites in
// each page-level authored .templ file matches the budget exactly, in both
// directions, and that every budget entry carries a justification note.
func checkPageCardRaw(t *testing.T) {
	for _, finding := range pageCardRawNoteFindings(pageCardRawBudget()) {
		t.Error(finding)
	}

	actual, examples, err := pageCardRawCounts()
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, finding := range pageCardRawFindings(actual, examples, pageCardRawBudget()) {
		t.Error(finding)
	}
}

// pageCardRawCounts counts ui.CardRaw occurrences per page-level .templ basename
// and records the first hit line per file.
func pageCardRawCounts() (map[string]int, map[string]int, error) {
	actual := map[string]int{}
	examples := map[string]int{} // first hit line per file

	for _, file := range pageCardRawSources() {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file, err)
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
	return actual, examples, nil
}

// pageCardRawFindings compares the observed page-level ui.CardRaw counts against
// the budget and returns one finding per mismatch. It is pure so the ratchet can
// be tested with a synthetic budget.
func pageCardRawFindings(actual, examples map[string]int, budgetEntries []pageCardRawBudgetEntry) []string {
	budget := map[string]pageCardRawBudgetEntry{}
	for _, b := range budgetEntries {
		budget[b.file] = b
	}

	var findings []string
	for _, file := range sortedPageCardRawFiles(actual, budget) {
		a, aOK := actual[file]
		b, bOK := budget[file]
		switch {
		case !bOK:
			findings = append(findings, fmt.Sprintf("page-card-raw: page %s opens the raw ui.CardRaw shell at line %d (%d occurrence%s) — render components.PanelCard / components.TableCard, or budget the site with a justification note in pageCardRawBudget",
				file, examples[file], a, pluralS(a)))
		case !aOK:
			findings = append(findings, fmt.Sprintf("page-card-raw: budget entry %s (%d) no longer matches the tree (actual 0) — tighten/adjust the budget in this same change",
				file, b.count))
		case a > b.count:
			findings = append(findings, fmt.Sprintf("page-card-raw: %s grew to %d (budget %d) — render components.PanelCard / components.TableCard, or lower the budget in this change (e.g. %s:%d)",
				file, a, b.count, file, examples[file]))
		case a < b.count:
			findings = append(findings, fmt.Sprintf("page-card-raw: %s dropped to %d (budget %d) — tighten/adjust the budget in this same change (e.g. %s:%d)",
				file, a, b.count, file, examples[file]))
		}
	}
	return findings
}

// pageCardRawNoteFindings returns a finding for every budget entry whose
// justification note is blank.
func pageCardRawNoteFindings(budget []pageCardRawBudgetEntry) []string {
	return emptyBudgetNotes(budget,
		func(b pageCardRawBudgetEntry) string { return b.file },
		func(b pageCardRawBudgetEntry) string { return b.note })
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

// TestPageCardRawBudgetRequiresNote proves the note guard is wired into the real
// check path: a budget entry with an empty note must produce a finding, and the
// shipped budget must produce none.
func TestPageCardRawBudgetRequiresNote(t *testing.T) {
	if got := pageCardRawNoteFindings(pageCardRawBudget()); len(got) != 0 {
		t.Fatalf("shipped page-card-raw budget has entries without a justification note: %v", got)
	}

	bad := append(slices.Clone(pageCardRawBudget()), pageCardRawBudgetEntry{
		file:  "no_such_page.templ",
		count: 1,
		note:  "  ",
	})
	got := pageCardRawNoteFindings(bad)
	if len(got) != 1 {
		t.Fatalf("empty-note budget entry: got %d note findings, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "no_such_page.templ") || !strings.Contains(got[0], "justification") {
		t.Errorf("empty-note finding does not name the entry and the requirement: %q", got[0])
	}
}
