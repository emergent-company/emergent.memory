package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testShareCatalog builds the real shareable catalog for a bare service, the
// same way HandleListToolCatalog does for a project admin.
func testShareCatalog(t *testing.T) []CatalogToolDTO {
	t.Helper()
	svc := &Service{}
	catalog := BuildToolCatalog(context.Background(), svc, "proj-1")
	require.NotEmpty(t, catalog, "shareable catalog must not be empty")
	return catalog
}

func TestShareTemplatesIDsAreFrozenAndUnique(t *testing.T) {
	// The ids are a client contract (rendered as data-share-template values and
	// possibly persisted). Reordering this list is fine; renaming an id is not.
	want := []string{
		"read-only",
		"knowledge-writer",
		"schema-admin",
		"agent-observer",
		"documents-only",
		"full-access",
	}
	templates := ShareTemplates()
	got := make([]string, 0, len(templates))
	for _, tmpl := range templates {
		got = append(got, tmpl.ID)
	}
	assert.Equal(t, want, got)

	seen := map[string]bool{}
	defaults := 0
	for _, tmpl := range templates {
		assert.NotEmpty(t, tmpl.Label, "template %q needs a label", tmpl.ID)
		assert.NotEmpty(t, tmpl.Description, "template %q needs a description", tmpl.ID)
		assert.False(t, seen[tmpl.ID], "duplicate template id %q", tmpl.ID)
		seen[tmpl.ID] = true
		if tmpl.Default {
			defaults++
			assert.Equal(t, "read-only", tmpl.ID, "the read-only preset is the create-form default")
		}
	}
	assert.Equal(t, 1, defaults, "exactly one template must be the default")
}

func TestResolveShareTemplatesAgainstRealCatalog(t *testing.T) {
	catalog := testShareCatalog(t)
	templates := ResolveShareTemplates(catalog)
	require.Len(t, templates, len(shareTemplates), "every preset must resolve against the real catalog")

	byScope := map[string]string{}
	for _, tool := range catalog {
		byScope[tool.Name] = tool.RequiredScope
	}
	has := map[string]bool{}
	for _, tool := range catalog {
		has[tool.Name] = true
	}

	for _, tmpl := range templates {
		assert.NotEmpty(t, tmpl.Tools, "template %q resolved to no tools", tmpl.ID)
		assert.Equal(t, len(tmpl.Tools), tmpl.ToolCount, "template %q count must mirror its tools", tmpl.ID)
		seen := map[string]bool{}
		for _, name := range tmpl.Tools {
			assert.True(t, has[name], "template %q names %q, which is not in the shareable catalog", tmpl.ID, name)
			assert.False(t, seen[name], "template %q lists %q twice", tmpl.ID, name)
			seen[name] = true
		}
	}

	// Full access is exactly the catalog: no preset can exceed what a key may carry.
	for _, tmpl := range templates {
		if tmpl.ID == "full-access" {
			assert.Equal(t, len(catalog), tmpl.ToolCount)
		}
	}
}

// TestShareTemplatesNeverCarryNonShareableScopes guards the one thing a preset
// must never do: hand a share key a scope the allowlist would refuse. The
// catalog is pre-filtered, so this asserts the contract at the template layer
// too, where a scope is written by hand.
func TestShareTemplatesNeverCarryNonShareableScopes(t *testing.T) {
	for _, tmpl := range ShareTemplates() {
		for _, scope := range tmpl.Scopes {
			assert.False(t, isNonShareableScope(scope), "template %q includes non-shareable scope %q", tmpl.ID, scope)
		}
	}
	// And through resolution, no resolved tool may itself be unshareable.
	catalog := testShareCatalog(t)
	shareable := map[string]bool{}
	for _, tool := range catalog {
		shareable[tool.Name] = true
	}
	for _, tmpl := range ResolveShareTemplates(catalog) {
		for _, name := range tmpl.Tools {
			assert.True(t, shareable[name], "template %q leaked unshareable tool %q", tmpl.ID, name)
			assert.False(t, agentExecutionTools[name], "template %q leaked agent-execution tool %q", tmpl.ID, name)
			assert.False(t, agentMutationTools[name], "template %q leaked agent-mutation tool %q", tmpl.ID, name)
		}
	}
}

// TestReadOnlyTemplateCoversLegacyScopes keeps the default preset at least as
// capable as the legacy read-only share token, so an outside client migrated to
// a template-keyed share does not lose tools it used to have.
func TestReadOnlyTemplateCoversLegacyScopes(t *testing.T) {
	catalog := testShareCatalog(t)
	var readOnly *ShareTemplateDTO
	for _, tmpl := range ResolveShareTemplates(catalog) {
		if tmpl.ID == "read-only" {
			readOnly = &tmpl
			break
		}
	}
	require.NotNil(t, readOnly, "read-only template must resolve")
	granted := map[string]bool{}
	for _, name := range readOnly.Tools {
		granted[name] = true
	}
	scopeSet := map[string]bool{}
	for _, scope := range readOnlyMCPScopes {
		scopeSet[scope] = true
	}
	for _, tool := range catalog {
		if scopeSet[tool.RequiredScope] {
			assert.True(t, granted[tool.Name], "read-only template must include %q (scope %q)", tool.Name, tool.RequiredScope)
		}
	}
}

func TestResolveShareTemplatesDropsUnknownTools(t *testing.T) {
	catalog := []CatalogToolDTO{
		{Name: "entity-search", RequiredScope: "graph:read"},
		{Name: "entity-create", RequiredScope: "graph:write"},
	}
	templates := ResolveShareTemplates(catalog)
	require.NotEmpty(t, templates)

	byID := map[string]ShareTemplateDTO{}
	for _, tmpl := range templates {
		byID[tmpl.ID] = tmpl
	}

	// documents-only names tools the catalog lacks: it resolves to nothing and is
	// therefore omitted rather than offered as an empty preset.
	_, ok := byID["documents-only"]
	assert.False(t, ok, "a preset that resolves to no catalog tools must be omitted")

	assert.ElementsMatch(t, []string{"entity-search"}, byID["read-only"].Tools)
	assert.ElementsMatch(t, []string{"entity-search"}, byID["schema-admin"].Tools)
	assert.ElementsMatch(t, []string{"entity-search", "entity-create"}, byID["knowledge-writer"].Tools)
	assert.ElementsMatch(t, []string{"entity-search", "entity-create"}, byID["full-access"].Tools)
}
