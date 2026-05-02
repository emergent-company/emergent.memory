package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// Module provides graph domain dependencies.
var Module = fx.Module("graph",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(NewSessionService),
	fx.Provide(NewSessionHandler),
	fx.Provide(provideSchemaProvider),
	fx.Provide(provideInverseTypeProvider),
	fx.Provide(provideEmbeddingService),
	fx.Provide(provideBranchStore),
	fx.Invoke(RegisterRoutes),
)

// provideBranchStore bridges *branches.Store to the graph.branchStoreIface.
func provideBranchStore(s *branches.Store) branchStoreIface {
	return s
}

// provideEmbeddingService bridges *embeddings.Service to the graph.EmbeddingService interface.
func provideEmbeddingService(svc *embeddings.Service) EmbeddingService {
	return svc
}

// ProvideSchemaProvider creates a schema provider (exported for tests).
func ProvideSchemaProvider(db bun.IDB, log *slog.Logger) SchemaProvider {
	return &schemaProviderAdapter{
		db:          db,
		log:         log,
		schemaCache: make(map[string]*cachedSchemas),
	}
}

func provideSchemaProvider(db bun.IDB, log *slog.Logger) SchemaProvider {
	return ProvideSchemaProvider(db, log)
}

// schemaProviderAdapter adapts schema queries without importing extraction package.
type schemaProviderAdapter struct {
	db  bun.IDB
	log *slog.Logger

	cacheMu     sync.RWMutex
	schemaCache map[string]*cachedSchemas

	// Metrics
	metricsMu     sync.RWMutex
	cacheHits     int64
	cacheMisses   int64
	dbLoadSuccess int64
	dbLoadErrors  int64
}

type cachedSchemas struct {
	schemas *ExtractionSchemas
	expiry  time.Time
}

const schemaCacheTTL = 5 * time.Minute

func (p *schemaProviderAdapter) GetProjectSchemas(ctx context.Context, projectID string) (*ExtractionSchemas, error) {
	p.cacheMu.RLock()
	if cached, ok := p.schemaCache[projectID]; ok && time.Now().Before(cached.expiry) {
		schemas := cached.schemas
		p.cacheMu.RUnlock()
		p.incrementCacheHit()
		p.log.Debug("schema cache hit", slog.String("project_id", projectID))
		return schemas, nil
	}
	p.cacheMu.RUnlock()

	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	if cached, ok := p.schemaCache[projectID]; ok && time.Now().Before(cached.expiry) {
		p.incrementCacheHit()
		p.log.Debug("schema cache hit (double-check)", slog.String("project_id", projectID))
		return cached.schemas, nil
	}

	p.incrementCacheMiss()
	p.log.Debug("schema cache miss, loading from database", slog.String("project_id", projectID))

	type GraphSchema struct {
		bun.BaseModel           `bun:"kb.graph_schemas,alias:gs"`
		ID                      string          `bun:"id,pk,type:uuid"`
		Name                    string          `bun:"name,notnull"`
		Version                 string          `bun:"version,notnull"`
		ObjectTypeSchemas       json.RawMessage `bun:"object_type_schemas,type:jsonb"`
		RelationshipTypeSchemas json.RawMessage `bun:"relationship_type_schemas,type:jsonb"`
	}

	type ProjectSchemaAssignment struct {
		bun.BaseModel `bun:"kb.project_schemas,alias:ps"`
		ProjectID     string       `bun:"project_id,notnull,type:uuid"`
		SchemaID      string       `bun:"schema_id,notnull,type:uuid"`
		Active        bool         `bun:"active,default:true"`
		Schema        *GraphSchema `bun:"rel:belongs-to,join:schema_id=id"`
	}

	var assignments []ProjectSchemaAssignment
	err := p.db.NewSelect().
		Model(&assignments).
		Relation("Schema").
		Where("ps.project_id = ?", projectID).
		Where("ps.active = true").
		Where("ps.removed_at IS NULL").
		Scan(ctx)

	objectSchemas := make(map[string]agents.ObjectSchema)
	relationshipSchemas := make(map[string]agents.RelationshipSchema)

	if err != nil && err != sql.ErrNoRows {
		p.incrementDBLoadError()
		p.log.Warn("error loading schemas from database",
			slog.String("project_id", projectID),
			slog.String("error", err.Error()))
		// Still cache the empty result to avoid repeated DB queries
		schemas := &ExtractionSchemas{
			ObjectSchemas:       objectSchemas,
			RelationshipSchemas: relationshipSchemas,
		}
		p.schemaCache[projectID] = &cachedSchemas{
			schemas: schemas,
			expiry:  time.Now().Add(schemaCacheTTL),
		}
		return schemas, nil
	}

	for _, assignment := range assignments {
		if assignment.Schema == nil {
			continue
		}

		pack := assignment.Schema

		// parseObjectTypeSchemasToMap handles both storage formats:
		//   - Array format: [{name, label, description, properties, ...}, ...]
		//   - Map format:   {TypeName: {label, description, properties, ...}, ...}
		// epf-engine v3 and blueprint seeds use the map format; user-uploaded
		// YAML files typically use the array format.
		objTypeMap := parseObjectTypeSchemasToMap(pack.ObjectTypeSchemas)
		for typeName, raw := range objTypeMap {
			var schemaMap map[string]any
			if err := json.Unmarshal(raw, &schemaMap); err != nil {
				continue
			}

			schema := agents.ObjectSchema{Name: typeName}

			if desc, ok := schemaMap["description"].(string); ok {
				schema.Description = desc
			}

			if props, ok := schemaMap["properties"].(map[string]any); ok {
				schema.Properties = make(map[string]agents.PropertyDef)
				for propName, propRaw := range props {
					propMap, ok := propRaw.(map[string]any)
					if !ok {
						continue
					}
					propDef := agents.PropertyDef{}
					if t, ok := propMap["type"].(string); ok {
						propDef.Type = t
					}
					if d, ok := propMap["description"].(string); ok {
						propDef.Description = d
					}
					schema.Properties[propName] = propDef
				}
			}

			if req, ok := schemaMap["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						schema.Required = append(schema.Required, s)
					}
				}
			}

			objectSchemas[typeName] = schema
		}

		for typeName, schema := range parseRelationshipTypeSchemasToMap(pack.RelationshipTypeSchemas) {
			relationshipSchemas[typeName] = schema
		}
	}

	schemas := &ExtractionSchemas{
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relationshipSchemas,
	}

	p.schemaCache[projectID] = &cachedSchemas{
		schemas: schemas,
		expiry:  time.Now().Add(schemaCacheTTL),
	}

	p.incrementDBLoadSuccess()
	p.log.Debug("schema cached",
		slog.String("project_id", projectID),
		slog.Int("object_types", len(objectSchemas)),
		slog.Int("relationship_types", len(relationshipSchemas)))

	return schemas, nil
}

func (p *schemaProviderAdapter) incrementCacheHit() {
	p.metricsMu.Lock()
	p.cacheHits++
	p.metricsMu.Unlock()
}

func (p *schemaProviderAdapter) incrementCacheMiss() {
	p.metricsMu.Lock()
	p.cacheMisses++
	p.metricsMu.Unlock()
}

func (p *schemaProviderAdapter) incrementDBLoadSuccess() {
	p.metricsMu.Lock()
	p.dbLoadSuccess++
	p.metricsMu.Unlock()
}

func (p *schemaProviderAdapter) incrementDBLoadError() {
	p.metricsMu.Lock()
	p.dbLoadErrors++
	p.metricsMu.Unlock()
}

func (p *schemaProviderAdapter) Metrics() SchemaProviderMetrics {
	p.metricsMu.RLock()
	defer p.metricsMu.RUnlock()
	return SchemaProviderMetrics{
		CacheHits:     p.cacheHits,
		CacheMisses:   p.cacheMisses,
		DBLoadSuccess: p.dbLoadSuccess,
		DBLoadErrors:  p.dbLoadErrors,
	}
}

// InvalidateProjectCache evicts the cached schemas for a project so the next
// call to GetProjectSchemas fetches fresh data from the database.
func (p *schemaProviderAdapter) InvalidateProjectCache(projectID string) {
	p.cacheMu.Lock()
	delete(p.schemaCache, projectID)
	p.cacheMu.Unlock()
	p.log.Debug("schema cache invalidated", slog.String("project_id", projectID))
}

type SchemaProviderMetrics struct {
	CacheHits     int64
	CacheMisses   int64
	DBLoadSuccess int64
	DBLoadErrors  int64
}

type JSONMap map[string]any

func (j *JSONMap) Scan(value any) error {
	if value == nil {
		*j = make(map[string]any)
		return nil
	}
	switch v := value.(type) {
	case []byte:
		m := make(map[string]any)
		if err := json.Unmarshal(v, &m); err != nil {
			return err
		}
		*j = m
		return nil
	case string:
		m := make(map[string]any)
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			return err
		}
		*j = m
		return nil
	default:
		return nil
	}
}

// =============================================================================
// Inverse Type Provider
// =============================================================================

// ProvideInverseTypeProvider creates an inverse type provider (exported for tests).
func ProvideInverseTypeProvider(db bun.IDB, log *slog.Logger) InverseTypeProvider {
	return &inverseTypeProviderAdapter{
		db:    db,
		log:   log,
		cache: make(map[string]*cachedInverseMap),
	}
}

func provideInverseTypeProvider(db bun.IDB, log *slog.Logger) InverseTypeProvider {
	return ProvideInverseTypeProvider(db, log)
}

// inverseTypeProviderAdapter loads inverseType mappings from schema JSONB.
type inverseTypeProviderAdapter struct {
	db  bun.IDB
	log *slog.Logger

	cacheMu sync.RWMutex
	cache   map[string]*cachedInverseMap
}

type cachedInverseMap struct {
	// inverseMap: relType -> inverseType (e.g. "PARENT_OF" -> "CHILD_OF")
	inverseMap map[string]string
	expiry     time.Time
}

const inverseMapCacheTTL = 5 * time.Minute

func (p *inverseTypeProviderAdapter) GetInverseType(ctx context.Context, projectID string, relType string) (string, bool) {
	inverseMap := p.getOrLoadInverseMap(ctx, projectID)
	if inverseMap == nil {
		return "", false
	}
	inverse, ok := inverseMap[relType]
	return inverse, ok
}

func (p *inverseTypeProviderAdapter) getOrLoadInverseMap(ctx context.Context, projectID string) map[string]string {
	p.cacheMu.RLock()
	if cached, ok := p.cache[projectID]; ok && time.Now().Before(cached.expiry) {
		m := cached.inverseMap
		p.cacheMu.RUnlock()
		return m
	}
	p.cacheMu.RUnlock()

	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := p.cache[projectID]; ok && time.Now().Before(cached.expiry) {
		return cached.inverseMap
	}

	// Load from DB: get relationship_type_schemas from all active schemas for this project
	query := `
		SELECT gs.relationship_type_schemas
		FROM kb.project_schemas ps
		JOIN kb.graph_schemas gs ON ps.schema_id = gs.id
		WHERE ps.project_id = ? AND ps.active = true
		AND ps.removed_at IS NULL
		AND gs.relationship_type_schemas IS NOT NULL
	`

	var results []struct {
		RelationshipTypeSchemas json.RawMessage `bun:"relationship_type_schemas"`
	}
	_, err := p.db.NewRaw(query, projectID).Exec(ctx, &results)
	if err != nil {
		p.log.Warn("failed to load inverse type mappings",
			slog.String("project_id", projectID),
			slog.String("error", err.Error()))
		// Cache empty map to avoid repeated failures
		p.cache[projectID] = &cachedInverseMap{
			inverseMap: make(map[string]string),
			expiry:     time.Now().Add(inverseMapCacheTTL),
		}
		return nil
	}

	inverseMap := make(map[string]string)
	for _, row := range results {
		if row.RelationshipTypeSchemas == nil {
			continue
		}

		var schemas map[string]struct {
			InverseType  string `json:"inverseType"`
			InverseLabel string `json:"inverseLabel"`
		}
		if err := json.Unmarshal(row.RelationshipTypeSchemas, &schemas); err != nil {
			p.log.Warn("failed to parse relationship type schemas",
				slog.String("project_id", projectID),
				slog.String("error", err.Error()))
			continue
		}

		for relType, schema := range schemas {
			if schema.InverseType != "" {
				inverseMap[relType] = schema.InverseType
			} else if schema.InverseLabel != "" {
				// Derive the inverse type key from the human-readable label when
				// inverseType is not explicitly set. Converts "Has Employees" → "has_employees".
				derived := labelToTypeKey(schema.InverseLabel)
				if derived != "" {
					inverseMap[relType] = derived
					p.log.Debug("derived inverse type from inverseLabel",
						slog.String("rel_type", relType),
						slog.String("inverse_label", schema.InverseLabel),
						slog.String("derived_type", derived))
				}
			}
		}
	}

	p.cache[projectID] = &cachedInverseMap{
		inverseMap: inverseMap,
		expiry:     time.Now().Add(inverseMapCacheTTL),
	}

	p.log.Debug("inverse type map cached",
		slog.String("project_id", projectID),
		slog.Int("mappings", len(inverseMap)))

	return inverseMap
}

// labelToTypeKey converts a human-readable relationship label to a snake_case type key.
// E.g. "Has Employees" → "has_employees", "CHILD_OF" → "child_of".
// Returns empty string if the input is blank.
func labelToTypeKey(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	// Lowercase and replace spaces/hyphens with underscores.
	key := strings.ToLower(label)
	key = strings.ReplaceAll(key, " ", "_")
	key = strings.ReplaceAll(key, "-", "_")
	// Collapse consecutive underscores.
	for strings.Contains(key, "__") {
		key = strings.ReplaceAll(key, "__", "_")
	}
	return key
}

// parseObjectTypeSchemasToMap normalises the two storage formats used for
// object_type_schemas in kb.graph_schemas:
//
//   - Array format (user YAML files): [{name, label, description, properties, ...}, ...]
//   - Map format  (blueprint seeds / epf-engine v3): {TypeName: {label, description, properties, ...}, ...}
//
// Returns a map of typeName → raw JSON definition, or nil on empty/invalid input.
func parseObjectTypeSchemasToMap(data json.RawMessage) map[string]json.RawMessage {
	if len(data) == 0 {
		return nil
	}

	// Try array format first.
	var arr []struct {
		Name        string          `json:"name"`
		Label       string          `json:"label"`
		Description string          `json:"description"`
		Properties  json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		result := make(map[string]json.RawMessage, len(arr))
		for _, item := range arr {
			if item.Name == "" {
				continue
			}
			schema := map[string]json.RawMessage{}
			if len(item.Properties) > 0 {
				schema["properties"] = item.Properties
			}
			if item.Label != "" {
				lb, _ := json.Marshal(item.Label)
				schema["label"] = lb
			}
			if item.Description != "" {
				desc, _ := json.Marshal(item.Description)
				schema["description"] = desc
			}
			schemaBytes, err := json.Marshal(schema)
			if err != nil {
				continue
			}
			result[item.Name] = schemaBytes
		}
		if len(result) > 0 {
			return result
		}
	}

	// Fall back to map format.
	var objMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &objMap); err == nil && len(objMap) > 0 {
		return objMap
	}

	return nil
}

// parseRelationshipTypeSchemasToMap normalises the two JSONB storage formats for
// relationship_type_schemas into a map[name]agents.RelationshipSchema.
//
// Storage formats:
//   - Map format:   {"belongs_to": {"sourceTypes": [...], "targetTypes": [...]}, ...}
//   - Array format: [{"name": "belongs_to", "sourceType": "Service", "targetType": "Domain"}, ...]
//
// The array format allows multiple entries with the same relationship name but different
// sourceType/targetType pairs. These are merged into a single RelationshipSchema with
// combined SourceTypes/TargetTypes slices (avoiding last-one-wins overwriting).
func parseRelationshipTypeSchemasToMap(data json.RawMessage) map[string]agents.RelationshipSchema {
	if len(data) == 0 {
		return nil
	}

	result := make(map[string]agents.RelationshipSchema)

	// Try array format first (used by user-uploaded YAML/JSON schemas).
	type relEntryRaw struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		SourceType   string          `json:"sourceType"`
		TargetType   string          `json:"targetType"`
		SourceTypes  []string        `json:"sourceTypes"`
		TargetTypes  []string        `json:"targetTypes"`
		Source       string          `json:"source"`
		Target       string          `json:"target"`
		SnakeSrcType string          `json:"source_type"`
		SnakeTgtType string          `json:"target_type"`
		Properties   json.RawMessage `json:"properties"`
		Required     []string        `json:"required"`
	}

	var arr []relEntryRaw
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		for _, entry := range arr {
			if entry.Name == "" {
				continue
			}

			// Collect all source/target types from all naming conventions.
			srcTypes := append(entry.SourceTypes, entry.SnakeSrcType, entry.SourceType, entry.Source)
			tgtTypes := append(entry.TargetTypes, entry.SnakeTgtType, entry.TargetType, entry.Target)

			// Filter empty strings.
			var filteredSrc, filteredTgt []string
			for _, s := range srcTypes {
				if s != "" {
					filteredSrc = append(filteredSrc, s)
				}
			}
			for _, t := range tgtTypes {
				if t != "" {
					filteredTgt = append(filteredTgt, t)
				}
			}

			existing, alreadySeen := result[entry.Name]
			if alreadySeen {
				// Merge source/target types from this entry into the existing schema.
				existing.SourceTypes = appendUnique(existing.SourceTypes, filteredSrc...)
				existing.TargetTypes = appendUnique(existing.TargetTypes, filteredTgt...)
				result[entry.Name] = existing
			} else {
				schema := agents.RelationshipSchema{
					Name:        entry.Name,
					Description: entry.Description,
					SourceTypes: filteredSrc,
					TargetTypes: filteredTgt,
				}
				if len(entry.Properties) > 0 {
					var propMap map[string]agents.PropertyDef
					if json.Unmarshal(entry.Properties, &propMap) == nil {
						schema.Properties = propMap
					}
				}
				schema.Required = entry.Required
				result[entry.Name] = schema
			}
		}
		if len(result) > 0 {
			return result
		}
	}

	// Fall back to map format (used by epf-engine v3 seeds and blueprints).
	type relMapEntry struct {
		Description string          `json:"description"`
		SourceTypes []string        `json:"sourceTypes"`
		TargetTypes []string        `json:"targetTypes"`
		SourceType  string          `json:"sourceType"`
		TargetType  string          `json:"targetType"`
		Properties  json.RawMessage `json:"properties"`
		Required    []string        `json:"required"`
	}
	var mapFmt map[string]relMapEntry
	if err := json.Unmarshal(data, &mapFmt); err == nil && len(mapFmt) > 0 {
		for name, entry := range mapFmt {
			src := append(entry.SourceTypes, entry.SourceType)
			tgt := append(entry.TargetTypes, entry.TargetType)
			var filteredSrc, filteredTgt []string
			for _, s := range src {
				if s != "" {
					filteredSrc = append(filteredSrc, s)
				}
			}
			for _, t := range tgt {
				if t != "" {
					filteredTgt = append(filteredTgt, t)
				}
			}
			schema := agents.RelationshipSchema{
				Name:        name,
				Description: entry.Description,
				SourceTypes: filteredSrc,
				TargetTypes: filteredTgt,
				Required:    entry.Required,
			}
			if len(entry.Properties) > 0 {
				var propMap map[string]agents.PropertyDef
				if json.Unmarshal(entry.Properties, &propMap) == nil {
					schema.Properties = propMap
				}
			}
			result[name] = schema
		}
		if len(result) > 0 {
			return result
		}
	}

	return nil
}

// appendUnique appends values to a slice, skipping duplicates.
func appendUnique(slice []string, values ...string) []string {
	seen := make(map[string]struct{}, len(slice))
	for _, s := range slice {
		seen[s] = struct{}{}
	}
	for _, v := range values {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			slice = append(slice, v)
		}
	}
	return slice
}
