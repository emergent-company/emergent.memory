// Package toolgroups defines the server-owned capability taxonomy that maps an
// agent-facing tool name to a stable tool group slug. It is the single source of
// truth for group membership, reused by group policy resolution
// (AgentDefinition.effectiveToolPolicy) and by the agent definition read DTO
// (the computed toolGroups payload rendered by the gateway).
package toolgroups

import (
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
)

// Stable group slugs (the frozen D6 contract). Group ids are never display
// labels; a label change must not detach a stored policy from its tools.
const (
	GroupSearch        = "search"
	GroupGraphRead     = "graph-read"
	GroupGraphWrite    = "graph-write"
	GroupSchemaRead    = "schema-read"
	GroupSchemaWrite   = "schema-write"
	GroupSchemaMigrate = "schema-migrate"
	GroupBranches      = "branches"
	GroupJournal       = "journal"
	GroupDocuments     = "documents"
	GroupSkills        = "skills"
	GroupAgents        = "agents"
	GroupProjects      = "projects"
	GroupChat          = "chat"
	GroupAdmin         = "admin"
	GroupWorkspaceRead = "workspace-read"
	GroupWorkspaceExec = "workspace-exec"
	GroupWeb           = "web"
	GroupOther         = "other"
)

// Group describes one tool group: a stable id, a display label, and a short
// human-readable description rendered by the gateway.
type Group struct {
	ID          string
	Label       string
	Description string
}

// Groups is the ordered catalog of tool groups in the frozen contract order
// (design.md D6). Order is part of the contract: the gateway renders groups in
// this order.
var Groups = []Group{
	{ID: GroupSearch, Label: "Search", Description: "Search the knowledge graph semantically and by keyword."},
	{ID: GroupGraphRead, Label: "Graph · Read", Description: "Read, search, and traverse graph objects."},
	{ID: GroupGraphWrite, Label: "Graph · Write", Description: "Create, update, or delete graph objects."},
	{ID: GroupSchemaRead, Label: "Schema · Read", Description: "Read schemas and entity type definitions."},
	{ID: GroupSchemaWrite, Label: "Schema · Write", Description: "Create, update, or delete schemas and entity types."},
	{ID: GroupSchemaMigrate, Label: "Schema · Migrate", Description: "Preview, execute, and roll back schema migrations."},
	{ID: GroupBranches, Label: "Branches", Description: "List, create, merge, and delete graph branches."},
	{ID: GroupJournal, Label: "Journal", Description: "Read and write journal entries."},
	{ID: GroupDocuments, Label: "Documents", Description: "Read and write project documents."},
	{ID: GroupSkills, Label: "Skills", Description: "Read and manage skills."},
	{ID: GroupAgents, Label: "Agents", Description: "Read and manage agents and agent definitions."},
	{ID: GroupProjects, Label: "Projects", Description: "Read and manage project resources."},
	{ID: GroupChat, Label: "Chat", Description: "Read and administer chat sessions."},
	{ID: GroupAdmin, Label: "Admin", Description: "Administrative operations requiring elevated scope."},
	{ID: GroupWorkspaceRead, Label: "Workspace · Read", Description: "Read files and search the workspace."},
	{ID: GroupWorkspaceExec, Label: "Workspace · Execute", Description: "Run commands and write files in the workspace."},
	{ID: GroupWeb, Label: "Web", Description: "Web search and fetch."},
	{ID: GroupOther, Label: "Other", Description: "Tools without a specific capability group."},
}

// scopeToGroup maps an MCP required scope to a group id. It covers the full
// scope taxonomy so any scope resolvable via mcp.LookupToolScope lands in a
// known group. Umbrella token scopes (data:read/data:write) and account:read
// are intentionally absent: they are never a tool's RequiredScope, and an
// unmapped scope resolves to GroupOther.
var scopeToGroup = map[string]string{
	"search":          GroupSearch,
	"graph:read":      GroupGraphRead,
	"graph:write":     GroupGraphWrite,
	"schema:read":     GroupSchemaRead,
	"schema:write":    GroupSchemaWrite,
	"schema:migrate":  GroupSchemaMigrate,
	"branches:read":   GroupBranches,
	"branches:write":  GroupBranches,
	"journal:read":    GroupJournal,
	"journal:write":   GroupJournal,
	"documents:read":  GroupDocuments,
	"documents:write": GroupDocuments,
	"skills:read":     GroupSkills,
	"skills:write":    GroupSkills,
	"agents:read":     GroupAgents,
	"agents:write":    GroupAgents,
	"projects:read":   GroupProjects,
	"projects:write":  GroupProjects,
	"chat:use":        GroupChat,
	"chat:admin":      GroupChat,
	"admin":           GroupAdmin,
	"admin:all":       GroupAdmin,
	"admin:write":     GroupAdmin,
}

// staticToolGroup maps unscoped, agent-facing tool names to a group. These use
// the LLM-facing names from domain/agents/workspace_tools.go (workspace_bash,
// workspace_read, …) and the web MCP tool names — NOT sandbox.ValidToolNames,
// which are config keys for the sandbox allowlist.
//
// The Google-native tools (google_search, url_context, code_execution) are
// intentionally absent: they are configured under Model.NativeTools and injected
// directly into genConfig.Tools in executor.go, bypassing the tool-policy
// callback, so group policy cannot govern them. They never belong to the web
// group (or any group).
var staticToolGroup = map[string]string{
	"workspace_read":     GroupWorkspaceRead,
	"workspace_glob":     GroupWorkspaceRead,
	"workspace_grep":     GroupWorkspaceRead,
	"workspace_ast_grep": GroupWorkspaceRead,
	"workspace_write":    GroupWorkspaceExec,
	"workspace_edit":     GroupWorkspaceExec,
	"workspace_git":      GroupWorkspaceExec,
	"workspace_bash":     GroupWorkspaceExec,
	"run_python":         GroupWorkspaceExec,
	"run_go":             GroupWorkspaceExec,

	"web-fetch":         GroupWeb,
	"web-search-brave":  GroupWeb,
	"web-search-reddit": GroupWeb,
}

// GroupForScope returns the group id for a tool given its required scope (the
// authoritative source when the caller holds the tool catalog). A non-empty
// scope is mapped through the single scope→group table; an empty scope
// (unscoped tool) falls back to the static tool-name map and then GroupOther.
//
// toolName is only consulted for unscoped tools, so a scope that carries no
// group mapping resolves to GroupOther regardless of name.
func GroupForScope(scope, toolName string) string {
	if scope != "" {
		if g, ok := scopeToGroup[scope]; ok {
			return g
		}
		return GroupOther
	}
	return groupForUnscopedTool(toolName)
}

// GroupForTool returns the group id for a tool name when no catalog is
// available (the no-catalog fallback). It resolves in order:
//
//  1. the tool's required scope via mcp.LookupToolScope (static core tools only);
//  2. the static mapping for unscoped workspace / web tools;
//  3. GroupOther for dynamic tools, external MCP tools, relay tools, and
//     anything unmatched.
//
// Callers that hold the tool catalog (mcp.Service.GetToolDefinitions) should
// prefer GroupForScope with the catalog's per-tool RequiredScope, which also
// covers dynamic tools whose scope is set at definition time.
func GroupForTool(tool string) string {
	if tool == "" {
		return GroupOther
	}
	if scope, ok := mcp.LookupToolScope(tool); ok {
		return GroupForScope(scope, tool)
	}
	return groupForUnscopedTool(tool)
}

// groupForUnscopedTool resolves a tool with no required scope: the static
// workspace / web map, then GroupOther for external/relay names and anything
// unmatched. GroupOther is display/membership only — it is never a group-policy
// source (see AgentDefinition.effectiveToolPolicyFor), so an external or relay
// tool falls through to the default policy regardless of any @group:other entry.
func groupForUnscopedTool(tool string) string {
	if g, ok := staticToolGroup[tool]; ok {
		return g
	}
	// External MCP and relay tool names are prefixed and deferred (proposal
	// non-goal): they carry no group policy and belong to GroupOther.
	if mcpregistry.IsExternalTool(tool) {
		return GroupOther
	}
	return GroupOther
}
