package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/journal"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/domain/sessiontodos"
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

	// Branch service (for graph-branch-* tools)
	branchSvc *branches.Service

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

	// Email jobs service (for MCP invite emails)
	emailSvc *email.JobsService

	// Journal service (for journal-list and journal-add-note tools)
	journalSvc *journal.Service

	// Schemas service (for schema migration tools)
	schemasSvc *schemas.Service

	// Session todos service (for session-todo-list and session-todo-update tools)
	sessionTodoSvc *sessiontodos.Service

	// Embedding worker controller (for pause/resume/config tools)
	// Typed as interface to avoid import cycle with extraction package.
	embeddingCtl EmbeddingControlHandler

	// Session title handler (injected to break import cycle with agents domain)
	sessionTitleHandler SessionTitleHandler

	// graphObjectTitlePatcher patches graph object Properties.title when set_session_title runs.
	// Stored as a func to avoid a circular import (mcp already imports graph).
	graphObjectTitlePatcher func(ctx context.Context, projectID, objectID, title string) error

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
	BranchSvc          *branches.Service
	ProviderCredSvc    *provider.CredentialService
	ProviderCatalogSvc *provider.ModelCatalogService
	ApitokenSvc        *apitoken.Service
	EmailSvc           *email.JobsService
	JournalSvc         *journal.Service
	SchemasSvc         *schemas.Service
	SessionTodoSvc     *sessiontodos.Service
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
		branchSvc:          p.BranchSvc,
		providerCredSvc:    p.ProviderCredSvc,
		providerCatalogSvc: p.ProviderCatalogSvc,
		apitokenSvc:        p.ApitokenSvc,
		emailSvc:           p.EmailSvc,
		tempoBaseURL:       tempoURL,
		serverPort:         cfg.ServerPort,
		journalSvc:         p.JournalSvc,
		schemasSvc:         p.SchemasSvc,
		sessionTodoSvc:     p.SessionTodoSvc,
	}
}

// SetAgentToolHandler sets the agent tool handler (called after construction to break circular init)
func (s *Service) SetAgentToolHandler(h AgentToolHandler) {
	s.agentToolHandler = h
}

// SetSessionTitleHandler sets the session title handler (called after construction to break circular init)
func (s *Service) SetSessionTitleHandler(h SessionTitleHandler) {
	s.sessionTitleHandler = h
}

// SetGraphObjectPatcher sets the func used to patch graph object Properties.title
// when set_session_title is called. Called after construction to avoid circular init.
func (s *Service) SetGraphObjectPatcher(fn func(ctx context.Context, projectID, objectID, title string) error) {
	s.graphObjectTitlePatcher = fn
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
			Description: "List all available entity types in the knowledge graph with instance counts and relationship types. Pass branch to see counts for a specific branch (e.g. \"plan/main\"); omit for main branch. By default only returns types with no namespace set. Pass namespace to filter by a specific namespace, or namespace=\"all\" to see all types regardless of namespace.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"branch": {
						Type:        "string",
						Description: "Branch name or UUID to count entities on (e.g. \"plan/main\"). Omit for main branch.",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace filter. Omit to see only types with no namespace. Pass a specific namespace (e.g. \"system\") to see only that namespace. Pass \"all\" to see all types regardless of namespace.",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "entity-query",
			Description: "Query entity instances by type with pagination and filtering. Returns actual entity data from the knowledge graph. Pass ids[] to fetch specific entities by canonical ID (bypasses type/pagination). Use branch parameter to query a specific branch (e.g. \"plan/main\"); omit for main branch.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"type_name": {
						Type:        "string",
						Description: "Filter by entity type (e.g. 'APIEndpoint'). Omit to return all entity types.",
					},
					"ids": {
						Type:        "array",
						Description: "Optional list of canonical entity IDs to fetch directly. When provided, type_name and pagination params are ignored.",
						Items:       &PropertySchema{Type: "string"},
					},
					"branch": {
						Type:        "string",
						Description: "Optional branch name (e.g. \"plan/main\") or branch UUID to query. Omit to query the main branch.",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 10, max: 200)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(200),
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
					"fields": {
						Type:        "array",
						Description: "Optional list of property field names to return from the properties blob (e.g. [\"method\",\"path\"]). id, key, name, type, created_at, updated_at are always returned for free — do not include them here. When omitted, all properties are returned.",
						Items:       &PropertySchema{Type: "string"},
					},
					"filters": {
						Type:        "object",
						Description: "Optional property equality filters as key-value pairs (e.g. {\"status\": \"delivered\", \"priority\": \"high\"}). Only objects whose properties match ALL filters are returned.",
					},
				},
				Required: []string{"type_name"},
			},
		},
		{
			Name:        "entity-history",
			Description: "Get the version history of an entity by canonical ID or key. Returns a list of versions with their physical IDs, version numbers, and timestamps. Use entity-query with ids=[physical_id] to fetch the full properties of a specific historical version.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"entity_id": {
						Type:        "string",
						Description: "The canonical UUID or key of the entity to get history for",
					},
				},
				Required: []string{"entity_id"},
			},
		},
		{
			Name:        "entity-search",
			Description: "Search entities by text query across name, key, and description fields. By default only searches types with no namespace. Pass namespace to search a specific namespace, or namespace=\"all\" to search all.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"query": {
						Type:        "string",
						Description: "Search query text",
					},
					"branch": {
						Type:        "string",
						Description: "Optional branch name (e.g. \"plan/main\") or branch UUID to query. Omit to query across all branches.",
					},
					"type_name": {
						Type:        "string",
						Description: "Optional entity type filter",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace filter. Omit for types with no namespace. Pass specific namespace (e.g. \"system\") or \"all\" to include all namespaces.",
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
						Description: "The UUID or key of the entity to get edges for",
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
					"schema_id": {
						Type:        "string",
						Description: "The UUID of the schema to retrieve",
					},
				},
				Required: []string{"schema_id"},
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
					"schema_id": {
						Type:        "string",
						Description: "The UUID of the memory schema to delete",
					},
				},
				Required: []string{"schema_id"},
			},
		},
		{
			Name:        "schema-history",
			Description: "List the full installation history for schemas in this project, including schemas that have been uninstalled. Useful for auditing which schemas were used over time.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
			},
		},
		{
			Name:        "schema-compiled-types",
			Description: "Return the compiled (merged) set of object and relationship types currently active in this project, across all installed schemas. Optionally includes shadow detection metadata.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"verbose": {
						Type:        "boolean",
						Description: "When true, includes schemaId, schemaName, schemaVersion, and shadowed flag for each type",
					},
				},
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
					"branch": {
						Type:        "string",
						Description: "Optional branch name or UUID. If set, entities are created in this branch instead of the main graph.",
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
						Description: "UUID or key of the entity to update",
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
					"branch": {
						Type:        "string",
						Description: "Optional branch name or UUID. If set, the update is applied in this branch.",
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
						Description: "UUID or key of the entity to delete",
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
						Description: "UUID or key of the entity to restore",
					},
				},
				Required: []string{"entity_id"},
			},
		},
		{
			Name:        "graph-branch-list",
			Description: "List all branches in the project.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
			},
		},
		{
			Name:        "graph-branch-create",
			Description: "Create a new branch in the project.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Branch name (e.g. \"plan/my-feature\")",
					},
					"description": {
						Type:        "string",
						Description: "Optional human-readable description",
					},
					"parent_branch_id": {
						Type:        "string",
						Description: "Optional parent branch UUID. If omitted, branch has no parent.",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "graph-branch-merge",
			Description: "Merge a source branch into the main graph (or a target branch). By default performs a dry-run; set execute=true to apply.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"source_branch": {
						Type:        "string",
						Description: "Source branch name or UUID to merge from",
					},
					"target_branch": {
						Type:        "string",
						Description: "Optional target branch name or UUID. If omitted, merges into main graph.",
					},
					"execute": {
						Type:        "boolean",
						Description: "If true, apply the merge. Default false (dry-run).",
					},
					"conflict_strategy": {
						Type:        "string",
						Description: "How to handle conflicting objects (same key, different value). Options: \"enrich\" (default — target wins same keys, source adds new keys), \"overwrite\" (source wins all conflicting keys), \"preserve_target\" (skip conflict objects, keep target), \"block\" (abort if any conflicts exist, legacy behavior).",
					},
				},
				Required: []string{"source_branch"},
			},
		},
		{
			Name:        "graph-branch-delete",
			Description: "Delete a branch by name or UUID.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"branch": {
						Type:        "string",
						Description: "Branch name or UUID to delete",
					},
				},
				Required: []string{"branch"},
			},
		},
		{
			Name:        "search-hybrid",
			Description: "Advanced search combining full-text, semantic similarity, and graph context. Most powerful search option for AI agents. Supports optional recency and access-frequency ranking boosts. By default only searches types with no namespace; pass namespace to target a specific namespace or \"all\" for everything.",
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
					"namespace": {
						Type:        "string",
						Description: "Namespace filter. Omit for types with no namespace. Pass specific namespace (e.g. \"system\") or \"all\" to include all namespaces.",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results (default: 20, max: 100)",
						Minimum:     intPtr(1),
						Maximum:     intPtr(100),
						Default:     20,
					},
					"recency_boost": {
						Type:        "number",
						Description: "Boost score by recency of creation. 0 = disabled (default). Typical range: 0.5–2.0. Higher values favour newer objects.",
					},
					"recency_half_life": {
						Type:        "number",
						Description: "Half-life in hours for the recency decay sigmoid (default: 168 = 7 days). Only used when recency_boost > 0.",
					},
					"access_boost": {
						Type:        "number",
						Description: "Boost score by how recently the object was accessed. 0 = disabled (default). Typical range: 0.5–2.0.",
					},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "search-semantic",
			Description: "Search entities by semantic meaning using vector embeddings. Finds conceptually similar entities even with different wording. By default only searches types with no namespace; pass namespace to target a specific namespace or \"all\" for everything.",
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
					"namespace": {
						Type:        "string",
						Description: "Namespace filter. Omit for types with no namespace. Pass specific namespace (e.g. \"system\") or \"all\" to include all namespaces.",
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
						Description: "UUID or key of the starting entity (e.g. 'sc-taskify-create-task' or a full UUID)",
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
		// New System A migration tools — delegate to schemas.Service
		{
			Name:        "schema-migrate-preview",
			Description: "Preview a schema migration from one installed schema version to another. Runs a dry-run against all project objects, returning per-type risk breakdown (safe/cautious/risky/dangerous) and an overall risk level. NO CHANGES ARE MADE. Use before execute to understand impact.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"from_schema_id": {
						Type:        "string",
						Description: "UUID of the currently installed schema to migrate from",
					},
					"to_schema_id": {
						Type:        "string",
						Description: "UUID of the target schema to migrate to",
					},
				},
				Required: []string{"from_schema_id", "to_schema_id"},
			},
		},
		{
			Name:        "schema-migrate-execute",
			Description: "Execute a schema migration for all project objects, applying type/property renames and archiving removed properties. Returns counts of migrated and failed objects.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"from_schema_id": {
						Type:        "string",
						Description: "UUID of the schema to migrate from",
					},
					"to_schema_id": {
						Type:        "string",
						Description: "UUID of the schema to migrate to",
					},
					"force": {
						Type:        "boolean",
						Description: "Force migration even if risk level is dangerous (default: false)",
					},
					"max_objects": {
						Type:        "number",
						Description: "Maximum number of objects to migrate in this batch (0 = no limit)",
						Minimum:     intPtr(0),
					},
				},
				Required: []string{"from_schema_id", "to_schema_id"},
			},
		},
		{
			Name:        "schema-migrate-rollback",
			Description: "Rollback a schema migration by restoring archived property data for objects migrated to a given schema version. Optionally re-installs old schema types in a single transaction.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"to_version": {
						Type:        "string",
						Description: "Schema version to roll back to (e.g., '1.2.0')",
					},
					"restore_type_registry": {
						Type:        "boolean",
						Description: "If true, re-installs old schema types and removes new type additions (default: false)",
					},
				},
				Required: []string{"to_version"},
			},
		},
		{
			Name:        "schema-migrate-commit",
			Description: "Permanently discard migration archive data up to a given schema version. This prunes migration_archive entries from all project objects for versions <= through_version. Irreversible — only do this after confirming rollback is no longer needed.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"through_version": {
						Type:        "string",
						Description: "Archive entries for versions <= this value will be deleted (e.g., '1.2.0')",
					},
				},
				Required: []string{"through_version"},
			},
		},
		{
			Name:        "schema-migration-job-status",
			Description: "Get the current status and progress of an async schema migration job. Returns status (pending/running/completed/failed), objects migrated/failed counts, and any error message.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"job_id": {
						Type:        "string",
						Description: "UUID of the migration job to check",
					},
				},
				Required: []string{"job_id"},
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

	// Journal tools
	tools = append(tools, ToolDefinition{
		Name:        "journal-list",
		Description: "List recent project journal entries — graph mutations (create, update, delete, batch, merge) and notes. Use since to filter by age (e.g. '7d', '24h'). Returns entries with event type, actor, metadata, and attached notes.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"since": {
					Type:        "string",
					Description: "Show entries from the last N days/hours/minutes (e.g. '7d', '24h', '30m') or ISO-8601 timestamp. Defaults to last 7 days.",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of entries to return. Default 100.",
				},
				"branch_id": {
					Type:        "string",
					Description: "Optional branch UUID to filter entries by branch. Omit to view main branch entries only.",
				},
				"include_branches": {
					Type:        "boolean",
					Description: "When true, includes entries from the main branch and all merged branches in a unified time-sorted feed.",
				},
			},
			Required: []string{},
		},
	})
	tools = append(tools, ToolDefinition{
		Name:        "journal-add-note",
		Description: "Add a markdown note to the project journal. Notes can be standalone or attached to a specific journal entry. Use to record observations, decisions, or context about graph changes.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"body": {
					Type:        "string",
					Description: "Markdown body of the note.",
				},
				"journal_id": {
					Type:        "string",
					Description: "Optional UUID of a journal entry to attach this note to.",
				},
			},
			Required: []string{"body"},
		},
	})

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

// readOnlyToolNames is the allowlist of tools exposed to read-only share tokens
// (scopes: data:read, schema:read, agents:read, projects:read — no write scopes).
// Only graph-querying and knowledge-retrieval tools are included.
var readOnlyToolNames = map[string]bool{
	// Project context
	"project-get": true,
	// Schema / type inspection
	"schema-version":   true,
	"entity-type-list": true,
	"schema-list":      true,
	"schema-get":       true,
	// Graph querying
	"entity-query":      true,
	"entity-history":    true,
	"entity-search":     true,
	"entity-edges-get":  true,
	"relationship-list": true,
	"graph-traverse":    true,
	"tag-list":          true,
	// Semantic / hybrid search
	"search-hybrid":    true,
	"search-semantic":  true,
	"search-similar":   true,
	"search-knowledge": true,
	// Journal (read)
	"journal-list": true,
}

// isReadOnlyToken returns true when the token has only read scopes and no write scopes.
// A token is considered read-only when it lacks any "*:write" or "*:admin" scope.
func isReadOnlyToken(scopes []string) bool {
	for _, s := range scopes {
		if len(s) > 6 && (s[len(s)-6:] == ":write" || s[len(s)-6:] == ":admin") {
			return false
		}
	}
	return true
}

// FilterToolsForScopes filters the tool list based on token scopes.
// Read-only tokens (no write/admin scopes) receive only the graph-querying allowlist.
func FilterToolsForScopes(tools []ToolDefinition, scopes []string) []ToolDefinition {
	if !isReadOnlyToken(scopes) {
		return tools
	}
	filtered := make([]ToolDefinition, 0, len(readOnlyToolNames))
	for _, t := range tools {
		if readOnlyToolNames[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
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
			Name:        "memory-guide",
			Description: "Get guidance on how to use this knowledge base — what it contains, how to query it, and which tools to use for different tasks",
			Arguments:   []PromptArgument{},
		},
	}
}

// ExecuteTool executes an MCP tool and returns the result
func (s *Service) ExecuteTool(ctx context.Context, projectID string, toolName string, args map[string]any) (*ToolResult, error) {
	// Handle hidden built-in tools first — these are always available, never listed,
	// and cannot be blocked by scope filters or tool whitelists.
	if toolName == "set_session_title" {
		return s.executeSetSessionTitle(ctx, projectID, args)
	}
	switch toolName {
	case "project-get":
		return s.executeGetProjectInfo(ctx, projectID)
	case "project-create":
		return s.executeCreateProject(ctx, args)
	case "schema-version":
		return s.executeSchemaVersion(ctx)
	case "entity-type-list":
		return s.executeListEntityTypes(ctx, projectID, args)
	case "entity-query":
		return s.executeQueryEntities(ctx, projectID, args)
	case "entity-history":
		return s.executeEntityHistory(ctx, projectID, args)
	case "entity-search":
		return s.executeSearchEntities(ctx, projectID, args)
	case "entity-edges-get":
		return s.executeGetEntityEdges(ctx, projectID, args)
	case "schema-list":
		return s.executeListSchemas(ctx, projectID, args)
	case "schema-get":
		return s.executeGetSchema(ctx, projectID, args)
	case "schema-list-available":
		return s.executeGetAvailableTemplates(ctx, projectID, args)
	case "schema-list-installed":
		return s.executeGetInstalledTemplates(ctx, projectID, args)
	case "schema-assign":
		return s.executeAssignSchema(ctx, projectID, args)
	case "schema-assignment-update":
		return s.executeUpdateTemplateAssignment(ctx, projectID, args)
	case "schema-uninstall":
		return s.executeUninstallSchema(ctx, projectID, args)
	case "schema-create":
		return s.executeCreateSchema(ctx, projectID, args)
	case "schema-delete":
		return s.executeDeleteSchema(ctx, projectID, args)
	case "schema-history":
		return s.executeSchemaHistory(ctx, projectID)
	case "schema-compiled-types":
		return s.executeSchemaCompiledTypes(ctx, projectID, args)
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
	case "graph-branch-list":
		return s.executeGraphBranchList(ctx, projectID)
	case "graph-branch-create":
		return s.executeGraphBranchCreate(ctx, projectID, args)
	case "graph-branch-merge":
		return s.executeGraphBranchMerge(ctx, projectID, args)
	case "graph-branch-delete":
		return s.executeGraphBranchDelete(ctx, projectID, args)
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
	case "schema-migrate-preview":
		return s.executeSchemaMigratePreview(ctx, projectID, args)
	case "schema-migrate-execute":
		return s.executeSchemaMigrateExecute(ctx, projectID, args)
	case "schema-migrate-rollback":
		return s.executeSchemaMigrateRollback(ctx, projectID, args)
	case "schema-migrate-commit":
		return s.executeSchemaMigrateCommit(ctx, projectID, args)
	case "schema-migration-job-status":
		return s.executeSchemaMigrationJobStatus(ctx, projectID, args)

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

	// ACP (Agent Communication Protocol) tools
	case "acp-list-agents":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "acp-trigger-run":
		return s.delegateAgentTool(ctx, projectID, toolName, args)
	case "acp-get-run-status":
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
		return s.executeGetSkill(ctx, projectID, args)
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

	// Journal tools
	case "journal-list":
		return s.executeJournalList(ctx, projectID, args)
	case "journal-add-note":
		return s.executeJournalAddNote(ctx, projectID, args)

	default:
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}
}

// executeSchemaVersion returns schema version metadata
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
func (s *Service) executeListEntityTypes(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	branchRef, _ := args["branch"].(string)
	branchID, err := s.resolveBranchID(ctx, projectID, branchRef)
	if err != nil {
		return nil, err
	}

	namespaceFilter, _ := args["namespace"].(string)

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

		branchClause := "AND go.branch_id IS NULL"
		branchArgs := []any{}
		if branchID != nil {
			branchClause = "AND go.branch_id = ?"
			branchArgs = []any{*branchID}
		}

		// Build namespace clause
		namespaceClause := "AND tr.namespace IS NULL"
		var namespaceArgs []any
		if namespaceFilter == "all" {
			namespaceClause = ""
		} else if namespaceFilter != "" {
			namespaceClause = "AND tr.namespace = ?"
			namespaceArgs = []any{namespaceFilter}
		}

		// Query entity types
		queryArgs := []any{projectUUID}
		queryArgs = append(queryArgs, branchArgs...)
		queryArgs = append(queryArgs, projectUUID)
		queryArgs = append(queryArgs, namespaceArgs...)
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
				AND go.supersedes_id IS NULL
				`+branchClause+`
			WHERE tr.enabled = true 
				AND tr.project_id = ?
				`+namespaceClause+`
			GROUP BY tr.type_name, tr.description
			ORDER BY tr.type_name
		`, queryArgs...).Scan(ctx, &types)
		if err != nil {
			return err
		}

		// Fallback: discover types directly from graph_objects if registry empty on branch
		if len(types) == 0 && branchID != nil {
			err = tx.NewRaw(`
				SELECT DISTINCT go.type as name, '' as description, COUNT(*)::int as instance_count
				FROM kb.graph_objects go
				WHERE go.project_id = ?
				  AND go.deleted_at IS NULL
				  AND go.supersedes_id IS NULL
				  AND go.branch_id = ?
				GROUP BY go.type
				ORDER BY go.type
			`, projectUUID, *branchID).Scan(ctx, &types)
			if err != nil {
				return err
			}
		}

		relBranchClause := "AND gr.branch_id IS NULL"
		srcBranchClause := "AND src.branch_id IS NULL"
		dstBranchClause := "AND dst.branch_id IS NULL"
		relBranchArgs := []any{projectUUID}
		if branchID != nil {
			relBranchClause = "AND gr.branch_id = ?"
			srcBranchClause = "AND src.branch_id = ?"
			dstBranchClause = "AND dst.branch_id = ?"
			relBranchArgs = append(relBranchArgs, *branchID, *branchID, *branchID)
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
				`+relBranchClause+`
				AND src.deleted_at IS NULL
				`+srcBranchClause+`
				AND dst.deleted_at IS NULL
				`+dstBranchClause+`
			GROUP BY gr.type, src.type, dst.type
			ORDER BY gr.type, count DESC
		`, relBranchArgs...).Scan(ctx, &relTypes)
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

	// Resolve optional branch parameter.
	branchRef, _ := args["branch"].(string)
	branchID, err := s.resolveBranchID(ctx, projectID, branchRef)
	if err != nil {
		return nil, err
	}

	// ids[] fast-path: fetch specific entities by canonical ID, bypassing type/pagination.
	if rawIDs, ok := args["ids"]; ok {
		var idStrs []string
		switch v := rawIDs.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					idStrs = append(idStrs, s)
				}
			}
		case []string:
			idStrs = v
		}
		if len(idStrs) > 0 {
			return s.executeQueryEntitiesByIDs(ctx, projectID, idStrs, branchID, args)
		}
	}

	if typeName == "" {
		// type_name is now optional
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
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

	// Parse optional fields projection — only return requested property keys.
	// Always include "name" so display is consistent.
	var fieldProjection string
	if rawFields, ok := args["fields"]; ok {
		var fieldList []string
		switch v := rawFields.(type) {
		case []any:
			for _, f := range v {
				if s, ok := f.(string); ok && s != "" {
					fieldList = append(fieldList, s)
				}
			}
		case []string:
			fieldList = v
		}
		if len(fieldList) > 0 {
			// Ensure "name" is always present for display.
			hasName := false
			for _, f := range fieldList {
				if f == "name" {
					hasName = true
					break
				}
			}
			if !hasName {
				fieldList = append([]string{"name"}, fieldList...)
			}
			// Build: jsonb_build_object('key1', go.properties->'key1', 'key2', go.properties->'key2', ...)
			// Field names are safe — they come from the agent, not user input, but we still
			// sanitize by allowing only alphanumeric + underscore + hyphen.
			parts := make([]string, 0, len(fieldList)*2)
			for _, f := range fieldList {
				if isValidPropertyKey(f) {
					parts = append(parts, fmt.Sprintf("'%s', go.properties->'%s'", f, f))
				}
			}
			if len(parts) > 0 {
				fieldProjection = "jsonb_build_object(" + strings.Join(parts, ", ") + ")"
			}
		}
	}
	if fieldProjection == "" {
		fieldProjection = "go.properties"
	}

	type entityRow struct {
		ID              uuid.UUID      `bun:"id"`
		CanonicalID     uuid.UUID      `bun:"canonical_id"`
		Key             string         `bun:"key"`
		Name            string         `bun:"name"`
		TypeName        string         `bun:"type_name"`
		Version         int            `bun:"version"`
		Properties      map[string]any `bun:"properties,type:jsonb"`
		CreatedAt       time.Time      `bun:"created_at"`
		UpdatedAt       time.Time      `bun:"updated_at"`
		TypeDescription string         `bun:"type_description"`
	}

	// Parse optional property equality filters: {"status": "delivered", "priority": "high"}
	// Each key-value pair becomes: go.properties->>'key' = 'value'
	// Keys are sanitized to alphanumeric+underscore+hyphen to prevent injection.
	var filterClause string
	if rawFilters, ok := args["filters"].(map[string]any); ok {
		var parts []string
		for k, v := range rawFilters {
			if !isValidPropertyKey(k) {
				continue
			}
			valStr := fmt.Sprintf("%v", v)
			// Use parameterized-style quoting: embed value safely via fmt.Sprintf with %q
			// then strip Go quotes — simpler to just use string interpolation with escaping.
			// We escape single quotes in the value to prevent SQL injection.
			safeVal := strings.ReplaceAll(valStr, "'", "''")
			parts = append(parts, fmt.Sprintf("go.properties->>'%s' = '%s'", k, safeVal))
		}
		if len(parts) > 0 {
			filterClause = " AND " + strings.Join(parts, " AND ")
		}
	}

	type_clause := ""
	type_args := []any{}
	if typeName != "" {
		type_clause = "AND go.type = ?"
		type_args = []any{typeName}
	}

	var entities []entityRow
	var total int

	// Build branch filter clause: NULL = main branch, non-NULL = specific branch.
	branchClause := "AND go.branch_id IS NULL"
	branchArgs := []any{}
	if branchID != nil {
		branchClause = "AND go.branch_id = ?"
		branchArgs = []any{*branchID}
	}

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		// Query entities (latest version only: supersedes_id IS NULL means no newer version exists)
		err := tx.NewRaw(`
			SELECT 
				go.id,
				go.canonical_id,
				go.key,
				COALESCE(go.properties->>'name', '') as name,
				go.version,
				`+fieldProjection+` as properties,
				go.created_at,
				COALESCE(go.updated_at, go.created_at) as updated_at,
				go.type as type_name,
				COALESCE(tr.description, '') as type_description
			FROM kb.graph_objects go
			LEFT JOIN kb.project_object_schema_registry tr ON tr.type_name = go.type AND tr.project_id = go.project_id
			WHERE go.deleted_at IS NULL
				AND go.project_id = ?
				AND go.supersedes_id IS NULL
				`+type_clause+`
				`+branchClause+`
				`+filterClause+`
			ORDER BY `+orderExpr+`
			LIMIT ? OFFSET ?
		`, append(append([]any{projectUUID}, type_args...), append(branchArgs, limit, offset)...)...).Scan(ctx, &entities)
		if err != nil {
			return err
		}

		// Get total count (latest version only)
		err = tx.NewRaw(`
			SELECT COUNT(*)
			FROM kb.graph_objects go
			WHERE go.deleted_at IS NULL
				AND go.project_id = ?
				AND go.supersedes_id IS NULL
				`+type_clause+`
				`+branchClause+`
				`+filterClause+`
		`, append(append([]any{projectUUID}, type_args...), branchArgs...)...).Scan(ctx, &total)
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
			Version:    e.Version,
			Properties: e.Properties,
			CreatedAt:  e.CreatedAt,
			UpdatedAt:  e.UpdatedAt,
		}
	}

	// Detect unrecognized parameters and surface them as a warning so callers
	// know their extra keys (e.g. filter, status, entity_type) had no effect.
	knownQueryEntitiesParams := map[string]struct{}{
		"type_name": {}, "ids": {}, "limit": {}, "offset": {}, "sort_by": {}, "sort_order": {}, "include_relationships": {}, "fields": {}, "branch": {}, "filters": {},
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

		relBranchClause := "AND gr.branch_id IS NULL"
		dstBranchClause := "AND dst.branch_id IS NULL"
		relQueryArgs := []any{bun.In(canonicalIDs), projectUUID}
		if branchID != nil {
			relBranchClause = "AND gr.branch_id = ?"
			dstBranchClause = "AND dst.branch_id = ?"
			relQueryArgs = append(relQueryArgs, *branchID, *branchID)
		}
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
					AND dst.supersedes_id IS NULL `+dstBranchClause+` AND dst.deleted_at IS NULL
			WHERE gr.src_id IN (?)
				AND gr.deleted_at IS NULL
				AND gr.project_id = ?
				AND gr.supersedes_id IS NULL
				`+relBranchClause+`
		`, relQueryArgs...).Scan(ctx, &relRows)
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

// executeQueryEntitiesByIDs fetches specific entities by canonical ID list.
func (s *Service) executeQueryEntitiesByIDs(ctx context.Context, projectID string, idStrs []string, branchID *uuid.UUID, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	canonicalIDs := make([]uuid.UUID, 0, len(idStrs))
	for _, s := range idStrs {
		id, err := uuid.Parse(s)
		if err != nil {
			continue // skip malformed IDs
		}
		canonicalIDs = append(canonicalIDs, id)
	}
	if len(canonicalIDs) == 0 {
		return nil, fmt.Errorf("ids: no valid UUIDs provided")
	}

	type entityRow struct {
		ID          uuid.UUID      `bun:"id"`
		CanonicalID uuid.UUID      `bun:"canonical_id"`
		Key         string         `bun:"key"`
		Name        string         `bun:"name"`
		TypeName    string         `bun:"type_name"`
		Version     int            `bun:"version"`
		Properties  map[string]any `bun:"properties,type:jsonb"`
		CreatedAt   time.Time      `bun:"created_at"`
		UpdatedAt   time.Time      `bun:"updated_at"`
	}

	branchClause := "AND go.branch_id IS NULL"
	branchQueryArgs := []any{bun.In(canonicalIDs), projectUUID}
	if branchID != nil {
		branchClause = "AND go.branch_id = ?"
		branchQueryArgs = append(branchQueryArgs, *branchID)
	}

	var entities []entityRow
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}
		return tx.NewRaw(`
			SELECT
				go.id,
				go.canonical_id,
				go.key,
				COALESCE(go.properties->>'name', '') as name,
				go.version,
				go.properties,
				go.created_at,
				COALESCE(go.updated_at, go.created_at) as updated_at,
				go.type as type_name
			FROM kb.graph_objects go
			WHERE go.canonical_id IN (?)
				AND go.deleted_at IS NULL
				AND go.project_id = ?
				AND go.supersedes_id IS NULL
				`+branchClause+`
		`, branchQueryArgs...).Scan(ctx, &entities)
	})
	if err != nil {
		return nil, fmt.Errorf("query entities by ids: %w", err)
	}

	resultEntities := make([]Entity, len(entities))
	for i, e := range entities {
		resultEntities[i] = Entity{
			ID:         e.CanonicalID.String(),
			Key:        e.Key,
			Name:       e.Name,
			Type:       e.TypeName,
			Version:    e.Version,
			Properties: e.Properties,
			CreatedAt:  e.CreatedAt,
			UpdatedAt:  e.UpdatedAt,
		}
	}

	result := QueryEntitiesResult{
		ProjectID: projectID,
		Entities:  resultEntities,
		Pagination: &PaginationInfo{
			Total:   len(resultEntities),
			Limit:   len(resultEntities),
			Offset:  0,
			HasMore: false,
		},
	}
	return s.wrapResult(result)
}

// executeEntityHistory returns the version history of an entity by canonical ID.
func (s *Service) executeEntityHistory(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	entityIDStr, _ := args["entity_id"].(string)
	if entityIDStr == "" {
		return nil, fmt.Errorf("missing required parameter: entity_id")
	}
	canonicalID, err := uuid.Parse(entityIDStr)
	if err != nil {
		resolved, keyErr := s.resolveEntityIDByKey(ctx, projectUUID, entityIDStr)
		if keyErr != nil {
			return nil, fmt.Errorf("invalid entity_id: %q is not a UUID and key lookup failed: %w", entityIDStr, keyErr)
		}
		canonicalID = resolved
	}

	type versionRow struct {
		PhysicalID  uuid.UUID `bun:"id"`
		CanonicalID uuid.UUID `bun:"canonical_id"`
		Version     int       `bun:"version"`
		CreatedAt   time.Time `bun:"created_at"`
		UpdatedAt   time.Time `bun:"updated_at"`
	}

	var rows []versionRow
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}
		return tx.NewRaw(`
			SELECT
				go.id,
				go.canonical_id,
				go.version,
				go.created_at,
				COALESCE(go.updated_at, go.created_at) as updated_at
			FROM kb.graph_objects go
			WHERE go.canonical_id = ?
				AND go.deleted_at IS NULL
				AND go.project_id = ?
				AND go.branch_id IS NULL
			ORDER BY go.version ASC
		`, canonicalID, projectUUID).Scan(ctx, &rows)
	})
	if err != nil {
		return nil, fmt.Errorf("query entity history: %w", err)
	}

	type versionEntry struct {
		Version    int       `json:"version"`
		PhysicalID string    `json:"physical_id"`
		UpdatedAt  time.Time `json:"updated_at"`
	}
	versions := make([]versionEntry, len(rows))
	for i, r := range rows {
		versions[i] = versionEntry{
			Version:    r.Version,
			PhysicalID: r.PhysicalID.String(),
			UpdatedAt:  r.UpdatedAt,
		}
	}

	result := map[string]any{
		"entity_id": entityIDStr,
		"versions":  versions,
		"count":     len(versions),
	}
	return s.wrapResult(result)
}

// resolveBranchID resolves a branch name or UUID string to a branch UUID for the
// given project. Returns nil when the input is empty (meaning: use main branch).
// Returns an error when the branch cannot be found.
func (s *Service) resolveBranchID(ctx context.Context, projectID, branchRef string) (*uuid.UUID, error) {
	if branchRef == "" {
		return nil, nil
	}
	// Try parsing as UUID first.
	if id, err := uuid.Parse(branchRef); err == nil {
		return &id, nil
	}
	// Look up by name within the project.
	type branchRow struct {
		ID uuid.UUID `bun:"id"`
	}
	var row branchRow
	err := s.db.NewSelect().
		TableExpr("kb.branches").
		ColumnExpr("id").
		Where("name = ?", branchRef).
		Where("project_id = ?", projectID).
		Limit(1).
		Scan(ctx, &row)
	if err != nil {
		return nil, fmt.Errorf("branch %q not found: %w", branchRef, err)
	}
	return &row.ID, nil
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

	// Resolve optional branch parameter.
	branchRef, _ := args["branch"].(string)
	branchID, err := s.resolveBranchID(ctx, projectID, branchRef)
	if err != nil {
		return nil, err
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

	type entityRow struct {
		ID         uuid.UUID      `bun:"id"`
		Key        string         `bun:"key"`
		Name       string         `bun:"name"`
		TypeName   string         `bun:"type_name"`
		Properties map[string]any `bun:"properties,type:jsonb"`
		CreatedAt  time.Time      `bun:"created_at"`
		BranchID   *uuid.UUID     `bun:"branch_id"`
		BranchName *string        `bun:"branch_name"`
	}

	var entities []entityRow
	searchPattern := "%" + query + "%"

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		branchFilter := ""
		branchArgs := []any{}
		if branchID != nil {
			branchFilter = " AND go.branch_id = ?"
			branchArgs = []any{*branchID}
		}

		namespaceFilter, _ := args["namespace"].(string)
		namespaceJoin := `LEFT JOIN kb.project_object_schema_registry tr ON tr.type_name = go.type AND tr.project_id = go.project_id`
		namespaceClause := "AND (tr.namespace IS NULL OR tr.id IS NULL)"
		if namespaceFilter == "all" {
			namespaceClause = ""
		} else if namespaceFilter != "" {
			namespaceClause = "AND tr.namespace = ?"
		}

		baseQuery := `
			SELECT 
				go.id,
				go.key,
				COALESCE(go.properties->>'name', '') as name,
				go.properties,
				go.type as type_name,
				go.created_at,
				go.branch_id,
				b.name as branch_name
			FROM kb.graph_objects go
			LEFT JOIN kb.branches b ON b.id = go.branch_id
			` + namespaceJoin + `
			WHERE go.deleted_at IS NULL
				AND go.project_id = ?
				AND (
					go.key ILIKE ?
					OR go.properties->>'name' ILIKE ?
					OR go.properties->>'description' ILIKE ?
				)
				` + branchFilter + `
				` + namespaceClause + `
		`
		queryArgs := append([]any{projectUUID, searchPattern, searchPattern, searchPattern}, branchArgs...)
		if namespaceFilter != "all" && namespaceFilter != "" {
			queryArgs = append(queryArgs, namespaceFilter)
		}

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
		var branchIDStr *string
		if e.BranchID != nil {
			s := e.BranchID.String()
			branchIDStr = &s
		}
		resultEntities[i] = Entity{
			ID:         e.ID.String(),
			Key:        e.Key,
			Name:       e.Name,
			Type:       e.TypeName,
			Properties: e.Properties,
			CreatedAt:  e.CreatedAt,
			BranchID:   branchIDStr,
			BranchName: e.BranchName,
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
		resolved, keyErr := s.resolveEntityIDByKey(ctx, projectUUID, entityIDStr)
		if keyErr != nil {
			return nil, fmt.Errorf("invalid entity_id: %q is not a UUID and key lookup failed: %w", entityIDStr, keyErr)
		}
		entityID = resolved
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

// resolveEntityIDByKey looks up the canonical_id of a graph object by its key field.
// Returns an error if no object with that key exists in the project.
func (s *Service) resolveEntityIDByKey(ctx context.Context, projectID uuid.UUID, key string) (uuid.UUID, error) {
	var canonicalID uuid.UUID
	err := s.db.NewRaw(`
		SELECT canonical_id FROM kb.graph_objects
		WHERE key = ? AND project_id = ? AND deleted_at IS NULL
		ORDER BY version DESC
		LIMIT 1
	`, key, projectID).Scan(ctx, &canonicalID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("entity with key %q not found", key)
	}
	return canonicalID, nil
}

// executeUpdateEntity updates an existing entity's properties, labels, or status.
func (s *Service) executeUpdateEntity(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
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
		resolved, keyErr := s.resolveEntityIDByKey(ctx, projectUUID, entityIDStr)
		if keyErr != nil {
			return nil, fmt.Errorf("invalid entity_id: %q is not a UUID and key lookup failed: %w", entityIDStr, keyErr)
		}
		entityID = resolved
	}

	// Resolve optional branch param.
	var branchID *uuid.UUID
	if branchRef, _ := args["branch"].(string); branchRef != "" {
		branchID, err = s.resolveBranchID(ctx, projectID, branchRef)
		if err != nil {
			return nil, fmt.Errorf("resolve branch: %w", err)
		}
	}

	req := &graph.PatchGraphObjectRequest{
		BranchID: branchID,
	}

	if props, ok := args["properties"].(map[string]any); ok {
		req.Properties = props
	}

	if labels, ok := args["labels"].([]any); ok {
		labelStrs := make([]string, 0, len(labels))
		for _, l := range labels {
			if ls, ok := l.(string); ok {
				labelStrs = append(labelStrs, ls)
			}
		}
		req.Labels = labelStrs
	}

	if replaceLabels, ok := args["replace_labels"].(bool); ok {
		req.ReplaceLabels = replaceLabels
	}

	if status, ok := args["status"].(string); ok && status != "" {
		req.Status = &status
	}

	result, err := s.graphService.Patch(ctx, projectUUID, entityID, req, nil)
	if err != nil {
		return nil, fmt.Errorf("update entity: %w", err)
	}

	return s.wrapResult(map[string]any{
		"success": true,
		"entity":  result,
		"message": "Entity updated successfully",
	})
}

// executeDeleteEntity deletes an existing entity.
func (s *Service) executeDeleteEntity(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
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
		resolved, keyErr := s.resolveEntityIDByKey(ctx, projectUUID, entityIDStr)
		if keyErr != nil {
			return nil, fmt.Errorf("invalid entity_id: %q is not a UUID and key lookup failed: %w", entityIDStr, keyErr)
		}
		entityID = resolved
	}

	if err := s.graphService.Delete(ctx, projectUUID, entityID, nil, nil); err != nil {
		return nil, fmt.Errorf("delete entity: %w", err)
	}

	return s.wrapResult(map[string]any{
		"success": true,
		"message": "Entity deleted successfully",
	})
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
		resolved, keyErr := s.resolveEntityIDByKey(ctx, projectUUID, entityIDStr)
		if keyErr != nil {
			return nil, fmt.Errorf("invalid entity_id: %q is not a UUID and key lookup failed: %w", entityIDStr, keyErr)
		}
		entityID = resolved
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

// executeGraphBranchList lists all branches in the project.
func (s *Service) executeGraphBranchList(ctx context.Context, projectID string) (*ToolResult, error) {
	if s.branchSvc == nil {
		return nil, fmt.Errorf("branch service not available")
	}
	list, err := s.branchSvc.List(ctx, &projectID)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	return s.wrapResult(map[string]any{
		"branches": list,
	})
}

// executeGraphBranchCreate creates a new branch in the project.
func (s *Service) executeGraphBranchCreate(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.branchSvc == nil {
		return nil, fmt.Errorf("branch service not available")
	}
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}
	req := &branches.CreateBranchRequest{
		ProjectID: &projectID,
		Name:      name,
	}
	if desc, ok := args["description"].(string); ok && desc != "" {
		req.Description = &desc
	}
	if parentID, ok := args["parent_branch_id"].(string); ok && parentID != "" {
		req.ParentBranchID = &parentID
	}
	created, err := s.branchSvc.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create branch: %w", err)
	}
	return s.wrapResult(map[string]any{
		"success": true,
		"branch":  created,
		"message": fmt.Sprintf("Branch %q created", name),
	})
}

// executeGraphBranchMerge merges a source branch into main (or a target branch).
func (s *Service) executeGraphBranchMerge(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}
	srcRef, _ := args["source_branch"].(string)
	if srcRef == "" {
		return nil, fmt.Errorf("missing required parameter: source_branch")
	}
	srcID, err := s.resolveBranchID(ctx, projectID, srcRef)
	if err != nil || srcID == nil {
		return nil, fmt.Errorf("resolve source_branch: %w", err)
	}
	execute, _ := args["execute"].(bool)

	var targetBranchID *uuid.UUID
	if tgtRef, ok := args["target_branch"].(string); ok && tgtRef != "" {
		targetBranchID, err = s.resolveBranchID(ctx, projectID, tgtRef)
		if err != nil {
			return nil, fmt.Errorf("resolve target_branch: %w", err)
		}
	}

	req := &graph.BranchMergeRequest{
		SourceBranchID:   *srcID,
		Execute:          execute,
		ConflictStrategy: func() string { s, _ := args["conflict_strategy"].(string); return s }(),
	}
	result, err := s.graphService.MergeBranch(ctx, projectUUID, targetBranchID, req)
	if err != nil {
		return nil, fmt.Errorf("merge branch: %w", err)
	}
	return s.wrapResult(map[string]any{
		"result": result,
	})
}

// executeGraphBranchDelete deletes a branch by name or UUID.
func (s *Service) executeGraphBranchDelete(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.branchSvc == nil {
		return nil, fmt.Errorf("branch service not available")
	}
	branchRef, _ := args["branch"].(string)
	if branchRef == "" {
		return nil, fmt.Errorf("missing required parameter: branch")
	}
	branchID, err := s.resolveBranchID(ctx, projectID, branchRef)
	if err != nil || branchID == nil {
		return nil, fmt.Errorf("resolve branch: %w", err)
	}
	if err := s.branchSvc.Delete(ctx, branchID.String()); err != nil {
		return nil, fmt.Errorf("delete branch: %w", err)
	}
	return s.wrapResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("Branch %q deleted", branchRef),
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

	// Resolve optional branch param.
	var branchID *uuid.UUID
	if branchRef, _ := args["branch"].(string); branchRef != "" {
		branchID, err = s.resolveBranchID(ctx, projectID, branchRef)
		if err != nil {
			return nil, fmt.Errorf("resolve branch: %w", err)
		}
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
			BranchID:   branchID,
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
					BranchID:   branchID,
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

// getSchemaVersion computes a schema version hash
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

// isValidPropertyKey allows only safe characters in property field names used in SQL projection.
func isValidPropertyKey(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return len(s) > 0
}

func (s *Service) ReadResource(ctx context.Context, projectID, uri string) (*ResourceReadResult, error) {
	switch {
	case uri == "memory://schema/entity-types":
		return s.readEntityTypesResource(ctx, projectID)
	case uri == "memory://schema/relationships":
		return s.readRelationshipsResource(ctx, projectID)
	case uri == "memory://templates/catalog":
		return s.readTemplatesCatalogResource(ctx, projectID)
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
	result, err := s.executeListEntityTypes(ctx, projectID, map[string]any{})
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

func (s *Service) readTemplatesCatalogResource(ctx context.Context, projectID string) (*ResourceReadResult, error) {
	result, err := s.executeListSchemas(ctx, projectID, map[string]any{})
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
	result, err := s.executeGetInstalledTemplates(ctx, projectID, nil)
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
	case "memory-guide":
		return s.getMemoryGuidePrompt(ctx, projectID)
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

func (s *Service) getMemoryGuidePrompt(ctx context.Context, projectID string) (*PromptGetResult, error) {
	// Fetch project name and info
	var row struct {
		Name string  `bun:"name"`
		Info *string `bun:"project_info"`
	}
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("name, project_info").
		Where("id = ?", projectID).
		Scan(ctx, &row)
	if err != nil {
		return nil, fmt.Errorf("get project info: %w", err)
	}

	// Fetch entity types with counts
	projectUUID, _ := uuid.Parse(projectID)
	type typeRow struct {
		Name          string `bun:"name"`
		Description   string `bun:"description"`
		InstanceCount int    `bun:"instance_count"`
	}
	var types []typeRow
	_ = s.db.NewRaw(`
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

	// Build project info section
	projectName := row.Name
	if projectName == "" {
		projectName = "this knowledge base"
	}

	var sb strings.Builder

	// Project info block
	if row.Info != nil && *row.Info != "" {
		sb.WriteString("## About this knowledge base\n\n")
		sb.WriteString(*row.Info)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("## %s\n\n", projectName))
		sb.WriteString("You have access to a structured knowledge base. Use the tools below to explore and query it.\n\n")
	}

	// Entity types section
	if len(types) > 0 {
		sb.WriteString("## What's in here\n\n")
		for _, t := range types {
			if t.Description != "" {
				sb.WriteString(fmt.Sprintf("- **%s** (%d entries) — %s\n", t.Name, t.InstanceCount, t.Description))
			} else {
				sb.WriteString(fmt.Sprintf("- **%s** (%d entries)\n", t.Name, t.InstanceCount))
			}
		}
		sb.WriteString("\n")
	}

	// How to query section
	sb.WriteString("## How to query\n\n")
	sb.WriteString("For natural language questions (\"What do we know about X?\", \"Summarize decisions on Y\"):\n")
	sb.WriteString("→ Use **search-knowledge** — retrieves and synthesizes an answer in one step.\n\n")
	sb.WriteString("To find specific entities by type or attribute:\n")
	sb.WriteString("→ Use **entity-query** or **entity-search**\n\n")
	sb.WriteString("To explore how entities connect:\n")
	sb.WriteString("→ Use **graph-traverse** or **entity-edges-get**\n\n")
	sb.WriteString("For semantic / similarity search:\n")
	sb.WriteString("→ Use **search-hybrid** or **search-semantic**\n")

	return &PromptGetResult{
		Description: fmt.Sprintf("Guide to querying the %s knowledge base", projectName),
		Messages: []PromptMessage{
			{
				Role: "user",
				Content: PromptContent{
					Type: "text",
					Text: sb.String(),
				},
			},
		},
	}, nil
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

	// ACP (Agent Communication Protocol)
	case "acp-list-agents":
		return s.agentToolHandler.ExecuteACPListAgents(ctx, projectID, args)
	case "acp-trigger-run":
		return s.agentToolHandler.ExecuteACPTriggerRun(ctx, projectID, args)
	case "acp-get-run-status":
		return s.agentToolHandler.ExecuteACPGetRunStatus(ctx, projectID, args)

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

// executeJournalList lists recent journal entries for the project.
func (s *Service) executeJournalList(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.journalSvc == nil {
		return nil, fmt.Errorf("journal service not available")
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	params := journal.ListParams{
		ProjectID: projectUUID,
		Limit:     100,
		Page:      1,
	}

	if sinceStr, _ := args["since"].(string); sinceStr != "" {
		t, err := parseJournalSince(sinceStr)
		if err == nil {
			params.Since = &t
		}
	} else {
		// Default: last 7 days
		t := time.Now().UTC().Add(-7 * 24 * time.Hour)
		params.Since = &t
	}

	if limitVal, ok := args["limit"]; ok {
		switch v := limitVal.(type) {
		case float64:
			params.Limit = int(v)
		case int:
			params.Limit = v
		}
	}

	if branchIDStr, _ := args["branch_id"].(string); branchIDStr != "" {
		if id, err := uuid.Parse(branchIDStr); err == nil {
			params.BranchID = &id
		}
	}

	if includeBranches, _ := args["include_branches"].(bool); includeBranches {
		params.IncludeBranches = true
	}

	resp, err := s.journalSvc.List(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("journal list: %w", err)
	}

	return s.wrapResult(resp)
}

// executeJournalAddNote adds a note to the project journal.
func (s *Service) executeJournalAddNote(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.journalSvc == nil {
		return nil, fmt.Errorf("journal service not available")
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	body, _ := args["body"].(string)
	if body == "" {
		return nil, fmt.Errorf("body is required")
	}

	req := &journal.AddNoteRequest{
		Body:      body,
		ActorType: journal.ActorAgent,
	}

	if journalIDStr, _ := args["journal_id"].(string); journalIDStr != "" {
		id, err := uuid.Parse(journalIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid journal_id: %w", err)
		}
		req.JournalID = &id
	}

	note, err := s.journalSvc.AddNote(ctx, projectUUID, req)
	if err != nil {
		return nil, fmt.Errorf("add note: %w", err)
	}

	return s.wrapResult(note)
}

// parseJournalSince parses a since string: relative (e.g. "7d", "24h", "30m") or RFC3339.
func parseJournalSince(s string) (time.Time, error) {
	if len(s) >= 2 {
		unit := s[len(s)-1]
		if unit == 'd' || unit == 'h' || unit == 'm' || unit == 's' {
			n, err := strconv.Atoi(s[:len(s)-1])
			if err == nil {
				switch unit {
				case 'd':
					return time.Now().UTC().Add(-time.Duration(n) * 24 * time.Hour), nil
				case 'h':
					return time.Now().UTC().Add(-time.Duration(n) * time.Hour), nil
				case 'm':
					return time.Now().UTC().Add(-time.Duration(n) * time.Minute), nil
				case 's':
					return time.Now().UTC().Add(-time.Duration(n) * time.Second), nil
				}
			}
		}
	}
	return time.Parse(time.RFC3339, s)
}

// executeSetSessionTitle is the hidden built-in tool that updates the title of the
// current ACP session. It reads the session ID from context (injected by the agent
// executor) and updates the title in the database. If no session ID is found in
// context, it falls back to the optional "session_id" argument so agents that
// receive the session ID via a [Session: <id>] prompt tag can pass it explicitly.
//
// In addition to updating the ACP session record (kb.acp_sessions.title), it also
// patches the session's graph object Properties.title so external integrations that
// read sessions via the graph API (GET /api/graph/objects/:id) see the updated title.
func (s *Service) executeSetSessionTitle(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	title, _ := args["title"].(string)
	if title == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: `{"error":"title is required"}`}},
			IsError: true,
		}, nil
	}
	if len(title) > 512 {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: `{"error":"title exceeds maximum length of 512 characters"}`}},
			IsError: true,
		}, nil
	}

	// Prefer context-injected session ID; fall back to explicit arg.
	sessionID := ACPSessionIDFromContext(ctx)
	if sessionID == "" {
		sessionID, _ = args["session_id"].(string)
	}
	if sessionID == "" {
		// No session in context or args — silently succeed so agents don't fail when called
		// outside of an ACP session (e.g. direct MCP tool invocation).
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: `{"ok":true,"note":"no active session"}`}},
		}, nil
	}

	if s.sessionTitleHandler == nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: `{"error":"session title handler not configured"}`}},
			IsError: true,
		}, nil
	}

	if err := s.sessionTitleHandler.UpdateACPSessionTitle(ctx, projectID, sessionID, title); err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf(`{"error":"failed to update session title: %s"}`, err.Error())}},
			IsError: true,
		}, nil
	}

	// Also patch the graph object so callers using the graph API can read the title.
	// Best-effort: log but don't fail if the graph object doesn't exist or patcher not wired.
	if s.graphObjectTitlePatcher != nil {
		if err := s.graphObjectTitlePatcher(ctx, projectID, sessionID, title); err != nil {
			s.log.Warn("set_session_title: failed to patch graph object title (non-fatal)",
				slog.String("session_id", sessionID),
				slog.String("error", err.Error()),
			)
		}
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: `{"ok":true}`}},
	}, nil
}
