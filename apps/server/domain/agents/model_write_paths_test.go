package agents

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ---- MCP write paths (#1465) ----

// TestMCPCreateAgentDefinition_InvalidModelRejected proves the MCP create tool
// validates model references before any repository call. The handler carries a
// nil repo, so reaching validation (rather than a nil dereference) is the
// mutation check: reverting the validation makes this test panic instead of
// returning the structured error result.
func TestMCPCreateAgentDefinition_InvalidModelRejected(t *testing.T) {
	h := &MCPToolHandler{repo: nil}

	for _, model := range []string{"gpt-4o", "/deepseek-v4-flash", "openai//gpt-4o", "openai/"} {
		t.Run(model, func(t *testing.T) {
			res, err := h.ExecuteCreateAgentDefinition(context.Background(), "proj-test-id", map[string]any{
				"name":  "a",
				"model": model,
			})
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Len(t, res.Content, 1)
			require.Contains(t, res.Content[0].Text, "provider/model",
				"error result must name the expected form")
		})
	}
}

// TestMCPUpdateAgentDefinition_InvalidModelRejected proves the MCP update tool
// rejects a malformed model before the definition lookup, mirroring the REST
// path. Same mutation check as above: nil repo + fail-fast validation.
func TestMCPUpdateAgentDefinition_InvalidModelRejected(t *testing.T) {
	h := &MCPToolHandler{repo: nil}
	id := "00000000-0000-0000-0000-000000000000"

	cases := []struct {
		name      string
		model     string
		provider  string
		errSubstr string
	}{
		{name: "empty provider segment", model: "/deepseek-v4-flash", errSubstr: "provider/model"},
		{name: "no provider segment", model: "deepseek-v4-flash", errSubstr: "provider/model"},
		{name: "double separator", model: "openai//gpt-4o", errSubstr: "provider/model"},
		{name: "conflicting provider", model: "openai/gpt-4o", provider: "google", errSubstr: "conflicts"},
		{name: "provider without model", model: "", provider: "openai", errSubstr: "provider"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"definition_id": id, "model": tc.model}
			if tc.provider != "" {
				args["provider"] = tc.provider
			}
			res, err := h.ExecuteUpdateAgentDefinition(context.Background(), "proj-test-id", args)
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Len(t, res.Content, 1)
			require.Contains(t, res.Content[0].Text, tc.errSubstr)
		})
	}
}

// ---- Project agent override write path (#1465) ----

// TestSetAgentOverride_InvalidModelReturns400 proves the override HTTP handler
// validates model.name before persisting. Nil repo + 400 = validation fired
// first; without it the handler would dereference the nil repository.
func TestSetAgentOverride_InvalidModelReturns400(t *testing.T) {
	h := newTestHandler()
	c, _ := newEchoContextWithUser(http.MethodPut,
		"/api/projects/proj-test-id/agent-definitions/overrides/some-agent",
		`{"model":{"name":"/deepseek-v4-flash"}}`)
	c.SetParamNames("projectId", "agentName")
	c.SetParamValues("proj-test-id", "some-agent")

	err := h.SetAgentOverride(c)

	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T: %v", err, err)
	require.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	require.Contains(t, appErr.Message, "provider/model")
}

// TestApplyAgentOverride_MalformedStoredModelIgnored proves the runtime merge
// never copies a malformed legacy override value onto the definition.
func TestApplyAgentOverride_MalformedStoredModelIgnored(t *testing.T) {
	def := &AgentDefinition{Model: &ModelConfig{Name: "openai/gpt-4o"}}
	override := &AgentOverride{Model: &ModelConfig{Name: "/deepseek-v4-flash"}}

	ApplyAgentOverride(def, override)

	require.Equal(t, "openai/gpt-4o", def.Model.Name,
		"malformed stored override must not overwrite a valid definition model")
}

// TestApplyAgentOverride_StructuredProviderFolds proves the runtime merge
// accepts the structured provider form and stores the canonical prefixed name.
func TestApplyAgentOverride_StructuredProviderFolds(t *testing.T) {
	def := &AgentDefinition{Model: &ModelConfig{}}
	override := &AgentOverride{Model: &ModelConfig{Name: "gpt-4o", Provider: "openai"}}

	ApplyAgentOverride(def, override)

	require.Equal(t, "openai/gpt-4o", def.Model.Name)
	require.Empty(t, def.Model.Provider, "structured provider must be folded into the name")
}

// ---- Error message contract ----

// TestInvalidModelReferenceMessage is a light guard that the shared error keeps
// naming the expected "provider/model" form for all write paths.
func TestInvalidModelReferenceMessage(t *testing.T) {
	require.True(t, strings.Contains(ErrInvalidAgentModelRef.Error(), "provider/model"))
}
