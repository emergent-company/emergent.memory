package mcp

import (
	"context"
	"testing"
)

// agentToolScopeDecisions is the authority matrix this test pins: every
// agent-domain MCP tool name and the scope its HTTP equivalent enforces
// (domain/agents/routes.go). It is a literal list, not derived from
// agentToolRequiredScope, so a scope that silently changes on one side only
// fails here.
var agentToolScopeDecisions = map[string]string{
	// Agent definitions — /api/projects/:projectId/agent-definitions
	"agent-def-list":          "agents:read",
	"agent-def-get":           "agents:read",
	"agent-def-create":        "agents:write",
	"update_agent_definition": "agents:write",
	"agent-def-delete":        "agents:write",

	// Runtime agents — /api/projects/:projectId/agents
	"agent-list":    "agents:read",
	"agent-get":     "agents:read",
	"agent-create":  "agents:write",
	"update_agent":  "agents:write",
	"agent-delete":  "agents:write",
	"trigger_agent": "agents:write",

	// Agent runs — /api/projects/:projectId/agent-runs and /api/v1/runs
	"agent-run-list":       "agents:read",
	"agent-run-get":        "agents:read",
	"agent-run-messages":   "agents:read",
	"agent-run-tool-calls": "agents:read",
	"agent-run-status":     "agents:read",

	// Agent catalog
	"agent-list-available": "agents:read",
}

// scopedAgentToolHandler embeds the no-op stub and returns a fixed set of agent
// tool definitions (real names, no RequiredScope) so the test can assert the MCP
// layer assigns the HTTP-equivalent scope to each.
type scopedAgentToolHandler struct {
	stubAgentToolHandler
	defs []ToolDefinition
}

func (s *scopedAgentToolHandler) GetAgentToolDefinitions() []ToolDefinition {
	return s.defs
}

func (s *scopedAgentToolHandler) GetAgentToolDefinitionsForProject(_ context.Context, _ string) []ToolDefinition {
	return s.defs
}

// agentToolDefs builds minimal ToolDefinitions (no RequiredScope) for the full
// agent-tool catalog plus remember-status, mirroring domain/agents output.
func agentToolDefs() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(agentToolScopeDecisions)+1)
	for name := range agentToolScopeDecisions {
		defs = append(defs, ToolDefinition{Name: name})
	}
	// remember-status is also handler-provided and covered by the static map.
	defs = append(defs, ToolDefinition{Name: "remember-status"})
	return defs
}

func findToolByName(t *testing.T, tools []ToolDefinition, name string) *ToolDefinition {
	t.Helper()
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	t.Fatalf("tool %q not found in catalog", name)
	return nil
}

// TestAgentToolsRequireHTTPEquivalentScope asserts every agent tool resolves to
// the scope its HTTP route enforces, and — critically — that this holds on BOTH
// catalog paths: GetToolDefinitions (used by tools/list without a project) and
// GetToolDefinitionsForProject (which replaces agent tools with project-enriched
// definitions that carry no RequiredScope).
func TestAgentToolsRequireHTTPEquivalentScope(t *testing.T) {
	svc := &Service{agentToolHandler: &scopedAgentToolHandler{defs: agentToolDefs()}}

	for _, path := range []string{"GetToolDefinitions", "GetToolDefinitionsForProject"} {
		t.Run(path, func(t *testing.T) {
			var tools []ToolDefinition
			if path == "GetToolDefinitions" {
				tools = svc.GetToolDefinitions()
			} else {
				tools = svc.GetToolDefinitionsForProject(context.Background(), "proj-1")
			}
			for name, want := range agentToolScopeDecisions {
				got := findToolByName(t, tools, name).RequiredScope
				if got != want {
					t.Errorf("tool %q scope = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// TestAgentToolScopesAreKnownVocabulary asserts every scope the agent-tool map
// declares is part of the MCP tool-scope vocabulary, so umbrella-scope
// projection never silently drops it (the same guard domain/agents runs for its
// own declarations).
func TestAgentToolScopesAreKnownVocabulary(t *testing.T) {
	for name, scope := range agentToolRequiredScope {
		if !IsToolScope(scope) {
			t.Errorf("agent tool %q requires scope %q not in the MCP tool-scope vocabulary", name, scope)
		}
	}
}

// TestAgentToolScopeGateRefusesInsufficientCaller is the fail-first parity check.
// It drives the exact tools/call gate the transports use (handler.go,
// streamable_http_handler.go, sse_handler.go): a tool whose RequiredScope is not
// in the caller's expanded scope set must be refused. A graph-only caller must
// not reach any agent tool, while a caller holding the matching scope must.
func TestAgentToolScopeGateRefusesInsufficientCaller(t *testing.T) {
	svc := &Service{agentToolHandler: &scopedAgentToolHandler{defs: agentToolDefs()}}
	// Build the tool index the transports consult via GetToolByName.
	svc.GetToolDefinitionsForProject(context.Background(), "proj-1")

	cases := []struct {
		name       string
		tool       string
		caller     []string
		wantRefuse bool
	}{
		{"write tool / graph-only caller", "agent-create", []string{"graph:read", "graph:write"}, true},
		{"write tool / read-only agent caller", "agent-create", []string{"agents:read"}, true},
		{"trigger / graph-only caller", "trigger_agent", []string{"graph:read"}, true},
		{"trigger / read-only agent caller", "trigger_agent", []string{"agents:read"}, true},
		{"delete / read-only agent caller", "agent-delete", []string{"agents:read"}, true},
		{"write tool / writer caller (no over-correction)", "agent-create", []string{"agents:write"}, false},
		{"trigger / writer caller (no over-correction)", "trigger_agent", []string{"agents:write"}, false},
		{"read tool / reader caller (no over-correction)", "agent-list", []string{"agents:read"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := svc.GetToolByName(tc.tool)
			if def == nil {
				t.Fatalf("tool %q missing from index", tc.tool)
			}
			if def.RequiredScope == "" {
				t.Fatalf("tool %q declares no RequiredScope — gate is bypassed", tc.tool)
			}
			expanded := expandScopesSet(tc.caller)
			refused := !expanded[def.RequiredScope]
			if refused != tc.wantRefuse {
				t.Errorf("caller %v on %q: refused=%v, want %v (required scope %q)", tc.caller, tc.tool, refused, tc.wantRefuse, def.RequiredScope)
			}
		})
	}
}
