package main

import (
	"strings"
	"testing"
)

// orgToolbarBulkMarker is the bulk submit's onclick, which only the toolbar
// button carries: `data-project-delete-busy` is also on the shared delete
// dialog's confirm button and inside the page's inline script, so it cannot
// identify the toolbar control on its own.
const orgToolbarBulkMarker = `onclick="openBulkProjectDeleteDialog(this); return false;"`

// renderOrgLandingAdmin renders the org landing page for an org admin with one
// deletable project, which is the only shape that renders the bulk toolbar.
func renderOrgLandingAdmin(t *testing.T) string {
	t.Helper()
	return renderHTML(t, OrgLandingPage(orgPageData{
		Org:        &Org{ID: "org_1", Name: "Acme"},
		Projects:   []ProjectRef{{ID: "p1", Name: "Acme's Project"}},
		IsOrgAdmin: true,
	}))
}

// TestOrgProjectsToolbarDeleteHiddenOnLoad guards the server-rendered closed
// state of the bulk-delete submit: it must carry an inline `display: none`, not
// only `x-show`. Alpine applies `x-show` when it boots, so without the inline
// state the destructive button paints on first render and disappears a moment
// later — a red "Delete N selected" control on a page where nothing is selected.
// The objects browser's bulk bar (objects_browser.templ) renders the same inline
// state for the same reason.
func TestOrgProjectsToolbarDeleteHiddenOnLoad(t *testing.T) {
	html := renderOrgLandingAdmin(t)

	btn := strings.Index(html, orgToolbarBulkMarker)
	if btn < 0 {
		t.Fatalf("bulk delete button not rendered for an org admin")
	}
	// The wrapper that owns the button, not any other x-show on the page.
	open := strings.LastIndex(html[:btn], `x-show="count > 0"`)
	if open < 0 {
		t.Fatalf("delete button is not inside an x-show=\"count > 0\" wrapper")
	}
	if wrapper := html[open:btn]; !strings.Contains(wrapper, `style="display: none"`) {
		t.Error("delete-selected wrapper must render with inline display:none so the destructive button cannot paint before Alpine boots")
	}
}

// TestOrgProjectsToolbarSelectHintVisibleOnLoad covers the complement: with no
// selection the hint is the only thing in the toolbar, so it must be rendered
// visible rather than waiting for Alpine to un-hide it.
func TestOrgProjectsToolbarSelectHintVisibleOnLoad(t *testing.T) {
	html := renderOrgLandingAdmin(t)

	i := strings.Index(html, `x-show="count == 0"`)
	if i < 0 {
		t.Fatalf("select hint not rendered for an org admin")
	}
	start := strings.LastIndex(html[:i], "<")
	end := strings.Index(html[i:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("could not isolate the hint's opening tag")
	}
	if tag := html[start : i+end]; strings.Contains(tag, "display: none") {
		t.Errorf("select hint must be visible on first render, got %s", tag)
	}
}

// TestOrgProjectsToolbarAbsentForMember keeps the admin-only gate honest: a
// plain member's landing page has no bulk-delete control at all.
func TestOrgProjectsToolbarAbsentForMember(t *testing.T) {
	html := renderHTML(t, OrgLandingPage(orgPageData{
		Org:      &Org{ID: "org_1", Name: "Acme"},
		Projects: []ProjectRef{{ID: "p1", Name: "Acme's Project"}},
	}))
	if strings.Contains(html, orgToolbarBulkMarker) {
		t.Error("a non-admin must not get the bulk-delete control")
	}
}
