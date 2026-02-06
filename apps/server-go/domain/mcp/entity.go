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
	Tools ToolsCapability `json:"tools"`
}

// ToolsCapability describes tool-related capabilities
type ToolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

// ToolsListResult represents the result of tools/list method
type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
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
