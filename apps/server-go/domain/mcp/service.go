package mcp

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/domain/graph"
	"github.com/emergent/emergent-core/internal/database"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Service handles MCP business logic and tool execution
type Service struct {
	db           bun.IDB
	graphService *graph.Service
	log          *slog.Logger

	// Schema version caching
	cacheMu       sync.RWMutex
	cachedVersion string
	cacheExpiry   time.Time
}

// NewService creates a new MCP service
func NewService(db bun.IDB, graphService *graph.Service, log *slog.Logger) *Service {
	return &Service{
		db:           db,
		graphService: graphService,
		log:          log.With(logger.Scope("mcp.svc")),
	}
}

// GetToolDefinitions returns all available MCP tools
func (s *Service) GetToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
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

	edges, err := s.graphService.GetEdges(ctx, projectUUID, entityID)
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

// getSchemaVersion computes or returns cached schema version
func (s *Service) getSchemaVersion(ctx context.Context) (string, error) {
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
			WHERE project_id = ? AND type_name = ANY(?)
		`, projectUUID, typesToInstall).Scan(ctx, &existingTypes)
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
			WHERE ptr.template_pack_id = ? AND go.project_id = ? AND go.deleted_at IS NULL
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
