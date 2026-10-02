package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// writeFixtureDir creates a small gateway-shaped source tree exercising the
// generator's contracts: exact-return filtering, _test.go exclusion, a
// hand-written factory, same-package unqualified calls, the dual fm/form alias,
// .Render/templ.WithChildren shapes, a component-typed-parameter dynamic render,
// and an unresolved same-package callee (leaf-ify).
func writeFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	mainSrc := `package main

import (
	"context"
	"io"

	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
	fm "github.com/emergent-company/go-daisy/components/form"
	"github.com/emergent-company/go-daisy/components/form"
	"github.com/emergent-company/go-daisy/components/nav"
	components "github.com/emergent-company/emergent.memory/apps/web-ui/components"
)

// PageA renders a same-package composite, a components-package entry, and
// go-daisy primitives from three packages (ui via alias, form via dual
// fm/unaliased aliases, nav unaliased).
func PageA(data string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		Composite("x").Render(ctx, w)
		components.PanelCard("", nil).Render(templ.WithChildren(ctx, templ.NopComponent), w)
		ui.Button(ui.ButtonProps{}).Render(ctx, w)
		fm.FormControl("a").Render(ctx, w)
		form.FormControl("b").Render(ctx, w)
		nav.Link(nav.LinkProps{}).Render(ctx, w)
		return nil
	})
}

// Composite is a same-package unqualified render target for PageA.
func Composite(extra string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return nil
	})
}

// Widget renders a component-typed parameter — dynamic, must be skipped.
func Widget(leading templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		leading.Render(ctx, w)
		return nil
	})
}

// Leafy renders an unresolved same-package callee — must leaf-ify to "main".
func Leafy() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		helper("x").Render(ctx, w)
		return nil
	})
}

// partialWithTitle is a hand-written factory (templ.Component return via a
// templ.ComponentFunc literal); its only render is the dynamic param.
func partialWithTitle(title string, content templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		content.Render(ctx, w)
		return nil
	})
}

// DialogAutoOpen returns templ.Attributes — must be rejected.
func DialogAutoOpen() templ.Attributes {
	return templ.Attributes{"data-dialog-autoopen": "true"}
}

// SomeScript returns templ.ComponentScript — must be rejected.
func SomeScript() templ.ComponentScript {
	return templ.ComponentScript{}
}
`

	componentsSrc := `package components

import (
	"context"
	"io"

	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

func PanelCard(extra string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		ui.CardRaw("card "+extra, "", nil).Render(ctx, w)
		return nil
	})
}

func StatusBadge(label string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		ui.Badge(ui.BadgeProps{Label: label}).Render(ctx, w)
		return nil
	})
}
`

	testSrc := `package main

import (
	"context"
	"io"

	"github.com/a-h/templ"
	components "github.com/emergent-company/emergent.memory/apps/web-ui/components"
)

// testOnly renders a components-package entry but lives in a _test.go file and
// must be excluded from definitions and from used-by.
func testOnly() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		components.PanelCard("", nil).Render(ctx, w)
		return nil
	})
}
`

	// fixtures_test_templ.go mimics the components package's test-only fixtures
	// (fixtures_test.templ generates it without a _test.go suffix).
	fixturesSrc := `package components

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

func tableCardFixture() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		PanelCard("").Render(ctx, w)
		return nil
	})
}
`

	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("main.go", mainSrc)
	write("main_test.go", testSrc)
	if err := os.MkdirAll(filepath.Join(dir, "components"), 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	write(filepath.Join("components", "panel.go"), componentsSrc)
	write(filepath.Join("components", "fixtures_test_templ.go"), fixturesSrc)
	return dir
}

// contains reports whether set contains want.
func contains(set map[string]bool, want string) bool {
	return set[want]
}

func TestGenerateGraphEdges(t *testing.T) {
	dir := writeFixtureDir(t)
	g, err := buildGraph(dir)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}

	// uses edges from PageA.
	for _, want := range []string{"Composite", "PanelCard", "Button", "FormControl", "Link"} {
		if !contains(g.uses["PageA"], want) {
			t.Errorf("graphUses[PageA] = %v, missing %q", g.uses["PageA"], want)
		}
	}
	// Dual fm/form aliases collapse to one FormControl node.
	if got := count(g.uses["PageA"], "FormControl"); got != 1 {
		t.Errorf("graphUses[PageA] FormControl count = %d, want 1 (dual alias must de-dup)", got)
	}

	// daisy components and packages for PageA.
	if !contains(g.daisyComponents["PageA"], "Button") || !contains(g.daisyComponents["PageA"], "FormControl") || !contains(g.daisyComponents["PageA"], "Link") {
		t.Errorf("graphDaisyComponents[PageA] = %v, want Button/FormControl/Link", g.daisyComponents["PageA"])
	}
	if !contains(g.daisyPackages["PageA"], "github.com/emergent-company/go-daisy/components/form") {
		t.Errorf("graphDaisyPackages[PageA] = %v, missing form package", g.daisyPackages["PageA"])
	}

	// forward edge from a components-package entry to its go-daisy primitive.
	if !contains(g.uses["PanelCard"], "CardRaw") {
		t.Errorf("graphUses[PanelCard] = %v, missing CardRaw", g.uses["PanelCard"])
	}
	if !contains(g.daisyComponents["PanelCard"], "CardRaw") {
		t.Errorf("graphDaisyComponents[PanelCard] = %v, missing CardRaw", g.daisyComponents["PanelCard"])
	}

	// used-by: PanelCard is rendered by PageA (a page) but NOT by the _test.go
	// function.
	if !contains(g.usedBy["PanelCard"], "PageA") {
		t.Errorf("graphUsedBy[PanelCard] = %v, missing PageA", g.usedBy["PanelCard"])
	}
	if contains(g.usedBy["PanelCard"], "testOnly") {
		t.Errorf("graphUsedBy[PanelCard] = %v, must exclude _test.go call sites", g.usedBy["PanelCard"])
	}
	if contains(g.uses["testOnly"], "PanelCard") {
		t.Errorf("graphUses has a key %q for an excluded _test.go definition", "testOnly")
	}
	if _, ok := g.uses["tableCardFixture"]; ok {
		t.Errorf("graphUses has a key %q for an excluded fixtures_test definition", "tableCardFixture")
	}
	if contains(g.usedBy["PanelCard"], "tableCardFixture") {
		t.Errorf("graphUsedBy[PanelCard] = %v, must exclude fixtures_test call sites", g.usedBy["PanelCard"])
	}

	// leaf-ify: unresolved same-package callee becomes a package leaf.
	if !contains(g.uses["Leafy"], "main") {
		t.Errorf("graphUses[Leafy] = %v, want package leaf %q", g.uses["Leafy"], "main")
	}

	// dynamic component-typed params are skipped (no dangling edges).
	if _, ok := g.uses["Widget"]; ok {
		t.Errorf("graphUses[Widget] = %v, want no uses (dynamic param)", g.uses["Widget"])
	}
	if _, ok := g.uses["partialWithTitle"]; ok {
		t.Errorf("graphUses[partialWithTitle] = %v, want no uses (dynamic param)", g.uses["partialWithTitle"])
	}

	// exact-return filter: Attributes/ComponentScript returners are absent.
	for _, name := range []string{"DialogAutoOpen", "SomeScript"} {
		if _, ok := g.uses[name]; ok {
			t.Errorf("graphUses contains rejected returner %q", name)
		}
	}
}

func TestGenerateGraphDeterministic(t *testing.T) {
	dir := writeFixtureDir(t)
	first, err := generateGraph(dir)
	if err != nil {
		t.Fatalf("generateGraph: %v", err)
	}
	for range 3 {
		again, err := generateGraph(dir)
		if err != nil {
			t.Fatalf("generateGraph: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("generateGraph is not deterministic across runs")
		}
	}
}

func count(set map[string]bool, want string) int {
	if set[want] {
		return 1
	}
	return 0
}
