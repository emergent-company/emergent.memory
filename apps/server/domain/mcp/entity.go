package mcp

import (
	"context"
	"time"
)

// acpSessionIDKey is the context key for propagating ACP session ID into tool execution.
type acpSessionIDKey struct{}

// RelaySession is a minimal view of a connected MCP relay session.
type RelaySession struct {
	ProjectID  string
	InstanceID string
	Tools      map[string]any // raw tools/list result from the relay
}

// RelayToolProvider is the interface for querying connected MCP relay sessions.
// Implemented by mcprelay.Service — defined here to avoid a circular import.
type RelayToolProvider interface {
	ListByProject(projectID string) []*RelaySession
	CallTool(ctx context.Context, projectID, instanceID, toolName string, args map[string]any) (map[string]any, error)
}

// SessionTitleHandler is the interface for updating ACP session titles.
// Implemented by the agents domain to avoid circular imports.
type SessionTitleHandler interface {
	UpdateACPSessionTitle(ctx context.Context, projectID, sessionID, title string) error
}

// SessionHistoryProvider retrieves the unified timeline for an ACP session.
// Implemented by agents.Repository to avoid a circular import (mcp → agents).
// Returns items as map[string]any so no shared types are needed across the boundary.
type SessionHistoryProvider interface {
	GetConversationFullHistoryRaw(ctx context.Context, acpSessionID string) ([]map[string]any, error)
}

// ContextWithACPSessionID stores the ACP session ID in context.
// Called by the agent executor before running tools so that built-in tools
// like set_session_title can update session metadata.
func ContextWithACPSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, acpSessionIDKey{}, sessionID)
}

// ACPSessionIDFromContext retrieves the ACP session ID from context.
// Returns empty string if not set.
func ACPSessionIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(acpSessionIDKey{}).(string)
	return v
}

// MCPRegistryToolHandler is the interface for executing MCP registry management tools.
// Implemented by the mcpregistry domain to avoid circular imports (mcpregistry → mcp).
type MCPRegistryToolHandler interface {
	ExecuteListMCPServers(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetMCPServer(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteCreateMCPServer(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteUpdateMCPServer(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteDeleteMCPServer(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteToggleMCPServerTool(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteSyncMCPServerTools(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// Official MCP Registry browse/install tools
	ExecuteSearchMCPRegistry(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetMCPRegistryServer(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteInstallMCPFromRegistry(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// Inspect/test-connection tool
	ExecuteInspectMCPServer(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// GetMCPRegistryToolDefinitions returns tool definitions for all MCP registry tools
	GetMCPRegistryToolDefinitions() []ToolDefinition

	// ResolveBuiltinToolSettings resolves the effective enabled/config for a builtin tool
	// using three-tier inheritance: project → org → global.
	// source is one of "project", "org", or "global".
	ResolveBuiltinToolSettings(ctx context.Context, projectID, toolName string) (enabled bool, config map[string]any, source string, err error)

	// ResolveBuiltinToolConfig resolves only the effective config map for a builtin tool.
	ResolveBuiltinToolConfig(ctx context.Context, projectID, toolName string) (config map[string]any, source string, err error)
}

// AgentToolHandler is the interface for executing agent-related MCP tools.
// Implemented by the agents domain to avoid circular imports (agents → mcp).
type AgentToolHandler interface {
	// Agent Definitions
	ExecuteListAgentDefinitions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetAgentDefinition(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteCreateAgentDefinition(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteUpdateAgentDefinition(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteDeleteAgentDefinition(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// Agents (runtime)
	ExecuteListAgents(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetAgent(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteCreateAgent(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteUpdateAgent(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteDeleteAgent(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteTriggerAgent(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// Agent Runs
	ExecuteListAgentRuns(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetAgentRun(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetAgentRunMessages(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetAgentRunToolCalls(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetRunStatus(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// Agent Catalog
	ExecuteListAvailableAgents(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// Agent Questions
	ExecuteListAgentQuestions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteListProjectAgentQuestions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteRespondToAgentQuestion(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// Agent Hooks
	ExecuteListAgentHooks(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteCreateAgentHook(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteDeleteAgentHook(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// ADK Sessions
	ExecuteListADKSessions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteGetADKSession(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// ACP (Agent Communication Protocol)
	ExecuteACPListAgents(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteACPTriggerRun(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteACPGetRunStatus(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)
	ExecuteACPGetRunEvents(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error)

	// GetAgentToolDefinitions returns tool definitions for all agent tools
	GetAgentToolDefinitions() []ToolDefinition

	// GetAgentToolDefinitionsForProject returns tool definitions with the trigger_agent
	// description dynamically enriched with the live agent catalog for the given project.
	// Falls back to GetAgentToolDefinitions when projectID is empty.
	GetAgentToolDefinitionsForProject(ctx context.Context, projectID string) []ToolDefinition
}

// InitializeParams represents the params for initialize method
type InitializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    any        `json:"capabilities"`
	ClientInfo      ClientInfo `json:"clientInfo"`
	ProjectID       string     `json:"project_id,omitempty"` // Optional project context
}

// ClientInfo represents client metadata
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult represents the result of initialize method
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      map[string]string  `json:"serverInfo"`
	ProjectContext  map[string]string  `json:"projectContext,omitempty"`
}

// ServerCapabilities describes what the server supports
type ServerCapabilities struct {
	Tools     ToolsCapability     `json:"tools"`
	Resources ResourcesCapability `json:"resources"`
	Prompts   PromptsCapability   `json:"prompts"`
}

// ToolsCapability describes tool-related capabilities
type ToolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

// ResourcesCapability describes resource-related capabilities
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe"`
	ListChanged bool `json:"listChanged"`
}

// PromptsCapability describes prompt-related capabilities
type PromptsCapability struct {
	ListChanged bool `json:"listChanged"`
}

// ToolsListResult represents the result of tools/list method
type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

// ResourcesListResult represents the result of resources/list method
type ResourcesListResult struct {
	Resources []ResourceDefinition `json:"resources"`
}

// ResourceDefinition describes an MCP resource
type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceReadParams represents params for resources/read method
type ResourceReadParams struct {
	URI string `json:"uri"`
}

// ResourceContents represents the contents of a resource
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// ResourceReadResult represents the result of resources/read method
type ResourceReadResult struct {
	Contents []ResourceContents `json:"contents"`
}

// PromptsListResult represents the result of prompts/list method
type PromptsListResult struct {
	Prompts []PromptDefinition `json:"prompts"`
}

// PromptDefinition describes an MCP prompt template
type PromptDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes a prompt template argument
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptGetParams represents params for prompts/get method
type PromptGetParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// PromptMessage represents a message in a prompt result
type PromptMessage struct {
	Role    string        `json:"role"`
	Content PromptContent `json:"content"`
}

// PromptContent represents the content of a prompt message
type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// PromptGetResult represents the result of prompts/get method
type PromptGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// ToolDefinition describes an MCP tool
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
	// ConfigKeys lists setup-time configuration keys required by this tool
	// (e.g. ["api_key"]). Empty for tools that need no configuration.
	ConfigKeys []string `json:"configKeys,omitempty"`
}

// InputSchema is a JSON schema for tool parameters
type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// PropertySchema describes a single property in a JSON schema
type PropertySchema struct {
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Enum        []string        `json:"enum,omitempty"`
	Minimum     *int            `json:"minimum,omitempty"`
	Maximum     *int            `json:"maximum,omitempty"`
	Default     any             `json:"default,omitempty"`
	Items       *PropertySchema `json:"items,omitempty"` // for type: "array"
}

// ToolsCallParams represents the params for tools/call method
type ToolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolResult represents the result of a tool call (MCP content format)
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock represents a piece of content in tool results
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// EntityType represents an entity type with count
type EntityType struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Count       int    `json:"count"`
}

// RelationshipType represents a relationship type with source/destination type info
type RelationshipType struct {
	Type     string `json:"type"`
	FromType string `json:"from_type"`
	ToType   string `json:"to_type"`
	Count    int    `json:"count"`
}

// EntityTypesResult represents the result of list_entity_types tool
type EntityTypesResult struct {
	ProjectID     string             `json:"projectId"`
	Types         []EntityType       `json:"types"`
	Relationships []RelationshipType `json:"relationships"`
	Total         int                `json:"total"`
}

// Entity represents a graph entity
type Entity struct {
	ID         string         `json:"id"`
	Key        string         `json:"key"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Version    int            `json:"version"`
	Properties map[string]any `json:"properties"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at,omitempty"`
	BranchID   *string        `json:"branch_id,omitempty"`
	BranchName *string        `json:"branch_name,omitempty"`
}

// PaginationInfo contains pagination metadata
type PaginationInfo struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// QueryEntitiesResult represents the result of query_entities tool
type QueryEntitiesResult struct {
	ProjectID  string          `json:"projectId"`
	Entities   []Entity        `json:"entities"`
	Pagination *PaginationInfo `json:"pagination"`
	Warning    string          `json:"warning,omitempty"`
}

// SearchEntitiesResult represents the result of search_entities tool
type SearchEntitiesResult struct {
	ProjectID string   `json:"projectId"`
	Query     string   `json:"query"`
	Entities  []Entity `json:"entities"`
	Count     int      `json:"count"`
}

// SchemaVersionResult represents the result of schema_version tool
type SchemaVersionResult struct {
	Version      string `json:"version"`
	Timestamp    string `json:"timestamp"`
	PackCount    int    `json:"pack_count"`
	CacheHintTTL int    `json:"cache_hint_ttl"`
}

// QueryEntitiesArgs represents arguments for query_entities tool
type QueryEntitiesArgs struct {
	TypeName  string `json:"type_name"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
}

// SearchEntitiesArgs represents arguments for search_entities tool
type SearchEntitiesArgs struct {
	Query    string `json:"query"`
	TypeName string `json:"type_name,omitempty"`
	Limit    int    `json:"limit"`
}

// EdgeInfo represents a relationship edge with connected entity info
type EdgeInfo struct {
	RelationshipID   string          `json:"relationship_id"`
	RelationshipType string          `json:"relationship_type"`
	ConnectedEntity  ConnectedEntity `json:"connected_entity"`
	Properties       map[string]any  `json:"properties,omitempty"`
}

// ConnectedEntity represents basic info about an entity connected via an edge
type ConnectedEntity struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Key        string         `json:"key,omitempty"`
	Name       string         `json:"name,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// GetEntityEdgesResult represents the result of get_entity_edges tool
type GetEntityEdgesResult struct {
	EntityID string     `json:"entity_id"`
	Incoming []EdgeInfo `json:"incoming"`
	Outgoing []EdgeInfo `json:"outgoing"`
}

// ============================================================================
// Schema DTOs
// ============================================================================

// MemorySchema represents a memory schema from the global registry
type MemorySchema struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Version                 string         `json:"version"`
	Description             string         `json:"description,omitempty"`
	Author                  string         `json:"author,omitempty"`
	Source                  string         `json:"source,omitempty"`
	License                 string         `json:"license,omitempty"`
	RepositoryURL           string         `json:"repository_url,omitempty"`
	DocumentationURL        string         `json:"documentation_url,omitempty"`
	ObjectTypeSchemas       map[string]any `json:"object_type_schemas"`
	RelationshipTypeSchemas map[string]any `json:"relationship_type_schemas,omitempty"`
	UIConfigs               map[string]any `json:"ui_configs,omitempty"`
	ExtractionPrompts       map[string]any `json:"extraction_prompts,omitempty"`
	SQLViews                []any          `json:"sql_views,omitempty"`
	Checksum                string         `json:"checksum,omitempty"`
	Draft                   bool           `json:"draft"`
	PublishedAt             string         `json:"published_at"`
	DeprecatedAt            string         `json:"deprecated_at,omitempty"`
	CreatedAt               string         `json:"created_at"`
	UpdatedAt               string         `json:"updated_at"`
}

// MemorySchemaSummary represents a summary of a memory schema (for listings)
type MemorySchemaSummary struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description,omitempty"`
	Author       string   `json:"author,omitempty"`
	Source       string   `json:"source,omitempty"`
	ObjectTypes  []string `json:"object_types"`
	TypeCount    int      `json:"type_count"`
	PublishedAt  string   `json:"published_at"`
	DeprecatedAt string   `json:"deprecated_at,omitempty"`
}

// ListSchemasResult represents the result of list_schemas tool
type ListSchemasResult struct {
	Packs   []MemorySchemaSummary `json:"packs"`
	Total   int                   `json:"total"`
	Page    int                   `json:"page"`
	Limit   int                   `json:"limit"`
	HasMore bool                  `json:"has_more"`
}

// GetSchemaResult represents the result of get_schema tool
type GetSchemaResult struct {
	Pack *MemorySchema `json:"pack"`
}

// ObjectTypeInfo represents info about an object type in a template
type ObjectTypeInfo struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	SampleCount int    `json:"sample_count"`
}

// AvailableTemplate represents a template available for a project
type AvailableTemplate struct {
	ID                string           `json:"id"`
	Name              string           `json:"name"`
	Version           string           `json:"version"`
	Description       string           `json:"description,omitempty"`
	Author            string           `json:"author,omitempty"`
	Source            string           `json:"source,omitempty"`
	ObjectTypes       []ObjectTypeInfo `json:"object_types"`
	RelationshipTypes []string         `json:"relationship_types,omitempty"`
	RelationshipCount int              `json:"relationship_count"`
	Installed         bool             `json:"installed"`
	Active            bool             `json:"active,omitempty"`
	AssignmentID      string           `json:"assignment_id,omitempty"`
	PublishedAt       string           `json:"published_at"`
}

// GetAvailableTemplatesResult represents the result of get_available_templates tool
type GetAvailableTemplatesResult struct {
	ProjectID string              `json:"project_id"`
	Templates []AvailableTemplate `json:"templates"`
	Total     int                 `json:"total"`
}

// InstalledTemplate represents an installed memory schema
type InstalledTemplate struct {
	AssignmentID   string           `json:"assignment_id"`
	SchemaID       string           `json:"schema_id"`
	Name           string           `json:"name"`
	Version        string           `json:"version"`
	Description    string           `json:"description,omitempty"`
	ObjectTypes    []ObjectTypeInfo `json:"object_types"`
	Active         bool             `json:"active"`
	InstalledAt    string           `json:"installed_at"`
	Customizations map[string]any   `json:"customizations,omitempty"`
}

// GetInstalledTemplatesResult represents the result of get_installed_templates tool
type GetInstalledTemplatesResult struct {
	ProjectID string              `json:"project_id"`
	Templates []InstalledTemplate `json:"templates"`
	Total     int                 `json:"total"`
}

// AssignSchemaResult represents the result of assign_schema tool
type AssignSchemaResult struct {
	Success        bool           `json:"success"`
	AssignmentID   string         `json:"assignment_id"`
	InstalledTypes []string       `json:"installed_types"`
	DisabledTypes  []string       `json:"disabled_types,omitempty"`
	Conflicts      []TypeConflict `json:"conflicts,omitempty"`
}

// TypeConflict represents a type installation conflict
type TypeConflict struct {
	Type       string `json:"type"`
	Issue      string `json:"issue"`
	Resolution string `json:"resolution"`
}

// UpdateTemplateAssignmentResult represents the result of update_template_assignment tool
type UpdateTemplateAssignmentResult struct {
	Success      bool   `json:"success"`
	AssignmentID string `json:"assignment_id"`
	Active       bool   `json:"active"`
	Message      string `json:"message"`
}

// UninstallSchemaResult represents the result of uninstall_schema tool
type UninstallSchemaResult struct {
	Success      bool   `json:"success"`
	AssignmentID string `json:"assignment_id"`
	Message      string `json:"message"`
}

// CreateSchemaResult represents the result of create_schema tool
type CreateSchemaResult struct {
	Success bool          `json:"success"`
	Pack    *MemorySchema `json:"pack"`
	Message string        `json:"message"`
}

// DeleteSchemaResult represents the result of delete_schema tool
type DeleteSchemaResult struct {
	Success  bool   `json:"success"`
	SchemaID string `json:"schema_id"`
	Message  string `json:"message"`
}

// ============================================================================
// Entity CRUD DTOs
// ============================================================================

// CreateEntityResult represents the result of create_entity tool
type CreateEntityResult struct {
	Success bool           `json:"success"`
	Entity  *CreatedEntity `json:"entity"`
	Message string         `json:"message"`
}

// CreatedEntity represents a newly created entity
type CreatedEntity struct {
	ID          string         `json:"id"`
	CanonicalID string         `json:"canonical_id"`
	Type        string         `json:"type"`
	Key         string         `json:"key,omitempty"`
	Status      string         `json:"status,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	Version     int            `json:"version"`
	CreatedAt   string         `json:"created_at"`
}

// CreateRelationshipResult represents the result of create_relationship tool
type CreateRelationshipResult struct {
	Success      bool                 `json:"success"`
	Relationship *CreatedRelationship `json:"relationship"`
	Message      string               `json:"message"`
}

// CreatedRelationship represents a newly created relationship
type CreatedRelationship struct {
	ID          string         `json:"id"`
	CanonicalID string         `json:"canonical_id"`
	Type        string         `json:"type"`
	SourceID    string         `json:"source_id"`
	TargetID    string         `json:"target_id"`
	Properties  map[string]any `json:"properties,omitempty"`
	Weight      float64        `json:"weight,omitempty"`
	Version     int            `json:"version"`
	CreatedAt   string         `json:"created_at"`
}

// UpdateEntityResult represents the result of update_entity tool
type UpdateEntityResult struct {
	Success bool           `json:"success"`
	Entity  *CreatedEntity `json:"entity"`
	Message string         `json:"message"`
}

// DeleteEntityResult represents the result of delete_entity tool
type DeleteEntityResult struct {
	Success  bool   `json:"success"`
	EntityID string `json:"entity_id"`
	Message  string `json:"message"`
}
