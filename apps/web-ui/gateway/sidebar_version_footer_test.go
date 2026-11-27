package main

import (
	"strings"
	"testing"

	"github.com/emergent-company/go-daisy/components/layout"
)

// The build-version box is the sidebar's last element (#1133): the feedback on
// [data-testid="app-version"] asked to move it to the bottom of the sidebar, and
// the pinned external destinations (Marketplace, Docs) already occupy the
// bottom edge above it. Both configurations are pinned here because the
// external block is optional — with it, the block draws the separating rule and
// the version footer sits under it; without it, the version footer keeps its own
// top border so it does not float unseparated at the bottom.

func TestAppSidebarVersionFooterStaysLast(t *testing.T) {
	external := []layout.SidebarItem{
		{Label: "Marketplace", Href: "https://market.example", Icon: "lucide--store"},
		{Label: "Docs", Href: "https://docs.example", Icon: "lucide--book-open"},
	}
	cases := []struct {
		name          string
		items         []layout.SidebarItem
		wantOwnBorder bool
	}{
		{"with external destinations", external, false},
		{"without external destinations", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := renderHTML(t, appSidebar(sidebarGroupsWithWarn("", false, sidebarWarns{}), tc.items, false))

			version := strings.Index(html, `data-testid="app-version"`)
			if version < 0 {
				t.Fatal("sidebar footer must render the build version")
			}
			if block := strings.Index(html, `data-testid="sidebar-external"`); block >= 0 && block >= version {
				t.Fatalf("build-version footer is not below the external block: block=%d version=%d", block, version)
			}
			assertVersionFooterIsLast(t, html, version)

			tag := versionFooterTag(t, html, version)
			if got := strings.Contains(tag, "border-t"); got != tc.wantOwnBorder {
				t.Fatalf("footer top border = %v, want %v (tag: %s)", got, tc.wantOwnBorder, tag)
			}
		})
	}
}

// assertVersionFooterIsLast proves nothing but the footer wrapper and the
// sidebar itself close after the version paragraph: the version box is the
// sidebar's final element, not merely the final line of some inner block.
func assertVersionFooterIsLast(t *testing.T, html string, version int) {
	t.Helper()
	pClose := strings.Index(html[version:], "</p>")
	if pClose < 0 {
		t.Fatal("version paragraph is not closed")
	}
	after := strings.TrimSpace(html[version+pClose+len("</p>"):])
	if !strings.HasPrefix(after, "</div></div>") {
		if len(after) > 120 {
			after = after[:120]
		}
		t.Fatalf("build-version footer is not the sidebar's last element, %q follows it", after)
	}
}

// versionFooterTag returns the opening <div ...> of the build-version footer
// wrapper that contains the version paragraph at offset version.
func versionFooterTag(t *testing.T, html string, version int) string {
	t.Helper()
	open := strings.LastIndex(html[:version], `<div class="sidebar-footer`)
	if open < 0 {
		before := html[:version]
		if len(before) > 120 {
			before = before[len(before)-120:]
		}
		t.Fatalf("no sidebar-footer wrapper before the version paragraph: %s", before)
	}
	end := strings.Index(html[open:], ">")
	if end < 0 {
		t.Fatalf("unterminated sidebar-footer wrapper: %s", html[open:])
	}
	return html[open : open+end+1]
}

// TestSidebarVersionFooterClearsTheHomeIndicator pins the companion CSS rule:
// now that the version footer is the drawer's last element, it (not the
// external block) carries the home-indicator clearance.
func TestSidebarVersionFooterClearsTheHomeIndicator(t *testing.T) {
	rule := cssRuleBlock(t, readAppCSS(t), "#_layout-sidebar .sidebar-footer")
	if !strings.Contains(rule, "padding-bottom") {
		t.Fatalf("version footer rule sets no bottom padding: %s", rule)
	}
	if !strings.Contains(rule, "env(safe-area-inset-bottom") {
		t.Fatalf("version footer rule does not honour the bottom safe area: %s", rule)
	}
	// The inset moved with the last element: the scrolling nav keeps its own
	// scroll-end padding, and the pinned external block (still .sidebar-menu for
	// the menu-item styling) must not stack a second inset above the footer.
	nav := cssRuleBlock(t, readAppCSS(t), "#_layout-sidebar .sidebar-menu.grow")
	if !strings.Contains(nav, "env(safe-area-inset-bottom") {
		t.Fatalf("scrolling nav lost its scroll-end safe-area padding: %s", nav)
	}
}
