package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/web-ui/components"
	"github.com/emergent-company/go-daisy/devmode"
)

// component_annotation_coverage_test.go enforces ui-rule-dev-annotation: every
// inventory component names its own boundary with
// data-component="<package>/<Symbol>" so the element-level feedback overlay can
// report the component instead of whatever annotated ancestor it happens to sit
// under.
//
// The annotation goes through go-daisy's devmode.Attrs(ctx, …), never as a
// hardcoded attribute. The gateway flags every request as dev mode (main.go's
// componentAnnotations middleware), so today the HTML is identical either way —
// but the middleware stays the single switch. A hardcoded attribute would
// survive that switch being turned off, leaving go-daisy's own ~180 annotated
// components bare while these roots kept their attribute: one overlay contract,
// two behaviours, and no way to disable annotation for a build.
//
// The gate is tight in both directions, like the other ratchets: a component
// that is not annotated and not excused fails, an exception that has become
// annotated fails, and a hardcoded data-component fails.

// exportedTemplRE matches an exported templ component declaration.
var exportedTemplRE = regexp.MustCompile(`(?m)^templ ([A-Z][A-Za-z0-9_]*)\(`)

// annotationCallRE matches devmode.Attrs(ctx, "<qualified>") in a templ file,
// tolerating a line break between the arguments.
func annotationCallRE(qualified string) *regexp.Regexp {
	return regexp.MustCompile(`devmode\.Attrs\(\s*ctx,\s*"` + regexp.QuoteMeta(qualified) + `"\s*\)`)
}

// annotationLiteralRE matches a hardcoded data-component attribute.
func annotationLiteralRE(qualified string) *regexp.Regexp {
	return regexp.MustCompile(`data-component="` + regexp.QuoteMeta(qualified) + `"`)
}

// componentAnnotationExceptions names every exported component that does not put
// data-component on an element it owns, with the reason. Two shapes are allowed:
//
//   - adapter: the root element belongs to an upstream go-daisy component, which
//     annotates it with its own name (components.StatusBadge reports ui/Badge).
//     Wrapping the primitive in a display:contents boundary would name the
//     adapter but add DOM to every call site; adopting it is a go-daisy change.
//   - delegating root: the component renders another component's element and
//     owns no root of its own (components.SettingsRail renders components.SubNav).
var componentAnnotationExceptions = map[string]string{
	// adapters over one upstream primitive — the upstream root carries its name
	"AlertModal":        "adapter: root is ui.Dialog (ui/Dialog)",
	"ApprovalCard":      "adapter: root is ui.CardRaw (ui/CardRaw)",
	"PanelCard":         "adapter: root is ui.CardRaw (ui/CardRaw)",
	"QuestionCard":      "adapter: root is ui.CardRaw (ui/CardRaw)",
	"SaveChangesButton": "adapter: root is ui.Button (ui/Button)",
	"StatusBadge":       "adapter: root is ui.Badge (ui/Badge)",
	"TableCard":         "adapter: root is ui.CardRaw (ui/CardRaw)",

	// delegating roots — no element of their own to annotate
	"DeleteWarning":      "delegating root: renders ui.EmptyNote",
	"SelectOptionGroups": "no single root: renders one <optgroup> per group",
	"SettingsRail":       "delegating root: renders components.SubNav",
	"SnippetCard":        "delegating root: renders ui.SoftBox",
}

// gatewayChromeAnnotations are the gateway.* inventory components that own a root
// element and must name it. They are listed rather than discovered because the
// gateway package also holds ~100 page templates and local helpers, which are
// deliberately out of the inventory's scope.
var gatewayChromeAnnotations = []string{
	"pageHead",
	"pageError",
	"pageErrorNotice",
	"sectionCount",
}

// gatewayChromeExceptions are the gateway.* inventory components with no root to
// annotate, or a root the overlay should not target.
var gatewayChromeExceptions = map[string]string{
	"appShell":     "document shell: the root is <html>, never an overlay target",
	"pageHeader":   "delegating root: renders pageHead (gateway/pageHead)",
	"detailHeader": "delegating root: renders pageHead (gateway/pageHead)",
}

// TestComponentsAnnotateTheirRoot is the coverage half of ui-rule-dev-annotation.
func TestComponentsAnnotateTheirRoot(t *testing.T) {
	files, err := filepath.Glob("components/*.templ")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob components/*.templ: %v (found %d)", err, len(files))
	}

	declared := map[string]string{} // component name -> file source
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, m := range exportedTemplRE.FindAllSubmatch(src, -1) {
			declared[string(m[1])] = string(src)
		}
	}
	if len(declared) == 0 {
		t.Fatal("no exported templ components found in components/*.templ")
	}

	var missing []string
	for name, src := range declared {
		qualified := "components/" + name
		annotated := annotationCallRE(qualified).MatchString(src)
		reason, excused := componentAnnotationExceptions[name]

		if annotationLiteralRE(qualified).MatchString(src) {
			t.Errorf("%s hardcodes data-component=%q; use devmode.Attrs(ctx, %q) so the "+
				"annotation stays behind the dev-mode switch", name, qualified, qualified)
		}
		switch {
		case annotated && excused:
			t.Errorf("%s is annotated but still listed as an exception (%s); drop the entry", name, reason)
		case !annotated && !excused:
			missing = append(missing, name)
		}
	}
	for name, reason := range componentAnnotationExceptions {
		if _, ok := declared[name]; !ok {
			t.Errorf("exception %q (%s) is not an exported component any more; drop the entry", name, reason)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("components with no data-component on their root: %s\n"+
			"Add { devmode.Attrs(ctx, \"components/<Name>\")... } to the component's root element, "+
			"or list it in componentAnnotationExceptions with the reason (the overlay reports the "+
			"nearest annotated ancestor otherwise, usually layout/Container).", strings.Join(missing, ", "))
	}
}

// TestGatewayChromeAnnotations asserts the gateway.* chrome components that own a
// root element name it. They all live in ui.templ, so the check is a source scan
// over that file.
func TestGatewayChromeAnnotations(t *testing.T) {
	src, err := os.ReadFile("ui.templ")
	if err != nil {
		t.Fatalf("read ui.templ: %v", err)
	}
	text := string(src)

	for _, name := range gatewayChromeAnnotations {
		qualified := "gateway/" + name
		if !strings.Contains(text, `templ `+name+`(`) {
			t.Errorf("gatewayChromeAnnotations lists %q but ui.templ declares no such component", name)
			continue
		}
		if !annotationCallRE(qualified).MatchString(text) {
			t.Errorf("%s does not annotate its root with devmode.Attrs(ctx, %q)", name, qualified)
		}
		if annotationLiteralRE(qualified).MatchString(text) {
			t.Errorf("%s hardcodes data-component=%q; use devmode.Attrs so the annotation "+
				"stays behind the dev-mode switch", name, qualified)
		}
	}
	for name, reason := range gatewayChromeExceptions {
		qualified := "gateway/" + name
		if !strings.Contains(text, `templ `+name+`(`) && !strings.Contains(text, `func `+name+`(`) {
			t.Errorf("gatewayChromeExceptions lists %q (%s) but ui.templ declares no such component", name, reason)
		}
		if annotationCallRE(qualified).MatchString(text) {
			t.Errorf("%s is annotated but still listed as an exception (%s); drop the entry", name, reason)
		}
	}
}

// TestAnnotationIsGated pins the property that makes the annotation a switch
// rather than a permanent part of the markup: with a plain request context
// (production without the middleware) no annotation is emitted, and with dev
// mode on it is. Turn componentAnnotations off and the whole overlay contract
// disappears from the HTML in one place.
func TestAnnotationIsGated(t *testing.T) {
	// Rendered directly, without dev mode: nothing to see.
	var plain bytes.Buffer
	if err := components.MetaChip("lucide--tag", "3").Render(t.Context(), &plain); err != nil {
		t.Fatalf("render MetaChip: %v", err)
	}
	if strings.Contains(plain.String(), "data-component") {
		t.Errorf("MetaChip annotated without dev mode: %s", plain.String())
	}

	// With dev mode: annotated.
	var dev bytes.Buffer
	if err := components.MetaChip("lucide--tag", "3").Render(devmode.WithDevMode(t.Context()), &dev); err != nil {
		t.Fatalf("render MetaChip: %v", err)
	}
	if !strings.Contains(dev.String(), `data-component="components/MetaChip"`) {
		t.Errorf("MetaChip not annotated in dev mode: %s", dev.String())
	}
}

// TestDevAnnotationCarriesNoProps enforces ui-rule-dev-annotation-props: dev-mode
// annotation is always on, so it must carry the component name only. A data-props
// attribute would serialise the props (object titles, agent names, member emails,
// prompt text) into every rendered page.
func TestDevAnnotationCarriesNoProps(t *testing.T) {
	roots := []string{".", "components"}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".templ") {
				continue
			}
			path := filepath.Join(root, entry.Name())
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if strings.Contains(string(src), "data-props") {
				t.Errorf("%s emits data-props; annotation carries names only (ui-rule-dev-annotation-props)", path)
			}
		}
	}
}
