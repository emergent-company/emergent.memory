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

// checkPageCardRaw asserts that every budget entry carries a justification note
// and that the number of direct ui.CardRaw call sites in each page-level
// authored .templ file matches the budget exactly, in both directions.
func checkPageCardRaw(t *testing.T) {
	findings, err := pageCardRawRatchetFindings(pageCardRawBudget())
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, finding := range findings {
		t.Error(finding)
	}
}

// pageCardRawRatchetFindings is the whole gate for one budget: the note guard
// plus the count check. Kept separate from checkPageCardRaw (and taking the
// budget as an argument) so a test can drive the exact code path the ratchet
// runs with a synthetic note-less entry — a test that only called the helpers
// would stay green if the wiring below were deleted.
func pageCardRawRatchetFindings(budget []pageCardRawBudgetEntry) ([]string, error) {
	findings := pageCardRawNoteFindings(budget)

	actual, examples, err := pageCardRawCounts()
	if err != nil {
		return nil, err
	}
	return append(findings, pageCardRawFindings(actual, examples, budget)...), nil
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
	// A count below 1 can never match (an absent file reports !aOK), and an entry
	// that allows nothing is a contradiction: report it separately so it also
	// surfaces alongside any count finding for the same file.
	for _, b := range budgetEntries {
		if b.count < 1 {
			findings = append(findings, fmt.Sprintf("page-card-raw: budget entry %s declares count %d — an entry exists to allow at least one site, so delete it instead of budgeting zero",
				b.file, b.count))
		}
	}
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
// gate: it drives pageCardRawRatchetFindings — the exact function
// TestPageCardRawConsistencyRatchet asserts on — with a note-less entry, so
// deleting the guard from that function fails this test. The shipped budget must
// produce no note finding.
func TestPageCardRawBudgetRequiresNote(t *testing.T) {
	findings, err := pageCardRawRatchetFindings(pageCardRawBudget())
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("shipped page-card-raw budget is not clean: %v", findings)
	}

	bad := append(slices.Clone(pageCardRawBudget()), pageCardRawBudgetEntry{
		file:  "no_such_page.templ",
		count: 1,
		note:  "  ",
	})
	findings, err = pageCardRawRatchetFindings(bad)
	if err != nil {
		t.Fatalf("%v", err)
	}
	var noteFindings []string
	for _, f := range findings {
		if strings.Contains(f, "no justification note") {
			noteFindings = append(noteFindings, f)
		}
	}
	if len(noteFindings) != 1 {
		t.Fatalf("empty-note budget entry through the ratchet: got %d note findings, want 1 (all: %v)", len(noteFindings), findings)
	}
	if !strings.Contains(noteFindings[0], "no_such_page.templ") {
		t.Errorf("empty-note finding does not name the entry: %q", noteFindings[0])
	}
}

// TestPageCardRawFindingsDetectsSites covers the count check with synthetic maps:
// since the shipped budget is empty, the load-bearing branch (any page-level
// ui.CardRaw is a finding) is otherwise only exercised by a red tree, and a
// retired budget that no longer bites would look identically green.
func TestPageCardRawFindingsDetectsSites(t *testing.T) {
	note := "synthetic: a pinned gap"
	cases := []struct {
		name     string
		actual   map[string]int
		examples map[string]int
		budget   []pageCardRawBudgetEntry
		want     int
	}{
		{"unbudgeted page", map[string]int{"a.templ": 1}, map[string]int{"a.templ": 7}, nil, 1},
		{"budget entry no longer present", nil, nil, []pageCardRawBudgetEntry{{file: "a.templ", count: 1, note: note}}, 1},
		{"grew", map[string]int{"a.templ": 2}, map[string]int{"a.templ": 7}, []pageCardRawBudgetEntry{{file: "a.templ", count: 1, note: note}}, 1},
		{"dropped", map[string]int{"a.templ": 1}, map[string]int{"a.templ": 7}, []pageCardRawBudgetEntry{{file: "a.templ", count: 2, note: note}}, 1},
		{"zero-count entry", map[string]int{"a.templ": 0}, map[string]int{"a.templ": 7}, []pageCardRawBudgetEntry{{file: "a.templ", count: 0, note: note}}, 1},
		{"zero-count entry masks nothing", map[string]int{"a.templ": 2}, map[string]int{"a.templ": 7}, []pageCardRawBudgetEntry{{file: "a.templ", count: 0, note: note}}, 2},
		{"clean", nil, nil, nil, 0},
		{"exact match", map[string]int{"a.templ": 2}, map[string]int{"a.templ": 7}, []pageCardRawBudgetEntry{{file: "a.templ", count: 2, note: note}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pageCardRawFindings(c.actual, c.examples, c.budget)
			if len(got) != c.want {
				t.Fatalf("got %d finding(s), want %d: %v", len(got), c.want, got)
			}
			// A zero-count entry must be reported by its own branch, not merely by
			// the generic "no longer matches the tree" one.
			for _, entry := range c.budget {
				if entry.count < 1 {
					if !strings.Contains(strings.Join(got, "\n"), "declares count 0") {
						t.Errorf("zero-count entry produced no count finding, only: %v", got)
					}
				}
			}
		})
	}
}
