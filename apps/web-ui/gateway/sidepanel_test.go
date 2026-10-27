package main

import (
	"os"
	"strings"
	"testing"
)

// openingTagForID returns the opening tag whose id attribute equals id, or ""
// when no such element is rendered. It lets a test assert on one element's
// attributes without another element on the page satisfying the same token.
func openingTagForID(html, id string) string {
	marker := `id="` + id + `"`
	i := strings.Index(html, marker)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(html[:i], "<")
	if start < 0 {
		return ""
	}
	end := strings.Index(html[i:], ">")
	if end < 0 {
		return ""
	}
	return html[start : i+end+1]
}

// TestSidePanelClosedIsInert pins the global assistant drawer's closed-state
// a11y contract (#1241). The server always renders the panel closed
// (translate-x-full + aria-hidden="true"); transform is visual only, so without
// `inert` its header controls stay keyboard-focusable inside an aria-hidden
// subtree (WCAG 4.1.2). `inert` must be in the server-rendered markup so the
// closed panel is correct on first paint, before sidepanel.js runs.
//
// There is no server-rendered *open* state — open/close is client-only — so the
// complement (inert removed on open) is pinned by TestSidePanelInertToggleWiring
// below. What this unit test cannot cover is real focus behaviour: that Tab
// actually skips the panel and lands on the next control is only observable in a
// browser (or jsdom), not in a Go string assertion.
func TestSidePanelClosedIsInert(t *testing.T) {
	html := renderHTML(t, sidePanel(nil, "assistant-agent", sidePanelScope{}))
	tag := openingTagForID(html, "sidepanel-panel")
	if tag == "" {
		t.Fatalf("sidepanel-panel not rendered:\n%s", html)
	}
	for _, want := range []string{`inert`, `aria-hidden="true"`, `translate-x-full`} {
		if !strings.Contains(tag, want) {
			t.Errorf("closed #sidepanel-panel tag missing %q: %s", want, tag)
		}
	}
}

// TestSidePanelRendersScope pins the tenancy data attributes the client keys its
// persisted transcript by. Without them sidepanel.js cannot tell which project,
// org or user a stored conversation belongs to, which is exactly how one
// project's transcript came back in another project's drawer.
func TestSidePanelRendersScope(t *testing.T) {
	scope := sidePanelScope{OrgID: "org-1", ProjectID: "proj-1", UserID: "user-1"}
	html := renderHTML(t, sidePanel(nil, "assistant-agent", scope))
	tag := openingTagForID(html, "sidepanel-panel")
	if tag == "" {
		t.Fatalf("sidepanel-panel not rendered:\n%s", html)
	}
	for _, want := range []string{
		`data-agent-id="assistant-agent"`,
		`data-org-id="org-1"`,
		`data-project-id="proj-1"`,
		`data-user-id="user-1"`,
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("#sidepanel-panel tag missing %q: %s", want, tag)
		}
	}
}

// TestSidePanelStorageScopeWiring pins the client half of the contract:
// sidepanel.js must key its persisted conversation off the panel's own scope
// attributes (not a browser-global key) and must drop a payload whose scope
// does not match the live one. It also retires the unscoped v1 key, whose
// payload cannot be attributed to a tenancy and would otherwise keep
// resurfacing in unrelated projects.
func TestSidePanelStorageScopeWiring(t *testing.T) {
	src, err := os.ReadFile("webui/static/js/sidepanel.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	for _, want := range []string{
		`var LS_PREFIX = "memory.sidepanel.v2";`,
		`var LS_KEY_LEGACY = "memory.sidepanel.v1";`,
		`panel.getAttribute("data-project-id")`,
		`panel.getAttribute("data-org-id")`,
		`panel.getAttribute("data-user-id")`,
		`function sameScope(a, b)`,
		`if (!st || !sameScope(st.scope, scope))`,
		`localStorage.removeItem(LS_KEY_LEGACY)`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("sidepanel.js missing %q", want)
		}
	}
}

// TestSidePanelInertToggleWiring pins the client-side half of the contract:
// sidepanel.js must drop `inert` when the drawer opens (re-entering the tab
// order) and restore it when the drawer closes (keeping off-screen controls out
// of the tab order). The server-rendered state covers first paint; this covers
// every open/close that follows.
func TestSidePanelInertToggleWiring(t *testing.T) {
	src, err := os.ReadFile("webui/static/js/sidepanel.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	if !strings.Contains(js, `panel.removeAttribute("inert")`) {
		t.Error(`sidepanel.js open() must removeAttribute("inert")`)
	}
	if !strings.Contains(js, `panel.setAttribute("inert", "")`) {
		t.Error(`sidepanel.js close() must setAttribute("inert", "")`)
	}
}
