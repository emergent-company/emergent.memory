package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
)

// This file pins the local consequence of go-daisy#150. Neither ui.Badge nor
// ui.Alert has a Class prop in the pinned go-daisy v0.20.0, and both emit their
// own class attribute *before* spreading Attrs
// (components/ui/badge.templ:101,109; alert.templ:25). A "class" key passed
// through Attrs therefore renders a SECOND class attribute; an HTML parser keeps
// the first occurrence, so the caller's utility is dropped silently — no error,
// no visual hint that the attribute was ignored.
//
// The gate is a textual ratchet, the same shape as
// TestTemplNoTableClassInAttrsRatchet: the dropped utility cannot be observed in
// rendered output, because parsing has already resolved (and discarded) the
// second attribute, so the authored call site is the only place the mistake is
// visible. The restored layout is pinned by DOM assertions instead: the sidebar
// badges render only the component's own classes (the nav row's gap spaces
// them), and each caller-supplied alert margin renders on a wrapper element.

const goDaisyUIImport = "github.com/emergent-company/go-daisy/components/ui"

// badgeAlertAttrsClassRE matches the "class" key of a templ.Attributes literal.
var badgeAlertAttrsClassRE = regexp.MustCompile(`"class"\s*:`)

// badgeAlertAttrsClassCalls counts the ui.Badge/ui.Alert calls in src and returns
// a one-line excerpt for each whose argument list passes a "class" key. The
// package is located through the file's own import alias (resolveAliases), so an
// aliased import cannot slip past the scan.
//
// Known gap, shared with the table ratchet: the scan reads the literal call
// site. An attributes map built in a variable
// (`attrs := templ.Attributes{"class": …}; @ui.Badge(ui.BadgeProps{Attrs: attrs})`)
// is invisible to it.
func badgeAlertAttrsClassCalls(src string) (calls int, hits []string) {
	for alias, path := range resolveAliases([]byte(src)) {
		if path != goDaisyUIImport {
			continue
		}
		for _, name := range []string{"Badge", "Alert"} {
			selector := alias + "." + name + "("
			for i := 0; ; {
				j := strings.Index(src[i:], selector)
				if j < 0 {
					break
				}
				start := i + j
				args := balancedCall(src[start:])
				calls++
				if badgeAlertAttrsClassRE.MatchString(args) {
					// balancedCall includes the receiver and the opening parenthesis,
					// so the excerpt already names the component.
					hits = append(hits, collapseCallArgs(args))
				}
				i = start + 1
			}
		}
	}
	return calls, hits
}

// collapseCallArgs flattens a call's argument list for a readable failure
// message.
func collapseCallArgs(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 180 {
		s = s[:177] + "..."
	}
	return s
}

// TestTemplNoBadgeOrAlertClassInAttrsRatchet keeps a caller's utility out of
// ui.Badge/ui.Alert Attrs. Neither component has a Class prop yet (go-daisy#150);
// Badge's own class list has to cover it, and an Alert caller's spacing goes on
// a wrapping element.
//
// The Ratchet suffix puts this in the required ci job's `go test -run Ratchet
// ./...` selection.
func TestTemplNoBadgeOrAlertClassInAttrsRatchet(t *testing.T) {
	files := append(globOrEmpty("*.templ"), globOrEmpty(filepath.Join("components", "*.templ"))...)
	if len(files) == 0 {
		t.Fatal("no .templ files found — the scan is not looking at the gateway")
	}
	calls := 0
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		n, hits := badgeAlertAttrsClassCalls(string(src))
		calls += n
		for _, hit := range hits {
			t.Errorf("%s: %s passes \"class\" through Attrs; ui.Badge/ui.Alert emit their own class attribute first and spread Attrs afterwards, so the caller's classes render as a second attribute the browser ignores — drop the key (the component owns its class list) or put the utility on a wrapping element", name, hit)
		}
	}
	if calls == 0 {
		t.Fatal("no ui.Badge/ui.Alert calls found — the scan is not matching the components")
	}
}

// TestBadgeOrAlertAttrsClassDetector pins both directions of the scan: it must
// fire on the shapes the dropped class takes (single-line, multi-line, aliased
// import) and stay quiet on components that do merge a caller class
// (DropdownMenu) and on other components (out of this issue's scope).
func TestBadgeOrAlertAttrsClassDetector(t *testing.T) {
	const uiImport = `import ui "github.com/emergent-company/go-daisy/components/ui"`
	const aliasedImport = `import daisy "github.com/emergent-company/go-daisy/components/ui"`

	cases := []struct {
		name  string
		src   string
		calls int
		hits  int
	}{
		{
			name:  "badge class in Attrs",
			src:   uiImport + "\n@ui.Badge(ui.BadgeProps{Attrs: templ.Attributes{\"class\": \"ml-1\"}})",
			calls: 1, hits: 1,
		},
		{
			name:  "alert class in Attrs",
			src:   uiImport + "\n@ui.Alert(ui.AlertProps{Attrs: templ.Attributes{\"class\": \"mb-4\"}})",
			calls: 1, hits: 1,
		},
		{
			name:  "multi-line attributes map",
			src:   uiImport + "\n@ui.Badge(ui.BadgeProps{\n\tLabel: \"7\",\n\tAttrs: templ.Attributes{\n\t\t\"data-testid\": \"b\",\n\t\t\"class\":       \"ml-1\",\n\t},\n})",
			calls: 1, hits: 1,
		},
		{
			name:  "aliased import",
			src:   aliasedImport + "\n@daisy.Badge(daisy.BadgeProps{Attrs: templ.Attributes{\"class\": \"ml-1\"}})",
			calls: 1, hits: 1,
		},
		{
			name:  "non-class attributes are allowed",
			src:   uiImport + "\n@ui.Alert(ui.AlertProps{Attrs: templ.Attributes{\"data-testid\": \"board-cap-notice\"}})",
			calls: 1, hits: 0,
		},
		{
			name:  "DropdownMenu merges a caller class, so it is not flagged",
			src:   uiImport + "\n@ui.DropdownMenu(templ.Attributes{\"class\": \"w-64\"})",
			calls: 0, hits: 0,
		},
		{
			name:  "other components are out of scope",
			src:   uiImport + "\n@ui.Button(ui.ButtonProps{Attrs: templ.Attributes{\"class\": \"shrink-0\"}})",
			calls: 0, hits: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls, hits := badgeAlertAttrsClassCalls(c.src)
			if calls != c.calls || len(hits) != c.hits {
				t.Errorf("calls=%d hits=%d (%v), want calls=%d hits=%d", calls, len(hits), hits, c.calls, c.hits)
			}
		})
	}
}

// TestSidebarNavBadgesRenderOnlyTheirOwnClasses pins the badge half of the fix.
// The nav badges used to pass "class": "ml-1" through Attrs, which ui.Badge
// dropped: the rendered class attribute is the component's own, and the nav
// row's `gap-2` plus app.css's `order: 1`
// (TestSidebarNavBadgesSitFlushRight) own the spacing. The utilities are gone
// rather than silently ignored.
func TestSidebarNavBadgesRenderOnlyTheirOwnClasses(t *testing.T) {
	cases := []struct {
		name   string
		testID string
		badge  templ.Component
		want   []string
	}{
		{"documents", "documents-problem-badge", sidebarWarnBadge(7), []string{"badge", "badge-error", "badge-xs"}},
		{"approvals", "approvals-nav-badge", sidebarApprovalsBadge(3), []string{"badge", "badge-primary", "badge-xs"}},
		{"budget warning", "budget-nav-badge", sidebarBudgetBadge("warning"), []string{"badge", "badge-warning", "badge-xs", "max-w-20"}},
		{"budget exhausted", "budget-nav-badge", sidebarBudgetBadge("exhausted"), []string{"badge", "badge-error", "badge-xs", "max-w-20"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseDOM(t, renderHTML(t, tc.badge))
			badge := findElementByAttr(doc, "data-testid", tc.testID)
			if badge == nil {
				t.Fatalf("badge with data-testid=%q not rendered", tc.testID)
			}
			for _, want := range tc.want {
				if !hasClasses(badge, want) {
					t.Errorf("badge class %q missing %q", classAttr(badge), want)
				}
			}
			if tokens := strings.Fields(classAttr(badge)); slices.Contains(tokens, "ml-1") {
				t.Errorf("badge class %q still carries ml-1, which ui.Badge cannot render (its own class attribute comes first and Attrs cannot extend it)", classAttr(badge))
			}
		})
	}
}

// alertNodes returns every ui.Alert root (a <div role="alert">) in the tree.
func alertNodes(doc *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" && attrValue(n, "role") == "alert" {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// TestAlertCallerSpacingRendersOnAWrappingElement pins the Alert half of the
// fix: with no Class prop (go-daisy#150) the four call sites that wanted `mb-4`
// now render it on a wrapper, so the spacing the author asked for actually
// applies instead of being dropped. Each fixture below is one of the fixed call
// sites; the wanted alert count proves the render still exercises it.
func TestAlertCallerSpacingRendersOnAWrappingElement(t *testing.T) {
	cases := []struct {
		name string
		c    templ.Component
		want int
	}{
		{
			name: "runs failure alert",
			c:    runMetaHeader(sampleRunFull(), runTraceMeta{}),
			want: 1,
		},
		{
			name: "board error and cap notice",
			c:    BoardColumns(make([]WorkItem, boardItemLimit), nil, boardAllowances{}, boardStatusMap{}, "the board failed to load"),
			want: 2,
		},
		{
			name: "platform email test result alerts",
			c: PlatformEmailSettingsPage(platformEmailData{
				Settings: &instanceEmailSettings{},
				TestMsg:  "Test email sent",
				TestErr:  errors.New("smtp: connection refused"),
			}),
			want: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseDOM(t, renderHTML(t, tc.c))
			alerts := alertNodes(doc)
			if len(alerts) != tc.want {
				t.Fatalf("rendered %d alerts, want %d — the fixture no longer exercises the fixed call site(s)", len(alerts), tc.want)
			}
			for _, alert := range alerts {
				if alert.Parent == nil || !hasClasses(alert.Parent, "mb-4") {
					t.Errorf("alert %s is not wrapped in the caller's spacing element (parent %s); a \"class\" in Attrs cannot reach ui.Alert's own class list", nodeDesc(alert), nodeDesc(alert.Parent))
				}
			}
		})
	}
}
