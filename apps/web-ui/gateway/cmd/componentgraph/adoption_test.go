package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// adoptionPageTempl is a small authored-template fixture exercising the
// scanner's contracts: raw badge/card/btn/dialog markup, a class={ expression
// (single-line), a templ.Attributes map-form class, an adoption:ignore
// suppression, and a raw <table class="table"> inside TableCard (which must not
// be flagged — the caller owns the table, TableCard owns only the wrapper).
const adoptionPageTempl = `package main

import (
	"github.com/a-h/templ"
	components "github.com/emergent-company/emergent.memory/apps/web-ui/components"
)

templ page() {
	<div>
		<span class="badge badge-xs">status</span>
		<div class="card card-border bg-base-100">panel</div>
		<button type="button" class="btn btn-primary">Save</button>
		<dialog class="modal">
			<div class="modal-box"></div>
			<form method="dialog" class="modal-backdrop"></form>
			<div class="modal-action"></div>
		</dialog>
		<!-- adoption:ignore badge -->
		<span class="badge badge-outline">suppressed</span>
		@components.TableCard(nil) {
			<table class="table">
				<tr><td>a</td></tr>
			</table>
		}
	</div>
}
`

// adoptionMultiTempl exercises the multi-line class={ ... } expression and the
// templ.Attributes map-form class attribute.
const adoptionMultiTempl = `package main

import (
	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

templ multi(active bool) {
	<div
		class={
			"btn btn-ghost",
			templ.KV("btn-active", active),
		}
	></div>
	@ui.DropdownMenu(templ.Attributes{"class": "badge"}) {
	}
}
`

// adoptionOpenerTempl opens a multi-line class={ ... } with owned tokens on the
// opening line, which must still be emitted.
const adoptionOpenerTempl = `package main

templ opener(active bool) {
	<div class={ "btn btn-ghost",
		"badge",
		templ.KV("badge-primary", active),
	}></div>
}
`

// adoptionCommentTempl hides owned markup inside multi-line /* */ and <!-- -->
// comments; none of it may be flagged, only the trailing real element.
const adoptionCommentTempl = `package main

templ commented() {
	/*
	<span class="badge">block</span>
	<div class="card card-border">block</div>
	*/
	<!--
	<span class="badge">html</span>
	<div class="card card-border">html</div>
	-->
	<span class="badge">real</span>
}
`

// writeAdoptionFixture creates a temp tree of .templ sources (plus a
// components/ subdirectory that the non-recursive glob must exclude).
func writeAdoptionFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("page.templ", adoptionPageTempl)
	write("multi.templ", adoptionMultiTempl)
	write("opener.templ", adoptionOpenerTempl)
	write("comment.templ", adoptionCommentTempl)
	// Skipped: devgallery prefix and _test.templ suffix.
	write("devgallery.templ", "package main\n\ntempl skipA() {\n\t<span class=\"badge\">x</span>\n}\n")
	write("fixture_test.templ", "package main\n\ntempl skipB() {\n\t<div class=\"card\">x</div>\n}\n")
	// Excluded by the non-recursive glob: components/ is never scanned.
	if err := os.MkdirAll(filepath.Join(dir, "components"), 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	write(filepath.Join("components", "secret.templ"), "package components\n\ntempl SecretRevealModal() {\n\t<dialog class=\"modal\"></dialog>\n}\n")
	return dir
}

// lineOf returns the 1-indexed line containing substr, or -1.
func lineOf(src, substr string) int {
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, substr) {
			return i + 1
		}
	}
	return -1
}

// findAdoptionFinding returns the finding matching (file, line, marker), if any.
func findAdoptionFinding(fs []adoptionFinding, file string, line int, marker string) (adoptionFinding, bool) {
	for _, f := range fs {
		if f.File == file && f.Line == line && f.Marker == marker {
			return f, true
		}
	}
	return adoptionFinding{}, false
}

func TestScanAdoption(t *testing.T) {
	dir := writeAdoptionFixture(t)
	fs, err := scanAdoption(dir)
	if err != nil {
		t.Fatalf("scanAdoption: %v", err)
	}

	assert := func(file string, line int, marker string, wantOwners []string) {
		t.Helper()
		f, ok := findAdoptionFinding(fs, file, line, marker)
		if !ok {
			t.Errorf("missing finding %s:%d %s (have %d findings)", file, line, marker, len(fs))
			return
		}
		if !slices.Equal(f.Owners, wantOwners) {
			t.Errorf("%s:%d %s owners = %v, want %v", file, line, marker, f.Owners, wantOwners)
		}
	}

	// Raw ad-hoc reuse in page.templ.
	assert("page.templ", lineOf(adoptionPageTempl, `class="badge badge-xs"`), "class:badge", []string{"Badge", "StatusBadge"})
	assert("page.templ", lineOf(adoptionPageTempl, `class="card card-border`), "class:card", []string{"CardRaw", "PanelCard", "TableCard"})
	assert("page.templ", lineOf(adoptionPageTempl, `class="card card-border`), "class:card-border", []string{"CardRaw", "PanelCard", "TableCard"})
	assert("page.templ", lineOf(adoptionPageTempl, `class="btn btn-primary"`), "class:btn", []string{"Button", "SaveChangesButton"})

	// The raw <dialog> element: both the tag and the modal wrapper classes.
	dialogLine := lineOf(adoptionPageTempl, `<dialog class="modal">`)
	assert("page.templ", dialogLine, "tag:dialog", []string{"Dialog", "SecretRevealModal"})
	assert("page.templ", dialogLine, "class:modal", []string{"Dialog", "SecretRevealModal"})
	assert("page.templ", lineOf(adoptionPageTempl, `class="modal-box"`), "class:modal-box", []string{"Dialog", "SecretRevealModal"})
	assert("page.templ", lineOf(adoptionPageTempl, `class="modal-backdrop"`), "class:modal-backdrop", []string{"Dialog", "SecretRevealModal"})
	// modal-action is not owned by any scanned component: a component's own
	// action row lives in the components package (never scanned), and authored
	// action rows sit inside an adopted dialog's children. The raw dialog itself
	// is still caught by the tag/class markers above.
	if _, ok := findAdoptionFinding(fs, "page.templ", lineOf(adoptionPageTempl, `class="modal-action"`), "class:modal-action"); ok {
		t.Errorf("caller-supplied modal-action must not be flagged")
	}

	// The adoption:ignore suppression: the following badge must not be flagged.
	if _, ok := findAdoptionFinding(fs, "page.templ", lineOf(adoptionPageTempl, `class="badge badge-outline"`), "class:badge"); ok {
		t.Errorf("suppressed badge was still flagged (adoption:ignore not honored)")
	}

	// The raw <table class="table"> inside TableCard must not be flagged: the
	// caller owns the table, the component owns only its card wrapper.
	if _, ok := findAdoptionFinding(fs, "page.templ", lineOf(adoptionPageTempl, `class="table"`), "class:table"); ok {
		t.Errorf("raw table inside TableCard was flagged (table is not an owned marker)")
	}

	// Multi-line class expression and map-form class attribute.
	assert("multi.templ", lineOf(adoptionMultiTempl, `"btn btn-ghost"`), "class:btn", []string{"Button", "SaveChangesButton"})
	assert("multi.templ", lineOf(adoptionMultiTempl, `"class": "badge"`), "class:badge", []string{"Badge", "StatusBadge"})

	// Opening-line tokens of a multi-line class={ ... } expression are emitted,
	// not silently dropped when the expression spans lines.
	assert("opener.templ", lineOf(adoptionOpenerTempl, `class={ "btn btn-ghost"`), "class:btn", []string{"Button", "SaveChangesButton"})
	assert("opener.templ", lineOf(adoptionOpenerTempl, `"badge",`), "class:badge", []string{"Badge", "StatusBadge"})

	// Markers inside multi-line /* */ and <!-- --> comments must not be flagged;
	// only the real element on the final line is a finding.
	realLine := lineOf(adoptionCommentTempl, `<span class="badge">real</span>`)
	assert("comment.templ", realLine, "class:badge", []string{"Badge", "StatusBadge"})
	for _, f := range fs {
		if f.File == "comment.templ" && f.Line != realLine {
			t.Errorf("comment.templ: %s flagged at line %d inside a comment (real line %d)", f.Marker, f.Line, realLine)
		}
	}

	// Skipped files and the excluded components/ package contribute nothing.
	for _, f := range fs {
		if f.File == "devgallery.templ" || f.File == "fixture_test.templ" || strings.Contains(f.File, "components") {
			t.Errorf("finding from excluded file %q", f.File)
		}
	}
}

func TestAdoptionDeterministic(t *testing.T) {
	dir := writeAdoptionFixture(t)
	first, err := generateAdoption(dir)
	if err != nil {
		t.Fatalf("generateAdoption: %v", err)
	}
	for range 3 {
		again, err := generateAdoption(dir)
		if err != nil {
			t.Fatalf("generateAdoption: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("generateAdoption is not byte-identical across runs")
		}
	}
}

func TestSignaturesInverted(t *testing.T) {
	sigs := componentSignatures()
	owners := invertSignatures(sigs)

	// Every marker maps to >=1 owner, every owner is a known component, and
	// every owner list is sorted.
	for id, sig := range sigs {
		for _, c := range sig.Classes {
			if len(owners["class:"+c]) == 0 {
				t.Errorf("marker %q (from %s) has no owner", "class:"+c, id)
			}
		}
		for _, tg := range sig.Tags {
			if len(owners["tag:"+tg]) == 0 {
				t.Errorf("marker %q (from %s) has no owner", "tag:"+tg, id)
			}
		}
	}
	for marker, owned := range owners {
		for _, o := range owned {
			if _, ok := sigs[o]; !ok {
				t.Errorf("marker %q owner %q is not a known component", marker, o)
			}
		}
		if !sort.StringsAreSorted(owned) {
			t.Errorf("marker %q owners %v are not sorted", marker, owned)
		}
	}

	// Pin the exact owner sets: the only shared-owner markers are the known
	// compatible pairs/triples, so no marker is claimed by conflicting,
	// incompatible components.
	for marker, want := range map[string][]string{
		"class:btn":            {"Button", "SaveChangesButton"},
		"class:badge":          {"Badge", "StatusBadge"},
		"class:alert":          {"Alert"},
		"class:card":           {"CardRaw", "PanelCard", "TableCard"},
		"class:card-border":    {"CardRaw", "PanelCard", "TableCard"},
		"class:modal":          {"Dialog", "SecretRevealModal"},
		"class:modal-box":      {"Dialog", "SecretRevealModal"},
		"class:modal-backdrop": {"Dialog", "SecretRevealModal"},
		"tag:dialog":           {"Dialog", "SecretRevealModal"},
		"class:toggle":         {"ToggleField", "ToggleInput"},
	} {
		if got := owners[marker]; !slices.Equal(got, want) {
			t.Errorf("owners[%q] = %v, want %v", marker, got, want)
		}
	}
}
