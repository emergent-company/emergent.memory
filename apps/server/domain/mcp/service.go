package mcp

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/database"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
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

	// Brave Search API configuration
	braveSearchAPIKey  string
	braveSearchTimeout time.Duration

	// Schema version caching
	cacheMu       sync.RWMutex
	cachedVersion string
	cacheExpiry   time.Time

	// Documents service (for document list/get/upload/delete tools)
	documentsSvc *documents.Service

	// Storage service (for document upload to object store)
	storageSvc *storage.Service

	// Skills repository (for skill CRUD tools)
	skillsRepo *skills.Repository

	// Provider services (for provider config and model catalog tools)
	providerCredSvc    *provider.CredentialService
	providerCatalogSvc *provider.ModelCatalogService

	// API token service (for token management tools)
	apitokenSvc *apitoken.Service

	// Embedding worker controller (for pause/resume/config tools)
	// Typed as interface to avoid import cycle with extraction package.
	embeddingCtl EmbeddingControlHandler

	// Tempo base URL for trace proxy (empty when tracing disabled)
	tempoBaseURL string

	// Server port for internal query calls (query_knowledge tool)
	serverPort int
}

// ServiceParams bundles optional dependencies for NewService.
type ServiceParams struct {
	fx.In

	DB           bun.IDB
	GraphService *graph.Service
	SearchSvc    *search.Service
	Cfg          *config.Config
	Log          *slog.Logger

	DocumentsSvc       *documents.Service
	SkillsRepo         *skills.Repository
	ProviderCredSvc    *provider.CredentialService
	ProviderCatalogSvc *provider.ModelCatalogService
	ApitokenSvc        *apitoken.Service
}

// NewService creates a new MCP service
func NewService(p ServiceParams) *Service {
	cfg := p.Cfg
	timeout := cfg.BraveSearch.Timeout
	if timeout == 0 {
		timeout = braveSearchDefaultTimeout
	}
	tempoURL := ""
	if cfg.Otel.Enabled() {
		tempoURL = cfg.Otel.InternalTempoQueryURL()
	}
	return &Service{
		db:                 p.DB,
		graphService:       p.GraphService,
		searchSvc:          p.SearchSvc,
		braveSearchAPIKey:  cfg.BraveSearch.APIKey,
		braveSearchTimeout: timeout,
		log:                p.Log.With(logger.Scope("mcp.svc")),
		documentsSvc:       p.DocumentsSvc,
		skillsRepo:         p.SkillsRepo,
		providerCredSvc:    p.ProviderCredSvc,
		providerCatalogSvc: p.ProviderCatalogSvc,
		apitokenSvc:        p.ApitokenSvc,
		tempoBaseURL:       tempoURL,
		serverPort:         cfg.ServerPort,
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

// SetEmbeddingControlHandler sets the embedding worker controller (injected to break import cycle with extraction).
func (s *Service) SetEmbeddingControlHandler(h EmbeddingControlHandler) {
	s.embeddingCtl = h
}

// GetToolDefinitions returns all available MCP tools
func (s *Service) GetToolDefinitions() []ToolDefinition {
	tools := []ToolDefinition{
		{
			Name:        "project-get",
			Description: "Returns the project info document — a markdown document describing this knowledge base's purpose, goals, audience, and context. Call this to understand what this project is about before working with its data.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "project-create",
			Description: "Create a new project under the authenticated user's organization. Returns the new project's id, name, and orgId. If org_id is omitted it is resolved from the caller's authentication context.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Name of the new project",
					},
					"org_id": {
						Type:        "string",
						Description: "UUID of the organization to create the project under. Optional — defaults to the caller's organization from auth context.",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "schema-version",
			Description: "Get the current schema version and metadata. Returns version hash, timestamp, total types, and relationships.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "entity-type-list",
			Description: "List all available entity types in the knowledge graph with instance counts. Helps discover what entities can be queried.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "entity-query",
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
					"include_relationships": {
						Type:        "boolean",
						Description: "When true, each entity result includes its outgoing relationships (type, target_id, target_type, target_key). Eliminates the need for a separate traverse_graph call in simple cases.",
						Default:     false,
					},
				},
				Required: []string{"type_name"},
			},
		},
		{
			Name:        "entity-search",
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
			Name:        "entity-edges-get",
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
			Name:        "schema-list",
			Description: "List all available memory schemas in the global registry. Schemas define object types, relationships, and extraction prompts for knowledge graph entities.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"search": {
						Type:        "string",
						Description: "Optional search term to filter schemas by name or description",
					},
					"include_deprecated": {
						Type:        "boolean",
						Description: "Include deprecated schemas (default: false)",
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
			Name:        "schema-get",
			Description: "Get detailed information about a specific memory schema including all type definitions, UI configs, and extraction prompts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"pack_id": {
						Type:        "string",
						Description: "The UUID of the schema to retrieve",
					},
				},
				Required: []string{"pack_id"},
			},
		},
		{
			Name:        "schema-list-available",
			Description: "Get all schemas available for a project with their installation status. Shows which schemas are installed, active, and their object type counts.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "schema-list-installed",
			Description: "Get all schemas currently installed in the project with their configuration and active status.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "schema-assign",
			Description: "Install a memory schema to the project. This registers the schema's object types in the project's type registry, making them available for entity creation and extraction.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"schema_id": {
						Type:        "string",
						Description: "The UUID of the schema to install",
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
				Required: []string{"schema_id"},
			},
		},
		{
			Name:        "schema-assignment-update",
			Description: "Update a schema assignment. Toggle active status or modify customizations.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"assignment_id": {
						Type:        "string",
						Description: "The UUID of the schema assignment to update",
					},
					"active": {
						Type:        "boolean",
						Description: "Set the active status of the schema",
					},
				},
				Required: []string{"assignment_id"},
			},
		},
		{
			Name:        "schema-uninstall",
			Description: "Remove a memory schema from the project. This will fail if any objects still exist using types from this schema.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"assignment_id": {
						Type:        "string",
						Description: "The UUID of the schema assignment to remove",
					},
				},
				Required: []string{"assignment_id"},
			},
		},
		{
			Name:        "schema-create",
			Description: "Create a new memory schema in the global registry. Requires object type schemas at minimum.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Name of the schema",
					},
					"version": {
						Type:        "string",
						Description: "Version string (e.g., \"1.0.0\")",
					},
					"description": {
						Type:        "string",
						Description: "Description of the schema",
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
			Name:        "schema-delete",
			Description: "Delete a memory schema from the global registry. Cannot delete system schemas or schemas that are currently installed in any project.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"pack_id": {
						Type:        "string",
						Description: "The UUID of the memory schema to delete",
					},
				},
				Required: []string{"pack_id"},
			},
		},
		{
			Name:        "entity-create",
			Description: "Create one or more entities (graph objects) in the project. Always pass an 'entities' array — use a single-element array for one entity. Each entity type should match a type defined in an installed schema. Returns slim {id, type, key} per entity. Each entity spec may include an optional 'relationships' array to create outgoing relationships atomically in the same call, avoiding a separate create_relationship call.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"entities": {
						Type:        "array",
						Description: "Array of entity specifications to create. Each item: {type (required), properties, key, status, labels, relationships?: [{type, source_id|target_id, properties}]}. Use source_id when the pre-existing entity should be the relationship source (source_id→new_entity). Use target_id when the new entity should be the source (new_entity→target_id).",
					},
				},
				Required: []string{"entities"},
			},
		},
		{
			Name:        "relationship-create",
			Description: "Create one or more relationships between entities. Always pass a 'relationships' array — use a single-element array for one relationship. Each relationship type should match a type defined in an installed schema. Returns slim {id, type, source_id, target_id} per relationship. TIP: for creating an entity and linking it in one call, use the 'relationships' field on the entity spec in create_entity instead.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"relationships": {
						Type:        "array",
						Description: "Array of relationship specifications to create. Each item: {type (required), source_id (required), target_id (required), properties, weight}",
					},
				},
				Required: []string{"relationships"},
			},
		},
		{
			Name:        "entity-update",
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
			Name:        "entity-delete",
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
			Name:        "entity-restore",
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
			Name:        "search-hybrid",
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
			Name:        "search-semantic",
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
			Name:        "search-similar",
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
			Name:        "graph-traverse",
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
			Name:        "relationship-list",
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
			Name:        "relationship-update",
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
			Name:        "relationship-delete",
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
			Name:        "tag-list",
			Description: "Get all unique tags/labels used in the project with counts.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "schema-migration-preview",
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
			Name:        "migration-archive-list",
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
			Name:        "migration-archive-get",
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

	// Always expose Brave Search — execution falls back to env key, then project/org
	// config. If no key is available at any tier the tool returns a clear error message.
	tools = append(tools, getBraveSearchToolDefinition())

	// Always expose webfetch — no API key required, fetches any public URL.
	tools = append(tools, getWebFetchToolDefinition())

	// Expose Reddit search — returns error if credentials not configured.
	tools = append(tools, getRedditSearchToolDefinition())

	// Append agent tool definitions if handler is available
	if s.agentToolHandler != nil {
		tools = append(tools, s.agentToolHandler.GetAgentToolDefinitions()...)
	}

	// Append agent extension tools (questions, hooks, ADK sessions)
	tools = append(tools, agentExtToolDefinitions()...)

	// Append MCP registry tool definitions if handler is available
	if s.mcpRegistryToolHandler != nil {
		tools = append(tools, s.mcpRegistryToolHandler.GetMCPRegistryToolDefinitions()...)
	}

	// Append new domain tool definitions
	tools = append(tools, skillsToolDefinitions()...)
	tools = append(tools, documentsToolDefinitions()...)
	tools = append(tools, embeddingsToolDefinitions()...)
	tools = append(tools, providerToolDefinitions()...)
	tools = append(tools, tokenToolDefinitions()...)
	tools = append(tools, traceToolDefinitions()...)
	tools = append(tools, queryToolDefinitions()...)

	return tools
}

// GetToolDefinitionsForProject returns tool definitions with dynamic content (e.g. agent
// catalog injected into trigger_agent description) for a specific project.
// Falls back to GetToolDefinitions when projectID is empty.
func (s *Service) GetToolDefinitionsForProject(ctx context.Context, projectID string) []ToolDefinition {
	if projectID == "" || s.agentToolHandler == nil {
		return s.GetToolDefinitions()
	}

	// Start with the full static tool list (includes static agent tools)
	tools := s.GetToolDefinitions()

	// Find the range of agent tools and replace them with project-enriched versions
	enriched := s.agentToolHandler.GetAgentToolDefinitionsForProject(ctx, projectID)
	enrichedByName := make(map[string]ToolDefinition, len(enriched))
	for _, t := range enriched {
		enrichedByName[t.Name] = t
	}

	for i, tool := range tools {
		if et, ok := enrichedByName[tool.Name]; ok {
			tools[i] = et
		}
	}

	return tools
}

func (s *Service) GetResourceDefinitions() []ResourceDefinition {
	return []ResourceDefinition{
		{
			URI:         "memory://schema/entity-types",
			Name:        "Entity Type Schema",
			Description: "Complete catalog of all available entity types in the knowledge graph with their counts and relationship types",
			MimeType:    "application/json",
		},
		{
			URI:         "memory://schema/relationships",
			Name:        "Relationship Types Registry",
			Description: "All valid relationship types, their constraints, and usage statistics",
			MimeType:    "application/json",
		},
		{
			URI:         "memory://templates/catalog",
			Name:        "Schema Catalog",
			Description: "Available memory schemas with descriptions, object types, and metadata",
			MimeType:    "application/json",
		},
		{
			URI:         "memory://project/{project_id}/metadata",
			Name:        "Project Metadata",
			Description: "Current project information including entity counts, active templates, and statistics",
			MimeType:    "application/json",
		},
		{
			URI:         "memory://project/{project_id}/recent-entities",
			Name:        "Recent Entities",
			Description: "Recently created or modified entities for context (last 50)",
			MimeType:    "application/json",
		},
		{
			URI:         "memory://project/{project_id}/templates",
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
			Description: "Step-by-step workflow for creating a new entity using a schema",
			Arguments: []PromptArgument{
				{
					Name:        "entity_type",
					Description: "Type of entity to create",
					Required:    true,
				},
				{
					Name:        "schema",
					Description: "Memory schema to use (optional, will suggest if not provided)",
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
	case "project-get":
		return s.executeGetProjectInfo(ctx, projectID)
	case "project-create":
		return s.executeCreateProject(ctx, args)
	case "schema-version":
		return s.executeSchemaVersion(ctx)
	case "entity-type-list":
		return s.executeListEntityTypes(ctx, projectID)
	case "entity-query":
		return s.executeQueryEntities(ctx, projectID, args)
	case "entity-search":
		return s.executeSearchEntities(ctx, projectID, args)
	case "entity-edges-get":
		return s.executeGetEntityEdges(ctx, projectID, args)
	case "schema-list":
		return s.executeListSchemas(ctx, args)
	case "schema-get":
		return s.executeGetSchema(ctx, args)
	case "schema-list-available":
		return s.executeGetAvailableTemplates(ctx, projectID)
	case "schema-list-installed":
		return s.executeGetInstalledTemplates(ctx, projectID)
	case "schema-assign":
		return s.executeAssignSchema(ctx, projectID, args)
	case "schema-assignment-update":
		return s.executeUpdateTemplateAssignment(ctx, projectID, args)
	case "schema-uninstall":
		return s.executeUninstallSchema(ctx, projectID, args)
	case "schema-create":
		return s.executeCreateSchema(ctx, args)
	case "schema-delete":
		return s.executeDeleteSchema(ctx, args)
	case "entity-create":
		return s.executeBatchCreateEntities(ctx, projectID, args)
	case "relationship-create":
		return s.executeBatchCreateRelationships(ctx, projectID, args)
	case "entity-update":
		return s.executeUpdateEntity(ctx, projectID, args)
	case "entity-delete":
		return s.executeDeleteEntity(ctx, projectID, args)
	case "entity-restore":
		return s.executeRestoreEntity(ctx, projectID, args)
	case "search-hybrid":
		return s.executeHybridSearch(ctx, projectID, args)
	case "search-semantic":
		return s.executeSemanticSearch(ctx, projectID, args)
	case "search-similar":
		return s.executeFindSimilar(ctx, projectID, args)
	case "graph-traverse":
		return s.executeTraverseGraph(ctx, projectID, args)
	case "relationship-list":
		return s.executeListRelationships(ctx, projectID, args)
	case "relationship-update":
		return s.executeUpdateRelationship(ctx, projectID, args)
	case "relationship-delete":
		return s.executeDeleteRelationship(ctx, projectID, args)
	case "tag-list":
		return s.executeListTags(ctx, projectID)
	case "schema-migration-preview":
		return s.executePreviewSchemaMigration(ctx, projectID, args)
	case "migration-archive-list":
		return s.executeListMigrationArchives(ctx, projectID, args)
	case "migration-archive-get":
		return s.executeGetMigrationArchive(ctx, projectID, args)

	// Web tools
	case "web-search-brave":
		return s.executeBraveWebSearch(ctx, projectID, args)
	case "web-fetch":
		return s.executeWebFetch(ctx, args)
	case "web-search-reddit":
		return s.executeRedditSearch(ctx, projectID, args)

	// Agent Definition tools
	case "agent-def-list":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-def-get":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-def-create":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "update_agent_definition":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-def-delete":
		return s.delegateAgentTool(ctx, projectID, toolName, args)

	// Agent (runtime) tools
	case "agent-list":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-get":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-create":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "update_agent":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-delete":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "trigger_agent":
		return s.delegateAgentTool(ctx, projectID, toolName, args)

	// Agent Run tools
	case "agent-run-list":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-run-get":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-run-messages":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-run-tool-calls":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "agent-run-status":
		return s.delegateAgentTool(ctx, projectID, toolName, args)

	// Agent Catalog tools
	case "agent-list-available":
		return s.delegateAgentTool(ctx, projectID, toolName, args)

	// MCP Registry tools
	case "mcp-server-list":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "mcp-server-get":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "mcp-server-create":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "update_mcp_server":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "mcp-server-delete":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "toggle_mcp_server_tool":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "sync_mcp_server_tools":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)

	// Official MCP Registry browse/install tools
	case "search_mcp_registry":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "mcp-registry-get":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "mcp-registry-install":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)
	case "mcp-server-inspect":
		return s.delegateRegistryTool(ctx, projectID, toolName, args)

	// Agent Questions, Hooks, and ADK Sessions
	case "agent-question-list":
		return s.executeListAgentQuestions(ctx, projectID, args)
	case "agent-question-list-project":
		return s.executeListProjectAgentQuestions(ctx, projectID, args)
	case "agent-question-respond":
		return s.executeRespondToAgentQuestion(ctx, projectID, args)
	case "agent-hook-list":
		return s.executeListAgentHooks(ctx, projectID, args)
	case "agent-hook-create":
		return s.executeCreateAgentHook(ctx, projectID, args)
	case "agent-hook-delete":
		return s.executeDeleteAgentHook(ctx, projectID, args)
	case "adk-session-list":
		return s.executeListADKSessions(ctx, projectID, args)
	case "adk-session-get":
		return s.executeGetADKSession(ctx, projectID, args)

	// Skills tools
	case "skill-list":
		return s.executeListSkills(ctx, projectID)
	case "skill-get":
		return s.executeGetSkill(ctx, args)
	case "skill-create":
		return s.executeCreateSkill(ctx, projectID, args)
	case "skill-update":
		return s.executeUpdateSkill(ctx, args)
	case "skill-delete":
		return s.executeDeleteSkill(ctx, args)

	// Documents tools
	case "document-list":
		return s.executeListDocuments(ctx, projectID, args)
	case "document-get":
		return s.executeGetDocument(ctx, projectID, args)
	case "document-upload":
		return s.executeUploadDocument(ctx, projectID, args)
	case "document-delete":
		return s.executeDeleteDocument(ctx, projectID, args)

	// Embeddings tools
	case "embedding-status":
		return s.executeGetEmbeddingStatus(ctx)
	case "embedding-pause":
		return s.executePauseEmbeddings(ctx)
	case "embedding-resume":
		return s.executeResumeEmbeddings(ctx)
	case "embedding-config-update":
		return s.executeUpdateEmbeddingConfig(ctx, args)

	// Provider tools
	case "provider-list-org":
		return s.executeListOrgProviders(ctx, args)
	case "provider-configure-org":
		return s.executeConfigureOrgProvider(ctx, args)
	case "provider-configure-project":
		return s.executeConfigureProjectProvider(ctx, projectID, args)
	case "provider-models-list":
		return s.executeListProviderModels(ctx, args)
	case "provider-usage-get":
		return s.executeGetProviderUsage(ctx, args)
	case "provider-test":
		return s.executeTestProvider(ctx, args)

	// API Token tools
	case "token-list":
		return s.executeListProjectAPITokens(ctx, projectID)
	case "token-create":
		return s.executeCreateProjectAPIToken(ctx, projectID, args)
	case "token-get":
		return s.executeGetProjectAPIToken(ctx, projectID, args)
	case "token-revoke":
		return s.executeRevokeProjectAPIToken(ctx, projectID, args)

	// Trace tools
	case "trace-list":
		return s.executeListTraces(ctx, args)
	case "trace-get":
		return s.executeGetTrace(ctx, args)

	// Query Knowledge tool
	case "search-knowledge":
		return s.executeQueryKnowledge(ctx, projectID, args)

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

	// Count schemas
	var packCount int
	err = s.db.NewSelect().
		TableExpr("kb.graph_schemas").
		ColumnExpr("COUNT(*)").
		Scan(ctx, &packCount)
	if err != nil {
		s.log.Warn("failed to count schemas", logger.Error(err))
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

// executeGetProjectInfo returns the project info document for the given project.
func (s *Service) executeGetProjectInfo(ctx context.Context, projectID string) (*ToolResult, error) {
	var info *string
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("project_info").
		Where("id = ?", projectID).
		Scan(ctx, &info)
	if err != nil {
		return nil, fmt.Errorf("get project info: %w", err)
	}

	if info == nil || *info == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "No project info has been configured for this project."}},
		}, nil
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: *info}},
	}, nil
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
			FROM kb.project_object_schema_registry tr
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
		CanonicalID     uuid.UUID      `bun:"canonical_id"`
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

		// Query entities (latest version only: supersedes_id IS NULL means no newer version exists,
		// branch_id IS NULL restricts to the main branch)
		err := tx.NewRaw(`
			SELECT 
				go.id,
				go.canonical_id,
				go.key,
				COALESCE(go.properties->>'name', '') as name,
				go.properties,
				go.created_at,
				COALESCE(go.updated_at, go.created_at) as updated_at,
				go.type as type_name,
				COALESCE(tr.description, '') as type_description
			FROM kb.graph_objects go
			LEFT JOIN kb.project_object_schema_registry tr ON tr.type_name = go.type AND tr.project_id = go.project_id
			WHERE go.type = ?
				AND go.deleted_at IS NULL
				AND go.project_id = ?
				AND go.supersedes_id IS NULL
				AND go.branch_id IS NULL
			ORDER BY `+orderExpr+`
			LIMIT ? OFFSET ?
		`, typeName, projectUUID, limit, offset).Scan(ctx, &entities)
		if err != nil {
			return err
		}

		// Get total count (latest version only, main branch)
		err = tx.NewRaw(`
			SELECT COUNT(*)
			FROM kb.graph_objects go
			WHERE go.type = ?
				AND go.deleted_at IS NULL
				AND go.project_id = ?
				AND go.supersedes_id IS NULL
				AND go.branch_id IS NULL
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
			ID:         e.CanonicalID.String(),
			Key:        e.Key,
			Name:       e.Name,
			Type:       e.TypeName,
			Properties: e.Properties,
			CreatedAt:  e.CreatedAt,
			UpdatedAt:  e.UpdatedAt,
		}
	}

	// Detect unrecognized parameters and surface them as a warning so callers
	// know their extra keys (e.g. filter, status, entity_type) had no effect.
	knownQueryEntitiesParams := map[string]struct{}{
		"type_name": {}, "limit": {}, "offset": {}, "sort_by": {}, "sort_order": {}, "include_relationships": {},
	}
	var unknownParams []string
	for k := range args {
		if _, known := knownQueryEntitiesParams[k]; !known {
			unknownParams = append(unknownParams, k)
		}
	}
	var queryEntitiesWarning string
	if len(unknownParams) > 0 {
		sort.Strings(unknownParams)
		queryEntitiesWarning = fmt.Sprintf(
			"unrecognized parameters ignored (query_entities does not support server-side filtering): %s. "+
				"Filter results client-side by inspecting entity properties.",
			strings.Join(unknownParams, ", "),
		)
	}

	// Optionally enrich each entity with its outgoing relationships.
	includeRels, _ := args["include_relationships"].(bool)
	if includeRels && len(resultEntities) > 0 {
		// Build a list of canonical IDs to query relationships for.
		canonicalIDs := make([]uuid.UUID, len(resultEntities))
		for i, e := range resultEntities {
			id, _ := uuid.Parse(e.ID)
			canonicalIDs[i] = id
		}

		type relRow struct {
			SrcID   uuid.UUID `bun:"src_id"`
			RelType string    `bun:"rel_type"`
			DstID   uuid.UUID `bun:"dst_id"`
			DstType string    `bun:"dst_type"`
			DstKey  string    `bun:"dst_key"`
		}
		var relRows []relRow

		_ = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
				return err
			}
			return tx.NewRaw(`
				SELECT
					gr.src_id,
					gr.type AS rel_type,
					gr.dst_id,
					dst.type AS dst_type,
					COALESCE(dst.key, '') AS dst_key
				FROM kb.graph_relationships gr
				JOIN kb.graph_objects dst ON dst.canonical_id = gr.dst_id
					AND dst.supersedes_id IS NULL AND dst.branch_id IS NULL AND dst.deleted_at IS NULL
				WHERE gr.src_id = ANY(?)
					AND gr.deleted_at IS NULL
					AND gr.project_id = ?
					AND gr.supersedes_id IS NULL
					AND gr.branch_id IS NULL
			`, bun.In(canonicalIDs), projectUUID).Scan(ctx, &relRows)
		})

		// Build an index from entity ID → edges
		type edgeRef struct {
			Type       string `json:"type"`
			TargetID   string `json:"target_id"`
			TargetType string `json:"target_type"`
			TargetKey  string `json:"target_key,omitempty"`
		}
		edgesByEntityID := make(map[string][]edgeRef, len(resultEntities))
		for _, row := range relRows {
			srcStr := row.SrcID.String()
			edgesByEntityID[srcStr] = append(edgesByEntityID[srcStr], edgeRef{
				Type:       row.RelType,
				TargetID:   row.DstID.String(),
				TargetType: row.DstType,
				TargetKey:  row.DstKey,
			})
		}

		// Attach to result entities via a wrapper type that includes edges.
		type entityWithRels struct {
			Entity
			Relationships []edgeRef `json:"relationships,omitempty"`
		}
		enriched := make([]entityWithRels, len(resultEntities))
		for i, e := range resultEntities {
			enriched[i] = entityWithRels{
				Entity:        e,
				Relationships: edgesByEntityID[e.ID],
			}
		}

		result := struct {
			ProjectID  string           `json:"projectId"`
			Entities   []entityWithRels `json:"entities"`
			Pagination *PaginationInfo  `json:"pagination"`
			Warning    string           `json:"warning,omitempty"`
		}{
			ProjectID:  projectID,
			Entities:   enriched,
			Pagination: &PaginationInfo{Total: total, Limit: limit, Offset: offset, HasMore: offset+limit < total},
			Warning:    queryEntitiesWarning,
		}
		return s.wrapResult(result)
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
		Warning: queryEntitiesWarning,
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

	// Resolve the canonical ID for relationship traversal
	canonicalID, err := s.resolveCanonicalID(ctx, projectUUID, entityID)
	if err == nil && canonicalID != uuid.Nil {
		entityID = canonicalID
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

// resolveCanonicalID looks up the canonical ID for a given entity version ID.
func (s *Service) resolveCanonicalID(ctx context.Context, projectID, entityID uuid.UUID) (uuid.UUID, error) {
	var canonicalID uuid.UUID
	err := s.db.NewRaw(`
		SELECT canonical_id FROM kb.graph_objects 
		WHERE (id = ? OR canonical_id = ?) AND project_id = ? 
		LIMIT 1
	`, entityID, entityID, projectID).Scan(ctx, &canonicalID)

	if err != nil {
		return uuid.Nil, err
	}
	return canonicalID, nil
}

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

	// Resolve the canonical ID for relationship traversal
	canonicalID, err := s.resolveCanonicalID(ctx, projectUUID, startEntityID)
	if err == nil && canonicalID != uuid.Nil {
		startEntityID = canonicalID
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
		// Normalize human-readable aliases to the values expected by ExpandGraph.
		switch d {
		case "outgoing":
			direction = "out"
		case "incoming":
			direction = "in"
		default:
			direction = d
		}
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

// executeBatchCreateEntities creates one or more entities. Always expects an "entities" array.
// Backward-compat: if flat fields (type, properties, etc.) are passed instead, wraps them as a single-element array.
//
// Each entity spec may include an optional "relationships" array to atomically create relationships
// from the new entity to existing entities in the same call:
//
//	{
//	  "type": "TaskResult",
//	  "properties": {...},
//	  "relationships": [
//	    {"type": "has_result", "target_id": "<existing-entity-id>", "properties": {}}
//	  ]
//	}
func (s *Service) executeBatchCreateEntities(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	// Support flat single-entity form for backward compat
	var entitiesRaw []any
	if arr, ok := args["entities"].([]any); ok {
		entitiesRaw = arr
	} else if _, hasType := args["type"]; hasType {
		entitiesRaw = []any{args}
	}

	if len(entitiesRaw) == 0 {
		return nil, fmt.Errorf("missing or empty entities array")
	}

	if len(entitiesRaw) > 100 {
		return nil, fmt.Errorf("batch size exceeded: maximum 100 entities per request")
	}

	// slimEntity is the minimal response for a created entity.
	type slimRelationship struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		TargetID string `json:"target_id"`
	}
	type slimEntity struct {
		ID            string             `json:"id"`
		Type          string             `json:"type"`
		Key           string             `json:"key,omitempty"`
		Relationships []slimRelationship `json:"relationships,omitempty"`
	}
	type batchResult struct {
		Success bool        `json:"success"`
		Entity  *slimEntity `json:"entity,omitempty"`
		Error   string      `json:"error,omitempty"`
		Index   int         `json:"index"`
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

		properties, _ := entityMap["properties"].(map[string]any)
		if properties == nil {
			properties = make(map[string]any)
		}

		// status flows through properties JSONB; the graph layer syncs the
		// status column from properties["status"] at creation time.
		// Also support top-level "status" field on the entity map — mirror it
		// into properties so it is stored in JSONB.
		var status *string
		if st, ok := entityMap["status"].(string); ok && st != "" {
			status = &st
			properties["status"] = st
		} else if st, ok := properties["status"].(string); ok && st != "" {
			status = &st
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

		slim := &slimEntity{
			ID:   result.CanonicalID.String(),
			Type: result.Type,
		}
		if result.Key != nil {
			slim.Key = *result.Key
		}

		// Create any inline relationships declared on this entity spec.
		if relsRaw, ok := entityMap["relationships"].([]any); ok && len(relsRaw) > 0 {
			for _, relRaw := range relsRaw {
				relMap, ok := relRaw.(map[string]any)
				if !ok {
					continue
				}
				relType, _ := relMap["type"].(string)
				if relType == "" {
					continue
				}
				relProps, _ := relMap["properties"].(map[string]any)

				// Support both source_id and target_id in inline relationship specs.
				// source_id: the pre-existing entity is the src (source_id → new_entity as dst).
				// target_id: the new entity is the src (new_entity → target_id as dst).
				var relSrcID, relDstID uuid.UUID
				if sourceIDStr, ok := relMap["source_id"].(string); ok && sourceIDStr != "" {
					sourceID, err := uuid.Parse(sourceIDStr)
					if err != nil {
						s.log.Warn("skipping inline relationship: invalid source_id",
							"rel_type", relType, "source_id", sourceIDStr)
						continue
					}
					relSrcID = sourceID
					relDstID = result.CanonicalID
				} else {
					targetIDStr, _ := relMap["target_id"].(string)
					targetID, err := uuid.Parse(targetIDStr)
					if err != nil {
						s.log.Warn("skipping inline relationship: invalid target_id",
							"rel_type", relType, "target_id", targetIDStr)
						continue
					}
					relSrcID = result.CanonicalID
					relDstID = targetID
				}

				relReq := &graph.CreateGraphRelationshipRequest{
					Type:       relType,
					SrcID:      relSrcID,
					DstID:      relDstID,
					Properties: relProps,
				}
				relResult, err := s.graphService.CreateRelationship(ctx, projectUUID, relReq)
				if err != nil {
					s.log.Warn("failed to create inline relationship",
						"rel_type", relType, "src", relSrcID, "dst", relDstID, logger.Error(err))
					continue
				}
				slim.Relationships = append(slim.Relationships, slimRelationship{
					ID:       relResult.CanonicalID.String(),
					Type:     relResult.Type,
					TargetID: relResult.DstID.String(),
				})
			}
		}

		results = append(results, batchResult{
			Success: true,
			Entity:  slim,
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

// executeBatchCreateRelationships creates one or more relationships. Always expects a "relationships" array.
// Backward-compat: if flat fields (type, source_id, target_id, etc.) are passed instead, wraps them as a single-element array.
func (s *Service) executeBatchCreateRelationships(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	// Support flat single-relationship form for backward compat
	var relationshipsRaw []any
	if arr, ok := args["relationships"].([]any); ok {
		relationshipsRaw = arr
	} else if _, hasType := args["type"]; hasType {
		relationshipsRaw = []any{args}
	}

	if len(relationshipsRaw) == 0 {
		return nil, fmt.Errorf("missing or empty relationships array")
	}

	if len(relationshipsRaw) > 100 {
		return nil, fmt.Errorf("batch size exceeded: maximum 100 relationships per request")
	}

	// slimRelationship is the minimal response for a created relationship.
	type slimRelationship struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		SourceID string `json:"source_id"`
		TargetID string `json:"target_id"`
	}
	type batchResult struct {
		Success      bool              `json:"success"`
		Relationship *slimRelationship `json:"relationship,omitempty"`
		Error        string            `json:"error,omitempty"`
		Index        int               `json:"index"`
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
			relType, _ = relMap["relationship_type"].(string)
		}
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
		if srcIDStr == "" {
			srcIDStr, _ = relMap["from_id"].(string)
		}
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
		if dstIDStr == "" {
			dstIDStr, _ = relMap["to_id"].(string)
		}
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
			Success: true,
			Relationship: &slimRelationship{
				ID:       result.CanonicalID.String(),
				Type:     result.Type,
				SourceID: result.SrcID.String(),
				TargetID: result.DstID.String(),
			},
			Index: i,
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

	// Fetch schemas
	type packInfo struct {
		ID        string    `bun:"id"`
		UpdatedAt time.Time `bun:"updated_at"`
	}

	var packs []packInfo
	err := s.db.NewSelect().
		TableExpr("kb.graph_schemas").
		Column("id", "updated_at").
		OrderExpr("id ASC").
		Scan(ctx, &packs)

	if err != nil {
		return "", fmt.Errorf("query schemas: %w", err)
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

func (s *Service) executeListSchemas(ctx context.Context, args map[string]any) (*ToolResult, error) {
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
		TableExpr("kb.graph_schemas").
		Column("id", "name", "version", "description", "author", "source", "object_type_schemas", "published_at", "deprecated_at").
		Where("draft = false")

	if !includeDeprecated {
		query = query.Where("deprecated_at IS NULL")
	}

	if search != "" {
		query = query.Where("(name ILIKE ? OR description ILIKE ?)", "%"+search+"%", "%"+search+"%")
	}

	countQuery := s.db.NewSelect().
		TableExpr("kb.graph_schemas").
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
		return nil, fmt.Errorf("count schemas: %w", err)
	}

	err = query.
		OrderExpr("published_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(ctx, &packs)

	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}

	summaries := make([]MemorySchemaSummary, len(packs))
	for i, p := range packs {
		objectTypes := make([]string, 0)
		for typeName := range p.ObjectTypeSchemas {
			objectTypes = append(objectTypes, typeName)
		}

		deprecatedAt := ""
		if p.DeprecatedAt != nil {
			deprecatedAt = p.DeprecatedAt.Format(time.RFC3339)
		}

		summaries[i] = MemorySchemaSummary{
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

	result := ListSchemasResult{
		Packs:   summaries,
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasMore: offset+limit < total,
	}

	return s.wrapResult(result)
}

func (s *Service) executeGetSchema(ctx context.Context, args map[string]any) (*ToolResult, error) {
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
		TableExpr("kb.graph_schemas").
		Column("*").
		Where("id = ?", packID).
		Scan(ctx, &pack)

	if err != nil {
		return nil, fmt.Errorf("schema not found: %s", packID)
	}

	deprecatedAt := ""
	if pack.DeprecatedAt != nil {
		deprecatedAt = pack.DeprecatedAt.Format(time.RFC3339)
	}

	result := GetSchemaResult{
		Pack: &MemorySchema{
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
		ID       string `bun:"id"`
		SchemaID string `bun:"schema_id"`
		Active   bool   `bun:"active"`
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
			TableExpr("kb.graph_schemas").
			Column("id", "name", "version", "description", "author", "source", "object_type_schemas", "relationship_type_schemas", "published_at").
			Where("deprecated_at IS NULL").
			Where("draft = false").
			OrderExpr("published_at DESC").
			Scan(ctx, &packs)
		if err != nil {
			return err
		}

		err = tx.NewSelect().
			TableExpr("kb.project_schemas").
			Column("id", "schema_id", "active").
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
		installedMap[i.SchemaID] = i
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
		SchemaID          string         `bun:"schema_id"`
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
				ptp.schema_id,
				tp.name,
				tp.version,
				tp.description,
				tp.object_type_schemas,
				ptp.active,
				ptp.installed_at,
				ptp.customizations
			FROM kb.project_schemas ptp
			JOIN kb.graph_schemas tp ON ptp.schema_id = tp.id
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
			SchemaID:       inst.SchemaID,
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

func (s *Service) executeAssignSchema(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	templatePackID, _ := args["schema_id"].(string)
	if templatePackID == "" {
		return nil, fmt.Errorf("missing required parameter: schema_id")
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
		TableExpr("kb.graph_schemas").
		Column("id", "name", "version", "object_type_schemas", "ui_configs", "extraction_prompts").
		Where("id = ?", templatePackID).
		Scan(ctx, &pack)

	if err != nil {
		return nil, fmt.Errorf("schema not found: %s", templatePackID)
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
			SELECT COUNT(*) FROM kb.project_schemas 
			WHERE project_id = ? AND schema_id = ?
		`, projectUUID, templatePackID).Scan(ctx, &existingCount)
		if err != nil {
			return err
		}
		if existingCount > 0 {
			return fmt.Errorf("schema %s@%s is already installed", pack.Name, pack.Version)
		}

		type existingTypeRow struct {
			TypeName string `bun:"type_name"`
		}
		var existingTypes []existingTypeRow
		err = tx.NewRaw(`
			SELECT type_name FROM kb.project_object_schema_registry 
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
			INSERT INTO kb.project_schemas (project_id, schema_id, active, customizations)
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
				INSERT INTO kb.project_object_schema_registry 
				(project_id, type_name, source, schema_id, json_schema, ui_config, extraction_config, enabled)
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
		return nil, fmt.Errorf("assign schema: %w", err)
	}

	disabledTypes := make([]string, 0)
	for _, t := range disabledTypesRaw {
		if ts, ok := t.(string); ok {
			disabledTypes = append(disabledTypes, ts)
		}
	}

	result := AssignSchemaResult{
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
			SELECT active FROM kb.project_schemas 
			WHERE id = ? AND project_id = ?
		`, assignmentID, projectUUID).Scan(ctx, &currentActive)
		if err != nil {
			return fmt.Errorf("assignment not found: %s", assignmentID)
		}

		newActive = active

		_, err = tx.NewRaw(`
			UPDATE kb.project_schemas 
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
		Message:      "Schema assignment updated successfully",
	}

	return s.wrapResult(result)
}

func (s *Service) executeUninstallSchema(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
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
			SELECT schema_id FROM kb.project_schemas 
			WHERE id = ? AND project_id = ?
		`, assignmentID, projectUUID).Scan(ctx, &templatePackID)
		if err != nil {
			return fmt.Errorf("assignment not found: %s", assignmentID)
		}

		var objectCount int
		err = tx.NewRaw(`
			SELECT COUNT(*) FROM kb.graph_objects go
			JOIN kb.project_object_schema_registry ptr ON go.type = ptr.type_name AND go.project_id = ptr.project_id
			WHERE ptr.schema_id = ? AND go.project_id = ? AND go.deleted_at IS NULL AND go.supersedes_id IS NULL
		`, templatePackID, projectUUID).Scan(ctx, &objectCount)
		if err != nil {
			return err
		}

		if objectCount > 0 {
			return fmt.Errorf("cannot uninstall: %d objects still exist using types from this schema", objectCount)
		}

		_, err = tx.NewRaw(`
			DELETE FROM kb.project_object_schema_registry 
			WHERE schema_id = ? AND project_id = ?
		`, templatePackID, projectUUID).Exec(ctx)
		if err != nil {
			return err
		}

		_, err = tx.NewRaw(`
			DELETE FROM kb.project_schemas WHERE id = ?
		`, assignmentID).Exec(ctx)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("uninstall schema: %w", err)
	}

	result := UninstallSchemaResult{
		Success:      true,
		AssignmentID: assignmentID,
		Message:      "Schema uninstalled successfully",
	}

	return s.wrapResult(result)
}

func (s *Service) executeCreateSchema(ctx context.Context, args map[string]any) (*ToolResult, error) {
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
		INSERT INTO kb.graph_schemas 
		(name, version, description, author, source, object_type_schemas, relationship_type_schemas, ui_configs, extraction_prompts, checksum)
		VALUES (?, ?, ?, ?, 'manual', ?, ?, ?, ?, ?)
		RETURNING id, published_at, created_at, updated_at
	`, name, version, description, author, string(objectTypeSchemasJSON), string(relationshipTypeSchemasJSON), string(uiConfigsJSON), string(extractionPromptsJSON), checksum).Scan(ctx, &newPack)

	if err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	result := CreateSchemaResult{
		Success: true,
		Pack: &MemorySchema{
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
		Message: "Schema created successfully",
	}

	return s.wrapResult(result)
}

func (s *Service) executeDeleteSchema(ctx context.Context, args map[string]any) (*ToolResult, error) {
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
		TableExpr("kb.graph_schemas").
		Column("id", "name", "source").
		Where("id = ?", packID).
		Scan(ctx, &pack)

	if err != nil {
		return nil, fmt.Errorf("schema not found: %s", packID)
	}

	if pack.Source == "system" {
		return nil, fmt.Errorf("cannot delete built-in schemas")
	}

	var installCount int
	err = s.db.NewRaw(`
		SELECT COUNT(*) FROM kb.project_schemas WHERE schema_id = ?
	`, packID).Scan(ctx, &installCount)

	if err != nil {
		return nil, fmt.Errorf("check installations: %w", err)
	}

	if installCount > 0 {
		return nil, fmt.Errorf("cannot delete schema \"%s\" because it is currently installed in %d project(s)", pack.Name, installCount)
	}

	_, err = s.db.NewRaw(`
		DELETE FROM kb.graph_schemas WHERE id = ?
	`, packID).Exec(ctx)

	if err != nil {
		return nil, fmt.Errorf("delete schema: %w", err)
	}

	result := DeleteSchemaResult{
		Success: true,
		PackID:  packID,
		Message: fmt.Sprintf("Schema \"%s\" deleted successfully", pack.Name),
	}

	return s.wrapResult(result)
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

	// status is intentionally kept inside properties so it flows through
	// to the JSONB properties column. The graph layer syncs the status
	// column from properties["status"] after the merge.
	var status *string
	if st, ok := args["status"].(string); ok && st != "" {
		status = &st
		// Mirror into properties so the JSONB is kept in sync.
		if properties == nil {
			properties = make(map[string]any)
		}
		properties["status"] = st
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
	case uri == "memory://schema/entity-types":
		return s.readEntityTypesResource(ctx, projectID)
	case uri == "memory://schema/relationships":
		return s.readRelationshipsResource(ctx, projectID)
	case uri == "memory://templates/catalog":
		return s.readTemplatesCatalogResource(ctx)
	case strings.HasPrefix(uri, "memory://project/") && strings.Contains(uri, "/metadata"):
		return s.readProjectMetadataResource(ctx, projectID)
	case strings.HasPrefix(uri, "memory://project/") && strings.Contains(uri, "/recent-entities"):
		return s.readRecentEntitiesResource(ctx, projectID)
	case strings.HasPrefix(uri, "memory://project/") && strings.Contains(uri, "/templates"):
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
				URI:      "memory://schema/entity-types",
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
				URI:      "memory://schema/relationships",
				MimeType: "application/json",
				Text:     string(jsonData),
			},
		},
	}, nil
}

func (s *Service) readTemplatesCatalogResource(ctx context.Context) (*ResourceReadResult, error) {
	result, err := s.executeListSchemas(ctx, map[string]any{})
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
				URI:      "memory://templates/catalog",
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
				URI:      fmt.Sprintf("memory://project/%s/metadata", projectID),
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
				URI:      fmt.Sprintf("memory://project/%s/recent-entities", projectID),
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
				URI:      fmt.Sprintf("memory://project/%s/templates", projectID),
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

	schema, _ := args["schema"].(string)
	templateHint := ""
	if schema != "" {
		templateHint = fmt.Sprintf(" using the '%s' schema", schema)
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
2. Show me the schema using get_schema%s
3. Ask me for each required field one by one
4. Once we have all the data:
   - For SINGLE entity: use create_entity
   - For MULTIPLE entities (2+): use batch_create_entities (100x faster!)
5. Confirm creation and suggest next steps (e.g., adding relationships)

PERFORMANCE TIP: If creating multiple entities, use batch_create_entities instead of repeated create_entity calls.
It can handle up to 100 entities in one request, which is dramatically faster.

Let's start by checking what templates are available for %s entities.`, entityType, templateHint,
						func() string {
							if schema != "" {
								return fmt.Sprintf(" with name='%s'", schema)
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
	case "agent-def-list":
		return s.agentToolHandler.ExecuteListAgentDefinitions(ctx, projectID, args)
	case "agent-def-get":
		return s.agentToolHandler.ExecuteGetAgentDefinition(ctx, projectID, args)
	case "agent-def-create":
		return s.agentToolHandler.ExecuteCreateAgentDefinition(ctx, projectID, args)
	case "update_agent_definition":
		return s.agentToolHandler.ExecuteUpdateAgentDefinition(ctx, projectID, args)
	case "agent-def-delete":
		return s.agentToolHandler.ExecuteDeleteAgentDefinition(ctx, projectID, args)

	// Agents (runtime)
	case "agent-list":
		return s.agentToolHandler.ExecuteListAgents(ctx, projectID, args)
	case "agent-get":
		return s.agentToolHandler.ExecuteGetAgent(ctx, projectID, args)
	case "agent-create":
		return s.agentToolHandler.ExecuteCreateAgent(ctx, projectID, args)
	case "update_agent":
		return s.agentToolHandler.ExecuteUpdateAgent(ctx, projectID, args)
	case "agent-delete":
		return s.agentToolHandler.ExecuteDeleteAgent(ctx, projectID, args)
	case "trigger_agent":
		return s.agentToolHandler.ExecuteTriggerAgent(ctx, projectID, args)

	// Agent Runs
	case "agent-run-list":
		return s.agentToolHandler.ExecuteListAgentRuns(ctx, projectID, args)
	case "agent-run-get":
		return s.agentToolHandler.ExecuteGetAgentRun(ctx, projectID, args)
	case "agent-run-messages":
		return s.agentToolHandler.ExecuteGetAgentRunMessages(ctx, projectID, args)
	case "agent-run-tool-calls":
		return s.agentToolHandler.ExecuteGetAgentRunToolCalls(ctx, projectID, args)
	case "agent-run-status":
		return s.agentToolHandler.ExecuteGetRunStatus(ctx, projectID, args)

	// Agent Catalog
	case "agent-list-available":
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
	case "mcp-server-list":
		return s.mcpRegistryToolHandler.ExecuteListMCPServers(ctx, projectID, args)
	case "mcp-server-get":
		return s.mcpRegistryToolHandler.ExecuteGetMCPServer(ctx, projectID, args)
	case "mcp-server-create":
		return s.mcpRegistryToolHandler.ExecuteCreateMCPServer(ctx, projectID, args)
	case "update_mcp_server":
		return s.mcpRegistryToolHandler.ExecuteUpdateMCPServer(ctx, projectID, args)
	case "mcp-server-delete":
		return s.mcpRegistryToolHandler.ExecuteDeleteMCPServer(ctx, projectID, args)
	case "toggle_mcp_server_tool":
		return s.mcpRegistryToolHandler.ExecuteToggleMCPServerTool(ctx, projectID, args)
	case "sync_mcp_server_tools":
		return s.mcpRegistryToolHandler.ExecuteSyncMCPServerTools(ctx, projectID, args)
	case "search_mcp_registry":
		return s.mcpRegistryToolHandler.ExecuteSearchMCPRegistry(ctx, projectID, args)
	case "mcp-registry-get":
		return s.mcpRegistryToolHandler.ExecuteGetMCPRegistryServer(ctx, projectID, args)
	case "mcp-registry-install":
		return s.mcpRegistryToolHandler.ExecuteInstallMCPFromRegistry(ctx, projectID, args)
	case "mcp-server-inspect":
		return s.mcpRegistryToolHandler.ExecuteInspectMCPServer(ctx, projectID, args)
	default:
		return nil, fmt.Errorf("unknown MCP registry tool: %s", toolName)
	}
}

// executeCreateProject creates a new project under the caller's organization.
// The org_id argument is optional; when absent it is resolved from the auth context.
func (s *Service) executeCreateProject(ctx context.Context, args map[string]any) (*ToolResult, error) {
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("create_project: 'name' is required")
	}

	// Resolve org_id: explicit arg → auth context → project lookup → error.
	orgID, _ := args["org_id"].(string)
	if orgID == "" {
		orgID = auth.OrgIDFromContext(ctx)
	}
	if orgID == "" {
		// For standalone/account-mode auth, OrgID is not injected into the context.
		// Fall back to looking up the org from the project ID in context.
		if projectID := auth.ProjectIDFromContext(ctx); projectID != "" {
			var resolvedOrgID string
			_ = s.db.NewRaw(
				"SELECT organization_id FROM kb.projects WHERE id = ? LIMIT 1",
				projectID,
			).Scan(ctx, &resolvedOrgID)
			orgID = resolvedOrgID
		}
	}
	if orgID == "" {
		return nil, fmt.Errorf("create_project: 'org_id' is required (could not resolve from auth context)")
	}

	// Validate that org_id is a UUID.
	if _, err := uuid.Parse(orgID); err != nil {
		return nil, fmt.Errorf("create_project: 'org_id' must be a valid UUID: %w", err)
	}

	// Insert the project directly via the shared DB handle.
	type projectRow struct {
		ID    string `bun:"id"`
		Name  string `bun:"name"`
		OrgID string `bun:"organization_id"`
	}
	var row projectRow
	err := s.db.NewRaw(
		"INSERT INTO kb.projects (name, organization_id) VALUES (?, ?) RETURNING id, name, organization_id",
		name, orgID,
	).Scan(ctx, &row)
	if err != nil {
		return nil, fmt.Errorf("create_project: insert failed: %w", err)
	}

	result := map[string]any{
		"id":    row.ID,
		"name":  row.Name,
		"orgId": row.OrgID,
	}
	b, _ := json.Marshal(result)
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: string(b)}},
	}, nil
}
