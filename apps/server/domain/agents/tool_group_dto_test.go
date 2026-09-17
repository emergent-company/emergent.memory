package agents

import (
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

var updateGolden = flag.Bool("updateGolden", false, "regenerate the tool-groups golden fixture")

// --- ToolGroupDTO computed catalog ---

func TestToolGroupsDTO_CatalogMembership(t *testing.T) {
	catalog := []mcp.ToolDefinition{
		{Name: "entity-search", RequiredScope: "graph:read"},
		{Name: "entity-create", RequiredScope: "graph:write"},
		{Name: "entity-update", RequiredScope: "graph:write"},
		{Name: "entity-delete", RequiredScope: "graph:write"},
		{Name: "schema-list", RequiredScope: "schema:read"},
	}
	d := &AgentDefinition{
		Tools:       []string{"entity-create", "entity-search"},
		BannedTools: []string{"entity-delete"},
	}

	dto := d.ToolGroupsWithCatalog(catalog)

	var graphRead, graphWrite, schemaRead *ToolGroupDTO
	for i := range dto {
		switch dto[i].ID {
		case "graph-read":
			graphRead = &dto[i]
		case "graph-write":
			graphWrite = &dto[i]
		case "schema-read":
			schemaRead = &dto[i]
		}
	}

	require.NotNil(t, graphWrite)
	// Full membership = catalog tools in the group (ordered), including banned.
	assert.Equal(t, []string{"entity-create", "entity-update", "entity-delete"}, graphWrite.Tools)
	assert.True(t, graphWrite.Enabled, "entity-create is enabled and not banned")

	require.NotNil(t, graphRead)
	assert.Equal(t, []string{"entity-search"}, graphRead.Tools)
	assert.True(t, graphRead.Enabled)

	require.NotNil(t, schemaRead)
	// schema-list is in the catalog but not referenced by the agent.
	assert.Equal(t, []string{"schema-list"}, schemaRead.Tools)
	assert.False(t, schemaRead.Enabled, "catalog-only member is not enabled on the agent")
}

func TestToolGroupsDTO_DynamicToolsResolveToProperGroup(t *testing.T) {
	catalog := []mcp.ToolDefinition{
		{Name: "document-create", RequiredScope: "documents:write"},
		{Name: "document-list", RequiredScope: "documents:read"},
		{Name: "skill-list", RequiredScope: "skills:read"},
	}
	d := &AgentDefinition{
		Tools: []string{"document-create", "skill-list"},
	}

	dto := d.ToolGroupsWithCatalog(catalog)

	var documents, skills, other *ToolGroupDTO
	for i := range dto {
		switch dto[i].ID {
		case "documents":
			documents = &dto[i]
		case "skills":
			skills = &dto[i]
		case "other":
			other = &dto[i]
		}
	}

	require.NotNil(t, documents, "documents group must be present for dynamic document tools")
	assert.Equal(t, []string{"document-create", "document-list"}, documents.Tools)
	require.NotNil(t, skills, "skills group must be present for dynamic skill tools")
	assert.Equal(t, []string{"skill-list"}, skills.Tools)
	// The no-catalog fallback would misroute these to "other"; the catalog path must not.
	assert.Nil(t, other, "dynamic tools must not land in other when the catalog provides scope")
}

func TestToolGroupsDTO_DisabledBannedGroupStillAppears(t *testing.T) {
	catalog := []mcp.ToolDefinition{
		{Name: "schema-create", RequiredScope: "schema:write"},
		{Name: "schema-delete", RequiredScope: "schema:write"},
		{Name: "entity-search", RequiredScope: "graph:read"},
	}
	d := &AgentDefinition{
		Tools:       []string{"entity-search"},
		BannedTools: []string{"schema-create", "schema-delete"},
	}

	dto := d.ToolGroupsWithCatalog(catalog)

	var schemaWrite *ToolGroupDTO
	for i := range dto {
		if dto[i].ID == "schema-write" {
			schemaWrite = &dto[i]
		}
	}
	require.NotNil(t, schemaWrite, "fully disabled group must still render so it can be re-enabled")
	assert.False(t, schemaWrite.Enabled)
	assert.Equal(t, []string{"schema-create", "schema-delete"}, schemaWrite.Tools)
}

func TestToolGroupsDTO_ToolOnlyInBannedAppears(t *testing.T) {
	// No catalog: the fallback must still surface a tool present only in
	// BannedTools (never dropped), using GroupForTool.
	d := &AgentDefinition{
		BannedTools: []string{"schema-create"},
	}
	dto := d.ToolGroupsWithCatalog(nil)

	require.Len(t, dto, 1, "a banned-only tool must still appear in exactly one group")
	assert.Equal(t, "schema-write", dto[0].ID)
	assert.Equal(t, []string{"schema-create"}, dto[0].Tools)
	assert.False(t, dto[0].Enabled)
}

func TestToolGroupsDTO_CatalogUnavailableFallback(t *testing.T) {
	d := &AgentDefinition{
		Tools:       []string{"entity-create", "entity-search"},
		BannedTools: []string{"entity-delete"},
	}
	dto := d.ToolGroupsWithCatalog(nil)

	// Fallback: agent-referenced tools only (Tools ∪ BannedTools), still grouped.
	ids := map[string][]string{}
	for _, g := range dto {
		ids[g.ID] = g.Tools
	}
	assert.Equal(t, []string{"entity-create", "entity-delete"}, ids["graph-write"],
		"banned member must remain in the fallback membership")
	assert.Equal(t, []string{"entity-search"}, ids["graph-read"])
}

func TestToolGroupsDTO_PolicyMapping(t *testing.T) {
	catalog := []mcp.ToolDefinition{
		{Name: "schema-migrate-execute", RequiredScope: "schema:migrate"},
		{Name: "entity-delete", RequiredScope: "graph:write"},
		{Name: "schema-get", RequiredScope: "schema:read"},
		{Name: "entity-query", RequiredScope: "graph:read"},
	}
	d := &AgentDefinition{
		Tools: []string{"schema-migrate-execute", "entity-delete", "schema-get", "entity-query"},
		ToolPolicies: map[string]ToolPolicy{
			"@group:schema-migrate": {Disabled: true}, // deny
			"@group:graph-write":    {Confirm: true},  // ask
			"@group:schema-read":    {},               // allow (stored but neither)
			// graph-read absent → ""
		},
	}
	dto := d.ToolGroupsWithCatalog(catalog)

	policy := map[string]string{}
	for _, g := range dto {
		policy[g.ID] = g.Policy
	}
	assert.Equal(t, "deny", policy["schema-migrate"])
	assert.Equal(t, "ask", policy["graph-write"])
	assert.Equal(t, "allow", policy["schema-read"])
	assert.Equal(t, "", policy["graph-read"], "absent group policy inherits the default")
}

// TestToolGroupsDTOJSONShape locks the frozen D6 contract: field names id,
// label, description, policy, enabled, tools; policy is always present (empty
// string means "inherit"); toolGroups omitted when empty.
func TestToolGroupsDTOJSONShape(t *testing.T) {
	d := &AgentDefinition{
		Tools: []string{"entity-delete", "remember"},
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {Confirm: true},
		},
	}
	b, err := json.Marshal(d.ToDTO())
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	groups, ok := m["toolGroups"].([]any)
	require.True(t, ok, "toolGroups must be present")
	require.NotEmpty(t, groups)

	g := groups[0].(map[string]any)
	for _, key := range []string{"id", "label", "description", "policy", "enabled", "tools"} {
		_, present := g[key]
		assert.True(t, present, "group entry must carry field %q", key)
	}

	// toolGroups omitted when no member tools exist.
	bare, err := json.Marshal((&AgentDefinition{ID: "ad-1"}).ToDTO())
	require.NoError(t, err)
	assert.NotContains(t, string(bare), "toolGroups")
}

// --- Cross-lane golden fixture ---

// goldenCatalog is the representative tool catalog used by the golden fixture:
// several members per group, dynamic documents/skills tools, and graph/schema
// tools exercised by the representative definition.
func goldenCatalog() []mcp.ToolDefinition {
	return []mcp.ToolDefinition{
		{Name: "search-hybrid", RequiredScope: "search"},
		{Name: "entity-search", RequiredScope: "graph:read"},
		{Name: "entity-query", RequiredScope: "graph:read"},
		{Name: "entity-create", RequiredScope: "graph:write"},
		{Name: "entity-update", RequiredScope: "graph:write"},
		{Name: "entity-delete", RequiredScope: "graph:write"},
		{Name: "schema-list", RequiredScope: "schema:read"},
		{Name: "schema-create", RequiredScope: "schema:write"},
		{Name: "schema-delete", RequiredScope: "schema:write"},
		{Name: "document-list", RequiredScope: "documents:read"},
		{Name: "document-create", RequiredScope: "documents:write"},
		{Name: "skill-list", RequiredScope: "skills:read"},
	}
}

// goldenDefinition is the representative agent definition used by the golden
// fixture. It exercises: several members per group, one group fully disabled
// and banned (schema-write), one group with a stored "ask" policy (documents),
// one member with an explicit per-tool override (entity-create), and one
// relay-style <instance>_<tool> name in def.Tools (relay1_reminders_list).
func goldenDefinition() *AgentDefinition {
	return &AgentDefinition{
		Tools: []string{
			"entity-create", "entity-search", "document-create",
			"skill-list", "search-hybrid", "relay1_reminders_list",
		},
		BannedTools: []string{"schema-create", "schema-delete"},
		ToolPolicies: map[string]ToolPolicy{
			"@group:documents": {Confirm: true}, // group "ask"
			"entity-create":    {Confirm: true}, // explicit per-tool override (lives in toolPolicies, not toolGroups)
		},
	}
}

// TestToolGroupsGoldenFixture regenerates and compares the cross-lane golden
// fixture that the gateway parses. Run with -updateGolden to regenerate after a
// deliberate change.
func TestToolGroupsGoldenFixture(t *testing.T) {
	groups := goldenDefinition().ToolGroupsWithCatalog(goldenCatalog())
	pretty, err := json.MarshalIndent(groups, "", "  ")
	require.NoError(t, err)
	pretty = append(pretty, '\n')

	path := goldenFixturePath(t)
	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, pretty, 0o644))
		t.Logf("wrote golden fixture: %s", path)
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "golden fixture missing: run with -updateGolden to generate")
	assert.Equal(t, string(want), string(pretty),
		"golden fixture drifted: run `go test ./domain/agents/ -run TestToolGroupsGoldenFixture -updateGolden` to regenerate")
}

// goldenFixturePath resolves the repo-root-relative golden fixture path from the
// test file location, so it works regardless of the working directory.
func goldenFixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	return filepath.Join(repoRoot, "openspec", "changes", "add-agent-tool-groups", "fixtures", "tool-groups.golden.json")
}

// TestUpdateAgentDefinitionDTO_IgnoresToolGroups proves the update path treats
// `toolGroups` as an unknown, read-only field: the gateway reuses its local
// agent DTO for PATCH and serializes `toolGroups` back, so the server must
// neither persist nor reject it. Echo's JSON binder ignores unknown fields, and
// UpdateAgentDefinitionDTO has no ToolGroups field, so the payload is dropped.
func TestUpdateAgentDefinitionDTO_IgnoresToolGroups(t *testing.T) {
	body := `{
		"name": "x",
		"toolGroups": [{"id":"graph-write","label":"Graph · Write","description":"d","policy":"ask","enabled":true,"tools":["entity-create"]}],
		"toolPolicies": {"@group:graph-write": {"confirm": true}}
	}`

	c, _ := newEchoContextWithUser(http.MethodPatch, "/api/projects/p/agent-definitions/id", body)
	var dto UpdateAgentDefinitionDTO
	require.NoError(t, c.Bind(&dto), "unknown toolGroups field must not reject the PATCH")

	// The @group: prefixed policy survives binding untouched.
	require.Contains(t, dto.ToolPolicies, "@group:graph-write")
	assert.True(t, dto.ToolPolicies["@group:graph-write"].Confirm)

	// toolGroups is read-only: it cannot round-trip through the update DTO.
	b, err := json.Marshal(dto)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "toolGroups")
}
