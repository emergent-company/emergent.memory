package mcp

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/database"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

type SchemaInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	ProjectID   string `json:"project_id,omitempty"`
	OrgID       string `json:"org_id,omitempty"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type TemplateInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	ProjectID   string `json:"project_id,omitempty"`
	OrgID       string `json:"org_id,omitempty"`
	Source      string `json:"source"`
}

func (s *Service) getSchemaVersion(ctx context.Context) (string, error) {
	s.cacheMu.RLock()
	if s.cachedVersion != "" && time.Now().Before(s.cacheExpiry) {
		version := s.cachedVersion
		s.cacheMu.RUnlock()
		return version, nil
	}
	s.cacheMu.RUnlock()

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	if s.cachedVersion != "" && time.Now().Before(s.cacheExpiry) {
		return s.cachedVersion, nil
	}

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

	composite := ""
	for _, p := range packs {
		composite += fmt.Sprintf("%s:%d|", p.ID, p.UpdatedAt.Unix())
	}

	hash := md5.Sum([]byte(composite))
	version := hex.EncodeToString(hash[:])[:16]

	s.cachedVersion = version
	s.cacheExpiry = time.Now().Add(60 * time.Second)

	return version, nil
}

func (s *Service) executeSchemaVersion(ctx context.Context) (*ToolResult, error) {
	version, err := s.getSchemaVersion(ctx)
	if err != nil {
		return nil, err
	}

	var packCount int
	err = s.db.NewSelect().
		TableExpr("kb.graph_schemas").
		ColumnExpr("COUNT(*)").
		Scan(ctx, &packCount)
	if err != nil {
		s.log.Warn("failed to count schemas")
		packCount = 0
	}

	result := SchemaVersionResult{
		Version:      version,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		PackCount:    packCount,
		CacheHintTTL: 300,
	}

	return s.wrapResult(result)
}

func (s *Service) executeListSchemas(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	search, _ := args["search"].(string)
	includeDeprecated, _ := args["include_deprecated"].(bool)

	orgID := auth.OrgIDFromContext(ctx)

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

	offset := 0
	if o, ok := args["offset"].(float64); ok {
		offset = int(o)
	}

	var rows []struct {
		ID          string `bun:"id"`
		Name        string `bun:"name"`
		Version     string `bun:"version"`
		Description string `bun:"description"`
		Visibility  string `bun:"visibility"`
		ProjectID   string `bun:"project_id"`
		OrgID       string `bun:"org_id"`
		Source      string `bun:"source"`
		CreatedAt   string `bun:"created_at"`
		UpdatedAt   string `bun:"updated_at"`
	}

	query := s.db.NewSelect().
		TableExpr("kb.graph_schemas").
		Column("id", "name", "version", "description", "visibility", "project_id", "org_id", "source", "created_at", "updated_at")

	if projectID != "" {
		query = query.Where("(project_id = ? OR (org_id = ? AND visibility = 'organization'))", projectID, orgID)
	} else {
		query = query.Where("(org_id = ? AND visibility = 'organization') OR (project_id IS NULL AND org_id IS NULL)", orgID)
	}

	if search != "" {
		query = query.Where("name ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	if !includeDeprecated {
		query = query.Where("deprecated_at IS NULL")
	}

	query = query.Order("updated_at DESC").Limit(limit).Offset(offset)

	err := query.Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}

	schemas := make([]SchemaInfo, len(rows))
	for i, r := range rows {
		schemas[i] = SchemaInfo{
			ID:          r.ID,
			Name:        r.Name,
			Version:     r.Version,
			Description: r.Description,
			Visibility:  r.Visibility,
			ProjectID:   r.ProjectID,
			OrgID:       r.OrgID,
			Source:      r.Source,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		}
	}

	// Get total count
	var total int
	countQuery := s.db.NewSelect().TableExpr("kb.graph_schemas")
	if projectID != "" {
		countQuery = countQuery.Where("(project_id = ? OR (org_id = ? AND visibility = 'organization'))", projectID, orgID)
	} else {
		countQuery = countQuery.Where("(org_id = ? AND visibility = 'organization') OR (project_id IS NULL AND org_id IS NULL)", orgID)
	}
	if search != "" {
		countQuery = countQuery.Where("name ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if !includeDeprecated {
		countQuery = countQuery.Where("deprecated_at IS NULL")
	}
	err = countQuery.Scan(ctx, &total)
	if err != nil {
		s.log.Warn("failed to count schemas", logger.Error(err))
		total = len(schemas)
	}

	result := map[string]any{
		"project_id": projectID,
		"schemas":    schemas,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	}
	return s.wrapResult(result)
}

func (s *Service) executeGetSchema(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	schemaID, _ := args["schema_id"].(string)
	if schemaID == "" {
		return nil, fmt.Errorf("schema_id is required")
	}

	orgID := auth.OrgIDFromContext(ctx)

	var schema SchemaInfo
	err := s.db.NewRaw(`
		SELECT id, name, version, description, visibility, project_id, org_id, source, created_at, updated_at
		FROM kb.graph_schemas
		WHERE id = ? AND (project_id = ? OR (org_id = ? AND visibility = 'organization'))
	`, schemaID, projectID, orgID).Scan(ctx, &schema)
	if err != nil {
		return nil, fmt.Errorf("schema not found: %s", schemaID)
	}

	// Get object types
	type objectTypeRow struct {
		TypeName      string         `bun:"type_name"`
		TypeSchema    map[string]any `bun:"json_schema,type:jsonb"`
		SchemaID      string         `bun:"schema_id"`
		SchemaName    string         `bun:"schema_name"`
		SchemaVersion string         `bun:"schema_version"`
	}

	var objRows []objectTypeRow
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		return tx.NewRaw(`
			SELECT por.type_name, por.json_schema, por.schema_id, gs.name AS schema_name, gs.version AS schema_version
			FROM kb.project_object_schema_registry por
			JOIN kb.project_schemas ps ON por.schema_id = ps.schema_id AND ps.project_id = por.project_id
			JOIN kb.graph_schemas gs ON por.schema_id = gs.id
			WHERE por.schema_id = ? AND ps.removed_at IS NULL
			ORDER BY por.type_name
		`, schemaID).Scan(ctx, &objRows)
	})
	if err != nil {
		s.log.Warn("failed to query object types", logger.Error(err))
	}

	objectTypes := make([]map[string]any, len(objRows))
	for i, r := range objRows {
		objType := map[string]any{
			"type_name":      r.TypeName,
			"type_schema":    r.TypeSchema,
			"schema_id":      r.SchemaID,
			"schema_name":    r.SchemaName,
			"schema_version": r.SchemaVersion,
		}
		objectTypes[i] = objType
	}

	// Get relationship types
	type relTypeRow struct {
		TypeName      string `bun:"type_name"`
		SchemaID      string `bun:"schema_id"`
		SchemaName    string `bun:"schema_name"`
		SchemaVersion string `bun:"schema_version"`
	}

	var relRows []relTypeRow
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		return tx.NewRaw(`
			SELECT per.type_name, per.schema_id, gs.name AS schema_name, gs.version AS schema_version
			FROM kb.project_edge_schema_registry per
			JOIN kb.project_schemas ps ON per.schema_id = ps.schema_id AND ps.project_id = per.project_id
			JOIN kb.graph_schemas gs ON per.schema_id = gs.id
			WHERE per.schema_id = ? AND ps.removed_at IS NULL
			ORDER BY per.type_name
		`, schemaID).Scan(ctx, &relRows)
	})
	if err != nil {
		s.log.Warn("failed to query relationship types", logger.Error(err))
	}

	relTypes := make([]map[string]any, len(relRows))
	for i, r := range relRows {
		relType := map[string]any{
			"type_name":      r.TypeName,
			"schema_id":      r.SchemaID,
			"schema_name":    r.SchemaName,
			"schema_version": r.SchemaVersion,
		}
		relTypes[i] = relType
	}

	result := map[string]any{
		"project_id":         projectID,
		"schema":             schema,
		"object_types":       objectTypes,
		"relationship_types": relTypes,
	}
	return s.wrapResult(result)
}

func (s *Service) executeGetAvailableTemplates(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	orgID := auth.OrgIDFromContext(ctx)

	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}

	search, _ := args["search"].(string)
	category, _ := args["category"].(string)

	var rows []struct {
		ID          string `bun:"id"`
		Name        string `bun:"name"`
		Version     string `bun:"version"`
		Description string `bun:"description"`
		Visibility  string `bun:"visibility"`
		ProjectID   string `bun:"project_id"`
		OrgID       string `bun:"org_id"`
		Source      string `bun:"source"`
		Categories  string `bun:"categories"`
	}

	query := s.db.NewSelect().
		TableExpr("kb.graph_schemas").
		Column("id", "name", "version", "description", "visibility", "project_id", "org_id", "source", "categories")

	query = query.Where("(org_id = ? AND visibility = 'organization') OR (project_id = ? AND visibility = 'project')", orgID, projectID)

	if search != "" {
		query = query.Where("name ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	if category != "" {
		query = query.Where("categories ? ?", category)
	}

	query = query.Where("source = 'template'").
		Where("deprecated_at IS NULL").
		Order("created_at DESC").
		Limit(limit)

	err := query.Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("get available templates: %w", err)
	}

	type templateRow struct {
		ID         string   `bun:"id"`
		Name       string   `bun:"name"`
		Version    string   `bun:"version"`
		Categories []string `bun:"categories"`
	}

	var templateRows []templateRow
	err = s.db.NewRaw(`
		SELECT id, name, version, categories
		FROM kb.graph_schemas
		WHERE source = 'template' AND deprecated_at IS NULL
		ORDER BY created_at DESC
		LIMIT 100
	`).Scan(ctx, &templateRows)
	if err != nil {
		s.log.Warn("failed to query template categories", logger.Error(err))
	}

	categories := make(map[string]bool)
	for _, r := range templateRows {
		if len(r.Categories) > 0 {
			for _, c := range r.Categories {
				categories[c] = true
			}
		}
	}

	templates := make([]TemplateInfo, len(rows))
	for i, r := range rows {
		template := TemplateInfo{
			ID:          r.ID,
			Name:        r.Name,
			Version:     r.Version,
			Description: r.Description,
			Visibility:  r.Visibility,
			ProjectID:   r.ProjectID,
			OrgID:       r.OrgID,
			Source:      r.Source,
		}
		templates[i] = template
	}

	result := map[string]any{
		"project_id": projectID,
		"templates":  templates,
		"total":      len(templates),
		"categories": categories,
	}
	return s.wrapResult(result)
}

func (s *Service) executeGetInstalledTemplates(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	type installedRow struct {
		ID            string     `bun:"id"`
		SchemaID      string     `bun:"schema_id"`
		Active        bool       `bun:"active"`
		InstalledAt   time.Time  `bun:"installed_at"`
		RemovedAt     *time.Time `bun:"removed_at"`
		SchemaName    string     `bun:"schema_name"`
		SchemaVersion string     `bun:"schema_version"`
	}

	var rows []installedRow
	err = s.db.NewRaw(`
		SELECT
			ps.id,
			ps.schema_id,
			ps.active,
			ps.installed_at,
			ps.removed_at,
			gs.name AS schema_name,
			gs.version AS schema_version
		FROM kb.project_schemas ps
		JOIN kb.graph_schemas gs ON ps.schema_id = gs.id
		WHERE ps.project_id = ?
		ORDER BY ps.installed_at DESC
	`, projectUUID).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("get installed templates: %w", err)
	}

	type templatesRow struct {
		ID      string `bun:"id"`
		Name    string `bun:"name"`
		Version string `bun:"version"`
		Source  string `bun:"source"`
	}

	var templates []templatesRow
	err = s.db.NewRaw(`
		SELECT id, name, version, source
		FROM kb.graph_schemas
		WHERE id IN (SELECT schema_id FROM kb.project_schemas WHERE project_id = ?)
		ORDER BY name
	`, projectUUID).Scan(ctx, &templates)
	if err != nil {
		s.log.Warn("failed to query template info", logger.Error(err))
	}

	type installedInfo struct {
		ID            string  `bun:"id"`
		SchemaID      string  `bun:"schema_id"`
		Active        bool    `bun:"active"`
		InstalledAt   string  `bun:"installed_at"`
		RemovedAt     *string `bun:"removed_at"`
		SchemaName    string  `bun:"schema_name"`
		SchemaVersion string  `bun:"schema_version"`
	}

	installed := make([]installedInfo, len(rows))
	for i, r := range rows {
		info := installedInfo{
			ID:            r.ID,
			SchemaID:      r.SchemaID,
			Active:        r.Active,
			InstalledAt:   r.InstalledAt.Format(time.RFC3339),
			SchemaName:    r.SchemaName,
			SchemaVersion: r.SchemaVersion,
		}
		if r.RemovedAt != nil {
			s := r.RemovedAt.Format(time.RFC3339)
			info.RemovedAt = &s
		}
		installed[i] = info
	}

	result := map[string]any{
		"project_id": projectID,
		"installed":  installed,
		"total":      len(installed),
		"templates":  templates,
	}
	return s.wrapResult(result)
}

func (s *Service) executeAssignSchema(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	schemaID, _ := args["schema_id"].(string)
	if schemaID == "" {
		return nil, fmt.Errorf("schema_id is required")
	}

	force, _ := args["force"].(bool)

	var schemaRow struct {
		ID         string `bun:"id"`
		Name       string `bun:"name"`
		Visibility string `bun:"visibility"`
		OrgID      string `bun:"org_id"`
	}
	err = s.db.NewRaw(`
		SELECT id, name, visibility, org_id
		FROM kb.graph_schemas
		WHERE id = ? AND (visibility = 'organization' OR (project_id = ? AND visibility = 'project'))
	`, schemaID, projectID).Scan(ctx, &schemaRow)
	if err != nil {
		return nil, fmt.Errorf("schema not found: %s", schemaID)
	}

	if schemaRow.Visibility == "project" {
		var existing struct {
			ID string `bun:"id"`
		}
		err = s.db.NewRaw(`
			SELECT id FROM kb.project_schemas WHERE project_id = ? AND schema_id = ?
		`, projectUUID, schemaID).Scan(ctx, &existing)
		if err == nil && existing.ID != "" {
			return &ToolResult{
				Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Schema \"%s\" is already assigned to this project.", schemaRow.Name)}},
			}, nil
		}
	}

	_, err = s.db.NewRaw(`
		INSERT INTO kb.project_schemas (project_id, schema_id, active)
		VALUES (?, ?, true)
		ON CONFLICT (project_id, schema_id) DO UPDATE
		SET active = true, removed_at = NULL
	`, projectUUID, schemaID).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("assign schema: %w", err)
	}

	if !force {
		// Run schema migration preview
		previewResult, err := s.schemasSvc.PreviewSchemaMigration(ctx, projectID, &schemas.SchemaMigrationPreviewRequest{
			FromSchemaID: "",
			ToSchemaID:   schemaID,
		})
		if err != nil {
			s.log.Warn("schema migration preview failed", logger.Error(err))
		} else {
			return &ToolResult{
				Content: []ContentBlock{
					{Type: "text", Text: fmt.Sprintf("Schema \"%s\" assigned successfully.\n\nMigration preview:\n%v", schemaRow.Name, previewResult)},
				},
			}, nil
		}
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Schema \"%s\" assigned successfully.", schemaRow.Name)}},
	}, nil
}

func (s *Service) executeUpdateTemplateAssignment(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	schemaID, _ := args["schema_id"].(string)
	if schemaID == "" {
		return nil, fmt.Errorf("schema_id is required")
	}

	active, _ := args["active"].(bool)

	var assigned struct {
		ID string `bun:"id"`
	}
	err = s.db.NewRaw(`
		UPDATE kb.project_schemas
		SET active = ?, removed_at = CASE WHEN ? THEN NULL ELSE removed_at END
		WHERE project_id = ? AND schema_id = ?
		RETURNING id
	`, active, active, projectUUID, schemaID).Scan(ctx, &assigned)
	if err != nil {
		return nil, fmt.Errorf("update template assignment: %w", err)
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Template assignment updated. Active: %v", active)}},
	}, nil
}

func (s *Service) executeUninstallSchema(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	schemaID, _ := args["schema_id"].(string)
	if schemaID == "" {
		return nil, fmt.Errorf("schema_id is required")
	}

	var schemaRow struct {
		ID   string `bun:"id"`
		Name string `bun:"name"`
	}
	err = s.db.NewRaw(`
		SELECT id, name FROM kb.graph_schemas WHERE id = ?
	`, schemaID).Scan(ctx, &schemaRow)
	if err != nil {
		return nil, fmt.Errorf("schema not found: %s", schemaID)
	}

	now := time.Now()
	var uninstalled struct {
		ID string `bun:"id"`
	}
	err = s.db.NewRaw(`
		UPDATE kb.project_schemas
		SET active = false, removed_at = ?
		WHERE project_id = ? AND schema_id = ? AND active = true
		RETURNING id
	`, now, projectUUID, schemaID).Scan(ctx, &uninstalled)
	if err != nil {
		return nil, fmt.Errorf("uninstall schema: %w", err)
	}

	if uninstalled.ID == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Schema \"%s\" is not currently installed in this project.", schemaRow.Name)}},
		}, nil
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Schema \"%s\" uninstalled successfully.", schemaRow.Name)}},
	}, nil
}

func (s *Service) executeCreateSchema(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	version, _ := args["version"].(string)
	if version == "" {
		version = "1.0.0"
	}

	description, _ := args["description"].(string)

	// Parse object types from JSON
	var objectTypes []map[string]any
	if objTypesJSON, ok := args["object_types"].(string); ok && objTypesJSON != "" {
		if err := json.Unmarshal([]byte(objTypesJSON), &objectTypes); err != nil {
			return nil, fmt.Errorf("invalid object_types JSON: %w", err)
		}
	}

	// Parse relationship types from JSON
	var relTypes []map[string]any
	if relTypesJSON, ok := args["relationship_types"].(string); ok && relTypesJSON != "" {
		if err := json.Unmarshal([]byte(relTypesJSON), &relTypes); err != nil {
			return nil, fmt.Errorf("invalid relationship_types JSON: %w", err)
		}
	}

	// Create schema
	var schemaID string
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		// Insert schema
		var createdSchema struct {
			ID string `bun:"id"`
		}
		err := tx.NewRaw(`
			INSERT INTO kb.graph_schemas (id, project_id, name, version, description, visibility, source)
			VALUES (gen_random_uuid(), ?, ?, ?, ?, 'project', 'custom')
			RETURNING id
		`, projectUUID, name, version, description).Scan(ctx, &createdSchema)
		if err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
		schemaID = createdSchema.ID

		// Insert object types
		for _, ot := range objectTypes {
			typeName, _ := ot["type_name"].(string)
			typeSchemaJSON, _ := json.Marshal(ot)
			if typeName != "" {
				_, err := tx.NewRaw(`
					INSERT INTO kb.project_object_schema_registry (project_id, schema_id, type_name, type_schema)
					VALUES (?, ?, ?, ?)
				`, projectUUID, schemaID, typeName, typeSchemaJSON).Exec(ctx)
				if err != nil {
					return fmt.Errorf("insert object type %s: %w", typeName, err)
				}
			}
		}

		// Insert relationship types
		for _, rt := range relTypes {
			typeName, _ := rt["type_name"].(string)
			if typeName != "" {
				relSchemaJSON, _ := json.Marshal(rt)
				_, err := tx.NewRaw(`
					INSERT INTO kb.project_edge_schema_registry (project_id, schema_id, type_name, type_schema)
					VALUES (?, ?, ?, ?)
				`, projectUUID, schemaID, typeName, relSchemaJSON).Exec(ctx)
				if err != nil {
					return fmt.Errorf("insert relationship type %s: %w", typeName, err)
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Schema \"%s\" created successfully with ID: %s", name, schemaID)}},
	}, nil
}

func (s *Service) executeDeleteSchema(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	_, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	packID, _ := args["schema_id"].(string)
	if packID == "" {
		return nil, fmt.Errorf("schema_id is required")
	}

	type packRow struct {
		ID     string `bun:"id"`
		Name   string `bun:"name"`
		Source string `bun:"source"`
	}

	var pack packRow
	err = s.db.NewSelect().
		TableExpr("kb.graph_schemas").
		Column("id", "name", "source").
		Where("id = ?", packID).
		Where("(project_id = ? OR (org_id = ? AND visibility = 'organization'))", projectID, auth.OrgIDFromContext(ctx)).
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
		DELETE FROM kb.graph_schemas WHERE id = ? AND (project_id = ? OR (org_id = ? AND visibility = 'organization'))
	`, packID, projectID, auth.OrgIDFromContext(ctx)).Exec(ctx)

	if err != nil {
		return nil, fmt.Errorf("delete schema: %w", err)
	}

	result := DeleteSchemaResult{
		Success:  true,
		SchemaID: packID,
		Message:  fmt.Sprintf("Schema \"%s\" deleted successfully", pack.Name),
	}

	return s.wrapResult(result)
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

func (s *Service) executeSchemaHistory(ctx context.Context, projectID string) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	type historyRow struct {
		ID          string     `bun:"id"`
		SchemaID    string     `bun:"schema_id"`
		Name        string     `bun:"name"`
		Version     string     `bun:"version"`
		Active      bool       `bun:"active"`
		InstalledAt time.Time  `bun:"installed_at"`
		RemovedAt   *time.Time `bun:"removed_at"`
	}

	var rows []historyRow
	err = s.db.NewRaw(`
		SELECT
			ptp.id,
			ptp.schema_id,
			gs.name,
			gs.version,
			ptp.active,
			ptp.installed_at,
			ptp.removed_at
		FROM kb.project_schemas ptp
		JOIN kb.graph_schemas gs ON ptp.schema_id = gs.id
		WHERE ptp.project_id = ?
		ORDER BY ptp.installed_at DESC
	`, projectUUID).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("schema history: %w", err)
	}

	type historyItem struct {
		ID          string  `json:"id"`
		SchemaID    string  `json:"schema_id"`
		Name        string  `json:"name"`
		Version     string  `json:"version"`
		Active      bool    `json:"active"`
		InstalledAt string  `json:"installed_at"`
		RemovedAt   *string `json:"removed_at,omitempty"`
		Status      string  `json:"status"`
	}

	items := make([]historyItem, len(rows))
	for i, r := range rows {
		item := historyItem{
			ID:          r.ID,
			SchemaID:    r.SchemaID,
			Name:        r.Name,
			Version:     r.Version,
			Active:      r.Active,
			InstalledAt: r.InstalledAt.Format(time.RFC3339),
			Status:      "installed",
		}
		if r.RemovedAt != nil {
			s := r.RemovedAt.Format(time.RFC3339)
			item.RemovedAt = &s
			item.Status = "uninstalled"
		}
		items[i] = item
	}

	result := map[string]any{
		"project_id": projectID,
		"history":    items,
		"total":      len(items),
	}
	return s.wrapResult(result)
}

func (s *Service) executeSchemaCompiledTypes(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project_id: %w", err)
	}

	verbose, _ := args["verbose"].(bool)

	type objectTypeRow struct {
		TypeName      string         `bun:"type_name"`
		TypeSchema    map[string]any `bun:"json_schema,type:jsonb"`
		SchemaID      string         `bun:"schema_id"`
		SchemaName    string         `bun:"schema_name"`
		SchemaVersion string         `bun:"schema_version"`
	}

	type relTypeRow struct {
		TypeName      string `bun:"type_name"`
		SchemaID      string `bun:"schema_id"`
		SchemaName    string `bun:"schema_name"`
		SchemaVersion string `bun:"schema_version"`
	}

	var objRows []objectTypeRow
	var relRows []relTypeRow

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.SetRLSContext(ctx, tx, projectID); err != nil {
			return err
		}

		if err := tx.NewRaw(`
			SELECT
				por.type_name,
				por.json_schema,
				por.schema_id,
				gs.name  AS schema_name,
				gs.version AS schema_version
			FROM kb.project_object_schema_registry por
			JOIN kb.project_schemas ps ON por.schema_id = ps.schema_id AND ps.project_id = por.project_id
			JOIN kb.graph_schemas gs ON por.schema_id = gs.id
			WHERE por.project_id = ? AND ps.removed_at IS NULL
			ORDER BY por.type_name
		`, projectUUID).Scan(ctx, &objRows); err != nil {
			return err
		}

		return tx.NewRaw(`
			SELECT
				per.type_name,
				per.schema_id,
				gs.name    AS schema_name,
				gs.version AS schema_version
			FROM kb.project_edge_schema_registry per
			JOIN kb.project_schemas ps ON per.schema_id = ps.schema_id AND ps.project_id = per.project_id
			JOIN kb.graph_schemas gs ON per.schema_id = gs.id
			WHERE per.project_id = ? AND ps.removed_at IS NULL
			ORDER BY per.type_name
		`, projectUUID).Scan(ctx, &relRows)
	})
	if err != nil {
		return nil, fmt.Errorf("compiled types: %w", err)
	}

	typeSchemaCount := make(map[string]int)
	for _, r := range objRows {
		typeSchemaCount[r.TypeName]++
	}

	type objectTypeOut struct {
		Name          string         `json:"name"`
		Description   string         `json:"description,omitempty"`
		Properties    map[string]any `json:"properties,omitempty"`
		SchemaID      string         `json:"schema_id,omitempty"`
		SchemaName    string         `json:"schema_name,omitempty"`
		SchemaVersion string         `json:"schema_version,omitempty"`
		Shadowed      bool           `json:"shadowed,omitempty"`
	}

	type relTypeOut struct {
		Name          string `json:"name"`
		SchemaID      string `json:"schema_id,omitempty"`
		SchemaName    string `json:"schema_name,omitempty"`
		SchemaVersion string `json:"schema_version,omitempty"`
	}

	objectTypes := make([]objectTypeOut, 0, len(objRows))
	seenObj := make(map[string]bool)
	for _, r := range objRows {
		shadowed := typeSchemaCount[r.TypeName] > 1 && seenObj[r.TypeName]
		seenObj[r.TypeName] = true

		out := objectTypeOut{Name: r.TypeName}
		if desc, ok := r.TypeSchema["description"].(string); ok {
			out.Description = desc
		}
		if props, ok := r.TypeSchema["properties"].(map[string]any); ok {
			out.Properties = props
		}
		if verbose {
			out.SchemaID = r.SchemaID
			out.SchemaName = r.SchemaName
			out.SchemaVersion = r.SchemaVersion
			out.Shadowed = shadowed
		}
		objectTypes = append(objectTypes, out)
	}

	relTypes := make([]relTypeOut, 0, len(relRows))
	for _, r := range relRows {
		out := relTypeOut{Name: r.TypeName}
		if verbose {
			out.SchemaID = r.SchemaID
			out.SchemaName = r.SchemaName
			out.SchemaVersion = r.SchemaVersion
		}
		relTypes = append(relTypes, out)
	}

	result := map[string]any{
		"project_id":         projectID,
		"object_types":       objectTypes,
		"relationship_types": relTypes,
		"total_object_types": len(objectTypes),
		"total_rel_types":    len(relTypes),
	}
	return s.wrapResult(result)
}

func (s *Service) executeSchemaMigratePreview(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.schemasSvc == nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "schemas service not available"}}}, nil
	}

	fromSchemaID, _ := args["from_schema_id"].(string)
	toSchemaID, _ := args["to_schema_id"].(string)
	if fromSchemaID == "" || toSchemaID == "" {
		return nil, fmt.Errorf("from_schema_id and to_schema_id are required")
	}

	result, err := s.schemasSvc.PreviewSchemaMigration(ctx, projectID, &schemas.SchemaMigrationPreviewRequest{
		FromSchemaID: fromSchemaID,
		ToSchemaID:   toSchemaID,
	})
	if err != nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("preview failed: %v", err)}}}, err
	}

	out, _ := json.Marshal(result)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}

func (s *Service) executeSchemaMigrateExecute(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.schemasSvc == nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "schemas service not available"}}}, nil
	}

	fromSchemaID, _ := args["from_schema_id"].(string)
	toSchemaID, _ := args["to_schema_id"].(string)
	if fromSchemaID == "" || toSchemaID == "" {
		return nil, fmt.Errorf("from_schema_id and to_schema_id are required")
	}

	force, _ := args["force"].(bool)
	maxObjects := 0
	if mo, ok := args["max_objects"].(float64); ok {
		maxObjects = int(mo)
	}

	result, err := s.schemasSvc.ExecuteSchemaMigration(ctx, projectID, &schemas.SchemaMigrationExecuteRequest{
		FromSchemaID: fromSchemaID,
		ToSchemaID:   toSchemaID,
		Force:        force,
		MaxObjects:   maxObjects,
	})
	if err != nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("execute failed: %v", err)}}}, err
	}

	out, _ := json.Marshal(result)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}

func (s *Service) executeSchemaMigrateRollback(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.schemasSvc == nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "schemas service not available"}}}, nil
	}

	toVersion, _ := args["to_version"].(string)
	if toVersion == "" {
		return nil, fmt.Errorf("to_version is required")
	}
	restoreTypeRegistry, _ := args["restore_type_registry"].(bool)

	result, err := s.schemasSvc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion:           toVersion,
		RestoreTypeRegistry: restoreTypeRegistry,
	})
	if err != nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("rollback failed: %v", err)}}}, err
	}

	out, _ := json.Marshal(result)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}

func (s *Service) executeSchemaMigrateCommit(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.schemasSvc == nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "schemas service not available"}}}, nil
	}

	throughVersion, _ := args["through_version"].(string)
	if throughVersion == "" {
		return nil, fmt.Errorf("through_version is required")
	}

	result, err := s.schemasSvc.CommitMigrationArchive(ctx, projectID, &schemas.CommitMigrationArchiveRequest{
		ThroughVersion: throughVersion,
	})
	if err != nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("commit failed: %v", err)}}}, err
	}

	out, _ := json.Marshal(result)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}

func (s *Service) executeSchemaMigrationJobStatus(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.schemasSvc == nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "schemas service not available"}}}, nil
	}

	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	job, err := s.schemasSvc.GetMigrationJobStatus(ctx, projectID, jobID)
	if err != nil {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("job status failed: %v", err)}}}, err
	}

	out, _ := json.Marshal(job)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}
