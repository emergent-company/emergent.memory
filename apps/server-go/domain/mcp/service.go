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
