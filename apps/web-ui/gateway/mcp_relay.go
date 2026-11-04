package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// --- MCP relay (external nodes) ---
//
// External MCP "relay" nodes are remote machines that connect to the backend
// MCP relay hub (outbound-WebSocket star) and serve local MCP tools (Apple
// Notes/Reminders, home automation, …). The backend exposes two project-scoped
// read endpoints for them, and unlike every other gateway client method those
// endpoints return PLAIN JSON — no successEnvelope wrapper — so they decode
// through their own path below.

// RelaySession is one currently-connected external MCP relay node, as reported
// by GET /api/mcp-relay/sessions. Sessions live in-memory on the backend: a
// session's presence IS the node's connection health.
type RelaySession struct {
	InstanceID  string    `json:"instance_id"`
	Version     string    `json:"version"`
	ToolCount   int       `json:"tool_count"`
	ConnectedAt time.Time `json:"connected_at"`
}

// RelayTool is one tool served by a relay node, normalized from the raw MCP
// tools/list payload the node registered (see extractRelayTools). The
// agent-facing name is derived once, at the UI boundary: <instance>_<tool>,
// matching how the backend names relay tools in the tool pool.
type RelayTool struct {
	Name        string
	Description string
}

// ListRelaySessions calls GET /api/mcp-relay/sessions and decodes the plain
// {sessions:[...]} body (no successEnvelope). Sessions are project-scoped, so
// the request carries the standard bearer token + session project headers.
func (m *MemoryClient) ListRelaySessions(ctx context.Context) ([]RelaySession, error) {
	var out struct {
		Sessions []RelaySession `json:"sessions"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/mcp-relay/sessions", nil, &out); err != nil {
		return nil, err
	}
	if out.Sessions == nil {
		return []RelaySession{}, nil
	}
	return out.Sessions, nil
}

// GetRelaySessionTools calls GET /api/mcp-relay/sessions/:instanceId/tools and
// returns the node's tools as a normalized name list. The endpoint relays the
// raw MCP tools/list payload the node registered, whose shape varies between
// connector implementations (nested {"tools":[...]} vs a bare array; tool
// entries carrying name/description at the top level or under a "tool"
// object) — extractRelayTools tolerates all of them.
func (m *MemoryClient) GetRelaySessionTools(ctx context.Context, instanceID string) ([]RelayTool, error) {
	var raw json.RawMessage
	if err := m.do(ctx, http.MethodGet, "/api/mcp-relay/sessions/"+url.PathEscape(instanceID)+"/tools", nil, &raw); err != nil {
		return nil, err
	}
	return extractRelayTools(raw), nil
}

// extractRelayTools pulls tool names (+ optional descriptions) out of a raw MCP
// tools/list response body. Connector payloads vary: the tool list may sit
// under {"tools":[...]} or be a bare JSON array, and each entry may carry
// name/description at the top level or nested under a "tool" object. Entries
// with no resolvable name are skipped; an unparseable body yields an empty
// list (never an error — shape quirks must not break the page).
func extractRelayTools(raw json.RawMessage) []RelayTool {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if arr, ok := obj["tools"].([]any); ok {
			return relayToolsFromAny(arr)
		}
		return nil
	}
	var arr []any
	if json.Unmarshal(raw, &arr) == nil {
		return relayToolsFromAny(arr)
	}
	return nil
}

// relayToolsFromAny normalizes one []any of tool entries into RelayTools.
func relayToolsFromAny(list []any) []RelayTool {
	var out []RelayTool
	for _, item := range list {
		var name, desc string
		if m, ok := item.(map[string]any); ok {
			name, _ = m["name"].(string)
			desc, _ = m["description"].(string)
			if sub, ok := m["tool"].(map[string]any); ok {
				if strings.TrimSpace(name) == "" {
					name, _ = sub["name"].(string)
				}
				if strings.TrimSpace(desc) == "" {
					desc, _ = sub["description"].(string)
				}
			}
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, RelayTool{Name: strings.TrimSpace(name), Description: strings.TrimSpace(desc)})
	}
	return out
}

// relayAgentToolName derives the agent-facing name of a relay tool
// (<instance>_<tool>) — the exact name an agent's flat tool whitelist
// references and the tool pool uses.
func relayAgentToolName(instanceID, tool string) string {
	return instanceID + "_" + tool
}

// relayNode bundles one connected relay session with the tools it serves, so
// the External MCP nodes page and the agent tool picker share a single shape.
// Tools are fetched per node (fetch-on-select / on-settings-load), so a node
// with an unreachable tools endpoint simply carries an empty Tools list.
type relayNode struct {
	Session RelaySession
	Tools   []RelayTool
}

// agentToolNames returns the node's agent-facing tool names
// (<instance>_<tool>), sorted for deterministic rendering.
func (n relayNode) agentToolNames() []string {
	out := make([]string, 0, len(n.Tools))
	for _, t := range n.Tools {
		out = append(out, relayAgentToolName(n.Session.InstanceID, t.Name))
	}
	return out
}

// relayNodeInUse mirrors mcpServerInUse for relay nodes: true when the agent
// already whitelists one of this node's agent-facing tools (drives the default
// open/collapsed state of the picker's <details> group).
func relayNodeInUse(node relayNode, agent *AgentDefinition) bool {
	if agent == nil {
		return false
	}
	for _, t := range node.Tools {
		if containsString(agent.Tools, relayAgentToolName(node.Session.InstanceID, t.Name)) {
			return true
		}
	}
	return false
}

// relayPickerGroups filters connected relay nodes down to those serving at
// least one tool: a node with nothing to offer would render an empty,
// uncheckable group in the agent tool picker.
func relayPickerGroups(nodes []relayNode) []relayNode {
	var out []relayNode
	for _, n := range nodes {
		if len(n.Tools) > 0 {
			out = append(out, n)
		}
	}
	return out
}
