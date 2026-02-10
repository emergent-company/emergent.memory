package mcp

import "time"

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
}

// InputSchema is a JSON schema for tool parameters
type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required"`
}

// PropertySchema describes a single property in a JSON schema
type PropertySchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Minimum     *int     `json:"minimum,omitempty"`
	Maximum     *int     `json:"maximum,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// ToolsCallParams represents the params for tools/call method
type ToolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolResult represents the result of a tool call (MCP content format)
type ToolResult struct {
	Content []ContentBlock `json:"content"`
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
	Properties map[string]any `json:"properties"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at,omitempty"`
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
// Template Pack DTOs
// ============================================================================

// TemplatePack represents a template pack from the global registry
type TemplatePack struct {
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

// TemplatePackSummary represents a summary of a template pack (for listings)
type TemplatePackSummary struct {
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

// ListTemplatePacksResult represents the result of list_template_packs tool
type ListTemplatePacksResult struct {
	Packs   []TemplatePackSummary `json:"packs"`
	Total   int                   `json:"total"`
	Page    int                   `json:"page"`
	Limit   int                   `json:"limit"`
	HasMore bool                  `json:"has_more"`
}

// GetTemplatePackResult represents the result of get_template_pack tool
type GetTemplatePackResult struct {
	Pack *TemplatePack `json:"pack"`
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

// InstalledTemplate represents an installed template pack
type InstalledTemplate struct {
	AssignmentID   string           `json:"assignment_id"`
	TemplatePackID string           `json:"template_pack_id"`
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

// AssignTemplatePackResult represents the result of assign_template_pack tool
type AssignTemplatePackResult struct {
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

// UninstallTemplatePackResult represents the result of uninstall_template_pack tool
type UninstallTemplatePackResult struct {
	Success      bool   `json:"success"`
	AssignmentID string `json:"assignment_id"`
	Message      string `json:"message"`
}

// CreateTemplatePackResult represents the result of create_template_pack tool
type CreateTemplatePackResult struct {
	Success bool          `json:"success"`
	Pack    *TemplatePack `json:"pack"`
	Message string        `json:"message"`
}

// DeleteTemplatePackResult represents the result of delete_template_pack tool
type DeleteTemplatePackResult struct {
	Success bool   `json:"success"`
	PackID  string `json:"pack_id"`
	Message string `json:"message"`
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
