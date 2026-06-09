package agentcompat

import "strings"

// InternalToolPrefix is prepended to every built-in Memory tool when exposing
// them through the OpenAI-compatible endpoint.  This creates a reserved
// namespace so client-supplied tools can never shadow internal ones.
const InternalToolPrefix = "memory_"

// internalToolNames is the canonical set of built-in Memory MCP tool names.
// When a request contains tools[], any name that collides with the exposed
// (prefixed) form is rejected as a conflict.
var internalToolNames = map[string]struct{}{
	"search-knowledge":       {},
	"entity-create":          {},
	"entity-get":             {},
	"entity-query":           {},
	"entity-search":          {},
	"entity-update":          {},
	"entity-delete":          {},
	"entity-edges-get":       {},
	"entity-type-list":       {},
	"relationship-create":    {},
	"relationship-delete":    {},
	"relationship-get":       {},
	"relationship-query":     {},
	"relationship-update":    {},
	"search-hybrid":          {},
	"vector-search":          {},
	"fts-search":             {},
	"document-get":           {},
	"document-list":          {},
	"document-upload":        {},
	"branch-list":            {},
	"branch-create":          {},
	"spawn_agents":           {},
	"list_available_agents":  {},
	"trigger-agent":          {},
	"agent-run-get":          {},
	"list-agent-definitions": {},
	"get-agent-definition":   {},
	"list-agents":            {},
	"set_session_title":      {},
	"mcp-server-list":        {},
	"mcp-server-get":         {},
	"search_mcp_registry":    {},
	"schema-list":            {},
	"schema-get":             {},
	"schema-create":          {},
	"schema-update":          {},
	"schema-delete":          {},
	"journal-list":           {},
	"skill-list":             {},
	"skill-get":              {},
	"provider-list":          {},
	"traces-list":            {},
}

// ExposedName returns the external name used in the OpenAI API for an internal
// tool.  "search-knowledge" → "memory_search-knowledge".
func ExposedName(internalName string) string {
	return InternalToolPrefix + internalName
}

// InternalName strips the prefix from an exposed tool name, returning the
// bare internal name.  Returns ("", false) if the name is not prefixed.
func InternalName(exposedName string) (string, bool) {
	after, ok := strings.CutPrefix(exposedName, InternalToolPrefix)
	return after, ok
}

// IsInternalTool reports whether name (after stripping the prefix) is a known
// built-in tool.  Accepts both the bare and prefixed form.
func IsInternalTool(name string) bool {
	bare, stripped := strings.CutPrefix(name, InternalToolPrefix)
	if stripped {
		_, ok := internalToolNames[bare]
		return ok
	}
	_, ok := internalToolNames[name]
	return ok
}

// IsClientTool reports whether the tool name refers to a client-supplied tool
// (i.e. it is NOT a known internal tool in either bare or prefixed form).
func IsClientTool(name string, clientTools []ClientToolDef) bool {
	for _, t := range clientTools {
		if t.Function.Name == name {
			return true
		}
	}
	return false
}

// ValidateClientTools returns an error string if any client tool name conflicts
// with the reserved "memory_" namespace (exact matches to exposed internal names).
// Returns "" when all names are clean.
func ValidateClientTools(tools []ClientToolDef) string {
	for _, t := range tools {
		if strings.HasPrefix(t.Function.Name, InternalToolPrefix) {
			return "tool name '" + t.Function.Name + "' uses the reserved prefix 'memory_'; choose a different name"
		}
	}
	return ""
}

// SystemPromptAppendix returns a short instruction block appended to the agent's
// system prompt that describes the memory_ prefix convention when client tools
// are present.
func SystemPromptAppendix(hasClientTools bool) string {
	if !hasClientTools {
		return ""
	}
	return `
## Tool naming convention
Built-in Memory tools are available under the "memory_" prefix (e.g. memory_search-knowledge, memory_entity-create).
Additional tools provided by the caller have plain names without any prefix.
Use memory_* tools to read/write the knowledge graph.
Use caller-provided tools when you need information or actions outside the graph.`
}
