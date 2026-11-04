package mcpregistry

import "strings"

// SlugifyServerName converts a server name into a function-name-safe prefix:
// lowercased; any run of characters outside [a-z0-9] collapses to a single "_";
// leading/trailing "_" trimmed; falls back to "server" when nothing remains.
// Deterministic and pure.
//
// This is the SINGLE canonical slug for MCP server names. The agents ToolPool
// prefixes external pool keys with SlugifyServerName(serverName) (e.g. server
// "E2E MCP 123" → key prefix "e2e_mcp_123"), and the proxy reverses that
// mapping on tool calls. The algorithm is byte-for-byte identical to the
// original slugifyFunctionPart in domain/agents/toolpool.go so already-synced
// pool keys never change. Do not change this function without a migration plan.
func SlugifyServerName(name string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	b.Grow(len(lower))
	prevUnderscore := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "server"
	}
	return out
}
