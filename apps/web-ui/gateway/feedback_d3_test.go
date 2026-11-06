package main

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// --- #1443: token scope picker — two-column rows + group-header toggle ---

// scopeGroupBlock returns the HTML of one scope-area block, from its
// data-scope-group marker up to the next group's marker (or the end): tests use
// it to assert each group owns exactly its own toggle and scope rows.
func scopeGroupBlock(html, label string) string {
	marker := `data-scope-group="` + label + `"`
	start := strings.Index(html, marker)
	if start < 0 {
		return ""
	}
	rest := html[start+1:]
	if next := strings.Index(rest, `data-scope-group="`); next >= 0 {
		return html[start : start+1+next]
	}
	return html[start:]
}

// scopeRowTag returns the full <tr>…</tr> that contains the checkbox for a
// scope value, so a test can assert the two columns (name + description) both
// live in one row.
func scopeRowTag(html, value string) string {
	idx := strings.Index(html, `value="`+value+`"`)
	if idx < 0 {
		return ""
	}
	start := strings.LastIndex(html[:idx], "<tr")
	if start < 0 {
		return ""
	}
	end := strings.Index(html[start:], "</tr>")
	if end < 0 {
		return ""
	}
	return html[start : start+end+len("</tr>")]
}

// scopeGroupToggleTag returns the opening <input …> of the header toggle inside
// a group block, so tests assert its checked/aria state without pinning
// attribute order.
func scopeGroupToggleTag(block string) string {
	idx := strings.Index(block, `data-testid="scope-group-toggle"`)
	if idx < 0 {
		return ""
	}
	start := strings.LastIndex(block[:idx], "<input")
	if start < 0 {
		return ""
	}
	end := strings.Index(block[start:], ">")
	if end < 0 {
		return ""
	}
	return block[start : start+end+1]
}

// TestRenderScopePickerTwoColumnGroups asserts the #1443 scope picker renders
// one bordered block per area, each with a header checkbox (labelled for its
// area) and one two-column row per scope — the scope name in mono on the left
// and its description on the right — with the group hint preserved.
func TestRenderScopePickerTwoColumnGroups(t *testing.T) {
	cfg := apiTokenScopePickerConfig{}
	groups := scopePickerGroupsFor(cfg)
	html := renderHTML(t, apiTokenScopePickerContent([]string{"documents:read"}, cfg))

	if got := strings.Count(html, `data-testid="scope-group"`); got != len(groups) {
		t.Errorf("scope-group blocks = %d, want %d", got, len(groups))
	}
	if got := strings.Count(html, `data-testid="scope-group-toggle"`); got != len(groups) {
		t.Errorf("scope-group toggles = %d, want %d", got, len(groups))
	}

	rows := 0
	for _, g := range groups {
		block := scopeGroupBlock(html, g.Label)
		if block == "" {
			t.Errorf("group %q block missing", g.Label)
			continue
		}
		if !strings.Contains(block, `aria-label="Select all `+g.Label+` scopes"`) {
			t.Errorf("group %q missing its header toggle aria-label", g.Label)
		}
		if !strings.Contains(block, g.Hint) {
			t.Errorf("group %q missing the hint %q", g.Label, g.Hint)
		}
		for _, opt := range g.Options {
			rows++
			if !strings.Contains(block, `value="`+opt.Value+`"`) {
				t.Errorf("group %q missing scope row %q", g.Label, opt.Value)
			}
			row := scopeRowTag(block, opt.Value)
			if row == "" {
				t.Errorf("scope %q has no row in group %q", opt.Value, g.Label)
				continue
			}
			// two columns: scope name and description both in the same row
			// (templ HTML-escapes the label text, so compare against its escape)
			if !strings.Contains(row, opt.Value) || !strings.Contains(row, templ.EscapeString(opt.Label)) {
				t.Errorf("scope row %q must carry name and description, got %q", opt.Value, row)
			}
			if !strings.Contains(row, `name="scopes"`) {
				t.Errorf("scope row %q must keep name=\"scopes\" for the form", opt.Value)
			}
			if !strings.Contains(row, `data-scope-group-row`) {
				t.Errorf("scope row %q must carry data-scope-group-row for the group toggle", opt.Value)
			}
		}
	}
	if got := strings.Count(html, `data-testid="scope-row"`); got != rows {
		t.Errorf("scope rows = %d, want %d", got, rows)
	}
	// the platform admin area/scopes stay out of the picker
	if strings.Contains(html, ">Admin<") || strings.Contains(html, `value="admin"`) || strings.Contains(html, `value="admin:all"`) {
		t.Error("scope picker must never offer the platform admin area or scopes")
	}
}

// TestRenderScopePickerGroupToggleState asserts the group-header toggle is
// pre-checked only when every scope in its area is selected; a partially
// selected group leaves it unchecked (the client scripts set indeterminate).
func TestRenderScopePickerGroupToggleState(t *testing.T) {
	all := []string{"schema:read", "schema:write", "schema:migrate", "documents:read"}
	html := renderHTML(t, apiTokenScopePickerContent(all, apiTokenScopePickerConfig{}))

	schemas := scopeGroupToggleTag(scopeGroupBlock(html, apiTokenAreaSchemas))
	if schemas == "" {
		t.Fatal("Schemas group header toggle missing")
	}
	if !strings.Contains(schemas, "checked") {
		t.Errorf("fully selected group's header toggle should be pre-checked, got %q", schemas)
	}

	docs := scopeGroupToggleTag(scopeGroupBlock(html, apiTokenAreaDocuments))
	if docs == "" {
		t.Fatal("Documents group header toggle missing")
	}
	if strings.Contains(docs, "checked") {
		t.Errorf("partially selected group's header toggle must not be pre-checked, got %q", docs)
	}
}

// TestRenderScopePickerProjectAdminStillGated asserts project:admin remains a
// Projects-group row (still gated by CanManage) in the new two-column layout.
func TestRenderScopePickerProjectAdminStillGated(t *testing.T) {
	gated := renderHTML(t, apiTokenScopePickerContent(nil, apiTokenScopePickerConfig{CanManage: true}))
	projects := scopeGroupBlock(gated, apiTokenAreaProjects)
	if !strings.Contains(projects, `value="project:admin"`) {
		t.Error("project:admin must render in the Projects group for an admin caller")
	}
	plain := renderHTML(t, apiTokenScopePickerContent(nil, apiTokenScopePickerConfig{}))
	if strings.Contains(plain, `value="project:admin"`) {
		t.Error("project:admin must not render for a non-admin caller")
	}
}

// --- #1493: org landing header "Add project" action ---

// TestRenderOrgLandingHeaderAddProject asserts the org landing header shows the
// primary "Add project" action, wired to the global create-project dialog, for
// an org admin.
func TestRenderOrgLandingHeaderAddProject(t *testing.T) {
	data := orgPageData{
		Org:        &Org{ID: "o1", Name: "Acme"},
		Projects:   []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		IsOrgAdmin: true,
	}
	html := renderHTML(t, OrgLandingPage(data))
	for _, want := range []string{
		"Add project",
		`onclick="openNewProjectModal()"`,
		`href="/orgs"`, // crumbs start at the nav group
	} {
		if !strings.Contains(html, want) {
			t.Errorf("admin org landing header missing %q", want)
		}
	}
}

// TestRenderOrgLandingHeaderAddProjectHiddenForMember asserts a non-admin sees
// no Add-project action (hide, don't disable).
func TestRenderOrgLandingHeaderAddProjectHiddenForMember(t *testing.T) {
	data := orgPageData{
		Org:        &Org{ID: "o1", Name: "Acme"},
		Projects:   []ProjectRef{{ID: "p1", Name: "Home", OrgID: "o1"}},
		IsOrgAdmin: false,
	}
	html := renderHTML(t, OrgLandingPage(data))
	for _, absent := range []string{"Add project", `onclick="openNewProjectModal()"`} {
		if strings.Contains(html, absent) {
			t.Errorf("non-admin org landing must not render %q", absent)
		}
	}
}
