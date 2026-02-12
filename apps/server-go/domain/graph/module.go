package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/domain/extraction/agents"
	"github.com/emergent/emergent-core/pkg/embeddings"
)

// Module provides graph domain dependencies.
var Module = fx.Module("graph",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(provideSchemaProvider),
	fx.Provide(provideInverseTypeProvider),
	fx.Provide(provideEmbeddingService),
	fx.Invoke(RegisterRoutes),
)

// provideEmbeddingService bridges *embeddings.Service to the graph.EmbeddingService interface.
func provideEmbeddingService(svc *embeddings.Service) EmbeddingService {
	return svc
}

// ProvideSchemaProvider creates a schema provider (exported for tests).
func ProvideSchemaProvider(db bun.IDB, log *slog.Logger) SchemaProvider {
	return &templatePackSchemaProviderAdapter{
		db:          db,
		log:         log,
		schemaCache: make(map[string]*cachedSchemas),
	}
}

func provideSchemaProvider(db bun.IDB, log *slog.Logger) SchemaProvider {
	return ProvideSchemaProvider(db, log)
}

// templatePackSchemaProviderAdapter adapts template pack queries without importing extraction package.
type templatePackSchemaProviderAdapter struct {
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

func (p *templatePackSchemaProviderAdapter) GetProjectSchemas(ctx context.Context, projectID string) (*ExtractionSchemas, error) {
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

	type GraphTemplatePack struct {
		bun.BaseModel           `bun:"kb.graph_template_packs,alias:gtp"`
		ID                      string  `bun:"id,pk,type:uuid"`
		Name                    string  `bun:"name,notnull"`
		Version                 string  `bun:"version,notnull"`
		ObjectTypeSchemas       JSONMap `bun:"object_type_schemas,type:jsonb,notnull"`
		RelationshipTypeSchemas JSONMap `bun:"relationship_type_schemas,type:jsonb,default:'{}'"`
	}

	type ProjectTemplatePack struct {
		bun.BaseModel  `bun:"kb.project_template_packs,alias:ptp"`
		ProjectID      string             `bun:"project_id,notnull,type:uuid"`
		TemplatePackID string             `bun:"template_pack_id,notnull,type:uuid"`
		Active         bool               `bun:"active,default:true"`
		TemplatePack   *GraphTemplatePack `bun:"rel:belongs-to,join:template_pack_id=id"`
	}

	var assignments []ProjectTemplatePack
	err := p.db.NewSelect().
		Model(&assignments).
		Relation("TemplatePack").
		Where("ptp.project_id = ?", projectID).
		Where("ptp.active = true").
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
		if assignment.TemplatePack == nil {
			continue
		}

		pack := assignment.TemplatePack

		for typeName, schemaRaw := range pack.ObjectTypeSchemas {
			schemaMap, ok := schemaRaw.(map[string]any)
			if !ok {
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

		for typeName, schemaRaw := range pack.RelationshipTypeSchemas {
			schemaMap, ok := schemaRaw.(map[string]any)
			if !ok {
				continue
			}

			schema := agents.RelationshipSchema{Name: typeName}

			if desc, ok := schemaMap["description"].(string); ok {
				schema.Description = desc
			}

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

func (p *templatePackSchemaProviderAdapter) incrementCacheHit() {
	p.metricsMu.Lock()
	p.cacheHits++
	p.metricsMu.Unlock()
}

func (p *templatePackSchemaProviderAdapter) incrementCacheMiss() {
	p.metricsMu.Lock()
	p.cacheMisses++
	p.metricsMu.Unlock()
}

func (p *templatePackSchemaProviderAdapter) incrementDBLoadSuccess() {
	p.metricsMu.Lock()
	p.dbLoadSuccess++
	p.metricsMu.Unlock()
}

func (p *templatePackSchemaProviderAdapter) incrementDBLoadError() {
	p.metricsMu.Lock()
	p.dbLoadErrors++
	p.metricsMu.Unlock()
}

func (p *templatePackSchemaProviderAdapter) Metrics() SchemaProviderMetrics {
	p.metricsMu.RLock()
	defer p.metricsMu.RUnlock()
	return SchemaProviderMetrics{
		CacheHits:     p.cacheHits,
		CacheMisses:   p.cacheMisses,
		DBLoadSuccess: p.dbLoadSuccess,
		DBLoadErrors:  p.dbLoadErrors,
	}
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

// inverseTypeProviderAdapter loads inverseType mappings from template pack JSONB.
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

	// Load from DB: get relationship_type_schemas from all active template packs for this project
	query := `
		SELECT tp.relationship_type_schemas
		FROM kb.project_template_packs ptp
		JOIN kb.graph_template_packs tp ON ptp.template_pack_id = tp.id
		WHERE ptp.project_id = ? AND ptp.active = true
		AND tp.relationship_type_schemas IS NOT NULL
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
			InverseType string `json:"inverseType"`
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
