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

// canonical_ratchet_test.go implements the third consistency ratchet gate: a
// component-vs-component check that keeps authored gateway sources aligned to
// the canonical-L0 primitive set and free of local/go-daisy name collisions.
//
// Rationale (issue #1809): the committed component graph
// (devgallery_graph_gen.go) and adoption findings (devgallery_adoption_gen.go)
// key components by their BARE identifier. A local component and a go-daisy
// primitive that share a name therefore merge into one graph node, and the
// committed graph is silently wrong. This gate is the machine-checkable half of
// that issue: it pins the primitive calls that must not be made directly and the
// names that collide, plus the latent name hazards that must stay unreferenced.
// Both checks are tight (exact equality), matching the shape of the other
// ratchet gates.
//
// Current state: zero local/go-daisy name collisions (the former ListRow
// collision was resolved by renaming the local component to LinkRow). One
// LATENT hazard is watched rather than resolved: the local
// components.StatusBadge (intent+icon wrapper) shares a bare identifier with
// go-daisy ui.StatusBadge, which exists in the pinned version but is never
// referenced. checkLatentCollisions guards both sides of that hazard so it
// cannot silently become an active collision.

// goDaisyPrefix matches any go-daisy import path, recognising go-daisy
// references for the collision check.
const goDaisyPrefix = "github.com/emergent-company/go-daisy/"

// --- deprecated-primitive check ---

// deprecatedKey identifies one deprecated primitive by its go-daisy import path
// and exported symbol.
type deprecatedKey struct {
	importPath string
	symbol     string
}

// deprecatedPrimitive is one go-daisy symbol that must not be called directly;
// authors must reach for its Canonical replacement instead.
type deprecatedPrimitive struct {
	importPath string
	symbol     string
	canonical  string
}

func deprecatedPrimitives() []deprecatedPrimitive {
	return []deprecatedPrimitive{
		{importPath: "github.com/emergent-company/go-daisy/components/layout", symbol: "Page", canonical: "layout.PageFull"},
		{importPath: "github.com/emergent-company/go-daisy/components/ui", symbol: "PaginationCircle", canonical: "ui.PaginationWithProps{Style: ui.PaginationStyleCircle}"},
		{importPath: "github.com/emergent-company/go-daisy/components/form", symbol: "FormToggle", canonical: "form.ToggleInput"},
	}
}

// deprecatedBudgetEntry declares the tolerated count of direct references to one
// deprecated primitive, plus the justification. Seeded empty: both deprecated
// primitives currently have zero call sites, so any hit is a regression. A
// future, justified exception is admitted only by adding an entry here with a
// count and note (same tight-ratchet shape as the other gates).
type deprecatedBudgetEntry struct {
	importPath string
	symbol     string
	count      int
	note       string
}

func deprecatedBudget() []deprecatedBudgetEntry {
	return []deprecatedBudgetEntry{}
}

// checkDeprecatedPrimitives asserts that no authored source calls a deprecated
// primitive directly. It scans authored top-level .templ files (skip set
// applied) plus non-test, non-generated top-level .go files. Import aliases are
// resolved per file so an aliased import cannot slip through; the match is
// word-bounded so layout.Page does not match layout.PageFull.
func checkDeprecatedPrimitives(t *testing.T) {
	actual := map[deprecatedKey]int{}
	examples := map[deprecatedKey]string{} // first hit "file:line" per key

	for _, file := range deprecatedSources() {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		aliases := resolveAliases(data)
		base := filepath.Base(file)
		lines := strings.Split(string(data), "\n")
		for _, dp := range deprecatedPrimitives() {
			for alias, path := range aliases {
				if path != dp.importPath {
					continue
				}
				re := regexp.MustCompile(`\b` + regexp.QuoteMeta(alias) + `\.` + regexp.QuoteMeta(dp.symbol) + `\b`)
				for i, line := range lines {
					if re.MatchString(line) {
						k := deprecatedKey{importPath: dp.importPath, symbol: dp.symbol}
						actual[k]++
						if _, ok := examples[k]; !ok {
							examples[k] = fmt.Sprintf("%s:%d", base, i+1)
						}
					}
				}
			}
		}
	}

	budget := map[deprecatedKey]deprecatedBudgetEntry{}
	for _, b := range deprecatedBudget() {
		budget[deprecatedKey{importPath: b.importPath, symbol: b.symbol}] = b
	}

	for _, k := range sortedDeprecatedKeys(actual, budget) {
		a, aOK := actual[k]
		b, bOK := budget[k]
		switch {
		case !bOK:
			t.Errorf("canonical-L0: deprecated primitive %s.%s still referenced in %s (%d occurrence%s) — use %s instead",
				aliasOf(k.importPath), k.symbol, examples[k], a, pluralS(a), deprecatedCanonical(k))
		case !aOK:
			t.Errorf("canonical-L0: budget entry %s.%s (%d, note: %q) no longer matches the tree (actual 0) — tighten/adjust the budget in this same change",
				aliasOf(k.importPath), k.symbol, b.count, b.note)
		case a > b.count:
			t.Errorf("canonical-L0: %s.%s grew to %d (budget %d) — use %s instead, or raise the budget in this change (e.g. %s)",
				aliasOf(k.importPath), k.symbol, a, b.count, deprecatedCanonical(k), examples[k])
		case a < b.count:
			t.Errorf("canonical-L0: %s.%s dropped to %d (budget %d) — tighten/adjust the budget in this same change (e.g. %s)",
				aliasOf(k.importPath), k.symbol, a, b.count, examples[k])
		}
	}
}

func sortedDeprecatedKeys(actual map[deprecatedKey]int, budget map[deprecatedKey]deprecatedBudgetEntry) []deprecatedKey {
	keys := slices.Collect(maps.Keys(actual))
	seen := map[deprecatedKey]bool{}
	for k := range actual {
		seen[k] = true
	}
	for k := range budget {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b deprecatedKey) int {
		if c := strings.Compare(a.importPath, b.importPath); c != 0 {
			return c
		}
		return strings.Compare(a.symbol, b.symbol)
	})
	return keys
}

// aliasOf returns the short package alias for an import path (its last element),
// used to render the qualified symbol in failure messages.
func aliasOf(importPath string) string {
	return filepath.Base(importPath)
}

// deprecatedCanonical returns the canonical replacement for a deprecated key.
func deprecatedCanonical(k deprecatedKey) string {
	for _, dp := range deprecatedPrimitives() {
		if dp.importPath == k.importPath && dp.symbol == k.symbol {
			return dp.canonical
		}
	}
	return ""
}

// deprecatedSources returns the authored sources scanned by the
// deprecated-primitive check: authored .templ files at the gateway root (skip
// set applied) and in components/, plus non-test, non-generated top-level .go
// files. components/*.templ is included because its components render go-daisy
// primitives and so can call a deprecated one directly; the collision check's
// own scan does not cover the deprecated-primitive check. components/*.go is
// still excluded: those files are non-rendering helpers (attribute merges, prop
// accessors) with no go-daisy imports, so they cannot call a deprecated
// primitive.
func deprecatedSources() []string {
	var files []string
	for _, m := range globOrEmpty("*.templ") {
		if skipPatternFile(filepath.Base(m)) {
			continue
		}
		files = append(files, m)
	}
	for _, m := range globOrEmpty(filepath.Join("components", "*.templ")) {
		if skipPatternFile(filepath.Base(m)) {
			continue
		}
		files = append(files, m)
	}
	for _, m := range globOrEmpty("*.go") {
		base := filepath.Base(m)
		if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_templ.go") || strings.HasSuffix(base, "_gen.go") {
			continue
		}
		files = append(files, m)
	}
	slices.Sort(files)
	return files
}

// --- local ↔ go-daisy name-collision check ---

// collisionEntry declares one tolerated local/go-daisy name collision. The
// resolution direction was previously carried as a note field; with zero
// collisions declared today the field is dropped and re-added when a collision
// re-appears.
type collisionEntry struct {
	name string
}

// declaredCollisions is the collision set pinned to the current tree. It is
// currently empty: the former ListRow collision was resolved by renaming the
// local components.ListRow to components.LinkRow, leaving zero local names that
// collide (by bare identifier) with a referenced go-daisy primitive. The
// StatusBadge name-share is a LATENT hazard (go-daisy ui.StatusBadge exists but
// is never referenced), watched by checkLatentCollisions rather than declared
// here.
func declaredCollisions() []collisionEntry {
	return []collisionEntry{}
}

// scanCollisionSources scans the authored .templ sources and the Go-defined
// component factories, returning two maps keyed by bare component name: local
// (name -> "file:line" of the local definition) and daisy (name -> "file:line"
// of the go-daisy reference). Shared by the active name-collision check and the
// latent-collision watch.
func scanCollisionSources(t *testing.T) (local, daisy map[string]string) {
	t.Helper()
	local = map[string]string{}
	daisy = map[string]string{}

	for _, file := range collisionTemplSources() {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		aliases := resolveAliases(data)
		base := filepath.Base(file)
		lines := strings.Split(string(data), "\n")

		for i, line := range lines {
			if m := localComponentRE.FindStringSubmatch(line); m != nil {
				if _, ok := local[m[1]]; !ok {
					local[m[1]] = fmt.Sprintf("%s:%d", base, i+1)
				}
			}
		}

		for alias, path := range aliases {
			if !strings.HasPrefix(path, goDaisyPrefix) {
				continue
			}
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(alias) + `\.([A-Z][A-Za-z0-9]*)\b`)
			for i, line := range lines {
				for _, m := range re.FindAllStringSubmatch(line, -1) {
					if _, ok := daisy[m[1]]; !ok {
						daisy[m[1]] = fmt.Sprintf("%s:%d", base, i+1)
					}
				}
			}
		}
	}

	// Go-defined components: the graph generator (buildGraph/scan) parses
	// package-main and components/ *.go for top-level functions returning
	// templ.Component and keys them by bare identifier exactly like templ
	// components. The templ scan above only sees `templ Name(` definitions, so a
	// hand-written .go factory whose exported name collides with a referenced
	// go-daisy primitive would otherwise slip past this gate. Collect those
	// names here so the collision check's local map matches the graph's node
	// set.
	for _, file := range goComponentSources() {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		base := filepath.Base(file)
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if m := goComponentRE.FindStringSubmatch(line); m != nil {
				if _, ok := local[m[1]]; !ok {
					local[m[1]] = fmt.Sprintf("%s:%d", base, i+1)
				}
			}
		}
	}

	return local, daisy
}

// checkNameCollision asserts that the set of local component names that collide
// with a referenced go-daisy primitive name matches the declared set exactly.
func checkNameCollision(t *testing.T) {
	local, daisy := scanCollisionSources(t)

	actual := map[string]bool{}
	for name := range local {
		if _, ok := daisy[name]; ok {
			actual[name] = true
		}
	}

	declared := map[string]collisionEntry{}
	for _, c := range declaredCollisions() {
		declared[c.name] = c
	}

	for _, name := range sortedCollisionNames(actual, declared) {
		_, aOK := actual[name]
		_, dOK := declared[name]
		switch {
		case !dOK:
			t.Errorf("name-collision: local component %q (%s) collides with go-daisy primitive %q (%s) — the graph keys by bare identifier so they merge; rename the local component or fold it onto the go-daisy primitive (declare it with a resolution note)",
				name, local[name], name, daisy[name])
		case !aOK:
			t.Errorf("name-collision: declared collision %q no longer occurs — tighten/adjust the declared set in this same change", name)
		}
	}
}

// --- latent local ↔ go-daisy name-hazard watch ---

// latentCollisionEntry declares one watched name-hazard: a local component whose
// bare identifier is shared by a go-daisy primitive that is currently
// unreferenced. It is not an active collision (the twin is never rendered), so
// it is not declared in declaredCollisions; instead the watch asserts both sides
// stay true — the local component remains defined and the go-daisy twin remains
// unreferenced — so the hazard cannot silently become an active collision.
type latentCollisionEntry struct {
	localName  string // local component name (components/<localName>)
	importPath string // go-daisy import path of the unreferenced twin
	symbol     string // go-daisy symbol that must stay unreferenced
	note       string
}

// latentCollisions is the watched-hazard set pinned to the current tree: the
// local components.StatusBadge (intent+icon wrapper, 37 direct
// components.StatusBadge( call sites) shares a bare identifier with go-daisy
// ui.StatusBadge, which exists in the pinned version but is never referenced.
// It is not renamed (37 sites, no active hazard); it is watched instead.
func latentCollisions() []latentCollisionEntry {
	return []latentCollisionEntry{
		{
			localName:  "StatusBadge",
			importPath: "github.com/emergent-company/go-daisy/components/ui",
			symbol:     "StatusBadge",
			note:       "local components.StatusBadge (intent+icon wrapper) vs go-daisy ui.StatusBadge (unreferenced); keep the 37-site local wrapper, watch for the twin being referenced",
		},
	}
}

// checkLatentCollisions asserts each declared latent hazard still has its local
// component defined and its go-daisy twin unreferenced. The bare-identifier map
// is shared with the active-collision check: if the twin is referenced anywhere,
// the name-collision check flags it too, but this watch names the hazard
// directly so the failure message points at the latent declaration to update.
func checkLatentCollisions(t *testing.T) {
	local, daisy := scanCollisionSources(t)

	for _, lc := range latentCollisions() {
		loc, locOK := local[lc.localName]
		dai, daiOK := daisy[lc.symbol]
		switch {
		case !locOK:
			t.Errorf("latent-collision: local component %q no longer defined — update latentCollisions() (note: %s); the go-daisy twin %s.%s is watched against it",
				lc.localName, lc.note, aliasOf(lc.importPath), lc.symbol)
		case daiOK:
			t.Errorf("latent-collision: go-daisy %s.%s is now referenced (%s) and collides with local component %q (%s) — the hazard became active; resolve it (rename the local component or fold onto the go-daisy primitive) and remove it from latentCollisions",
				aliasOf(lc.importPath), lc.symbol, dai, lc.localName, loc)
		}
	}
}

func sortedCollisionNames(actual map[string]bool, declared map[string]collisionEntry) []string {
	names := slices.Collect(maps.Keys(actual))
	seen := map[string]bool{}
	for name := range actual {
		seen[name] = true
	}
	for name := range declared {
		if !seen[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// collisionTemplSources returns the authored .templ sources scanned by the
// collision check: the gateway root (non-recursive) plus the components
// subpackage. No skip set is applied — the [A-Z] component-name regex naturally
// ignores the lowercase test fixtures (fixtures_test.templ).
func collisionTemplSources() []string {
	var files []string
	files = append(files, globOrEmpty("*.templ")...)
	files = append(files, globOrEmpty(filepath.Join("components", "*.templ"))...)
	slices.Sort(files)
	return files
}

// goComponentSources returns the authored, non-generated, non-test .go files
// that may define Go components: hand-written factories returning
// templ.Component (e.g. flashToast, partialWithTitle in ui.go). It mirrors the
// componentgraph generator's scan set — package-main and components/ *.go —
// minus the generated *_templ.go / *_gen.go files (templ components are already
// covered by the .templ scan; the _gen.go files carry data, not definitions) and
// the *_test.go files.
func goComponentSources() []string {
	var files []string
	for _, pattern := range []string{"*.go", filepath.Join("components", "*.go")} {
		for _, m := range globOrEmpty(pattern) {
			base := filepath.Base(m)
			if strings.HasSuffix(base, "_test.go") ||
				strings.HasSuffix(base, "_templ.go") ||
				strings.HasSuffix(base, "_gen.go") {
				continue
			}
			files = append(files, m)
		}
	}
	slices.Sort(files)
	return files
}

// --- import-alias resolution (shared by both checks) ---

// localComponentRE matches a `templ Name(...)` component definition. Component
// names are exported identifiers, so this naturally excludes lowercase test
// fixtures and helpers.
var localComponentRE = regexp.MustCompile(`(?m)^templ\s+([A-Z][A-Za-z0-9]*)\s*\(`)

// goComponentRE matches an exported Go component factory: a top-level
// `func Name(...) templ.Component`. The graph generator keys components by bare
// identifier from exactly these (hand-written .go factories returning
// templ.Component), so matching them here keeps the collision check's local set
// aligned with the graph's node set. Exported-only: go-daisy primitives are
// exported, so a lowercase helper can never share a bare identifier with one.
// Best-effort single-line match, the same posture as resolveAliases.
var goComponentRE = regexp.MustCompile(`(?m)^func\s+([A-Z][A-Za-z0-9]*)\s*\([^)]*\)\s+templ\.Component\b`)

var (
	// importBlockRE extracts an import ( ... ) block.
	importBlockRE = regexp.MustCompile(`(?s)\bimport\s*\((.*?)\)`)
	// importSingleRE matches a single-line import alias "path".
	importSingleRE = regexp.MustCompile(`(?m)^\s*import\s+(?:([A-Za-z_][A-Za-z0-9_]*)\s+)?"((?:[^"\\]|\\.)*)"`)
	// importSpecRE matches one spec inside an import block: optional alias + path.
	importSpecRE = regexp.MustCompile(`(?m)^\s*(?:([A-Za-z_][A-Za-z0-9_]*)\s+)?"((?:[^"\\]|\\.)*)"`)
)

// resolveAliases maps import alias -> import path for an authored file. Both
// .templ and .go files declare imports in standard Go syntax (block or
// single-line form), so one regex-driven parse covers both. Blank imports are
// dropped (a blank import is not a qualifier). The parse is best-effort, not a
// full parser — the same posture as the componentgraph scanner.
func resolveAliases(data []byte) map[string]string {
	aliases := map[string]string{}
	add := func(alias, path string) {
		if alias == "_" {
			return
		}
		if alias == "" {
			alias = filepath.Base(path)
		}
		aliases[alias] = path
	}
	for _, sm := range importSingleRE.FindAllSubmatch(data, -1) {
		add(string(sm[1]), string(sm[2]))
	}
	for _, bm := range importBlockRE.FindAllSubmatch(data, -1) {
		for _, sm := range importSpecRE.FindAllSubmatch(bm[1], -1) {
			add(string(sm[1]), string(sm[2]))
		}
	}
	return aliases
}

// globOrEmpty wraps filepath.Glob, returning an empty slice on error. The
// patterns are constant literals, so a Glob error is unreachable in practice.
func globOrEmpty(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

// TestCanonicalL0ConsistencyRatchet is the third consistency gate: deprecated
// primitives must not be called directly, and local component names must not
// collide with referenced go-daisy primitives.
func TestCanonicalL0ConsistencyRatchet(t *testing.T) {
	t.Run("deprecated-primitives", checkDeprecatedPrimitives)
	t.Run("name-collision", checkNameCollision)
	t.Run("latent-collision", checkLatentCollisions)
}
