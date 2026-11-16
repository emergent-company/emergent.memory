package main

import (
	"os"
	"strings"
	"testing"
)

// readAppCSS returns the authored Tailwind input stylesheet. Unlike the
// compiled webui/static/css/app.css (gitignored, build output), this is the
// source of truth for app-owned rules and is always present.
func readAppCSS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("webui/css/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	return string(b)
}

// TestSidebarNavBadgesSitFlushRight pins #810: the attention badges on sidebar
// nav rows (Documents problem count, Usage budget level) must sit flush with
// the row's trailing edge.
//
// go-daisy's SidebarNavItem emits `<grow label><Warn badge><htmx spinner>`, and
// htmx hides the spinner with `visibility: hidden` — not `display: none` — so
// the spinner keeps its ~1.5rem box in the flex row and pushes the badge away
// from the edge. The fix has to (a) collapse the idle spinner out of layout,
// (b) re-inline it while its own request runs so nav feedback survives, and
// (c) order the badge last so showing the spinner never shoves the badge left.
func TestSidebarNavBadgesSitFlushRight(t *testing.T) {
	css := readAppCSS(t)

	idle := cssRuleBlock(t, css, "#_layout-sidebar .menu-item > .htmx-indicator")
	if !strings.Contains(idle, "display: none") {
		t.Errorf("the idle sidebar nav spinner must be collapsed out of layout (display: none), or its invisible box keeps the badge off the right edge; got:\n%s", idle)
	}

	running := cssRuleBlock(t, css, "#_layout-sidebar .menu-item > .htmx-indicator.htmx-request")
	if !strings.Contains(running, "display: inline-flex") {
		t.Errorf("the sidebar nav spinner must be re-inlined while its request runs, or nav clicks lose their only in-row feedback; got:\n%s", running)
	}
	// htmx 4 marks the hx-indicator target itself (`.htmx-indicator.htmx-request`);
	// older markup scopes the class to an ancestor — cover both so the spinner
	// cannot stay display:none in either wiring.
	if !strings.Contains(css, "#_layout-sidebar .menu-item.htmx-request > .htmx-indicator") {
		t.Error("missing the ancestor-scoped show rule for the sidebar nav spinner (#_layout-sidebar .menu-item.htmx-request > .htmx-indicator)")
	}

	badge := cssRuleBlock(t, css, "#_layout-sidebar .menu-item > .badge")
	if !strings.Contains(badge, "order: 1") {
		t.Errorf("the sidebar nav badge must be ordered after the htmx spinner, so the badge stays at the trailing edge while a navigation request shows the spinner; got:\n%s", badge)
	}
}

// TestSidebarNavItemBadgeMarkupMatchesTheAlignmentRule pins the markup shape
// the rules above rely on: the badge is the last meaningful slot in the row and
// the htmx spinner comes after it, as direct children of a.menu-item. If
// go-daisy ever reorders or wraps those slots, the selectors stop matching and
// the badges drift off the right edge with every test still green otherwise.
func TestSidebarNavItemBadgeMarkupMatchesTheAlignmentRule(t *testing.T) {
	html := renderHTML(t, appSidebar(
		sidebarGroupsWithWarn("", false, sidebarWarns{Documents: 3, Budget: "warning"}),
		nil,
		false,
	))

	cases := []struct {
		name  string
		href  string
		label string
		badge string
	}{
		{name: "usage budget badge", href: "/usage", label: "Usage", badge: `data-testid="budget-nav-badge"`},
		{name: "documents problem badge", href: "/documents", label: "Documents", badge: `data-testid="documents-problem-badge"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anchor := sidebarAnchor(html, tc.href)
			if anchor == "" {
				t.Fatalf("no sidebar anchor for href %q", tc.href)
			}
			if !strings.Contains(anchor, `<span class="grow">`+tc.label+`</span>`) {
				t.Errorf("sidebar row for %q no longer grows its label span, so nothing pushes the badge to the trailing edge:\n%s", tc.label, anchor)
			}
			badgeAt := strings.Index(anchor, tc.badge)
			if badgeAt < 0 {
				t.Fatalf("sidebar row for %q has no badge (%s):\n%s", tc.label, tc.badge, anchor)
			}
			spinnerAt := strings.Index(anchor, "htmx-indicator")
			if spinnerAt < 0 {
				t.Fatalf("sidebar row for %q has no htmx spinner, so the CSS collapse rule has nothing to match:\n%s", tc.label, anchor)
			}
			if spinnerAt < badgeAt {
				t.Errorf("the htmx spinner now precedes the badge in the sidebar row %q; re-check the badge order/alignment rules in webui/css/app.css:\n%s", tc.label, anchor)
			}
		})
	}
}
