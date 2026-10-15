package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/stretchr/testify/require"
)

// TestAgentDefinitionToSummaryDTOIncludesSkills ensures the lightweight list DTO
// carries the bound skill names so clients can compute per-skill usage without
// fetching each definition in full.
func TestAgentDefinitionToSummaryDTOIncludesSkills(t *testing.T) {
	d := &AgentDefinition{
		ID:        "ad-1",
		ProjectID: "proj-1",
		Name:      "diane",
		Tools:     []string{"memory", "skill"},
		Skills:    []string{"diane.meetings", "research"},
	}

	s := d.ToSummaryDTO()

	require.Equal(t, "ad-1", s.ID)
	require.Equal(t, 2, s.ToolCount)
	require.Equal(t, []string{"diane.meetings", "research"}, s.Skills)
}

// TestAgentDefinitionDTOUIConfigRoundTrip locks the ui_config contract: the
// entity's raw JSON appearance blob must flow through both ToDTO (full) and
// ToSummaryDTO (list/picker) as uiConfig, and serialize as the same JSON the
// client sent in.
func TestAgentDefinitionDTOUIConfigRoundTrip(t *testing.T) {
	raw := json.RawMessage(`{"icon":"bot","color":"#FF00AA"}`)
	d := &AgentDefinition{
		ID:       "ad-1",
		UIConfig: raw,
	}

	full := d.ToDTO()
	require.Equal(t, raw, full.UIConfig)
	require.JSONEq(t, `{"icon":"bot","color":"#FF00AA"}`, string(full.UIConfig))

	summary := d.ToSummaryDTO()
	require.Equal(t, raw, summary.UIConfig)

	// nil uiConfig -> omitted from the JSON payload (omitempty), treated as
	// "no appearance" by clients.
	bare, err := json.Marshal((&AgentDefinition{ID: "ad-2"}).ToDTO())
	require.NoError(t, err)
	require.NotContains(t, string(bare), "uiConfig")
}

// TestAgentDefinitionToSummaryDTOIncludesWorkConfig locks the work-path status
// map onto the list summary: the gateway's board derives its drag/drawer actions
// from the agent definitions list, so workConfig.status must survive
// ToSummaryDTO. A zero config is omitted so unconfigured definitions stay lean.
func TestAgentDefinitionToSummaryDTOIncludesWorkConfig(t *testing.T) {
	d := &AgentDefinition{
		ID:         "ad-1",
		WorkConfig: AgentWorkConfig{Status: AgentWorkStatusConfig{Ready: "todo", InProgress: "doing"}},
	}

	s := d.ToSummaryDTO()
	require.NotNil(t, s.WorkConfig)
	require.Equal(t, "todo", s.WorkConfig.Status.Ready)
	require.Equal(t, "doing", s.WorkConfig.Status.InProgress)

	// The gateway's board reads this over the wire: the list payload must carry
	// workConfig.status with the exact JSON names boardStatusMapFromDefinitions
	// decodes (ready/inProgress/...). Asserting only the Go struct would miss a
	// JSON-tag drift that silently nil's the gateway's WorkConfig (#1428).
	encoded, err := json.Marshal(s)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(encoded, &payload))
	wc, ok := payload["workConfig"].(map[string]any)
	require.True(t, ok, "list payload must carry workConfig, got %s", encoded)
	status, ok := wc["status"].(map[string]any)
	require.True(t, ok, "workConfig must carry status, got %s", encoded)
	require.Equal(t, "todo", status["ready"])
	require.Equal(t, "doing", status["inProgress"])

	bare, err := json.Marshal((&AgentDefinition{ID: "ad-2"}).ToSummaryDTO())
	require.NoError(t, err)
	require.NotContains(t, string(bare), "workConfig")
}

// TestAgentToDTOIncludesAgentDefinitionID ensures the response DTO exposes the
// linked agent definition so clients can read back a scheduled agent's
// definition after create/update. The field was previously absent from
// AgentDTO, so the gateway could not preselect the saved definition.
func TestAgentToDTOIncludesAgentDefinitionID(t *testing.T) {
	defID := "ad-123"
	a := &Agent{
		ID:                "agent-1",
		Name:              "Test",
		AgentDefinitionID: &defID,
	}

	dto := a.ToDTO()

	require.NotNil(t, dto.AgentDefinitionID)
	require.Equal(t, "ad-123", *dto.AgentDefinitionID)
}

// TestAgentDefinitionDTOIncludesSourceBlueprintID locks the blueprint-provenance
// contract: the full definition DTO must surface SourceBlueprintID (with the
// wire name "sourceBlueprintId") so the gateway can warn when a user edits a
// blueprint-derived agent. A nil value is omitted (omitempty).
func TestAgentDefinitionDTOIncludesSourceBlueprintID(t *testing.T) {
	bp := "bp-123"
	d := &AgentDefinition{ID: "ad-1", SourceBlueprintID: &bp}

	full := d.ToDTO()
	require.NotNil(t, full.SourceBlueprintID)
	require.Equal(t, "bp-123", *full.SourceBlueprintID)

	encoded, err := json.Marshal(full)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(encoded, &payload))
	require.Equal(t, "bp-123", payload["sourceBlueprintId"],
		"wire name must be sourceBlueprintId, got %s", encoded)

	bare, err := json.Marshal((&AgentDefinition{ID: "ad-2"}).ToDTO())
	require.NoError(t, err)
	require.NotContains(t, string(bare), "sourceBlueprintId")
}

// TestAgentRunDTOJSONCarriesSpans locks the run DTO contract: when spans are
// attached (e.g. by Handler.enrichRunTrace) they serialize under "spans", and
// when absent the field is omitted entirely.
func TestAgentRunDTOJSONCarriesSpans(t *testing.T) {
	traceID := "trace-1"
	dto := &AgentRunDTO{
		ID:      "run-1",
		TraceID: &traceID,
		Spans: []RunTraceSpan{
			{
				SpanID:        "0000000000000001",
				Name:          "agent.run",
				StartUnixNano: 1700000000000000000,
				EndUnixNano:   1700000001000000000,
			},
			{
				SpanID:       "0000000000000002",
				ParentSpanID: "0000000000000001",
				Name:         "call_llm",
				InputTokens:  123,
				OutputTokens: 456,
				Model:        "deepseek-v4-flash",
			},
		},
	}

	b, err := json.Marshal(dto)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	spans, ok := m["spans"].([]any)
	require.True(t, ok, "spans field must be present when populated")
	require.Len(t, spans, 2)

	// Span nil -> omitted (omitempty).
	bare, err := json.Marshal(&AgentRunDTO{ID: "run-2"})
	require.NoError(t, err)
	require.NotContains(t, string(bare), `"spans"`)
	require.NotContains(t, string(bare), "spanId")
}

// TestAgentDefinitionToDTORedactsSandboxSecrets locks the secret-redaction
// contract (issue #1844): a git token stored in the sandbox env_vars must never
// surface — in the DTO or its JSON — through the agent-definition read path.
func TestAgentDefinitionToDTORedactsSandboxSecrets(t *testing.T) {
	const realToken = "ghp_realTokenValue"
	d := &AgentDefinition{
		ID: "ad-1",
		SandboxConfig: map[string]any{
			"enabled": true,
			"env_vars": map[string]any{
				"GH_TOKEN": realToken,
				"NODE_ENV": "production",
			},
		},
	}

	dto := d.ToDTO()
	env, ok := dto.SandboxConfig["env_vars"].(map[string]any)
	require.True(t, ok, "env_vars must be preserved in workspaceConfig")
	require.Equal(t, sandbox.RedactedSecretValue, env["GH_TOKEN"])
	require.Equal(t, "production", env["NODE_ENV"])

	b, err := json.Marshal(dto)
	require.NoError(t, err)
	require.NotContains(t, string(b), realToken, "raw git token must not appear in the DTO JSON")
}

// TestAgentRunDTOSpansCompanionToTokenUsage documents the pairing: enrichRunTrace
// populates both fields from a single trace fetch, so a DTO with token usage set
// may carry spans alongside it without breaking serialization.
func TestAgentRunDTOSpansCompanionToTokenUsage(t *testing.T) {
	dto := &AgentRunDTO{
		ID:         "run-1",
		TokenUsage: &RunTokenUsage{TotalInputTokens: 123, TotalOutputTokens: 456},
		Spans:      []RunTraceSpan{{SpanID: "s1", Name: "call_llm"}},
	}
	b, err := json.Marshal(dto)
	require.NoError(t, err)
	s := string(b)
	require.True(t, strings.Contains(s, `"tokenUsage"`))
	require.True(t, strings.Contains(s, `"spans"`))
}
