package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testShareCatalog builds the catalog a project admin sees when no
// project-specific dynamic tools are registered: BuildToolCatalog over a bare
// Service, which reads the static tool definitions and applies the same
// shareability filter the endpoint uses.
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
// capable as the legacy read-only share token for every tool the catalog
// exposes under those scopes, so an outside client migrated to a
// template-keyed share does not lose tools it used to have. The legacy token's
// non-tool scopes (chat:use, projects:read baseline) are covered separately by
// TestSearchKnowledgeShareDerivesChatUse.
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

// TestShareLoopbackToolsDeriveChatUse guards the runtime-only scope: these tools
// call a chat route loopback with the caller's token, and those routes require
// chat:use. A share without it would advertise the tool and then 403 every call.
// The legacy read-only token carried chat:use for exactly this reason.
func TestShareLoopbackToolsDeriveChatUse(t *testing.T) {
	svc := &Service{}
	for _, tool := range []string{"search-knowledge", "remember", "forget"} {
		scopes, err := deriveScopesForToolNames([]string{tool}, svc.GetToolByName)
		require.NoError(t, err)
		assert.Contains(t, scopes, "chat:use", "%s must derive the runtime loopback scope", tool)
	}
	// The transport scope is still derived alongside the runtime one.
	scopes, err := deriveScopesForToolNames([]string{"search-knowledge"}, svc.GetToolByName)
	require.NoError(t, err)
	assert.Contains(t, scopes, "search")

	plain, err := deriveScopesForToolNames([]string{"entity-search"}, svc.GetToolByName)
	require.NoError(t, err)
	assert.NotContains(t, plain, "chat:use", "a graph-only tool must not widen the token to chat")
}

// TestAddShareToolScopesRejectsNonShareable is the guard on the hand-maintained
// additional-scope map: a future entry must not slip an admin scope onto a
// share token.
func TestAddShareToolScopesRejectsNonShareable(t *testing.T) {
	seen := map[string]bool{}
	require.NoError(t, addShareToolScopes(seen, "search-knowledge", []string{"chat:use"}))
	assert.True(t, seen["chat:use"])

	for _, scope := range []string{"admin", "admin:all", "projects:write", "account:billing"} {
		err := addShareToolScopes(map[string]bool{}, "some-tool", []string{scope})
		require.Error(t, err, "scope %q must be refused", scope)
		assert.Contains(t, err.Error(), "administrative or project-management")
	}
}

// TestShareTemplateDerivedScopesPerPreset pins the exact token scopes each
// preset grants. A share token is an ordinary API token, so its scopes are
// honoured by REST routes too — a narrow preset silently acquiring a broad
// scope is a security regression, not just a naming problem. Adding a tool with
// a new scope to a preset must be a deliberate edit here as well.
func TestShareTemplateDerivedScopesPerPreset(t *testing.T) {
	want := map[string][]string{
		"read-only": {
			"agents:read", "chat:use", "documents:read", "graph:read",
			"journal:read", "projects:read", "schema:read", "search", "skills:read",
		},
		"knowledge-writer": {
			"agents:read", "chat:use", "documents:read", "documents:write",
			"graph:read", "graph:write", "journal:read", "journal:write",
			"projects:read", "schema:read", "search", "skills:read",
		},
		"schema-admin": {
			"graph:read", "projects:read", "schema:migrate", "schema:read",
			"schema:write", "skills:read",
		},
		"agent-observer": {"agents:read", "chat:use", "graph:read", "projects:read", "search"},
		"documents-only": {"documents:read", "documents:write", "projects:read", "schema:read", "search"},
	}
	svc := &Service{}
	for _, tmpl := range ResolveShareTemplates(testShareCatalog(t)) {
		expected, ok := want[tmpl.ID]
		if !ok {
			continue // full-access is pinned by the ceiling test above.
		}
		scopes, err := deriveScopesForToolNames(tmpl.Tools, svc.GetToolByName)
		require.NoError(t, err, "template %q", tmpl.ID)
		assert.Equal(t, expected, scopes, "template %q changed the scopes it grants", tmpl.ID)
	}

	// A Documents share must not be able to write knowledge or touch chat over
	// REST: no chat:use, no graph:write.
	documents, ok := want["documents-only"]
	require.True(t, ok)
	assert.NotContains(t, documents, "chat:use")
	assert.NotContains(t, documents, "graph:write")
}

// TestShareTemplateDerivedScopesStayWithinDocumentedSet pins the scopes a preset
// may put on a share token. Presets are the most likely place for a new scope to
// slip in unnoticed, and a share token is an ordinary API token: its scopes are
// honoured by REST routes too, not just the MCP handlers.
func TestShareTemplateDerivedScopesStayWithinDocumentedSet(t *testing.T) {
	allowed := map[string]bool{
		"projects:read":   true, // baseline
		"search":          true,
		"chat:use":        true,
		"graph:read":      true,
		"graph:write":     true,
		"documents:read":  true,
		"documents:write": true,
		"journal:read":    true,
		"journal:write":   true,
		"schema:read":     true,
		"schema:write":    true,
		"schema:migrate":  true,
		"skills:read":     true,
		"skills:write":    true,
		"agents:read":     true,
	}
	svc := &Service{}
	for _, tmpl := range ResolveShareTemplates(testShareCatalog(t)) {
		scopes, err := deriveScopesForToolNames(tmpl.Tools, svc.GetToolByName)
		require.NoError(t, err, "template %q", tmpl.ID)
		for _, scope := range scopes {
			assert.False(t, isNonShareableScope(scope), "template %q derived non-shareable scope %q", tmpl.ID, scope)
			assert.True(t, allowed[scope], "template %q derived %q — add it to the documented set only with a deliberate decision", tmpl.ID, scope)
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
