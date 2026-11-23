package main

// guard: the guard-test naming convention (#910) — this file makes ci.yml's
// `go test -run Ratchet ./...` selection self-enforcing, so it must gate every
// PR.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ratchet_naming_test.go enforces the convention that the required `ci` job
// selects the gateway's guards BY NAME.
//
// Why (#910). `ci.yml`'s "Consistency ratchets" step runs
// `go test -run 'Ratchet' ./...`: a gateway guard gates a feature PR only if its
// name contains `Ratchet`. Nothing enforced that, and #886 shipped two guards
// that missed it — TestBadgeMatchesUpstreamWithoutClass and
// TestAlertMatchesUpstreamWithoutClass, the parity half of the #855 class-merge
// fix — beside seven *Ratchet siblings in the same file. They were invisible to
// every feature PR and every push to `dev`, and would first have run on the
// `dev -> main` promotion, where each pass is a 48-check, ~17-minute suite and a
// rerun moves the head and invalidates the `Reviewed-SHA` marker (#649). A
// comment in ci.yml did not prevent it: the convention is only visible to
// somebody already reading the names that follow it. This test makes it
// discoverable and enforced instead.
//
// The convention, all four halves asserted below:
//
//  1. A guard file declares itself with a `// guard: <what it guards>` marker
//     in its HEADER, before the first import/declaration. The description is
//     required: an empty marker is an unreviewable claim, the same rule the
//     ratchets' budget tables apply to their notes. The header restriction is
//     what keeps a body comment that happens to start `// guard:` (one exists
//     in datasources_ui_test.go) from turning a plain test file into a guard.
//  2. EVERY top-level test in a marked file ends in `Ratchet`. That is the
//     stronger half: the #910 regression was not a file of guards with no
//     Ratchet test, it was one guard added beside its Ratchet siblings, so
//     "the file participates in the selection" has to mean each test does.
//  3. The marker set cannot drift from the names on disk: a file whose tests
//     ALL end in `Ratchet` must be marked, and so must a file whose name
//     contains "ratchet" (including the data-only budget tables that sit next
//     to a scanner).
//  4. `ci.yml`'s gateway invocation still selects `Ratchet`. If the selection
//     is ever widened or renamed, it must be changed together with this file
//     and the markers — otherwise every guard silently stops running.
//
// Scope. This is a source scan of the module's `*_test.go` files: it cannot
// tell whether an unmarked, plainly-named file was INTENDED to guard something
// (budget_test.go's TestTemplNoAttributeTypeRatchet lives in a mixed file and
// is one example that stays outside the marker set), so the residual gap is a
// guard written into a brand-new mixed test file — the place a review comment
// on the file has to catch it. The committed-fixture drift class (#1087) is
// pinned by name in committedFixtureGuards below, so renaming one of those
// guards out of the selection fails even after the file stops looking like a
// guard to the scan. Every other shape is a failing test.

const (
	// guardMarkerPrefix opens the declaration that a file is a guard. The
	// marker convention exists so a test file's intent is machine-readable;
	// the naming rule is what the `-run` selection actually reads.
	guardMarkerPrefix = "// guard:"
	// ratchetSuffix is the one name fragment ci.yml selects on. Renaming it
	// means changing ci.yml, this file, and every guard's name together.
	ratchetSuffix = "Ratchet"
)

var (
	// guardMarkerRE matches a marker line at the start of a line (comments are
	// not indented in this tree) and captures everything after the colon.
	guardMarkerRE = regexp.MustCompile(`(?m)^// guard:(.*)$`)
	// headerEndRE finds the first top-level declaration. The marker is only
	// read from the file header before it: `// guard:` is also plausible prose
	// inside a test body (datasources_ui_test.go already carries one at that
	// depth), and a file-marker convention that a body comment can trip is not
	// enforceable.
	headerEndRE = regexp.MustCompile(`(?m)^(import|func|type|const|var)\b`)
	// topLevelTestRE matches a top-level test declaration. TestMain is not a
	// guard and is excluded by name below.
	topLevelTestRE = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)
	// ratchetRunRE extracts the pattern from the gateway's test invocation,
	// e.g. `go test -run 'Ratchet' ./...`. `go test ./...` (web-ui.yml's full
	// suite) does not match, which is the distinction that matters.
	ratchetRunRE = regexp.MustCompile(`go test -run '([^']*)' \./\.\.\.`)
)

// guardTestFile is one parsed *_test.go file: its top-level test names and its
// guard marker, if any.
type guardTestFile struct {
	path      string
	tests     []string
	hasMarker bool
	marker    string
}

// parseGuardTestFile reads a test file's declarations out of its source.
func parseGuardTestFile(path string, src []byte) guardTestFile {
	file := guardTestFile{path: filepath.ToSlash(path)}
	header := string(src)
	if boundary := headerEndRE.FindStringIndex(header); boundary != nil {
		header = header[:boundary[0]]
	}
	if match := guardMarkerRE.FindStringSubmatch(header); match != nil {
		file.hasMarker = true
		file.marker = strings.TrimSpace(match[1])
	}
	for _, match := range topLevelTestRE.FindAllStringSubmatch(string(src), -1) {
		if match[1] == "TestMain" {
			continue
		}
		file.tests = append(file.tests, match[1])
	}
	return file
}

// collectGuardTestFiles walks root for test files, skipping the directories
// that hold vendored or generated third-party code rather than authored tests.
func collectGuardTestFiles(root string) ([]guardTestFile, error) {
	var files []guardTestFile
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "vendor", "node_modules", "testdata":
				return fs.SkipDir
			}
			if strings.HasPrefix(entry.Name(), ".") && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, parseGuardTestFile(path, src))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

// allRatchetNamed reports whether the file has tests and every one of them is
// selected by `-run Ratchet`.
func allRatchetNamed(tests []string) bool {
	if len(tests) == 0 {
		return false
	}
	for _, name := range tests {
		if !strings.HasSuffix(name, ratchetSuffix) {
			return false
		}
	}
	return true
}

// guardNamingFindings applies the convention to parsed files and returns one
// finding per violation.
func guardNamingFindings(files []guardTestFile) []string {
	var findings []string
	for _, file := range files {
		ratchetFileName := strings.Contains(strings.ToLower(filepath.Base(file.path)), "ratchet")

		if !file.hasMarker {
			switch {
			case ratchetFileName:
				findings = append(findings, fmt.Sprintf(
					"%s: the file name says it is a ratchet but the file has no `%s <what it guards>` marker — add one, or rename the file if it is not a guard (issue #910)", file.path, guardMarkerPrefix))
			case allRatchetNamed(file.tests):
				findings = append(findings, fmt.Sprintf(
					"%s: every test here ends in %q, so this is a guard file, but it carries no `%s <what it guards>` marker — add one so a test added here later cannot silently lose the selection (issue #910)", file.path, ratchetSuffix, guardMarkerPrefix))
			}
			continue
		}

		if strings.TrimSpace(file.marker) == "" {
			findings = append(findings, fmt.Sprintf(
				"%s: the `%s` marker has no description — say what this file guards, the way the budget tables require a justification note (issue #910)", file.path, guardMarkerPrefix))
		}
		for _, name := range file.tests {
			if !strings.HasSuffix(name, ratchetSuffix) {
				findings = append(findings, fmt.Sprintf(
					"%s: %s does not end in %q — every test in a `%s` file must, or `go test -run 'Ratchet' ./...` in the required ci job never runs it on a feature PR; rename it to %s%s (issue #910)", file.path, name, ratchetSuffix, guardMarkerPrefix, name, ratchetSuffix))
			}
		}
	}
	sort.Strings(findings)
	return findings
}

// ratchetSelectionPatterns extracts every `go test -run '<pattern>' ./...`
// invocation from a workflow's text.
func ratchetSelectionPatterns(workflow string) []string {
	var patterns []string
	for _, match := range ratchetRunRE.FindAllStringSubmatch(workflow, -1) {
		patterns = append(patterns, match[1])
	}
	return patterns
}

// TestGuardTestNamingConventionRatchet is the enforced convention: it fails
// when a guard file holds a test the required `ci` job cannot select, and when
// the marker set and the names on disk disagree.
func TestGuardTestNamingConventionRatchet(t *testing.T) {
	files, err := collectGuardTestFiles(".")
	if err != nil {
		t.Fatalf("walk the gateway module: %v", err)
	}

	// Prove the walk is not vacuous: it must reach a subpackage (this is where
	// #910's two hidden guards lived) and this file.
	reached := map[string]bool{
		"components/badge_alert_class_test.go": false,
		"ratchet_naming_test.go":               false,
	}
	for _, file := range files {
		if _, ok := reached[file.path]; ok {
			reached[file.path] = true
		}
	}
	for path, seen := range reached {
		if !seen {
			t.Errorf("the scan did not reach %s — a naming guard that cannot see the file it exists for is not a guard", path)
		}
	}

	for _, finding := range guardNamingFindings(files) {
		t.Error(finding)
	}
}

// TestGuardNamingFindingsCatchTheRegressionRatchet proves the rule above is not
// vacuous: it drives guardNamingFindings with synthetic files, including the
// exact #910 shape — a parity test added beside named Ratchet siblings.
func TestGuardNamingFindingsCatchTheRegressionRatchet(t *testing.T) {
	cases := []struct {
		name string
		file guardTestFile
		want []string
	}{
		{
			name: "#910: a guard added beside its Ratchet siblings",
			file: guardTestFile{
				path:      "components/badge_alert_class_test.go",
				hasMarker: true,
				marker:    "the class-hook wrappers",
				tests:     []string{"TestBadgeClassMergeRatchet", "TestBadgeMatchesUpstreamWithoutClass"},
			},
			want: []string{"TestBadgeMatchesUpstreamWithoutClass"},
		},
		{
			name: "conforming marked file",
			file: guardTestFile{
				path:      "some_ratchet_test.go",
				hasMarker: true,
				marker:    "the thing",
				tests:     []string{"TestThingRatchet", "TestOtherThingRatchet"},
			},
		},
		{
			name: "go test has tests but none gate",
			file: guardTestFile{
				path:  "plain_test.go",
				tests: []string{"TestThing", "TestOtherThing"},
			},
		},
		{
			name: "all Ratchet-named but no marker",
			file: guardTestFile{
				path:  "ratchet_naming_test.go",
				tests: []string{"TestThingRatchet"},
			},
			want: []string{"no `// guard: <what it guards>` marker"},
		},
		{
			name: "ratchet file name with no marker",
			file: guardTestFile{
				path:  "consistency_ratchet_budget_test.go",
				tests: []string{"TestThing"},
			},
			want: []string{"the file name says it is a ratchet"},
		},
		{
			name: "marker with no description",
			file: guardTestFile{
				path:      "some_ratchet_test.go",
				hasMarker: true,
				marker:    "   ",
				tests:     []string{"TestThingRatchet"},
			},
			want: []string{"has no description"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := guardNamingFindings([]guardTestFile{tc.file})
			if len(findings) != len(tc.want) {
				t.Fatalf("got %d findings, want %d: %v", len(findings), len(tc.want), findings)
			}
			joined := strings.Join(findings, "\n")
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("findings do not mention %q:\n%s", want, joined)
				}
			}
			for _, finding := range findings {
				if !strings.Contains(finding, tc.file.path) {
					t.Errorf("finding does not name the file %s: %s", tc.file.path, finding)
				}
			}
		})
	}
}

// committedFixtureGuards are the gateway's committed-fixture drift guards:
// each test's whole assertion compares freshly rendered output with a file
// committed to the repository, so compiling it is not enough — it has to RUN
// on the PR that changes the renderer or the fixture. They follow the same
// Ratchet-name route as every other guard rather than adding another `-run`
// alternative to ci.yml (#1087). This list is the one place that says which
// files the class is, so removing a marker and renaming a test out of the
// selection still fails here even though the file would then look like a plain
// test file to the scan above.
var committedFixtureGuards = []string{
	"object_preview_e2e_fixtures_test.go",
	"popover_runtime_fixture_test.go",
	"tool_frame_fixture_test.go",
}

// readRatchetSelectionPatterns reads ci.yml's gateway invocation patterns. It
// is shared by the selection checks so they cannot disagree about which file
// they are enforcing.
func readRatchetSelectionPatterns(t *testing.T) []string {
	t.Helper()
	const workflow = "../../../.github/workflows/ci.yml"
	src, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("read %s: %v — this guard compares the naming convention with the required ci job, so it needs the repository checkout around the gateway module", workflow, err)
	}
	patterns := ratchetSelectionPatterns(string(src))
	if len(patterns) == 0 {
		t.Fatalf("no `go test -run '<pattern>' ./...` invocation found in %s — the gateway's required test step changed shape; update this guard together with ci.yml (issue #910)", workflow)
	}
	return patterns
}

// TestCommittedFixtureGuardsAreSelectedRatchet pins the #1087 class. The three
// guards it names were committed-file comparisons that only ran in the
// promotion's full `go test`; a drifted fixture landed on `dev` green and first
// failed on `dev -> main`, where a rerun moves the head and invalidates the
// Reviewed-SHA marker (#649). Each one now has a `// guard:` marker and a
// Ratchet-named test, and this test reads the real ci.yml pattern to prove that
// selection runs every top-level test in the file. Add a file here when a new
// committed-fixture guard appears — that is the same-PR record rule 4 asks for.
func TestCommittedFixtureGuardsAreSelectedRatchet(t *testing.T) {
	files, err := collectGuardTestFiles(".")
	if err != nil {
		t.Fatalf("walk the gateway module: %v", err)
	}
	byPath := make(map[string]guardTestFile, len(files))
	for _, file := range files {
		byPath[file.path] = file
	}

	patterns := readRatchetSelectionPatterns(t)
	for _, path := range committedFixtureGuards {
		file, ok := byPath[path]
		if !ok {
			t.Errorf("%s is missing — the committed-fixture guard list in this file no longer matches the tree; update both together (issue #1087)", path)
			continue
		}
		if !file.hasMarker {
			t.Errorf("%s: no `%s <what it guards>` marker — it is a committed-fixture guard, so it must both declare that and be selected by name (issue #1087)", path, guardMarkerPrefix)
		}
		if len(file.tests) == 0 {
			t.Errorf("%s: no top-level tests found — the committed-fixture guard moved or was deleted; update this list with the move (issue #1087)", path)
			continue
		}
		for _, name := range file.tests {
			if !strings.HasSuffix(name, ratchetSuffix) {
				t.Errorf("%s: %s does not end in %q — a committed-fixture guard that `go test -run 'Ratchet'` cannot select never gates a feature PR; rename it to %s%s (issue #1087)", path, name, ratchetSuffix, name, ratchetSuffix)
			}
			selected := false
			for _, pattern := range patterns {
				matched, err := regexp.MatchString(pattern, name)
				if err != nil {
					t.Fatalf("ci.yml pattern %q is not a valid regexp: %v (issue #1087)", pattern, err)
				}
				if matched {
					selected = true
					break
				}
			}
			if !selected {
				t.Errorf("%s: ci.yml runs %v but none of them matches %s — a committed-fixture guard that the required job does not select first fails on the dev -> main promotion (issue #1087)", path, patterns, name)
			}
		}
	}
}

// TestRatchetSelectionStillSelectsTheConventionRatchet checks the other side of
// the convention: ci.yml's gateway invocation must still select `Ratchet`. A
// widened pattern is fine (keep a `Ratchet` alternative); removing it turns
// every guard this file approves into dead weight.
func TestRatchetSelectionStillSelectsTheConventionRatchet(t *testing.T) {
	patterns := readRatchetSelectionPatterns(t)
	for _, pattern := range patterns {
		for _, alternative := range strings.Split(pattern, "|") {
			if strings.TrimSpace(alternative) == ratchetSuffix {
				return
			}
		}
	}
	t.Fatalf("ci.yml runs %v but none of them selects %q — the `// guard:`/`Ratchet` convention in this file and every guard's name assume it does; change them together (issue #910)", patterns, ratchetSuffix)
}

// TestRatchetSelectionPatternParsingRatchet proves the workflow parse above is
// not vacuous: it must read both the one-line and the comment-inside-block
// forms, and must not mistake `go test ./...` for a guarded selection.
func TestRatchetSelectionPatternParsingRatchet(t *testing.T) {
	cases := []struct {
		name     string
		workflow string
		want     []string
	}{
		{
			name:     "one-line run",
			workflow: "        run: go test -run 'Ratchet' ./...\n",
			want:     []string{"Ratchet"},
		},
		{
			name:     "block with a comment",
			workflow: "        run: |\n          # guards must be named *Ratchet to run here\n          go test -run 'Ratchet|MatchesUpstream' ./...\n",
			want:     []string{"Ratchet|MatchesUpstream"},
		},
		{
			name:     "full suite is not a guard selection",
			workflow: "      - run: go test ./...\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ratchetSelectionPatterns(tc.workflow)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("patterns = %v, want %v", got, tc.want)
			}
		})
	}
}
