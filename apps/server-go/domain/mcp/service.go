package mcp

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/domain/graph"
	"github.com/emergent-company/emergent/domain/search"
	"github.com/emergent-company/emergent/internal/database"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Service handles MCP business logic and tool execution
type Service struct {
	db           bun.IDB
	graphService *graph.Service
	searchSvc    *search.Service
	log          *slog.Logger

	// Agent tool handler (injected to break import cycle)
	agentToolHandler AgentToolHandler

	// MCP registry tool handler (injected to break import cycle)
	mcpRegistryToolHandler MCPRegistryToolHandler

	// Schema version caching
	cacheMu       sync.RWMutex
	cachedVersion string
	cacheExpiry   time.Time
}

// NewService creates a new MCP service
func NewService(db bun.IDB, graphService *graph.Service, searchSvc *search.Service, log *slog.Logger) *Service {
	return &Service{
		db:           db,
		graphService: graphService,
		searchSvc:    searchSvc,
		log:          log.With(logger.Scope("mcp.svc")),
	}
}

// SetAgentToolHandler sets the agent tool handler (called after construction to break circular init)
func (s *Service) SetAgentToolHandler(h AgentToolHandler) {
	s.agentToolHandler = h
}

// SetMCPRegistryToolHandler sets the MCP registry tool handler (called after construction to break circular init)
func (s *Service) SetMCPRegistryToolHandler(h MCPRegistryToolHandler) {
	s.mcpRegistryToolHandler = h
}

// GetToolDefinitions returns all available MCP tools
func (s *Service) GetToolDefinitions() []ToolDefinition {
	tools := []ToolDefinition{
		{
			Name:        "schema_version",
			Description: "Get the current schema version and metadata. Returns version hash, timestamp, total types, and relationships.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "list_entity_types",
			Description: "List all available entity types in the knowledge graph with instance counts. Helps discover what entities can be queried.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "query_entities",
			Description: "Query entity instances by type with pagination and filtering. Returns actual entity data from the knowledge graph.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"type_name": {
						Type:        "string",
						Description: "Entity type to query (e.g., \"Decision\", \"Project\", \"Document\")",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 10, max: 50)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(50),
						Default:     10,
					},
					"offset": {
						Type:        "number",
						Description: "Pagination offset for results (default: 0)",
						Minimum:     intPtr(0),
						Default:     0,
					},
					"sort_by": {
						Type:        "string",
						Description: "Field to sort by (default: \"created_at\")",
						Enum:        []string{"created_at", "updated_at", "name"},
						Default:     "created_at",
					},
					"sort_order": {
						Type:        "string",
						Description: "Sort direction (default: \"desc\")",
						Enum:        []string{"asc", "desc"},
						Default:     "desc",
					},
				},
				Required: []string{"type_name"},
			},
		},
		{
			Name:        "search_entities",
			Description: "Search entities by text query across name, key, and description fields.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"query": {
						Type:        "string",
						Description: "Search query text",
					},
					"type_name": {
						Type:        "string",
						Description: "Optional entity type filter",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 10, max: 50)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(50),
						Default:     10,
					},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "get_entity_edges",
			Description: "Get all relationships (edges) for an entity. Returns incoming and outgoing relationships with connected entity information. Use this to traverse the graph and discover how entities are connected.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"entity_id": {
						Type:        "string",
						Description: "The UUID of the entity to get edges for",
					},
				},
				Required: []string{"entity_id"},
			},
		},
		{
			Name:        "list_template_packs",
			Description: "List all available template packs in the global registry. Template packs define object schemas, relationships, and extraction prompts for knowledge graph entities.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"search": {
						Type:        "string",
						Description: "Optional search term to filter packs by name or description",
					},
					"include_deprecated": {
						Type:        "boolean",
						Description: "Include deprecated template packs (default: false)",
						Default:     false,
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 20, max: 100)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(100),
						Default:     20,
					},
					"page": {
						Type:        "number",
						Description: "Page number for pagination (default: 1)",
						Minimum:     intPtr(1),
						Default:     1,
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "get_template_pack",
			Description: "Get detailed information about a specific template pack including all schemas, UI configs, and extraction prompts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"pack_id": {
						Type:        "string",
						Description: "The UUID of the template pack to retrieve",
					},
				},
				Required: []string{"pack_id"},
			},
		},
		{
			Name:        "get_available_templates",
			Description: "Get all template packs available for a project with their installation status. Shows which packs are installed, active, and their object type counts.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "get_installed_templates",
			Description: "Get all template packs currently installed in the project with their configuration and active status.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "assign_template_pack",
			Description: "Install a template pack to the project. This registers the pack's object types in the project's type registry, making them available for entity creation and extraction.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"template_pack_id": {
						Type:        "string",
						Description: "The UUID of the template pack to install",
					},
					"enabled_types": {
						Type:        "array",
						Description: "Optional list of specific type names to enable (default: all types)",
					},
					"disabled_types": {
						Type:        "array",
						Description: "Optional list of specific type names to disable",
					},
				},
				Required: []string{"template_pack_id"},
			},
		},
		{
			Name:        "update_template_assignment",
			Description: "Update a template pack assignment. Toggle active status or modify customizations.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"assignment_id": {
						Type:        "string",
						Description: "The UUID of the template pack assignment to update",
					},
					"active": {
						Type:        "boolean",
						Description: "Set the active status of the template pack",
					},
				},
				Required: []string{"assignment_id"},
			},
		},
		{
			Name:        "uninstall_template_pack",
			Description: "Remove a template pack from the project. This will fail if any objects still exist using types from this pack.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"assignment_id": {
						Type:        "string",
						Description: "The UUID of the template pack assignment to remove",
					},
				},
				Required: []string{"assignment_id"},
			},
		},
		{
			Name:        "create_template_pack",
			Description: "Create a new template pack in the global registry. Requires object type schemas at minimum.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Name of the template pack",
					},
					"version": {
						Type:        "string",
						Description: "Version string (e.g., \"1.0.0\")",
					},
					"description": {
						Type:        "string",
						Description: "Description of the template pack",
					},
					"author": {
						Type:        "string",
						Description: "Author name or organization",
					},
					"object_type_schemas": {
						Type:        "object",
						Description: "Object type schemas as a JSON object mapping type names to JSON Schema definitions",
					},
					"relationship_type_schemas": {
						Type:        "object",
						Description: "Optional relationship type schemas",
					},
					"ui_configs": {
						Type:        "object",
						Description: "Optional UI configuration per type",
					},
					"extraction_prompts": {
						Type:        "object",
						Description: "Optional extraction prompts per type",
					},
				},
				Required: []string{"name", "version", "object_type_schemas"},
			},
		},
		{
			Name:        "delete_template_pack",
			Description: "Delete a template pack from the global registry. Cannot delete system packs or packs that are currently installed in any project.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"pack_id": {
						Type:        "string",
						Description: "The UUID of the template pack to delete",
					},
				},
				Required: []string{"pack_id"},
			},
		},
		{
			Name:        "create_entity",
			Description: "Create a new entity (graph object) in the project. The entity type should match a type defined in an installed template pack.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"type": {
						Type:        "string",
						Description: "Entity type (e.g., \"Person\", \"Company\")",
					},
					"properties": {
						Type:        "object",
						Description: "Entity properties as key-value pairs matching the type schema",
					},
					"key": {
						Type:        "string",
						Description: "Optional unique key/identifier for the entity",
					},
					"status": {
						Type:        "string",
						Description: "Optional status (e.g., \"draft\", \"published\")",
					},
					"labels": {
						Type:        "array",
						Description: "Optional labels/tags for the entity",
					},
				},
				Required: []string{"type"},
			},
		},
		{
			Name:        "create_relationship",
			Description: "Create a relationship between two entities. The relationship type should match a type defined in an installed template pack.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"type": {
						Type:        "string",
						Description: "Relationship type (e.g., \"WORKS_AT\", \"KNOWS\")",
					},
					"source_id": {
						Type:        "string",
						Description: "UUID of the source entity",
					},
					"target_id": {
						Type:        "string",
						Description: "UUID of the target entity",
					},
					"properties": {
						Type:        "object",
						Description: "Optional relationship properties",
					},
					"weight": {
						Type:        "number",
						Description: "Optional relationship weight (default: 1.0)",
					},
				},
				Required: []string{"type", "source_id", "target_id"},
			},
		},
		{
			Name:        "update_entity",
			Description: "Update an existing entity by creating a new version. Properties are merged with existing values (null removes a property).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"entity_id": {
						Type:        "string",
						Description: "UUID of the entity to update",
					},
					"properties": {
						Type:        "object",
						Description: "Properties to update (merged with existing, null removes)",
					},
					"status": {
						Type:        "string",
						Description: "Optional new status",
					},
					"labels": {
						Type:        "array",
						Description: "Optional labels to add",
					},
					"replace_labels": {
						Type:        "boolean",
						Description: "If true, replace all labels instead of merging (default: false)",
					},
				},
				Required: []string{"entity_id"},
			},
		},
		{
			Name:        "delete_entity",
			Description: "Soft-delete an entity. The entity can be restored later.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"entity_id": {
						Type:        "string",
						Description: "UUID of the entity to delete",
					},
				},
				Required: []string{"entity_id"},
			},
		},
		{
			Name:        "restore_entity",
			Description: "Restore a soft-deleted entity.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"entity_id": {
						Type:        "string",
						Description: "UUID of the entity to restore",
					},
				},
				Required: []string{"entity_id"},
			},
		},
		{
			Name:        "hybrid_search",
			Description: "Advanced search combining full-text, semantic similarity, and graph context. Most powerful search option for AI agents.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"query": {
						Type:        "string",
						Description: "Search query text",
					},
					"types": {
						Type:        "array",
						Description: "Optional entity type filters (e.g., [\"Decision\", \"Project\"])",
					},
					"labels": {
						Type:        "array",
						Description: "Optional label filters",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 20, max: 100)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(100),
						Default:     20,
					},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "semantic_search",
			Description: "Search entities by semantic meaning using vector embeddings. Finds conceptually similar entities even with different wording.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"query": {
						Type:        "string",
						Description: "Natural language query describing what you're looking for",
					},
					"types": {
						Type:        "array",
						Description: "Optional entity type filters",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 20, max: 50)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(50),
						Default:     20,
					},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "find_similar",
			Description: "Find entities similar to a given entity based on semantic similarity.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"entity_id": {
						Type:        "string",
						Description: "UUID of the entity to find similar entities for",
					},
					"types": {
						Type:        "array",
						Description: "Optional entity type filters for results",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 10, max: 50)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(50),
						Default:     10,
					},
				},
				Required: []string{"entity_id"},
			},
		},
		{
			Name:        "traverse_graph",
			Description: "Multi-hop graph traversal starting from an entity. Discover non-obvious connections and relationships.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"start_entity_id": {
						Type:        "string",
						Description: "UUID of the starting entity",
					},
					"max_depth": {
						Type:        "number",
						Description: "Maximum traversal depth (default: 2, max: 5)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(5),
						Default:     2,
					},
					"relationship_types": {
						Type:        "array",
						Description: "Optional filter by specific relationship types",
					},
					"direction": {
						Type:        "string",
						Description: "Traversal direction: outgoing, incoming, or both (default: both)",
						Enum:        []string{"outgoing", "incoming", "both"},
						Default:     "both",
					},
					"query_context": {
						Type:        "string",
						Description: "Optional search query to prioritize edges by relevance during traversal. When provided, edges at each BFS level are sorted by semantic similarity to this query.",
					},
				},
				Required: []string{"start_entity_id"},
			},
		},
		{
			Name:        "list_relationships",
			Description: "Query relationships with optional filters. Returns paginated list of relationships in the knowledge graph.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"type": {
						Type:        "string",
						Description: "Optional filter by relationship type",
					},
					"source_id": {
						Type:        "string",
						Description: "Optional filter by source entity UUID",
					},
					"target_id": {
						Type:        "string",
						Description: "Optional filter by target entity UUID",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 50, max: 100)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(100),
						Default:     50,
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "update_relationship",
			Description: "Update an existing relationship's properties or weight.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"relationship_id": {
						Type:        "string",
						Description: "UUID of the relationship to update",
					},
					"properties": {
						Type:        "object",
						Description: "Properties to update (merged with existing)",
					},
					"weight": {
						Type:        "number",
						Description: "Optional new weight for the relationship",
					},
				},
				Required: []string{"relationship_id"},
			},
		},
		{
			Name:        "delete_relationship",
			Description: "Soft-delete a relationship between two entities.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"relationship_id": {
						Type:        "string",
						Description: "UUID of the relationship to delete",
					},
				},
				Required: []string{"relationship_id"},
			},
		},
		{
			Name:        "list_tags",
			Description: "Get all unique tags/labels used in the project with counts.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "batch_create_entities",
			Description: "Create multiple entities in a single request. Much more efficient than individual creates.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"entities": {
						Type:        "array",
						Description: "Array of entity specifications to create",
					},
				},
				Required: []string{"entities"},
			},
		},
		{
			Name:        "batch_create_relationships",
			Description: "Create multiple relationships in a single request. Efficient for building graph structures.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"relationships": {
						Type:        "array",
						Description: "Array of relationship specifications to create",
					},
				},
				Required: []string{"relationships"},
			},
		},
		{
			Name:        "preview_schema_migration",
			Description: "SAFE READ-ONLY: Preview what would happen if objects are migrated from one schema version to another. Shows risk assessment (safe/cautious/risky/dangerous), fields that would be dropped, type coercions, and validation errors. NO CHANGES ARE MADE. Use this before actual migration to understand impact. If dangerous, recommend user to use CLI: ./bin/migrate-schema -project <uuid> -from <old> -to <new>",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"from_version": {
						Type:        "string",
						Description: "Current schema version (e.g., '1.0.0')",
					},
					"to_version": {
						Type:        "string",
						Description: "Target schema version (e.g., '2.0.0')",
					},
					"sample_size": {
						Type:        "number",
						Description: "Number of objects to analyze (default: 10, max: 50)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(50),
						Default:     10,
					},
				},
				Required: []string{"from_version", "to_version"},
			},
		},
		{
			Name:        "list_migration_archives",
			Description: "SAFE READ-ONLY: List objects that have migration archives (dropped fields from previous migrations). Shows which objects have recoverable data and what fields were dropped. Use this to understand what data can be restored via rollback.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"limit": {
						Type:        "number",
						Description: "Maximum number of objects to return (default: 20, max: 100)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(100),
						Default:     20,
					},
					"offset": {
						Type:        "number",
						Description: "Pagination offset (default: 0)",
						Minimum:     intPtr(0),
						Default:     0,
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "get_migration_archive",
			Description: "SAFE READ-ONLY: Get detailed migration archive for a specific object. Shows complete history of dropped fields across all migrations, with timestamps and versions. Use this to see exactly what data would be restored if you rollback. For actual rollback, recommend user to use CLI: ./bin/migrate-schema -project <uuid> --rollback --rollback-version <version> -dry-run=false",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"object_id": {
						Type:        "string",
						Description: "UUID of the object to get archive for",
					},
				},
				Required: []string{"object_id"},
			},
		},
	}

	// Append agent tool definitions if handler is available
	if s.agentToolHandler != nil {
		tools = append(tools, s.agentToolHandler.GetAgentToolDefinitions()...)
	}

	// Append MCP registry tool definitions if handler is available
	if s.mcpRegistryToolHandler != nil {
		tools = append(tools, s.mcpRegistryToolHandler.GetMCPRegistryToolDefinitions()...)
	}

	return tools
}

func (s *Service) GetResourceDefinitions() []ResourceDefinition {
	return []ResourceDefinition{
		{
			URI:         "emergent://schema/entity-types",
			Name:        "Entity Type Schema",
			Description: "Complete catalog of all available entity types in the knowledge graph with their counts and relationship types",
			MimeType:    "application/json",
		},
		{
			URI:         "emergent://schema/relationships",
			Name:        "Relationship Types Registry",
			Description: "All valid relationship types, their constraints, and usage statistics",
			MimeType:    "application/json",
		},
		{
			URI:         "emergent://templates/catalog",
			Name:        "Template Pack Catalog",
			Description: "Available template packs with descriptions, object types, and metadata",
			MimeType:    "application/json",
		},
		{
			URI:         "emergent://project/{project_id}/metadata",
			Name:        "Project Metadata",
			Description: "Current project information including entity counts, active templates, and statistics",
			MimeType:    "application/json",
		},
		{
			URI:         "emergent://project/{project_id}/recent-entities",
			Name:        "Recent Entities",
			Description: "Recently created or modified entities for context (last 50)",
			MimeType:    "application/json",
		},
		{
			URI:         "emergent://project/{project_id}/templates",
			Name:        "Installed Templates",
			Description: "Templates currently installed and active in this project",
			MimeType:    "application/json",
		},
	}
}

func (s *Service) GetPromptDefinitions() []PromptDefinition {
	return []PromptDefinition{
		{
			Name:        "explore_entity_type",
			Description: "Guide to exploring entities of a specific type with filtering and relationship analysis",
			Arguments: []PromptArgument{
				{
					Name:        "entity_type",
					Description: "The entity type to explore (e.g., 'Decision', 'Project', 'Document')",
					Required:    true,
				},
			},
		},
		{
			Name:        "create_from_template",
			Description: "Step-by-step workflow for creating a new entity using a template pack",
			Arguments: []PromptArgument{
				{
					Name:        "entity_type",
					Description: "Type of entity to create",
					Required:    true,
				},
				{
					Name:        "template_pack",
					Description: "Template pack to use (optional, will suggest if not provided)",
					Required:    false,
				},
			},
		},
		{
			Name:        "analyze_relationships",
			Description: "Guide for discovering and analyzing entity relationships in the knowledge graph",
			Arguments: []PromptArgument{
				{
					Name:        "entity_name",
					Description: "Name or ID of the entity to analyze",
					Required:    true,
				},
			},
		},
		{
			Name:        "setup_research_project",
			Description: "Complete workflow for setting up a research project with tasks, documents, and relationships",
			Arguments: []PromptArgument{
				{
					Name:        "project_name",
					Description: "Name of the research project",
					Required:    true,
				},
				{
					Name:        "methodology",
					Description: "Research methodology (optional)",
					Required:    false,
				},
			},
		},
		{
			Name:        "find_related_entities",
			Description: "Discover entities related to a given entity through various relationship types",
			Arguments: []PromptArgument{
				{
					Name:        "entity_name",
					Description: "Starting entity name or ID",
					Required:    true,
				},
				{
					Name:        "relationship_type",
					Description: "Filter by specific relationship type (optional)",
					Required:    false,
				},
				{
					Name:        "depth",
					Description: "How many hops to traverse (default: 1)",
					Required:    false,
				},
			},
		},
	}
}

// ExecuteTool executes an MCP tool and returns the result
func (s *Service) ExecuteTool(ctx context.Context, projectID string, toolName string, args map[string]any) (*ToolResult, error) {
	switch toolName {
	case "schema_version":
		return s.executeSchemaVersion(ctx)
	case "list_entity_types":
		return s.executeListEntityTypes(ctx, projectID)
	case "query_entities":
		return s.executeQueryEntities(ctx, projectID, args)
	case "search_entities":
		return s.executeSearchEntities(ctx, projectID, args)
	case "get_entity_edges":
		return s.executeGetEntityEdges(ctx, projectID, args)
	case "list_template_packs":
		return s.executeListTemplatePacks(ctx, args)
	case "get_template_pack":
		return s.executeGetTemplatePack(ctx, args)
	case "get_available_templates":
		return s.executeGetAvailableTemplates(ctx, projectID)
	case "get_installed_templates":
		return s.executeGetInstalledTemplates(ctx, projectID)
	case "assign_template_pack":
		return s.executeAssignTemplatePack(ctx, projectID, args)
	case "update_template_assignment":
		return s.executeUpdateTemplateAssignment(ctx, projectID, args)
	case "uninstall_template_pack":
		return s.executeUninstallTemplatePack(ctx, projectID, args)
	case "create_template_pack":
		return s.executeCreateTemplatePack(ctx, args)
	case "delete_template_pack":
		return s.executeDeleteTemplatePack(ctx, args)
	case "create_entity":
		return s.executeCreateEntity(ctx, projectID, args)
	case "create_relationship":
		return s.executeCreateRelationship(ctx, projectID, args)
	case "update_entity":
		return s.executeUpdateEntity(ctx, projectID, args)
	case "delete_entity":
		return s.executeDeleteEntity(ctx, projectID, args)
	case "restore_entity":
		return s.executeRestoreEntity(ctx, projectID, args)
	case "hybrid_search":
		return s.executeHybridSearch(ctx, projectID, args)
	case "semantic_search":
		return s.executeSemanticSearch(ctx, projectID, args)
	case "find_similar":
		return s.executeFindSimilar(ctx, projectID, args)
	case "traverse_graph":
		return s.executeTraverseGraph(ctx, projectID, args)
	case "list_relationships":
		return s.executeListRelationships(ctx, projectID, args)
	case "update_relationship":
		return s.executeUpdateRelationship(ctx, projectID, args)
	case "delete_relationship":
		return s.executeDeleteRelationship(ctx, projectID, args)
	case "list_tags":
		return s.executeListTags(ctx, projectID)
	case "batch_create_entities":
		return s.executeBatchCreateEntities(ctx, projectID, args)
	case "batch_create_relationships":
		return s.executeBatchCreateRelationships(ctx, projectID, args)
	case "preview_schema_migration":
		return s.executePreviewSchemaMigration(ctx, projectID, args)
	case "list_migration_archives":
		return s.executeListMigrationArchives(ctx, projectID, args)
	case "get_migration_archive":
		return s.executeGetMigrationArchive(ctx, projectID, args)

	// Agent Definition tools
	case "list_agent_definitions":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "get_agent_definition":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "create_agent_definition":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "update_agent_definition":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "delete_agent_definition":
		return s.delegateAgentTool(ctx, projectID, toolName, args)

	// Agent (runtime) tools
	case "list_agents":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "get_agent":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "create_agent":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "update_agent":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "delete_agent":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "trigger_agent":
		return s.delegateAgentTool(ctx, projectID, toolName, args)

	// Agent Run tools
	case "list_agent_runs":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "get_agent_run":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "get_agent_run_messages":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "get_agent_run_tool_calls":
		return s.delegateAgentTool(ctx, projectID, toolName, args)

	// Agent Catalog tools
	case "list_available_agents":
		return s.delegateAgentTool(ctx, projectID, toolName, args)

	// MCP Registry tools
	case "list_mcp_servers":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "get_mcp_server":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "create_mcp_server":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "update_mcp_server":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "delete_mcp_server":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "toggle_mcp_server_tool":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "sync_mcp_server_tools":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)

	// Official MCP Registry browse/install tools
	case "search_mcp_registry":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "get_mcp_registry_server":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "install_mcp_from_registry":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "inspect_mcp_server":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)

	default:
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}
}

// executeSchemaVersion returns schema version metadata
func (s *Service) executeSchemaVersion(ctx context.Context) (*ToolResult, error) {
	version, err := s.getSchemaVersion(ctx)
	if err != nil {
		return nil, err
	}

	// Count template packs
	var packCount int
	err = s.db.NewSelect().
		TableExpr("kb.graph_template_packs").
		ColumnExpr("COUNT(*)").
		Scan(ctx, &packCount)
	if err != nil {
		s.log.Warn("failed to count template packs", logger.Error(err))
		packCount = 0
	}

	result := SchemaVersionResult{
		Version:      version,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		PackCount:    packCount,
		CacheHintTTL: 300, // 5 minutes
	}

	return s.wrapResult(result)
}

// executeListEntityTypes returns all entity types with counts
func (s *Service) executeListEntityTypes(ctx context.Context, projectID string) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	// Query type registry with counts
	type typeRow struct {
		Name          string `bun:"name"`
		Description   string `bun:"description"`
		InstanceCount int    `bun:"instance_count"`
	}

	type relTypeRow struct {
		Type     string `bun:"type"`
		FromType string `bun:"from_type"`
		ToType   string `bun:"to_type"`
		Count    int    `bun:"count"`
	}

	var types []typeRow
	var relTypes []relTypeRow

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Set RLS context
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		// Query entity types
		err := tx.NewRaw(`
			SELECT 
				tr.type_name as name,
				COALESCE(tr.description, '') as description,
				COUNT(go.id)::int as instance_count
			FROM kb.project_object_type_registry tr
			LEFT JOIN kb.graph_objects go 
				ON go.type = tr.type_name 
				AND go.deleted_at IS NULL 
				AND go.project_id = ?
			WHERE tr.enabled = true 
				AND tr.project_id = ?
			GROUP BY tr.type_name, tr.description
			ORDER BY tr.type_name
		`, projectUUID, projectUUID).Scan(ctx, &types)
		if err != nil {
			return err
		}

		// Query relationship types with from/to type info
		err = tx.NewRaw(`
			SELECT 
				gr.type,
				src.type as from_type,
				dst.type as to_type,
				COUNT(*)::int as count
			FROM kb.graph_relationships gr
			JOIN kb.graph_objects src ON gr.src_id = src.id
			JOIN kb.graph_objects dst ON gr.dst_id = dst.id
			WHERE gr.deleted_at IS NULL 
				AND gr.project_id = ?
				AND src.deleted_at IS NULL
				AND dst.deleted_at IS NULL
			GROUP BY gr.type, src.type, dst.type
			ORDER BY gr.type, count DESC
		`, projectUUID).Scan(ctx, &relTypes)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("query entity types: %w", err)
	}

	entityTypes := make([]EntityType, len(types))
	for i, t := range types {
		desc := t.Description
		if desc == "" {
			desc = "No description"
		}
		entityTypes[i] = EntityType{
			Name:        t.Name,
			Description: desc,
			Count:       t.InstanceCount,
		}
	}

	relationshipTypes := make([]RelationshipType, len(relTypes))
	for i, r := range relTypes {
		relationshipTypes[i] = RelationshipType{
			Type:     r.Type,
			FromType: r.FromType,
			ToType:   r.ToType,
			Count:    r.Count,
		}
	}

	result := EntityTypesResult{
		ProjectID:     projectID,
		Types:         entityTypes,
		Relationships: relationshipTypes,
		Total:         len(entityTypes),
	}

	return s.wrapResult(result)
}

// executeQueryEntities queries entities by type with pagination
func (s *Service) executeQueryEntities(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	// Parse arguments
	typeName, _ := args["type_name"].(string)
	if typeName == "" {
		return nil, fmt.Errorf("missing required parameter: type_name")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	offset := 0
	if o, ok := args["offset"].(float64); ok {
		offset = int(o)
	}
	if offset < 0 {
		offset = 0
	}

	sortBy := "created_at"
	if sb, ok := args["sort_by"].(string); ok && sb != "" {
		// Validate sort field
		switch sb {
		case "created_at", "updated_at", "name":
			sortBy = sb
		}
	}

	sortOrder := "DESC"
	if so, ok := args["sort_order"].(string); ok {
		if so == "asc" {
			sortOrder = "ASC"
		}
	}

	// Build sort expression based on field
	orderExpr := fmt.Sprintf("go.%s %s", sortBy, sortOrder)
	if sortBy == "name" {
		orderExpr = fmt.Sprintf("go.properties->>'name' %s NULLS LAST", sortOrder)
	}

	type entityRow struct {
		ID              uuid.UUID      `bun:"id"`
		Key             string         `bun:"key"`
		Name            string         `bun:"name"`
		TypeName        string         `bun:"type_name"`
		Properties      map[string]any `bun:"properties,type:jsonb"`
		CreatedAt       time.Time      `bun:"created_at"`
		UpdatedAt       time.Time      `bun:"updated_at"`
		TypeDescription string         `bun:"type_description"`
	}

	var entities []entityRow
	var total int

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		// Query entities
		err := tx.NewRaw(`
			SELECT 
				go.id,
				go.key,
				COALESCE(go.properties->>'name', '') as name,
				go.properties,
				go.created_at,
				COALESCE(go.updated_at, go.created_at) as updated_at,
				go.type as type_name,
				COALESCE(tr.description, '') as type_description
			FROM kb.graph_objects go
			LEFT JOIN kb.project_object_type_registry tr ON tr.type_name = go.type AND tr.project_id = go.project_id
			WHERE go.type = ?
				AND go.deleted_at IS NULL
				AND go.project_id = ?
			ORDER BY `+orderExpr+`
			LIMIT ? OFFSET ?
		`, typeName, projectUUID, limit, offset).Scan(ctx, &entities)
		if err != nil {
			return err
		}

		// Get total count
		err = tx.NewRaw(`
			SELECT COUNT(*)
			FROM kb.graph_objects go
			WHERE go.type = ?
				AND go.deleted_at IS NULL
				AND go.project_id = ?
		`, typeName, projectUUID).Scan(ctx, &total)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("query entities: %w", err)
	}

	// Transform to response format
	resultEntities := make([]Entity, len(entities))
	for i, e := range entities {
		resultEntities[i] = Entity{
			ID:         e.ID.String(),
			Key:        e.Key,
			Name:       e.Name,
			Type:       e.TypeName,
			Properties: e.Properties,
			CreatedAt:  e.CreatedAt,
			UpdatedAt:  e.UpdatedAt,
		}
	}

	result := QueryEntitiesResult{
		ProjectID: projectID,
		Entities:  resultEntities,
		Pagination: &PaginationInfo{
			Total:   total,
			Limit:   limit,
			Offset:  offset,
			HasMore: offset+limit < total,
		},
	}

	return s.wrapResult(result)
}

// executeSearchEntities searches entities by text
func (s *Service) executeSearchEntities(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	// Parse arguments
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	typeName, _ := args["type_name"].(string)

	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	type entityRow struct {
		ID         uuid.UUID      `bun:"id"`
		Key        string         `bun:"key"`
		Name       string         `bun:"name"`
		TypeName   string         `bun:"type_name"`
		Properties map[string]any `bun:"properties,type:jsonb"`
		CreatedAt  time.Time      `bun:"created_at"`
	}

	var entities []entityRow
	searchPattern := "%" + query + "%"

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		baseQuery := `
			SELECT 
				go.id,
				go.key,
				COALESCE(go.properties->>'name', '') as name,
				go.properties,
				go.type as type_name,
				go.created_at
			FROM kb.graph_objects go
			WHERE go.deleted_at IS NULL
				AND go.project_id = ?
				AND (
					go.key ILIKE ?
					OR go.properties->>'name' ILIKE ?
					OR go.properties->>'description' ILIKE ?
				)
		`
		queryArgs := []any{projectUUID, searchPattern, searchPattern, searchPattern}

		if typeName != "" {
			baseQuery += " AND go.type = ?"
			queryArgs = append(queryArgs, typeName)
		}

		baseQuery += " ORDER BY go.created_at DESC LIMIT ?"
		queryArgs = append(queryArgs, limit)

		return tx.NewRaw(baseQuery, queryArgs...).Scan(ctx, &entities)
	})

	if err != nil {
		return nil, fmt.Errorf("search entities: %w", err)
	}

	// Transform to response format
	resultEntities := make([]Entity, len(entities))
	for i, e := range entities {
		resultEntities[i] = Entity{
			ID:         e.ID.String(),
			Key:        e.Key,
			Name:       e.Name,
			Type:       e.TypeName,
			Properties: e.Properties,
			CreatedAt:  e.CreatedAt,
		}
	}

	result := SearchEntitiesResult{
		ProjectID: projectID,
		Query:     query,
		Entities:  resultEntities,
		Count:     len(resultEntities),
	}

	return s.wrapResult(result)
}

func (s *Service) executeGetEntityEdges(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	entityIDStr, _ := args["entity_id"].(string)
	if entityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: entity_id")
	}

	entityID, err := uuid.Parse(entityIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid entity_id: %w", err)
	}

	edges, err := s.graphService.GetEdges(ctx, projectUUID, entityID, graph.GetEdgesParams{})
	if err != nil {
		return nil, fmt.Errorf("get entity edges: %w", err)
	}

	incoming := make([]EdgeInfo, 0, len(edges.Incoming))
	for _, rel := range edges.Incoming {
		srcEntity, err := s.getEntityBasicInfo(ctx, projectUUID, rel.SrcID)
		if err != nil {
			continue
		}
		incoming = append(incoming, EdgeInfo{
			RelationshipID:   rel.ID.String(),
			RelationshipType: rel.Type,
			ConnectedEntity:  *srcEntity,
			Properties:       rel.Properties,
		})
	}

	outgoing := make([]EdgeInfo, 0, len(edges.Outgoing))
	for _, rel := range edges.Outgoing {
		dstEntity, err := s.getEntityBasicInfo(ctx, projectUUID, rel.DstID)
		if err != nil {
			continue
		}
		outgoing = append(outgoing, EdgeInfo{
			RelationshipID:   rel.ID.String(),
			RelationshipType: rel.Type,
			ConnectedEntity:  *dstEntity,
			Properties:       rel.Properties,
		})
	}

	result := GetEntityEdgesResult{
		EntityID: entityIDStr,
		Incoming: incoming,
		Outgoing: outgoing,
	}

	return s.wrapResult(result)
}

func (s *Service) getEntityBasicInfo(ctx context.Context, projectID, entityID uuid.UUID) (*ConnectedEntity, error) {
	type entityRow struct {
		ID         uuid.UUID      `bun:"id"`
		Type       string         `bun:"type"`
		Key        string         `bun:"key"`
		Properties map[string]any `bun:"properties,type:jsonb"`
	}

	var entity entityRow
	err := s.db.NewRaw(`
		SELECT id, type, COALESCE(key, '') as key, properties
		FROM kb.graph_objects
		WHERE id = ? AND project_id = ? AND deleted_at IS NULL
	`, entityID, projectID).Scan(ctx, &entity)

	if err != nil {
		return nil, err
	}

	name := ""
	if n, ok := entity.Properties["name"].(string); ok {
		name = n
	}

	return &ConnectedEntity{
		ID:         entity.ID.String(),
		Type:       entity.Type,
		Key:        entity.Key,
		Name:       name,
		Properties: entity.Properties,
	}, nil
}

// =============================================================================
// NEW MCP Tool Execution Methods
// =============================================================================

// executeRestoreEntity restores a soft-deleted entity
func (s *Service) executeRestoreEntity(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	entityIDStr, ok := args["entity_id"].(string)
	if !ok || entityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: entity_id")
	}

	entityID, err := uuid.Parse(entityIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid entity_id: %w", err)
	}

	result, err := s.graphService.Restore(ctx, projectUUID, entityID, nil)
	if err != nil {
		return nil, fmt.Errorf("restore entity: %w", err)
	}

	return s.wrapResult(map[string]any{
		"success": true,
		"entity":  result,
		"message": "Entity restored successfully",
	})
}

// executeHybridSearch performs hybrid search (FTS + vector + graph context + relationship embeddings)
func (s *Service) executeHybridSearch(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	var types []string
	if t, ok := args["types"].([]any); ok {
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}

	var labels []string
	if l, ok := args["labels"].([]any); ok {
		for _, v := range l {
			if str, ok := v.(string); ok {
				labels = append(labels, str)
			}
		}
	}

	if s.searchSvc != nil {
		unifiedReq := &search.UnifiedSearchRequest{
			Query: query,
			Limit: limit,
		}

		res, err := s.searchSvc.Search(ctx, projectUUID, unifiedReq, nil)
		if err != nil {
			s.log.WarnContext(ctx, "unified search failed, falling back to graph search",
				"error", err,
				"project_id", projectID,
			)
		} else {
			return s.wrapResult(s.mapUnifiedToSearchResponse(res, types, labels))
		}
	}

	req := &graph.HybridSearchRequest{
		Query:  query,
		Types:  types,
		Labels: labels,
		Limit:  limit,
	}

	results, err := s.graphService.HybridSearch(ctx, projectUUID, req, nil)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
	}

	return s.wrapResult(results)
}

// mapUnifiedToSearchResponse converts unified search results back to graph.SearchResponse
// for backward compatibility with existing MCP consumers. Relationship results are included
// as additional items with a synthetic object wrapper.
func (s *Service) mapUnifiedToSearchResponse(res *search.UnifiedSearchResponse, types, labels []string) *graph.SearchResponse {
	var items []*graph.SearchResultItem

	for _, r := range res.Results {
		switch r.Type {
		case search.ItemTypeGraph:
			if len(types) > 0 && !containsStr(types, r.ObjectType) {
				continue
			}

			canonicalID, _ := uuid.Parse(r.CanonicalID)
			objectID, _ := uuid.Parse(r.ObjectID)

			var key *string
			if r.Key != "" {
				k := r.Key
				key = &k
			}

			items = append(items, &graph.SearchResultItem{
				Object: &graph.GraphObjectResponse{
					ID:          objectID,
					CanonicalID: canonicalID,
					Type:        r.ObjectType,
					Key:         key,
					Properties:  r.Fields,
				},
				Score: r.Score,
			})

		case search.ItemTypeText:
			textID, _ := uuid.Parse(r.ID)
			textType := "text_chunk"

			props := map[string]any{
				"snippet": r.Snippet,
			}
			if r.Source != nil {
				props["source"] = *r.Source
			}
			if r.DocumentID != nil {
				props["document_id"] = *r.DocumentID
			}

			items = append(items, &graph.SearchResultItem{
				Object: &graph.GraphObjectResponse{
					ID:         textID,
					Type:       textType,
					Properties: props,
				},
				Score: r.Score,
			})
		}
	}

	if len(labels) > 0 {
		filtered := make([]*graph.SearchResultItem, 0, len(items))
		for _, item := range items {
			if item.Object.Type == "text_chunk" {
				filtered = append(filtered, item)
				continue
			}
			for _, objLabel := range item.Object.Labels {
				if containsStr(labels, objLabel) {
					filtered = append(filtered, item)
					break
				}
			}
		}
		items = filtered
	}

	return &graph.SearchResponse{
		Data:    items,
		Total:   len(items),
		HasMore: false,
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// executeSemanticSearch performs vector-based semantic search
func (s *Service) executeSemanticSearch(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	var types []string
	if t, ok := args["types"].([]any); ok {
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}

	// Use hybrid search with query text (which will auto-generate embeddings)
	req := &graph.HybridSearchRequest{
		Query: query,
		Types: types,
		Limit: limit,
		// Favor vector search heavily
		LexicalWeight: float32Ptr(0.2),
		VectorWeight:  float32Ptr(0.8),
	}

	results, err := s.graphService.HybridSearch(ctx, projectUUID, req, nil)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}

	return s.wrapResult(results)
}

// executeFindSimilar finds entities similar to a given entity
func (s *Service) executeFindSimilar(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	entityIDStr, ok := args["entity_id"].(string)
	if !ok || entityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: entity_id")
	}

	entityID, err := uuid.Parse(entityIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid entity_id: %w", err)
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	var types []string
	if t, ok := args["types"].([]any); ok {
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}

	var typeFilter *string
	if len(types) > 0 {
		typeFilter = &types[0]
	}

	req := &graph.SimilarObjectsRequest{
		Type:  typeFilter,
		Limit: limit,
	}

	results, err := s.graphService.FindSimilarObjects(ctx, projectUUID, entityID, req)
	if err != nil {
		return nil, fmt.Errorf("find similar: %w", err)
	}

	return s.wrapResult(map[string]any{
		"similar_entities": results,
		"total":            len(results),
	})
}

// executeTraverseGraph performs multi-hop graph traversal
func (s *Service) executeTraverseGraph(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	startEntityIDStr, ok := args["start_entity_id"].(string)
	if !ok || startEntityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: start_entity_id")
	}

	startEntityID, err := uuid.Parse(startEntityIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid start_entity_id: %w", err)
	}

	maxDepth := 2
	if d, ok := args["max_depth"].(float64); ok {
		maxDepth = int(d)
	}
	if maxDepth < 1 {
		maxDepth = 1
	}
	if maxDepth > 5 {
		maxDepth = 5
	}

	direction := "both"
	if d, ok := args["direction"].(string); ok {
		direction = d
	}

	var relationshipTypes []string
	if rt, ok := args["relationship_types"].([]any); ok {
		for _, v := range rt {
			if str, ok := v.(string); ok {
				relationshipTypes = append(relationshipTypes, str)
			}
		}
	}

	queryContext := ""
	if qc, ok := args["query_context"].(string); ok {
		queryContext = qc
	}

	req := &graph.TraverseGraphRequest{
		RootIDs:           []uuid.UUID{startEntityID},
		MaxDepth:          maxDepth,
		Direction:         direction,
		RelationshipTypes: relationshipTypes,
		QueryContext:      queryContext,
	}

	results, err := s.graphService.TraverseGraph(ctx, projectUUID, req)
	if err != nil {
		return nil, fmt.Errorf("traverse graph: %w", err)
	}

	return s.wrapResult(results)
}

// executeListRelationships lists relationships with optional filters
func (s *Service) executeListRelationships(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	params := graph.RelationshipListParams{
		ProjectID: projectUUID,
		Limit:     limit + 1, // Fetch one extra to check hasMore
	}

	if t, ok := args["type"].(string); ok && t != "" {
		params.Type = &t
	}

	if srcID, ok := args["source_id"].(string); ok && srcID != "" {
		id, err := uuid.Parse(srcID)
		if err != nil {
			return nil, fmt.Errorf("invalid source_id: %w", err)
		}
		params.SrcID = &id
	}

	if dstID, ok := args["target_id"].(string); ok && dstID != "" {
		id, err := uuid.Parse(dstID)
		if err != nil {
			return nil, fmt.Errorf("invalid target_id: %w", err)
		}
		params.DstID = &id
	}

	results, err := s.graphService.ListRelationships(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list relationships: %w", err)
	}

	return s.wrapResult(results)
}

// executeUpdateRelationship updates a relationship
func (s *Service) executeUpdateRelationship(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	relIDStr, ok := args["relationship_id"].(string)
	if !ok || relIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: relationship_id")
	}

	relID, err := uuid.Parse(relIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid relationship_id: %w", err)
	}

	req := &graph.PatchGraphRelationshipRequest{}

	if props, ok := args["properties"].(map[string]any); ok {
		req.Properties = props
	}

	if weight, ok := args["weight"].(float64); ok {
		w := float32(weight)
		req.Weight = &w
	}

	result, err := s.graphService.PatchRelationship(ctx, projectUUID, relID, req)
	if err != nil {
		return nil, fmt.Errorf("update relationship: %w", err)
	}

	return s.wrapResult(map[string]any{
		"success":      true,
		"relationship": result,
		"message":      "Relationship updated successfully",
	})
}

// executeDeleteRelationship soft-deletes a relationship
func (s *Service) executeDeleteRelationship(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	relIDStr, ok := args["relationship_id"].(string)
	if !ok || relIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: relationship_id")
	}

	relID, err := uuid.Parse(relIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid relationship_id: %w", err)
	}

	result, err := s.graphService.DeleteRelationship(ctx, projectUUID, relID)
	if err != nil {
		return nil, fmt.Errorf("delete relationship: %w", err)
	}

	return s.wrapResult(map[string]any{
		"success":      true,
		"relationship": result,
		"message":      "Relationship deleted successfully",
	})
}

// executeListTags gets all unique tags in the project
func (s *Service) executeListTags(ctx context.Context, projectID string) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	tags, err := s.graphService.GetTags(ctx, projectUUID, nil)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	return s.wrapResult(map[string]any{
		"tags":  tags,
		"total": len(tags),
	})
}

// executeBatchCreateEntities creates multiple entities in one request
func (s *Service) executeBatchCreateEntities(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	entitiesRaw, ok := args["entities"].([]any)
	if !ok || len(entitiesRaw) == 0 {
		return nil, fmt.Errorf("missing or empty entities array")
	}

	if len(entitiesRaw) > 100 {
		return nil, fmt.Errorf("batch size exceeded: maximum 100 entities per request")
	}

	type batchResult struct {
		Success bool                       `json:"success"`
		Entity  *graph.GraphObjectResponse `json:"entity,omitempty"`
		Error   string                     `json:"error,omitempty"`
		Index   int                        `json:"index"`
	}

	results := make([]batchResult, 0, len(entitiesRaw))
	successCount := 0
	failedCount := 0

	for i, entityRaw := range entitiesRaw {
		entityMap, ok := entityRaw.(map[string]any)
		if !ok {
			results = append(results, batchResult{
				Success: false,
				Error:   "invalid entity specification",
				Index:   i,
			})
			failedCount++
			continue
		}

		typeName, _ := entityMap["type"].(string)
		if typeName == "" {
			results = append(results, batchResult{
				Success: false,
				Error:   "missing entity type",
				Index:   i,
			})
			failedCount++
			continue
		}

		var key *string
		if k, ok := entityMap["key"].(string); ok && k != "" {
			key = &k
		}

		var status *string
		if st, ok := entityMap["status"].(string); ok && st != "" {
			status = &st
		}

		properties, _ := entityMap["properties"].(map[string]any)
		if properties == nil {
			properties = make(map[string]any)
		}

		var labels []string
		if l, ok := entityMap["labels"].([]any); ok {
			for _, v := range l {
				if str, ok := v.(string); ok {
					labels = append(labels, str)
				}
			}
		}

		req := &graph.CreateGraphObjectRequest{
			Type:       typeName,
			Key:        key,
			Status:     status,
			Properties: properties,
			Labels:     labels,
		}

		result, err := s.graphService.Create(ctx, projectUUID, req, nil)
		if err != nil {
			results = append(results, batchResult{
				Success: false,
				Error:   err.Error(),
				Index:   i,
			})
			failedCount++
			continue
		}

		results = append(results, batchResult{
			Success: true,
			Entity:  result,
			Index:   i,
		})
		successCount++
	}

	return s.wrapResult(map[string]any{
		"success": successCount,
		"failed":  failedCount,
		"total":   len(entitiesRaw),
		"results": results,
		"message": fmt.Sprintf("Batch create completed: %d succeeded, %d failed", successCount, failedCount),
	})
}

// executeBatchCreateRelationships creates multiple relationships in one request
func (s *Service) executeBatchCreateRelationships(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	relationshipsRaw, ok := args["relationships"].([]any)
	if !ok || len(relationshipsRaw) == 0 {
		return nil, fmt.Errorf("missing or empty relationships array")
	}

	if len(relationshipsRaw) > 100 {
		return nil, fmt.Errorf("batch size exceeded: maximum 100 relationships per request")
	}

	type batchResult struct {
		Success      bool                             `json:"success"`
		Relationship *graph.GraphRelationshipResponse `json:"relationship,omitempty"`
		Error        string                           `json:"error,omitempty"`
		Index        int                              `json:"index"`
	}

	results := make([]batchResult, 0, len(relationshipsRaw))
	successCount := 0
	failedCount := 0

	for i, relRaw := range relationshipsRaw {
		relMap, ok := relRaw.(map[string]any)
		if !ok {
			results = append(results, batchResult{
				Success: false,
				Error:   "invalid relationship specification",
				Index:   i,
			})
			failedCount++
			continue
		}

		relType, _ := relMap["type"].(string)
		if relType == "" {
			results = append(results, batchResult{
				Success: false,
				Error:   "missing relationship type",
				Index:   i,
			})
			failedCount++
			continue
		}

		srcIDStr, _ := relMap["source_id"].(string)
		srcID, err := uuid.Parse(srcIDStr)
		if err != nil {
			results = append(results, batchResult{
				Success: false,
				Error:   "invalid source_id",
				Index:   i,
			})
			failedCount++
			continue
		}

		dstIDStr, _ := relMap["target_id"].(string)
		dstID, err := uuid.Parse(dstIDStr)
		if err != nil {
			results = append(results, batchResult{
				Success: false,
				Error:   "invalid target_id",
				Index:   i,
			})
			failedCount++
			continue
		}

		properties, _ := relMap["properties"].(map[string]any)
		if properties == nil {
			properties = make(map[string]any)
		}

		var weight *float32
		if w, ok := relMap["weight"].(float64); ok {
			wf := float32(w)
			weight = &wf
		}

		req := &graph.CreateGraphRelationshipRequest{
			Type:       relType,
			SrcID:      srcID,
			DstID:      dstID,
			Properties: properties,
			Weight:     weight,
		}

		result, err := s.graphService.CreateRelationship(ctx, projectUUID, req)
		if err != nil {
			results = append(results, batchResult{
				Success: false,
				Error:   err.Error(),
				Index:   i,
			})
			failedCount++
			continue
		}

		results = append(results, batchResult{
			Success:      true,
			Relationship: result,
			Index:        i,
		})
		successCount++
	}

	return s.wrapResult(map[string]any{
		"success": successCount,
		"failed":  failedCount,
		"total":   len(relationshipsRaw),
		"results": results,
		"message": fmt.Sprintf("Batch create completed: %d succeeded, %d failed", successCount, failedCount),
	})
}

// Helper to create a float32 pointer
func float32Ptr(f float32) *float32 {
	return &f
}

// getSchemaVersion computes a schema version hash
func (s *Service) getSchemaVersion(ctx context.Context) (string, error) {
	// Check cache first
	s.cacheMu.RLock()
	if s.cachedVersion != "" && time.Now().Before(s.cacheExpiry) {
		version := s.cachedVersion
		s.cacheMu.RUnlock()
		return version, nil
	}
	s.cacheMu.RUnlock()

	// Compute new version
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Double-check after acquiring write lock
	if s.cachedVersion != "" && time.Now().Before(s.cacheExpiry) {
		return s.cachedVersion, nil
	}

	// Fetch template packs
	type packInfo struct {
		ID        string    `bun:"id"`
		UpdatedAt time.Time `bun:"updated_at"`
	}

	var packs []packInfo
	err := s.db.NewSelect().
		TableExpr("kb.graph_template_packs").
		Column("id", "updated_at").
		OrderExpr("id ASC").
		Scan(ctx, &packs)

	if err != nil {
		return "", fmt.Errorf("query template packs: %w", err)
	}

	// Build composite string
	composite := ""
	for _, p := range packs {
		composite += fmt.Sprintf("%s:%d|", p.ID, p.UpdatedAt.Unix())
	}

	// Compute MD5 hash
	hash := md5.Sum([]byte(composite))
	version := hex.EncodeToString(hash[:])[:16]

	// Cache result
	s.cachedVersion = version
	s.cacheExpiry = time.Now().Add(60 * time.Second)

	return version, nil
}

// wrapResult wraps a result in MCP ToolResult format
func (s *Service) wrapResult(data any) (*ToolResult, error) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	return &ToolResult{
		Content: []ContentBlock{
			{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}

// Helper function for pointer to int
func intPtr(i int) *int {
	return &i
}

func (s *Service) executeListTemplatePacks(ctx context.Context, args map[string]any) (*ToolResult, error) {
	search, _ := args["search"].(string)
	includeDeprecated, _ := args["include_deprecated"].(bool)

	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	page := 1
	if p, ok := args["page"].(float64); ok {
		page = int(p)
	}
	if page < 1 {
		page = 1
	}

	offset := (page - 1) * limit

	type packRow struct {
		ID                string         `bun:"id"`
		Name              string         `bun:"name"`
		Version           string         `bun:"version"`
		Description       string         `bun:"description"`
		Author            string         `bun:"author"`
		Source            string         `bun:"source"`
		ObjectTypeSchemas map[string]any `bun:"object_type_schemas,type:jsonb"`
		PublishedAt       time.Time      `bun:"published_at"`
		DeprecatedAt      *time.Time     `bun:"deprecated_at"`
	}

	var packs []packRow
	var total int

	query := s.db.NewSelect().
		TableExpr("kb.graph_template_packs").
		Column("id", "name", "version", "description", "author", "source", "object_type_schemas", "published_at", "deprecated_at").
		Where("draft = false")

	if !includeDeprecated {
		query = query.Where("deprecated_at IS NULL")
	}

	if search != "" {
		query = query.Where("(name ILIKE ? OR description ILIKE ?)", "%"+search+"%", "%"+search+"%")
	}

	countQuery := s.db.NewSelect().
		TableExpr("kb.graph_template_packs").
		ColumnExpr("COUNT(*)").
		Where("draft = false")

	if !includeDeprecated {
		countQuery = countQuery.Where("deprecated_at IS NULL")
	}
	if search != "" {
		countQuery = countQuery.Where("(name ILIKE ? OR description ILIKE ?)", "%"+search+"%", "%"+search+"%")
	}

	err := countQuery.Scan(ctx, &total)
	if err != nil {
		return nil, fmt.Errorf("count template packs: %w", err)
	}

	err = query.
		OrderExpr("published_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(ctx, &packs)

	if err != nil {
		return nil, fmt.Errorf("list template packs: %w", err)
	}

	summaries := make([]TemplatePackSummary, len(packs))
	for i, p := range packs {
		objectTypes := make([]string, 0)
		for typeName := range p.ObjectTypeSchemas {
			objectTypes = append(objectTypes, typeName)
		}

		deprecatedAt := ""
		if p.DeprecatedAt != nil {
			deprecatedAt = p.DeprecatedAt.Format(time.RFC3339)
		}

		summaries[i] = TemplatePackSummary{
			ID:           p.ID,
			Name:         p.Name,
			Version:      p.Version,
			Description:  p.Description,
			Author:       p.Author,
			Source:       p.Source,
			ObjectTypes:  objectTypes,
			TypeCount:    len(objectTypes),
			PublishedAt:  p.PublishedAt.Format(time.RFC3339),
			DeprecatedAt: deprecatedAt,
		}
	}

	result := ListTemplatePacksResult{
		Packs:   summaries,
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasMore: offset+limit < total,
	}

	return s.wrapResult(result)
}

func (s *Service) executeGetTemplatePack(ctx context.Context, args map[string]any) (*ToolResult, error) {
	packID, _ := args["pack_id"].(string)
	if packID == "" {
		return nil, fmt.Errorf("missing required parameter: pack_id")
	}

	type packRow struct {
		ID                      string         `bun:"id"`
		Name                    string         `bun:"name"`
		Version                 string         `bun:"version"`
		Description             string         `bun:"description"`
		Author                  string         `bun:"author"`
		Source                  string         `bun:"source"`
		License                 string         `bun:"license"`
		RepositoryURL           string         `bun:"repository_url"`
		DocumentationURL        string         `bun:"documentation_url"`
		ObjectTypeSchemas       map[string]any `bun:"object_type_schemas,type:jsonb"`
		RelationshipTypeSchemas map[string]any `bun:"relationship_type_schemas,type:jsonb"`
		UIConfigs               map[string]any `bun:"ui_configs,type:jsonb"`
		ExtractionPrompts       map[string]any `bun:"extraction_prompts,type:jsonb"`
		SQLViews                []any          `bun:"sql_views,type:jsonb"`
		Checksum                string         `bun:"checksum"`
		Draft                   bool           `bun:"draft"`
		PublishedAt             time.Time      `bun:"published_at"`
		DeprecatedAt            *time.Time     `bun:"deprecated_at"`
		CreatedAt               time.Time      `bun:"created_at"`
		UpdatedAt               time.Time      `bun:"updated_at"`
	}

	var pack packRow
	err := s.db.NewSelect().
		TableExpr("kb.graph_template_packs").
		Column("*").
		Where("id = ?", packID).
		Scan(ctx, &pack)

	if err != nil {
		return nil, fmt.Errorf("template pack not found: %s", packID)
	}

	deprecatedAt := ""
	if pack.DeprecatedAt != nil {
		deprecatedAt = pack.DeprecatedAt.Format(time.RFC3339)
	}

	result := GetTemplatePackResult{
		Pack: &TemplatePack{
			ID:                      pack.ID,
			Name:                    pack.Name,
			Version:                 pack.Version,
			Description:             pack.Description,
			Author:                  pack.Author,
			Source:                  pack.Source,
			License:                 pack.License,
			RepositoryURL:           pack.RepositoryURL,
			DocumentationURL:        pack.DocumentationURL,
			ObjectTypeSchemas:       pack.ObjectTypeSchemas,
			RelationshipTypeSchemas: pack.RelationshipTypeSchemas,
			UIConfigs:               pack.UIConfigs,
			ExtractionPrompts:       pack.ExtractionPrompts,
			SQLViews:                pack.SQLViews,
			Checksum:                pack.Checksum,
			Draft:                   pack.Draft,
			PublishedAt:             pack.PublishedAt.Format(time.RFC3339),
			DeprecatedAt:            deprecatedAt,
			CreatedAt:               pack.CreatedAt.Format(time.RFC3339),
			UpdatedAt:               pack.UpdatedAt.Format(time.RFC3339),
		},
	}

	return s.wrapResult(result)
}

func (s *Service) executeGetAvailableTemplates(ctx context.Context, projectID string) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	type packRow struct {
		ID                      string         `bun:"id"`
		Name                    string         `bun:"name"`
		Version                 string         `bun:"version"`
		Description             string         `bun:"description"`
		Author                  string         `bun:"author"`
		Source                  string         `bun:"source"`
		ObjectTypeSchemas       map[string]any `bun:"object_type_schemas,type:jsonb"`
		RelationshipTypeSchemas map[string]any `bun:"relationship_type_schemas,type:jsonb"`
		PublishedAt             time.Time      `bun:"published_at"`
	}

	type installedRow struct {
		ID             string `bun:"id"`
		TemplatePackID string `bun:"template_pack_id"`
		Active         bool   `bun:"active"`
	}

	type typeCountRow struct {
		Type  string `bun:"type"`
		Count int    `bun:"count"`
	}

	var packs []packRow
	var installed []installedRow
	var typeCounts []typeCountRow

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		err := tx.NewSelect().
			TableExpr("kb.graph_template_packs").
			Column("id", "name", "version", "description", "author", "source", "object_type_schemas", "relationship_type_schemas", "published_at").
			Where("deprecated_at IS NULL").
			Where("draft = false").
			OrderExpr("published_at DESC").
			Scan(ctx, &packs)
		if err != nil {
			return err
		}

		err = tx.NewSelect().
			TableExpr("kb.project_template_packs").
			Column("id", "template_pack_id", "active").
			Where("project_id = ?", projectUUID).
			Scan(ctx, &installed)
		if err != nil {
			return err
		}

		err = tx.NewRaw(`
			SELECT type, COUNT(*)::int as count 
			FROM kb.graph_objects 
			WHERE project_id = ? AND deleted_at IS NULL
			GROUP BY type
		`, projectUUID).Scan(ctx, &typeCounts)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("get available templates: %w", err)
	}

	installedMap := make(map[string]installedRow)
	for _, i := range installed {
		installedMap[i.TemplatePackID] = i
	}

	typeCountMap := make(map[string]int)
	for _, tc := range typeCounts {
		typeCountMap[tc.Type] = tc.Count
	}

	templates := make([]AvailableTemplate, len(packs))
	for i, p := range packs {
		objectTypes := make([]ObjectTypeInfo, 0)
		for typeName, schema := range p.ObjectTypeSchemas {
			desc := ""
			if schemaMap, ok := schema.(map[string]any); ok {
				if d, ok := schemaMap["description"].(string); ok {
					desc = d
				}
			}
			objectTypes = append(objectTypes, ObjectTypeInfo{
				Type:        typeName,
				Description: desc,
				SampleCount: typeCountMap[typeName],
			})
		}

		relTypes := make([]string, 0)
		for relType := range p.RelationshipTypeSchemas {
			relTypes = append(relTypes, relType)
		}

		inst, isInstalled := installedMap[p.ID]

		templates[i] = AvailableTemplate{
			ID:                p.ID,
			Name:              p.Name,
			Version:           p.Version,
			Description:       p.Description,
			Author:            p.Author,
			Source:            p.Source,
			ObjectTypes:       objectTypes,
			RelationshipTypes: relTypes,
			RelationshipCount: len(relTypes),
			Installed:         isInstalled,
			Active:            inst.Active,
			AssignmentID:      inst.ID,
			PublishedAt:       p.PublishedAt.Format(time.RFC3339),
		}
	}

	result := GetAvailableTemplatesResult{
		ProjectID: projectID,
		Templates: templates,
		Total:     len(templates),
	}

	return s.wrapResult(result)
}

func (s *Service) executeGetInstalledTemplates(ctx context.Context, projectID string) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	type installedRow struct {
		AssignmentID      string         `bun:"assignment_id"`
		TemplatePackID    string         `bun:"template_pack_id"`
		Name              string         `bun:"name"`
		Version           string         `bun:"version"`
		Description       string         `bun:"description"`
		ObjectTypeSchemas map[string]any `bun:"object_type_schemas,type:jsonb"`
		Active            bool           `bun:"active"`
		InstalledAt       time.Time      `bun:"installed_at"`
		Customizations    map[string]any `bun:"customizations,type:jsonb"`
	}

	type typeCountRow struct {
		Type  string `bun:"type"`
		Count int    `bun:"count"`
	}

	var installed []installedRow
	var typeCounts []typeCountRow

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		err := tx.NewRaw(`
			SELECT 
				ptp.id as assignment_id,
				ptp.template_pack_id,
				tp.name,
				tp.version,
				tp.description,
				tp.object_type_schemas,
				ptp.active,
				ptp.installed_at,
				ptp.customizations
			FROM kb.project_template_packs ptp
			JOIN kb.graph_template_packs tp ON ptp.template_pack_id = tp.id
			WHERE ptp.project_id = ?
			ORDER BY ptp.installed_at DESC
		`, projectUUID).Scan(ctx, &installed)
		if err != nil {
			return err
		}

		err = tx.NewRaw(`
			SELECT type, COUNT(*)::int as count 
			FROM kb.graph_objects 
			WHERE project_id = ? AND deleted_at IS NULL
			GROUP BY type
		`, projectUUID).Scan(ctx, &typeCounts)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("get installed templates: %w", err)
	}

	typeCountMap := make(map[string]int)
	for _, tc := range typeCounts {
		typeCountMap[tc.Type] = tc.Count
	}

	templates := make([]InstalledTemplate, len(installed))
	for i, inst := range installed {
		objectTypes := make([]ObjectTypeInfo, 0)
		for typeName, schema := range inst.ObjectTypeSchemas {
			desc := ""
			if schemaMap, ok := schema.(map[string]any); ok {
				if d, ok := schemaMap["description"].(string); ok {
					desc = d
				}
			}
			objectTypes = append(objectTypes, ObjectTypeInfo{
				Type:        typeName,
				Description: desc,
				SampleCount: typeCountMap[typeName],
			})
		}

		templates[i] = InstalledTemplate{
			AssignmentID:   inst.AssignmentID,
			TemplatePackID: inst.TemplatePackID,
			Name:           inst.Name,
			Version:        inst.Version,
			Description:    inst.Description,
			ObjectTypes:    objectTypes,
			Active:         inst.Active,
			InstalledAt:    inst.InstalledAt.Format(time.RFC3339),
			Customizations: inst.Customizations,
		}
	}

	result := GetInstalledTemplatesResult{
		ProjectID: projectID,
		Templates: templates,
		Total:     len(templates),
	}

	return s.wrapResult(result)
}

func (s *Service) executeAssignTemplatePack(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	templatePackID, _ := args["template_pack_id"].(string)
	if templatePackID == "" {
		return nil, fmt.Errorf("missing required parameter: template_pack_id")
	}

	type packRow struct {
		ID                string         `bun:"id"`
		Name              string         `bun:"name"`
		Version           string         `bun:"version"`
		ObjectTypeSchemas map[string]any `bun:"object_type_schemas,type:jsonb"`
		UIConfigs         map[string]any `bun:"ui_configs,type:jsonb"`
		ExtractionPrompts map[string]any `bun:"extraction_prompts,type:jsonb"`
	}

	var pack packRow
	err = s.db.NewSelect().
		TableExpr("kb.graph_template_packs").
		Column("id", "name", "version", "object_type_schemas", "ui_configs", "extraction_prompts").
		Where("id = ?", templatePackID).
		Scan(ctx, &pack)

	if err != nil {
		return nil, fmt.Errorf("template pack not found: %s", templatePackID)
	}

	allTypes := make([]string, 0)
	for typeName := range pack.ObjectTypeSchemas {
		allTypes = append(allTypes, typeName)
	}

	typesToInstall := allTypes

	var enabledTypesRaw, disabledTypesRaw []any
	if et, ok := args["enabled_types"].([]any); ok {
		enabledTypesRaw = et
	}
	if dt, ok := args["disabled_types"].([]any); ok {
		disabledTypesRaw = dt
	}

	if len(enabledTypesRaw) > 0 {
		enabledTypes := make([]string, 0, len(enabledTypesRaw))
		for _, t := range enabledTypesRaw {
			if ts, ok := t.(string); ok {
				enabledTypes = append(enabledTypes, ts)
			}
		}
		typesToInstall = make([]string, 0)
		for _, t := range enabledTypes {
			for _, at := range allTypes {
				if t == at {
					typesToInstall = append(typesToInstall, t)
					break
				}
			}
		}
	}

	if len(disabledTypesRaw) > 0 {
		disabledTypes := make([]string, 0, len(disabledTypesRaw))
		for _, t := range disabledTypesRaw {
			if ts, ok := t.(string); ok {
				disabledTypes = append(disabledTypes, ts)
			}
		}
		filtered := make([]string, 0)
		for _, t := range typesToInstall {
			disabled := false
			for _, dt := range disabledTypes {
				if t == dt {
					disabled = true
					break
				}
			}
			if !disabled {
				filtered = append(filtered, t)
			}
		}
		typesToInstall = filtered
	}

	var assignmentID string
	var conflicts []TypeConflict

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		var existingCount int
		err := tx.NewRaw(`
			SELECT COUNT(*) FROM kb.project_template_packs 
			WHERE project_id = ? AND template_pack_id = ?
		`, projectUUID, templatePackID).Scan(ctx, &existingCount)
		if err != nil {
			return err
		}
		if existingCount > 0 {
			return fmt.Errorf("template pack %s@%s is already installed", pack.Name, pack.Version)
		}

		type existingTypeRow struct {
			TypeName string `bun:"type_name"`
		}
		var existingTypes []existingTypeRow
		err = tx.NewRaw(`
			SELECT type_name FROM kb.project_object_type_registry 
			WHERE project_id = ? AND type_name IN (?)
		`, projectUUID, bun.In(typesToInstall)).Scan(ctx, &existingTypes)
		if err != nil {
			return err
		}

		conflictingTypes := make(map[string]bool)
		for _, et := range existingTypes {
			conflictingTypes[et.TypeName] = true
			conflicts = append(conflicts, TypeConflict{
				Type:       et.TypeName,
				Issue:      "Type already exists in project",
				Resolution: "skipped",
			})
		}

		finalTypes := make([]string, 0)
		for _, t := range typesToInstall {
			if !conflictingTypes[t] {
				finalTypes = append(finalTypes, t)
			}
		}

		customizations := map[string]any{}
		if len(disabledTypesRaw) > 0 {
			disabledTypes := make([]string, 0, len(disabledTypesRaw))
			for _, t := range disabledTypesRaw {
				if ts, ok := t.(string); ok {
					disabledTypes = append(disabledTypes, ts)
				}
			}
			customizations["disabledTypes"] = disabledTypes
		}

		customizationsJSON, _ := json.Marshal(customizations)

		err = tx.NewRaw(`
			INSERT INTO kb.project_template_packs (project_id, template_pack_id, active, customizations)
			VALUES (?, ?, true, ?)
			RETURNING id
		`, projectUUID, templatePackID, string(customizationsJSON)).Scan(ctx, &assignmentID)
		if err != nil {
			return err
		}

		for _, typeName := range finalTypes {
			schema := pack.ObjectTypeSchemas[typeName]
			uiConfig := pack.UIConfigs[typeName]
			extractionConfig := pack.ExtractionPrompts[typeName]

			schemaJSON, _ := json.Marshal(schema)
			uiConfigJSON, _ := json.Marshal(uiConfig)
			extractionConfigJSON, _ := json.Marshal(extractionConfig)

			_, err = tx.NewRaw(`
				INSERT INTO kb.project_object_type_registry 
				(project_id, type_name, source, template_pack_id, json_schema, ui_config, extraction_config, enabled)
				VALUES (?, ?, 'template', ?, ?, ?, ?, true)
			`, projectUUID, typeName, templatePackID, string(schemaJSON), string(uiConfigJSON), string(extractionConfigJSON)).Exec(ctx)
			if err != nil {
				return err
			}
		}

		typesToInstall = finalTypes
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("assign template pack: %w", err)
	}

	disabledTypes := make([]string, 0)
	for _, t := range disabledTypesRaw {
		if ts, ok := t.(string); ok {
			disabledTypes = append(disabledTypes, ts)
		}
	}

	result := AssignTemplatePackResult{
		Success:        true,
		AssignmentID:   assignmentID,
		InstalledTypes: typesToInstall,
		DisabledTypes:  disabledTypes,
		Conflicts:      conflicts,
	}

	return s.wrapResult(result)
}

func (s *Service) executeUpdateTemplateAssignment(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	assignmentID, _ := args["assignment_id"].(string)
	if assignmentID == "" {
		return nil, fmt.Errorf("missing required parameter: assignment_id")
	}

	active, hasActive := args["active"].(bool)

	if !hasActive {
		return nil, fmt.Errorf("at least one update field (active) is required")
	}

	var newActive bool

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		var currentActive bool
		err := tx.NewRaw(`
			SELECT active FROM kb.project_template_packs 
			WHERE id = ? AND project_id = ?
		`, assignmentID, projectUUID).Scan(ctx, &currentActive)
		if err != nil {
			return fmt.Errorf("assignment not found: %s", assignmentID)
		}

		newActive = active

		_, err = tx.NewRaw(`
			UPDATE kb.project_template_packs 
			SET active = ?, updated_at = now()
			WHERE id = ? AND project_id = ?
		`, newActive, assignmentID, projectUUID).Exec(ctx)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("update template assignment: %w", err)
	}

	result := UpdateTemplateAssignmentResult{
		Success:      true,
		AssignmentID: assignmentID,
		Active:       newActive,
		Message:      "Template pack assignment updated successfully",
	}

	return s.wrapResult(result)
}

func (s *Service) executeUninstallTemplatePack(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	assignmentID, _ := args["assignment_id"].(string)
	if assignmentID == "" {
		return nil, fmt.Errorf("missing required parameter: assignment_id")
	}

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		var templatePackID string
		err := tx.NewRaw(`
			SELECT template_pack_id FROM kb.project_template_packs 
			WHERE id = ? AND project_id = ?
		`, assignmentID, projectUUID).Scan(ctx, &templatePackID)
		if err != nil {
			return fmt.Errorf("assignment not found: %s", assignmentID)
		}

		var objectCount int
		err = tx.NewRaw(`
			SELECT COUNT(*) FROM kb.graph_objects go
			JOIN kb.project_object_type_registry ptr ON go.type = ptr.type_name AND go.project_id = ptr.project_id
			WHERE ptr.template_pack_id = ? AND go.project_id = ? AND go.deleted_at IS NULL AND go.supersedes_id IS NULL
		`, templatePackID, projectUUID).Scan(ctx, &objectCount)
		if err != nil {
			return err
		}

		if objectCount > 0 {
			return fmt.Errorf("cannot uninstall: %d objects still exist using types from this template pack", objectCount)
		}

		_, err = tx.NewRaw(`
			DELETE FROM kb.project_object_type_registry 
			WHERE template_pack_id = ? AND project_id = ?
		`, templatePackID, projectUUID).Exec(ctx)
		if err != nil {
			return err
		}

		_, err = tx.NewRaw(`
			DELETE FROM kb.project_template_packs WHERE id = ?
		`, assignmentID).Exec(ctx)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("uninstall template pack: %w", err)
	}

	result := UninstallTemplatePackResult{
		Success:      true,
		AssignmentID: assignmentID,
		Message:      "Template pack uninstalled successfully",
	}

	return s.wrapResult(result)
}

func (s *Service) executeCreateTemplatePack(ctx context.Context, args map[string]any) (*ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	version, _ := args["version"].(string)
	if version == "" {
		return nil, fmt.Errorf("missing required parameter: version")
	}

	objectTypeSchemas, ok := args["object_type_schemas"].(map[string]any)
	if !ok || len(objectTypeSchemas) == 0 {
		return nil, fmt.Errorf("missing required parameter: object_type_schemas")
	}

	description, _ := args["description"].(string)
	author, _ := args["author"].(string)
	relationshipTypeSchemas, _ := args["relationship_type_schemas"].(map[string]any)
	uiConfigs, _ := args["ui_configs"].(map[string]any)
	extractionPrompts, _ := args["extraction_prompts"].(map[string]any)

	if relationshipTypeSchemas == nil {
		relationshipTypeSchemas = make(map[string]any)
	}
	if uiConfigs == nil {
		uiConfigs = make(map[string]any)
	}
	if extractionPrompts == nil {
		extractionPrompts = make(map[string]any)
	}

	checksumContent := map[string]any{
		"object_type_schemas":       objectTypeSchemas,
		"relationship_type_schemas": relationshipTypeSchemas,
		"ui_configs":                uiConfigs,
		"extraction_prompts":        extractionPrompts,
	}
	checksumBytes, _ := json.Marshal(checksumContent)
	checksumHash := md5.Sum(checksumBytes)
	checksum := hex.EncodeToString(checksumHash[:])

	objectTypeSchemasJSON, _ := json.Marshal(objectTypeSchemas)
	relationshipTypeSchemasJSON, _ := json.Marshal(relationshipTypeSchemas)
	uiConfigsJSON, _ := json.Marshal(uiConfigs)
	extractionPromptsJSON, _ := json.Marshal(extractionPrompts)

	type packRow struct {
		ID          string    `bun:"id"`
		PublishedAt time.Time `bun:"published_at"`
		CreatedAt   time.Time `bun:"created_at"`
		UpdatedAt   time.Time `bun:"updated_at"`
	}

	var newPack packRow
	err := s.db.NewRaw(`
		INSERT INTO kb.graph_template_packs 
		(name, version, description, author, source, object_type_schemas, relationship_type_schemas, ui_configs, extraction_prompts, checksum)
		VALUES (?, ?, ?, ?, 'manual', ?, ?, ?, ?, ?)
		RETURNING id, published_at, created_at, updated_at
	`, name, version, description, author, string(objectTypeSchemasJSON), string(relationshipTypeSchemasJSON), string(uiConfigsJSON), string(extractionPromptsJSON), checksum).Scan(ctx, &newPack)

	if err != nil {
		return nil, fmt.Errorf("create template pack: %w", err)
	}

	result := CreateTemplatePackResult{
		Success: true,
		Pack: &TemplatePack{
			ID:                      newPack.ID,
			Name:                    name,
			Version:                 version,
			Description:             description,
			Author:                  author,
			Source:                  "manual",
			ObjectTypeSchemas:       objectTypeSchemas,
			RelationshipTypeSchemas: relationshipTypeSchemas,
			UIConfigs:               uiConfigs,
			ExtractionPrompts:       extractionPrompts,
			Checksum:                checksum,
			Draft:                   false,
			PublishedAt:             newPack.PublishedAt.Format(time.RFC3339),
			CreatedAt:               newPack.CreatedAt.Format(time.RFC3339),
			UpdatedAt:               newPack.UpdatedAt.Format(time.RFC3339),
		},
		Message: "Template pack created successfully",
	}

	return s.wrapResult(result)
}

func (s *Service) executeDeleteTemplatePack(ctx context.Context, args map[string]any) (*ToolResult, error) {
	packID, _ := args["pack_id"].(string)
	if packID == "" {
		return nil, fmt.Errorf("missing required parameter: pack_id")
	}

	type packRow struct {
		ID     string `bun:"id"`
		Name   string `bun:"name"`
		Source string `bun:"source"`
	}

	var pack packRow
	err := s.db.NewSelect().
		TableExpr("kb.graph_template_packs").
		Column("id", "name", "source").
		Where("id = ?", packID).
		Scan(ctx, &pack)

	if err != nil {
		return nil, fmt.Errorf("template pack not found: %s", packID)
	}

	if pack.Source == "system" {
		return nil, fmt.Errorf("cannot delete built-in template packs")
	}

	var installCount int
	err = s.db.NewRaw(`
		SELECT COUNT(*) FROM kb.project_template_packs WHERE template_pack_id = ?
	`, packID).Scan(ctx, &installCount)

	if err != nil {
		return nil, fmt.Errorf("check installations: %w", err)
	}

	if installCount > 0 {
		return nil, fmt.Errorf("cannot delete template pack \"%s\" because it is currently installed in %d project(s)", pack.Name, installCount)
	}

	_, err = s.db.NewRaw(`
		DELETE FROM kb.graph_template_packs WHERE id = ?
	`, packID).Exec(ctx)

	if err != nil {
		return nil, fmt.Errorf("delete template pack: %w", err)
	}

	result := DeleteTemplatePackResult{
		Success: true,
		PackID:  packID,
		Message: fmt.Sprintf("Template pack \"%s\" deleted successfully", pack.Name),
	}

	return s.wrapResult(result)
}

func (s *Service) executeCreateEntity(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	typeName, _ := args["type"].(string)
	if typeName == "" {
		return nil, fmt.Errorf("missing required parameter: type")
	}

	properties, _ := args["properties"].(map[string]any)

	var key *string
	if k, ok := args["key"].(string); ok && k != "" {
		key = &k
	}

	var status *string
	if st, ok := args["status"].(string); ok && st != "" {
		status = &st
	}

	var labels []string
	if l, ok := args["labels"].([]any); ok {
		for _, v := range l {
			if str, ok := v.(string); ok {
				labels = append(labels, str)
			}
		}
	}

	req := &graph.CreateGraphObjectRequest{
		Type:       typeName,
		Key:        key,
		Status:     status,
		Properties: properties,
		Labels:     labels,
	}

	result, err := s.graphService.Create(ctx, projectUUID, req, nil)
	if err != nil {
		return nil, fmt.Errorf("create entity: %w", err)
	}

	var statusStr string
	if result.Status != nil {
		statusStr = *result.Status
	}
	var keyStr string
	if result.Key != nil {
		keyStr = *result.Key
	}

	entityResult := CreateEntityResult{
		Success: true,
		Entity: &CreatedEntity{
			ID:          result.ID.String(),
			CanonicalID: result.CanonicalID.String(),
			Type:        result.Type,
			Key:         keyStr,
			Status:      statusStr,
			Properties:  result.Properties,
			Labels:      result.Labels,
			Version:     result.Version,
			CreatedAt:   result.CreatedAt.Format(time.RFC3339),
		},
		Message: fmt.Sprintf("Entity of type \"%s\" created successfully", typeName),
	}

	return s.wrapResult(entityResult)
}

func (s *Service) executeCreateRelationship(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	typeName, _ := args["type"].(string)
	if typeName == "" {
		return nil, fmt.Errorf("missing required parameter: type")
	}

	sourceIDStr, _ := args["source_id"].(string)
	if sourceIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}
	sourceID, err := uuid.Parse(sourceIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid source_id: %w", err)
	}

	targetIDStr, _ := args["target_id"].(string)
	if targetIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: target_id")
	}
	targetID, err := uuid.Parse(targetIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid target_id: %w", err)
	}

	properties, _ := args["properties"].(map[string]any)

	var weight *float32
	if w, ok := args["weight"].(float64); ok {
		wf := float32(w)
		weight = &wf
	}

	req := &graph.CreateGraphRelationshipRequest{
		Type:       typeName,
		SrcID:      sourceID,
		DstID:      targetID,
		Properties: properties,
		Weight:     weight,
	}

	result, err := s.graphService.CreateRelationship(ctx, projectUUID, req)
	if err != nil {
		return nil, fmt.Errorf("create relationship: %w", err)
	}

	var weightVal float64
	if result.Weight != nil {
		weightVal = float64(*result.Weight)
	}

	relResult := CreateRelationshipResult{
		Success: true,
		Relationship: &CreatedRelationship{
			ID:          result.ID.String(),
			CanonicalID: result.CanonicalID.String(),
			Type:        result.Type,
			SourceID:    result.SrcID.String(),
			TargetID:    result.DstID.String(),
			Properties:  result.Properties,
			Weight:      weightVal,
			Version:     result.Version,
			CreatedAt:   result.CreatedAt.Format(time.RFC3339),
		},
		Message: fmt.Sprintf("Relationship \"%s\" created successfully", typeName),
	}

	return s.wrapResult(relResult)
}

func (s *Service) executeUpdateEntity(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	entityIDStr, _ := args["entity_id"].(string)
	if entityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: entity_id")
	}
	entityID, err := uuid.Parse(entityIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid entity_id: %w", err)
	}

	properties, _ := args["properties"].(map[string]any)

	var status *string
	if st, ok := args["status"].(string); ok && st != "" {
		status = &st
	}

	var labels []string
	if l, ok := args["labels"].([]any); ok {
		for _, v := range l {
			if str, ok := v.(string); ok {
				labels = append(labels, str)
			}
		}
	}

	replaceLabels, _ := args["replace_labels"].(bool)

	req := &graph.PatchGraphObjectRequest{
		Properties:    properties,
		Labels:        labels,
		ReplaceLabels: replaceLabels,
		Status:        status,
	}

	result, err := s.graphService.Patch(ctx, projectUUID, entityID, req, nil)
	if err != nil {
		return nil, fmt.Errorf("update entity: %w", err)
	}

	var statusStr string
	if result.Status != nil {
		statusStr = *result.Status
	}
	var keyStr string
	if result.Key != nil {
		keyStr = *result.Key
	}

	entityResult := UpdateEntityResult{
		Success: true,
		Entity: &CreatedEntity{
			ID:          result.ID.String(),
			CanonicalID: result.CanonicalID.String(),
			Type:        result.Type,
			Key:         keyStr,
			Status:      statusStr,
			Properties:  result.Properties,
			Labels:      result.Labels,
			Version:     result.Version,
			CreatedAt:   result.CreatedAt.Format(time.RFC3339),
		},
		Message: "Entity updated successfully",
	}

	return s.wrapResult(entityResult)
}

func (s *Service) executeDeleteEntity(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	entityIDStr, _ := args["entity_id"].(string)
	if entityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: entity_id")
	}
	entityID, err := uuid.Parse(entityIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid entity_id: %w", err)
	}

	err = s.graphService.Delete(ctx, projectUUID, entityID, nil)
	if err != nil {
		return nil, fmt.Errorf("delete entity: %w", err)
	}

	result := DeleteEntityResult{
		Success:  true,
		EntityID: entityIDStr,
		Message:  "Entity deleted successfully",
	}

	return s.wrapResult(result)
}

func (s *Service) ReadResource(ctx context.Context, projectID, uri string) (*ResourceReadResult, error) {
	switch {
	case uri == "emergent://schema/entity-types":
		return s.readEntityTypesResource(ctx, projectID)
	case uri == "emergent://schema/relationships":
		return s.readRelationshipsResource(ctx, projectID)
	case uri == "emergent://templates/catalog":
		return s.readTemplatesCatalogResource(ctx)
	case strings.HasPrefix(uri, "emergent://project/") && strings.Contains(uri, "/metadata"):
		return s.readProjectMetadataResource(ctx, projectID)
	case strings.HasPrefix(uri, "emergent://project/") && strings.Contains(uri, "/recent-entities"):
		return s.readRecentEntitiesResource(ctx, projectID)
	case strings.HasPrefix(uri, "emergent://project/") && strings.Contains(uri, "/templates"):
		return s.readProjectTemplatesResource(ctx, projectID)
	default:
		return nil, fmt.Errorf("unknown resource URI: %s", uri)
	}
}

func (s *Service) readEntityTypesResource(ctx context.Context, projectID string) (*ResourceReadResult, error) {
	result, err := s.executeListEntityTypes(ctx, projectID)
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(result.Content[0].Text)
	if err != nil {
		return nil, err
	}

	return &ResourceReadResult{
		Contents: []ResourceContents{
			{
				URI:      "emergent://schema/entity-types",
				MimeType: "application/json",
				Text:     string(jsonData),
			},
		},
	}, nil
}

func (s *Service) readRelationshipsResource(ctx context.Context, projectID string) (*ResourceReadResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	var relationships []struct {
		Type     string `bun:"relationship_type"`
		FromType string `bun:"from_type"`
		ToType   string `bun:"to_type"`
		Count    int    `bun:"count"`
	}

	err = s.db.NewSelect().
		Table("kb.graph_relationships", "r").
		Column("r.relationship_type").
		Column("r.from_type").
		Column("r.to_type").
		ColumnExpr("COUNT(*) as count").
		Where("r.project_id = ?", projectUUID).
		Where("r.deleted_at IS NULL").
		Group("r.relationship_type", "r.from_type", "r.to_type").
		Scan(ctx, &relationships)

	if err != nil {
		return nil, fmt.Errorf("query relationships: %w", err)
	}

	jsonData, err := json.Marshal(map[string]any{
		"project_id":    projectID,
		"relationships": relationships,
		"total":         len(relationships),
		"timestamp":     time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}

	return &ResourceReadResult{
		Contents: []ResourceContents{
			{
				URI:      "emergent://schema/relationships",
				MimeType: "application/json",
				Text:     string(jsonData),
			},
		},
	}, nil
}

func (s *Service) readTemplatesCatalogResource(ctx context.Context) (*ResourceReadResult, error) {
	result, err := s.executeListTemplatePacks(ctx, map[string]any{})
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(result.Content[0].Text)
	if err != nil {
		return nil, err
	}

	return &ResourceReadResult{
		Contents: []ResourceContents{
			{
				URI:      "emergent://templates/catalog",
				MimeType: "application/json",
				Text:     string(jsonData),
			},
		},
	}, nil
}

func (s *Service) readProjectMetadataResource(ctx context.Context, projectID string) (*ResourceReadResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	var entityCount, relationshipCount int

	entityCount, err = s.db.NewSelect().
		Table("kb.graph_objects").
		Where("project_id = ?", projectUUID).
		Where("deleted_at IS NULL").
		Count(ctx)
	if err != nil {
		entityCount = 0
	}

	relationshipCount, err = s.db.NewSelect().
		Table("kb.graph_relationships").
		Where("project_id = ?", projectUUID).
		Where("deleted_at IS NULL").
		Count(ctx)
	if err != nil {
		relationshipCount = 0
	}

	jsonData, err := json.Marshal(map[string]any{
		"project_id":         projectID,
		"entity_count":       entityCount,
		"relationship_count": relationshipCount,
		"timestamp":          time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}

	return &ResourceReadResult{
		Contents: []ResourceContents{
			{
				URI:      fmt.Sprintf("emergent://project/%s/metadata", projectID),
				MimeType: "application/json",
				Text:     string(jsonData),
			},
		},
	}, nil
}

func (s *Service) readRecentEntitiesResource(ctx context.Context, projectID string) (*ResourceReadResult, error) {
	result, err := s.executeQueryEntities(ctx, projectID, map[string]any{
		"type_name":  "",
		"limit":      50,
		"offset":     0,
		"sort_by":    "updated_at",
		"sort_order": "desc",
	})
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(result.Content[0].Text)
	if err != nil {
		return nil, err
	}

	return &ResourceReadResult{
		Contents: []ResourceContents{
			{
				URI:      fmt.Sprintf("emergent://project/%s/recent-entities", projectID),
				MimeType: "application/json",
				Text:     string(jsonData),
			},
		},
	}, nil
}

func (s *Service) readProjectTemplatesResource(ctx context.Context, projectID string) (*ResourceReadResult, error) {
	result, err := s.executeGetInstalledTemplates(ctx, projectID)
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(result.Content[0].Text)
	if err != nil {
		return nil, err
	}

	return &ResourceReadResult{
		Contents: []ResourceContents{
			{
				URI:      fmt.Sprintf("emergent://project/%s/templates", projectID),
				MimeType: "application/json",
				Text:     string(jsonData),
			},
		},
	}, nil
}

func (s *Service) GetPrompt(ctx context.Context, projectID, name string, arguments map[string]any) (*PromptGetResult, error) {
	switch name {
	case "explore_entity_type":
		return s.getExploreEntityTypePrompt(arguments)
	case "create_from_template":
		return s.getCreateFromTemplatePrompt(arguments)
	case "analyze_relationships":
		return s.getAnalyzeRelationshipsPrompt(arguments)
	case "setup_research_project":
		return s.getSetupResearchProjectPrompt(arguments)
	case "find_related_entities":
		return s.getFindRelatedEntitiesPrompt(arguments)
	default:
		return nil, fmt.Errorf("unknown prompt: %s", name)
	}
}

func (s *Service) getExploreEntityTypePrompt(args map[string]any) (*PromptGetResult, error) {
	entityType, _ := args["entity_type"].(string)
	if entityType == "" {
		return nil, fmt.Errorf("missing required argument: entity_type")
	}

	return &PromptGetResult{
		Description: fmt.Sprintf("Explore %s entities in the knowledge graph", entityType),
		Messages: []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(`I want to explore %s entities in the knowledge graph.

Please help me:
1. Search for %s entities using advanced search tools
2. Show their key properties and metadata
3. Identify common relationship patterns
4. Suggest interesting entities to investigate further

Use the following tools (RECOMMENDED):
- hybrid_search with types=["%s"] - BEST option (combines full-text + semantic + graph context)
- semantic_search - Find conceptually similar entities
- find_similar entity_id="{id}" - Discover entities similar to a reference entity
- traverse_graph - Explore deep relationships (up to 5 hops)

Legacy alternatives (use only if hybrid_search is unavailable):
- query_entities with type_name="%s"
- search_entities for basic text search

Let's start by using hybrid_search to find the most relevant %s entities.`, entityType, entityType, entityType, entityType, entityType),
				},
			},
		},
	}, nil
}

func (s *Service) getCreateFromTemplatePrompt(args map[string]any) (*PromptGetResult, error) {
	entityType, _ := args["entity_type"].(string)
	if entityType == "" {
		return nil, fmt.Errorf("missing required argument: entity_type")
	}

	templatePack, _ := args["template_pack"].(string)
	templateHint := ""
	if templatePack != "" {
		templateHint = fmt.Sprintf(" using the '%s' template pack", templatePack)
	}

	return &PromptGetResult{
		Description: fmt.Sprintf("Create a new %s entity%s", entityType, templateHint),
		Messages: []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(`I want to create a new %s entity%s.

Please guide me through:
1. First, check available templates using get_available_templates
2. Show me the template schema using get_template_pack%s
3. Ask me for each required field one by one
4. Once we have all the data:
   - For SINGLE entity: use create_entity
   - For MULTIPLE entities (2+): use batch_create_entities (100x faster!)
5. Confirm creation and suggest next steps (e.g., adding relationships)

PERFORMANCE TIP: If creating multiple entities, use batch_create_entities instead of repeated create_entity calls.
It can handle up to 100 entities in one request, which is dramatically faster.

Let's start by checking what templates are available for %s entities.`, entityType, templateHint,
						func() string {
							if templatePack != "" {
								return fmt.Sprintf(" with name='%s'", templatePack)
							}
							return ""
						}(), entityType),
				},
			},
		},
	}, nil
}

func (s *Service) getAnalyzeRelationshipsPrompt(args map[string]any) (*PromptGetResult, error) {
	entityName, _ := args["entity_name"].(string)
	if entityName == "" {
		return nil, fmt.Errorf("missing required argument: entity_name")
	}

	return &PromptGetResult{
		Description: fmt.Sprintf("Analyze relationships for entity: %s", entityName),
		Messages: []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(`I want to analyze the relationships for "%s".

Please help me:
1. First, find the entity using hybrid_search or search_entities with query="%s"
2. Analyze relationships using OPTIMAL tools:
   - traverse_graph - RECOMMENDED for deep multi-hop exploration (handles recursion automatically)
   - get_entity_edges - For simple 1-hop neighborhood only
3. Categorize relationships by type (incoming vs outgoing)
4. Summarize the entity's position in the knowledge graph
5. Suggest related entities to explore using find_similar

RECOMMENDED APPROACH:
Use traverse_graph with max_depth=2 or 3 to get a complete picture of the entity's connections.
This is far more efficient than manually calling get_entity_edges multiple times.

Let's start by finding the entity.`, entityName, entityName),
				},
			},
		},
	}, nil
}

func (s *Service) getSetupResearchProjectPrompt(args map[string]any) (*PromptGetResult, error) {
	projectName, _ := args["project_name"].(string)
	if projectName == "" {
		return nil, fmt.Errorf("missing required argument: project_name")
	}

	methodology, _ := args["methodology"].(string)
	methodologyNote := ""
	if methodology != "" {
		methodologyNote = fmt.Sprintf("\nResearch methodology: %s", methodology)
	}

	return &PromptGetResult{
		Description: fmt.Sprintf("Set up research project: %s", projectName),
		Messages: []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(`I want to set up a complete research project called "%s".%s

Please help me create a structured research project with:
1. Main Project entity with research metadata
2. Phase-based Task entities (Setup, Data Collection, Analysis, Report)
3. Document placeholders for each phase
4. Relationships connecting everything (PART_OF, REFERENCES)

Workflow (OPTIMIZED):
1. Gather all entity data first (Project + all Tasks + Documents)
2. Use batch_create_entities to create ALL entities in ONE request (up to 100 entities)
   - This is 100x faster than individual create_entity calls
3. Gather all relationship data (PART_OF, REFERENCES)
4. Use batch_create_relationships to create ALL relationships in ONE request
5. Confirm creation and provide entity IDs for reference

CRITICAL PERFORMANCE TIP:
NEVER use individual create_entity or create_relationship calls in a loop!
Always collect data first, then use batch_create_* tools.

Example: For a project with 1 project entity + 4 tasks + 4 documents = 9 entities:
- OLD WAY: 9 separate create_entity calls (~9 seconds)
- NEW WAY: 1 batch_create_entities call (~0.09 seconds)

Ask me about:
- Research goal and objectives
- Timeline and milestones
- Required resources
- Success criteria

Let's start by defining the project's research goal.`, projectName, methodologyNote),
				},
			},
		},
	}, nil
}

func (s *Service) getFindRelatedEntitiesPrompt(args map[string]any) (*PromptGetResult, error) {
	entityName, _ := args["entity_name"].(string)
	if entityName == "" {
		return nil, fmt.Errorf("missing required argument: entity_name")
	}

	relationshipType, _ := args["relationship_type"].(string)
	depth := 1
	if depthFloat, ok := args["depth"].(float64); ok {
		depth = int(depthFloat)
	}

	filterNote := ""
	if relationshipType != "" {
		filterNote = fmt.Sprintf(" (filtering by relationship type: %s)", relationshipType)
	}

	return &PromptGetResult{
		Description: fmt.Sprintf("Find entities related to: %s", entityName),
		Messages: []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(`I want to discover all entities related to "%s"%s.

Search parameters:
- Depth: %d hop(s)
- Relationship filter: %s

Please:
1. Find the starting entity using hybrid_search or search_entities
2. Use traverse_graph for COMPLETE multi-hop exploration:
   - ONE call handles all depth levels automatically
   - Supports filtering by relationship type
   - Returns full graph structure
   - Much more efficient than manual recursion
3. Alternative (legacy): get_entity_edges%s (only for depth=1)
4. Group results by relationship type
5. Visualize the relationship graph structure
6. Suggest interesting patterns or insights using find_similar

RECOMMENDED APPROACH:
Use traverse_graph with these arguments:
{
  "start_entity_id": "uuid-from-search",
  "max_depth": %d,
  "direction": "both",  // or "outgoing"/"incoming"
  "relationship_types": %s  // optional filter
}

This single call replaces manual recursive exploration and is dramatically more efficient.

Let's begin by locating the entity.`, entityName, filterNote, depth,
						func() string {
							if relationshipType != "" {
								return relationshipType
							}
							return "all types"
						}(),
						func() string {
							if relationshipType != "" {
								return fmt.Sprintf(" (filter by type=%s)", relationshipType)
							}
							return ""
						}(),
						depth,
						func() string {
							if relationshipType != "" {
								return fmt.Sprintf(`["%s"]`, relationshipType)
							}
							return "null"
						}()),
				},
			},
		},
	}, nil
}

func (s *Service) executePreviewSchemaMigration(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	fromVersion, _ := args["from_version"].(string)
	toVersion, _ := args["to_version"].(string)
	sampleSize := 10
	if size, ok := args["sample_size"].(float64); ok {
		sampleSize = int(size)
	}

	if fromVersion == "" || toVersion == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Error: from_version and to_version are required"}},
		}, fmt.Errorf("from_version and to_version are required")
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Invalid project ID: %v", err)}},
		}, err
	}

	var objects []graph.GraphObject
	err = s.db.NewSelect().
		Model(&objects).
		Where("project_id = ?", projectUUID).
		Where("schema_version = ?", fromVersion).
		Limit(sampleSize).
		Scan(ctx)

	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Database error: %v", err)}},
		}, err
	}

	if len(objects) == 0 {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("No objects found with schema version %s", fromVersion)}},
		}, nil
	}

	output := fmt.Sprintf("Migration Preview: %s → %s (analyzing %d objects)\n\n", fromVersion, toVersion, len(objects))
	output += "⚠️  NOTE: This is a simplified preview. For full risk assessment, use the CLI:\n"
	output += fmt.Sprintf("  ./bin/migrate-schema -project %s -from %s -to %s\n\n", projectID, fromVersion, toVersion)
	output += fmt.Sprintf("Sample: Found %d objects with version %s\n", len(objects), fromVersion)
	output += "\n=== Next Steps ===\n"
	output += "1. Run CLI with -dry-run=true for detailed risk analysis\n"
	output += "2. Review dropped fields, type coercions, and validation errors\n"
	output += "3. If safe/cautious: Execute migration\n"
	output += "4. If risky/dangerous: Use --force or --confirm-data-loss flags\n\n"
	output += "CLI Commands:\n"
	output += fmt.Sprintf("  # Dry-run (safe, shows detailed analysis)\n")
	output += fmt.Sprintf("  ./bin/migrate-schema -project %s -from %s -to %s\n\n", projectID, fromVersion, toVersion)
	output += fmt.Sprintf("  # Execute (if safe)\n")
	output += fmt.Sprintf("  ./bin/migrate-schema -project %s -from %s -to %s -dry-run=false\n", projectID, fromVersion, toVersion)

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: output}},
	}, nil
}

func (s *Service) executeListMigrationArchives(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	offset := 0
	if o, ok := args["offset"].(float64); ok {
		offset = int(o)
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Invalid project ID: %v", err)}},
		}, err
	}

	var objects []graph.GraphObject
	err = s.db.NewSelect().
		Model(&objects).
		Where("project_id = ?", projectUUID).
		Where("migration_archive IS NOT NULL").
		Where("jsonb_array_length(migration_archive) > 0").
		Limit(limit).
		Offset(offset).
		Scan(ctx)

	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Database error: %v", err)}},
		}, err
	}

	if len(objects) == 0 {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "No objects found with migration archives"}},
		}, nil
	}

	output := fmt.Sprintf("Found %d objects with migration archives:\n\n", len(objects))
	for _, obj := range objects {
		archiveCount := len(obj.MigrationArchive)
		output += fmt.Sprintf("Object: %s\n", obj.ID)
		output += fmt.Sprintf("  Name: %s\n", obj.Properties["name"])
		output += fmt.Sprintf("  Type: %s\n", obj.Type)
		if obj.SchemaVersion != nil {
			output += fmt.Sprintf("  Current Version: %s\n", *obj.SchemaVersion)
		}
		output += fmt.Sprintf("  Archive Entries: %d\n", archiveCount)

		if archiveCount > 0 && len(obj.MigrationArchive) > 0 {
			latestArchive := obj.MigrationArchive[len(obj.MigrationArchive)-1]
			if fromVer, ok := latestArchive["from_version"].(string); ok {
				if toVer, ok := latestArchive["to_version"].(string); ok {
					output += fmt.Sprintf("  Latest Migration: %s → %s\n", fromVer, toVer)
				}
			}
		}
		output += "\n"
	}

	output += fmt.Sprintf("\nShowing %d-%d of available results\n", offset+1, offset+len(objects))
	output += "\nTo see detailed archive for a specific object, use: get_migration_archive\n"

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: output}},
	}, nil
}

func (s *Service) executeGetMigrationArchive(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	objectIDStr, _ := args["object_id"].(string)
	if objectIDStr == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Error: object_id is required"}},
		}, fmt.Errorf("object_id is required")
	}

	objectID, err := uuid.Parse(objectIDStr)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Invalid object_id: %v", err)}},
		}, err
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Invalid project ID: %v", err)}},
		}, err
	}

	var obj graph.GraphObject
	err = s.db.NewSelect().
		Model(&obj).
		Where("id = ?", objectID).
		Where("project_id = ?", projectUUID).
		Scan(ctx)

	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Object not found: %v", err)}},
		}, err
	}

	if len(obj.MigrationArchive) == 0 {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "No migration archive found for this object"}},
		}, nil
	}

	output := fmt.Sprintf("Migration Archive for Object: %s\n", obj.ID)
	output += fmt.Sprintf("Name: %s\n", obj.Properties["name"])
	output += fmt.Sprintf("Type: %s\n", obj.Type)
	if obj.SchemaVersion != nil {
		output += fmt.Sprintf("Current Version: %s\n\n", *obj.SchemaVersion)
	}

	output += fmt.Sprintf("Total Archive Entries: %d\n\n", len(obj.MigrationArchive))

	for i, entry := range obj.MigrationArchive {
		output += fmt.Sprintf("=== Archive Entry %d ===\n", i+1)
		if fromVer, ok := entry["from_version"].(string); ok {
			output += fmt.Sprintf("From Version: %s\n", fromVer)
		}
		if toVer, ok := entry["to_version"].(string); ok {
			output += fmt.Sprintf("To Version: %s\n", toVer)
		}
		if timestamp, ok := entry["timestamp"].(string); ok {
			output += fmt.Sprintf("Timestamp: %s\n", timestamp)
		}

		if droppedData, ok := entry["dropped_data"].(map[string]interface{}); ok {
			output += fmt.Sprintf("Dropped Fields (%d):\n", len(droppedData))
			for field, value := range droppedData {
				valueJSON, _ := json.Marshal(value)
				output += fmt.Sprintf("  - %s: %s\n", field, string(valueJSON))
			}
		}
		output += "\n"
	}

	output += "=== Rollback Instructions ===\n"
	output += "To restore dropped fields from a specific migration, use the CLI:\n\n"
	if len(obj.MigrationArchive) > 0 {
		latestArchive := obj.MigrationArchive[len(obj.MigrationArchive)-1]
		if toVer, ok := latestArchive["to_version"].(string); ok {
			output += fmt.Sprintf("  # Dry-run rollback (preview)\n")
			output += fmt.Sprintf("  ./bin/migrate-schema -project %s --rollback --rollback-version %s\n\n", projectID, toVer)
			output += fmt.Sprintf("  # Execute rollback\n")
			output += fmt.Sprintf("  ./bin/migrate-schema -project %s --rollback --rollback-version %s -dry-run=false\n", projectID, toVer)
		}
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: output}},
	}, nil
}

// delegateAgentTool dispatches agent-related tool calls to the AgentToolHandler.
func (s *Service) delegateAgentTool(ctx context.Context, projectID, toolName string, args map[string]any) (*ToolResult, error) {
	if s.agentToolHandler == nil {
		return nil, fmt.Errorf("agent tools not available: handler not configured")
	}

	switch toolName {
	// Agent Definitions
	case "list_agent_definitions":
		return s.agentToolHandler.ExecuteListAgentDefinitions(ctx, projectID, args)
	case "get_agent_definition":
		return s.agentToolHandler.ExecuteGetAgentDefinition(ctx, projectID, args)
	case "create_agent_definition":
		return s.agentToolHandler.ExecuteCreateAgentDefinition(ctx, projectID, args)
	case "update_agent_definition":
		return s.agentToolHandler.ExecuteUpdateAgentDefinition(ctx, projectID, args)
	case "delete_agent_definition":
		return s.agentToolHandler.ExecuteDeleteAgentDefinition(ctx, projectID, args)

	// Agents (runtime)
	case "list_agents":
		return s.agentToolHandler.ExecuteListAgents(ctx, projectID, args)
	case "get_agent":
		return s.agentToolHandler.ExecuteGetAgent(ctx, projectID, args)
	case "create_agent":
		return s.agentToolHandler.ExecuteCreateAgent(ctx, projectID, args)
	case "update_agent":
		return s.agentToolHandler.ExecuteUpdateAgent(ctx, projectID, args)
	case "delete_agent":
		return s.agentToolHandler.ExecuteDeleteAgent(ctx, projectID, args)
	case "trigger_agent":
		return s.agentToolHandler.ExecuteTriggerAgent(ctx, projectID, args)

	// Agent Runs
	case "list_agent_runs":
		return s.agentToolHandler.ExecuteListAgentRuns(ctx, projectID, args)
	case "get_agent_run":
		return s.agentToolHandler.ExecuteGetAgentRun(ctx, projectID, args)
	case "get_agent_run_messages":
		return s.agentToolHandler.ExecuteGetAgentRunMessages(ctx, projectID, args)
	case "get_agent_run_tool_calls":
		return s.agentToolHandler.ExecuteGetAgentRunToolCalls(ctx, projectID, args)

	// Agent Catalog
	case "list_available_agents":
		return s.agentToolHandler.ExecuteListAvailableAgents(ctx, projectID, args)

	default:
		return nil, fmt.Errorf("unknown agent tool: %s", toolName)
	}
}

// delegateRegistryTool dispatches MCP registry tool calls to the MCPRegistryToolHandler.
func (s *Service) delegateRegistryTool(ctx context.Context, projectID, toolName string, args map[string]any) (*ToolResult, error) {
	if s.mcpRegistryToolHandler == nil {
		return nil, fmt.Errorf("MCP registry tools not available: handler not configured")
	}

	switch toolName {
	case "list_mcp_servers":
		return s.mcpRegistryToolHandler.ExecuteListMCPServers(ctx, projectID, args)
	case "get_mcp_server":
		return s.mcpRegistryToolHandler.ExecuteGetMCPServer(ctx, projectID, args)
	case "create_mcp_server":
		return s.mcpRegistryToolHandler.ExecuteCreateMCPServer(ctx, projectID, args)
	case "update_mcp_server":
		return s.mcpRegistryToolHandler.ExecuteUpdateMCPServer(ctx, projectID, args)
	case "delete_mcp_server":
		return s.mcpRegistryToolHandler.ExecuteDeleteMCPServer(ctx, projectID, args)
	case "toggle_mcp_server_tool":
		return s.mcpRegistryToolHandler.ExecuteToggleMCPServerTool(ctx, projectID, args)
	case "sync_mcp_server_tools":
		return s.mcpRegistryToolHandler.ExecuteSyncMCPServerTools(ctx, projectID, args)
	case "search_mcp_registry":
		return s.mcpRegistryToolHandler.ExecuteSearchMCPRegistry(ctx, projectID, args)
	case "get_mcp_registry_server":
		return s.mcpRegistryToolHandler.ExecuteGetMCPRegistryServer(ctx, projectID, args)
	case "install_mcp_from_registry":
		return s.mcpRegistryToolHandler.ExecuteInstallMCPFromRegistry(ctx, projectID, args)
	case "inspect_mcp_server":
		return s.mcpRegistryToolHandler.ExecuteInspectMCPServer(ctx, projectID, args)
	default:
		return nil, fmt.Errorf("unknown MCP registry tool: %s", toolName)
	}
}
