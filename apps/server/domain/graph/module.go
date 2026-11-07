package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/schemanorm"
)

// Module provides graph domain dependencies.
var Module = fx.Module("graph",
	fx.Provide(NewRepository),
	fx.Provide(
		fx.Annotate(
			provideService,
			fx.ParamTags(``, ``, ``, ``, ``, ``, ``, `optional:"true"`),
		),
	),
	fx.Provide(NewHandler),
	fx.Provide(provideSchemaProvider),
	fx.Provide(provideInverseTypeProvider),
	fx.Provide(provideEmbeddingService),
	fx.Provide(provideBranchStore),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(RegisterIndexWarmup),
)

// provideService constructs the graph service and applies the bulk-update
// limits from application config. It keeps NewService's signature stable for
// tests while threading the env-driven BULK_UPDATE_* knobs into the runtime.
func provideService(repo *Repository, log *slog.Logger, schemaProvider SchemaProvider, inverseTypeProvider InverseTypeProvider, embeddings EmbeddingService, embeddingEnqueuer EmbeddingEnqueuer, relEmbeddingEnqueuer RelationshipEmbeddingEnqueuer, sink EventSink, branchStore branchStoreIface, eventsSvc *events.Service, cfg *config.Config) *Service {
	return NewService(repo, log, schemaProvider, inverseTypeProvider, embeddings, embeddingEnqueuer, relEmbeddingEnqueuer, sink, branchStore, eventsSvc).
		WithBulkUpdateLimits(cfg.Graph.BulkUpdateMaxItems, cfg.Graph.BulkUpdateMaxDuration)
}

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
		// The last install wins, matching GetCompiledTypesByProject and the
		// registry write path: assignments are read oldest-first and each pack's
		// definitions overwrite the map, so the newest install defines an object
		// type, a relationship type's name-wide default, and any identical
		// (name, source, target) triple. Without this ORDER BY Postgres returns
		// rows in arbitrary order and the effective definition is unpredictable
		// (issues #295, #322).
		Order("ps.installed_at ASC").
		Order("ps.created_at ASC").
		Order("ps.id ASC").
		Scan(ctx)

	if err != nil && err != sql.ErrNoRows {
		p.incrementDBLoadError()
		p.log.Warn("error loading schemas from database",
			slog.String("project_id", projectID),
			slog.String("error", err.Error()))
		// Still cache the empty result to avoid repeated DB queries
		schemas := projectSchemasFromPacks(nil)
		p.schemaCache[projectID] = &cachedSchemas{
			schemas: schemas,
			expiry:  time.Now().Add(schemaCacheTTL),
		}
		return schemas, nil
	}

	packs := make([]projectSchemaPack, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.Schema == nil {
			continue
		}
		packs = append(packs, projectSchemaPack{
			Version:                 assignment.Schema.Version,
			ObjectTypeSchemas:       assignment.Schema.ObjectTypeSchemas,
			RelationshipTypeSchemas: assignment.Schema.RelationshipTypeSchemas,
		})
	}

	schemas := projectSchemasFromPacks(packs)

	p.schemaCache[projectID] = &cachedSchemas{
		schemas: schemas,
		expiry:  time.Now().Add(schemaCacheTTL),
	}

	p.incrementDBLoadSuccess()
	p.log.Debug("schema cached",
		slog.String("project_id", projectID),
		slog.Int("object_types", len(schemas.ObjectSchemas)),
		slog.Int("relationship_types", countRelationshipTypeNames(schemas.RelationshipSchemas)))

	return schemas, nil
}

// projectSchemaPack is the part of one active pack assignment the runtime schema
// provider needs. Packs are supplied in precedence order — oldest install first
// — so a later entry overrides an earlier one (#295, #336).
type projectSchemaPack struct {
	Version                 string
	ObjectTypeSchemas       json.RawMessage
	RelationshipTypeSchemas json.RawMessage
}

// projectSchemasFromPacks merges packs (in precedence order) into the schema maps
// edge and object validation read. It is split out of GetProjectSchemas so the
// merge can be tested without a database.
func projectSchemasFromPacks(packs []projectSchemaPack) *ExtractionSchemas {
	objectSchemas := make(map[string]agents.ObjectSchema)
	relationshipSchemas := make(map[string]agents.RelationshipSchema)

	for _, pack := range packs {
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

			objectSchemas[typeName] = objectSchemaFromMap(typeName, pack.Version, schemaMap)
		}

		// Relationship schemas are keyed by (name, source, target), not by name
		// alone, so a same-named type from two packs keeps both definitions
		// (#322). A later pack still wins for the name-wide default and for an
		// identical triple, matching the object-type precedence above.
		for key, schema := range parseRelationshipTypeSchemasToMap(pack.RelationshipTypeSchemas) {
			relationshipSchemas[key] = schema
		}
	}

	return &ExtractionSchemas{
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relationshipSchemas,
	}
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

		var schemas map[string]relationshipTypeSchema
		if err := json.Unmarshal(row.RelationshipTypeSchemas, &schemas); err != nil {
			p.log.Warn("failed to parse relationship type schemas",
				slog.String("project_id", projectID),
				slog.String("error", err.Error()))
			continue
		}

		for relType, schema := range schemas {
			// Normalize the forward type key so it matches the stored type
			// (CreateRelationship always writes lower_snake_case via normalizeRelationType).
			normalizedKey, inverse, derived := inverseEntry(relType, schema)
			if normalizedKey == "" {
				continue
			}
			inverseMap[normalizedKey] = inverse
			if derived {
				p.log.Debug("derived inverse type from inverseLabel",
					slog.String("rel_type", normalizedKey),
					slog.String("inverse_label", schema.InverseLabel),
					slog.String("derived_type", inverse))
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

// relationshipTypeSchema is the subset of a pack's relationship-type schema the
// inverse map needs.
type relationshipTypeSchema struct {
	InverseType  string `json:"inverseType"`
	InverseLabel string `json:"inverseLabel"`
}

// inverseEntry maps one declared relationship type to the (lookup key, inverse
// type) pair GetInverseType answers with, and reports whether the inverse was
// derived from the human-readable label.
//
// Both sides are type keys that are compared against, or written as, a STORED
// type: maybeCreateInverse looks the map up with the normalised write type, and
// the value it returns is itself written through CreateRelationship. They
// therefore use the write path's normaliser, not the label conversion — keying
// them with the weaker helper made a pack declaring `inverseType: hasEmployees`
// store "hasemployees" against a stored "has_employees", so the lookup missed
// silently and no inverse relationship was ever created (#198).
//
// Only inverseLabel is human text, so only it goes through labelToTypeKey.
func inverseEntry(relType string, schema relationshipTypeSchema) (key, inverse string, derived bool) {
	key = normalizeRelationType(strings.TrimSpace(relType))
	if key == "" {
		return "", "", false
	}
	if schema.InverseType != "" {
		// Trim first: a declaration padded with whitespace would otherwise become
		// "_has_employees_" (normalizeRelationType only maps inner spaces), and a
		// whitespace-only declaration would pass the empty guard below.
		if inverse := normalizeRelationType(strings.TrimSpace(schema.InverseType)); inverse != "" {
			return key, inverse, false
		}
	}
	if schema.InverseLabel != "" {
		if inv := labelToTypeKey(schema.InverseLabel); inv != "" {
			return key, inv, true
		}
	}
	return "", "", false
}

// labelToTypeKey converts a human-readable relationship label to a snake_case
// type key. E.g. "Has Employees" → "has_employees", "CHILD_OF" → "child_of",
// "hasEmployees" → "has_employees".
//
// The camelCase split matters (#198): an inverseLabel is converted here and the
// result is written back as a relationship type through CreateRelationship, so
// it has to agree with normalizeRelationType — "hasemployees" would never match
// a stored "has_employees". Declared type keys (the forward type and
// inverseType) are converted by inverseEntry directly.
func labelToTypeKey(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	// Human labels use spaces and hyphens where a type key uses underscores.
	// This runs before normalizeRelationType, which splits camelCase but knows
	// nothing about hyphens or repeated separators.
	key := strings.ReplaceAll(label, " ", "_")
	key = strings.ReplaceAll(key, "-", "_")
	// Collapse consecutive underscores.
	for strings.Contains(key, "__") {
		key = strings.ReplaceAll(key, "__", "_")
	}
	return normalizeRelationType(key)
}

// objectSchemaFromMap converts a single decoded object-type definition (the
// per-type JSON object from object_type_schemas, after array/map normalisation)
// into the agents.ObjectSchema the graph service consumes. Work configuration
// (boardEnabled/allowedStatuses and the operational skip flags) is applied via
// ApplyConfig, so the graph path enforces the same per-type work config as the
// schemas/compiled-types path.
func objectSchemaFromMap(typeName, version string, schemaMap map[string]any) agents.ObjectSchema {
	schema := agents.ObjectSchema{Name: typeName, Version: version}

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
			schema.Properties[propName] = agents.PropertyDefFromMap(propMap)
		}
	}

	if req, ok := schemaMap["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	schema.ApplyConfig(schemaMap)

	return schema
}

// parseObjectTypeSchemasToMap normalises the two storage formats used for
// object_type_schemas in kb.graph_schemas:
//
//   - Array format (user YAML files): [{name, label, description, properties, ...}, ...]
//   - Map format  (blueprint seeds / epf-engine v3): {TypeName: {label, description, properties, ...}, ...}
//
// Returns a map of typeName → raw JSON definition, or nil on empty/invalid input.
//
// The parsing lives in pkg/schemanorm so the graph runtime path and the
// schemas/compiled-types path (domain/schemas) cannot drift: a previous copy in
// this file reconstructed array entries with only properties/label/description
// and dropped the object-driven work configuration, making runtime work-status
// enforcement inert. domain/schemas imports domain/graph, so the shared helper
// cannot live in either domain without an import cycle.
func parseObjectTypeSchemasToMap(data json.RawMessage) map[string]json.RawMessage {
	return schemanorm.ObjectTypeSchemasToMap(data)
}

// parseRelationshipTypeSchemasToMap normalises the two JSONB storage formats for
// relationship_type_schemas into the map entries the graph runtime validates
// against.
//
// Storage formats:
//   - Map format:   {"belongs_to": {"sourceTypes": [...], "targetTypes": [...]}, ...}
//   - Array format: [{"name": "belongs_to", "sourceType": "Service", "targetType": "Domain"}, ...]
//
// The array format allows multiple entries with the same relationship name but
// different sourceType/targetType pairs. Entries that repeat a name with the same
// property schema are folded into one variant (issue #115: `belongs_to` from four
// source types is one schema, not four). Entries that repeat a name with a
// DIFFERENT property schema stay separate (#322): `owns` Person→Vehicle
// (share_percent) and `owns` Person→Account (holder_role) are two schemas, and
// edges of each shape must validate against their own.
//
// Each declared identity is keyed with relationshipSchemaKey(name, source,
// target) — the identity domain/schemas uses for compiled types. The bare
// normalised name is also kept as the name-wide default, used only when an
// edge's endpoint pair declares no schema of its own (for example a dangling
// edge whose endpoint no longer resolves); within one pack the first
// declaration of a name defines that default.
//
// Keys are canonicalised with normalizeRelationType: relationship rows are stored
// lower_snake_case, so the registry must be keyed the same way or a declared
// `knows` schema is skipped by a `KNOWS` write (#176).
func parseRelationshipTypeSchemasToMap(data json.RawMessage) map[string]agents.RelationshipSchema {
	variants := parseRelationshipTypeVariants(data)
	if len(variants) == 0 {
		return nil
	}

	result := make(map[string]agents.RelationshipSchema)
	for _, variant := range variants {
		name := normalizeRelationType(variant.Schema.Name)
		if name == "" {
			continue
		}

		// Name-wide default. Per-pack this is the first declaration of the name,
		// which is deterministic because variants keep their declaration order;
		// across packs the newest install wins because the merge writes packs
		// oldest-first and later entries overwrite earlier ones.
		if _, exists := result[name]; !exists {
			fallback := variant.Schema
			fallback.SourceTypes = variant.SourceTypes
			fallback.TargetTypes = variant.TargetTypes
			result[name] = fallback
		}

		for _, endpoints := range variant.Endpoints {
			schema := variant.Schema
			schema.SourceTypes = narrowTypes(variant.SourceTypes, endpoints[0])
			schema.TargetTypes = narrowTypes(variant.TargetTypes, endpoints[1])
			result[relationshipSchemaKey(name, endpoints[0], endpoints[1])] = schema
		}
	}

	return result
}

// relationshipSchemaKeySeparator joins the parts of a relationship schema
// identity. A relationship type key is authored text and never carries a NUL, so
// a qualified key cannot be mistaken for the bare name of another type.
const relationshipSchemaKeySeparator = "\x00"

// relationshipSchemaKey is the identity of one relationship schema variant: the
// relationship type plus the endpoint object types it was declared for. An empty
// source or target type means "any", so a declaration that names neither is keyed
// by the bare name and acts as the name-wide default.
func relationshipSchemaKey(name, srcType, tgtType string) string {
	if srcType == "" && tgtType == "" {
		return name
	}
	return name + relationshipSchemaKeySeparator + srcType + relationshipSchemaKeySeparator + tgtType
}

// countRelationshipTypeNames counts the distinct relationship type names in the
// registry. The map also holds one entry per declared (name, source, target)
// identity (#322), so len() would overstate the number of types a project has.
func countRelationshipTypeNames(schemas map[string]agents.RelationshipSchema) int {
	names := 0
	for key := range schemas {
		if !strings.Contains(key, relationshipSchemaKeySeparator) {
			names++
		}
	}
	return names
}

// wildcardOr returns the declared endpoint types, or a single empty string
// ("any") when the declaration names none.
func wildcardOr(types []string) []string {
	if len(types) == 0 {
		return []string{""}
	}
	return types
}

// narrowTypes narrows a declaration's endpoint list to the single type a
// qualified key was built for, so the resolved schema describes its own identity.
// A wildcard (empty) endpoint keeps the declaration as authored.
func narrowTypes(declared []string, endpoint string) []string {
	if endpoint == "" {
		return declared
	}
	return []string{endpoint}
}

// relationshipSchemaVariant is one declared relationship schema together with the
// endpoints it was declared for. An empty SourceTypes/TargetTypes slice means
// "any" for that end; Endpoints holds the exact (source, target) pairs, so
// folding two entries into one variant never invents a cross-product pair that
// neither entry declared.
type relationshipSchemaVariant struct {
	Schema      agents.RelationshipSchema
	SourceTypes []string
	TargetTypes []string
	Endpoints   [][2]string
}

// addEndpoint records a declared endpoint pair once per variant.
func (v *relationshipSchemaVariant) addEndpoint(srcType, tgtType string) {
	for _, existing := range v.Endpoints {
		if existing[0] == srcType && existing[1] == tgtType {
			return
		}
	}
	v.Endpoints = append(v.Endpoints, [2]string{srcType, tgtType})
}

// relationshipVariantSignature identifies a variant's property schema. Repeated
// declarations that differ only in their endpoints share a signature and are
// folded into one variant; declarations that differ in name, description,
// properties or required fields stay apart.
func relationshipVariantSignature(schema agents.RelationshipSchema) string {
	props := schema.Properties
	if len(props) == 0 {
		// A missing `properties` and an empty object declare the same thing.
		props = nil
	}
	raw, err := json.Marshal(struct {
		Name        string
		Description string
		Properties  map[string]agents.PropertyDef
		Required    []string
	}{
		normalizeRelationType(schema.Name),
		schema.Description,
		props,
		schema.Required,
	})
	if err != nil {
		// Property definitions are plain JSON data, so this is unreachable in
		// practice; fall back to the pre-#322 behaviour (fold by name) rather
		// than inventing an unstable signature.
		return "unhashable:" + normalizeRelationType(schema.Name)
	}
	return string(raw)
}

// parseRelationshipTypeVariants decodes relationship_type_schemas into one variant
// per distinct property schema, preserving declaration order. Array-format
// entries are read in file order; map-format names are read in sorted order so
// map iteration can never decide the name-wide default.
func parseRelationshipTypeVariants(data json.RawMessage) []relationshipSchemaVariant {
	if len(data) == 0 {
		return nil
	}

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
		var variants []relationshipSchemaVariant
		index := make(map[string]int, len(arr))
		add := func(schema agents.RelationshipSchema, srcTypes, tgtTypes []string) {
			srcs, tgts := wildcardOr(srcTypes), wildcardOr(tgtTypes)
			signature := relationshipVariantSignature(schema)
			i, ok := index[signature]
			if !ok {
				i = len(variants)
				index[signature] = i
				variants = append(variants, relationshipSchemaVariant{Schema: schema})
			}
			variants[i].SourceTypes = appendUnique(variants[i].SourceTypes, srcTypes...)
			variants[i].TargetTypes = appendUnique(variants[i].TargetTypes, tgtTypes...)
			// One entry expands to its own source×target product; folding a
			// later entry in only adds the pairs that entry declared.
			for _, srcType := range srcs {
				for _, tgtType := range tgts {
					variants[i].addEndpoint(srcType, tgtType)
				}
			}
		}

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

			schema := agents.RelationshipSchema{
				Name:        entry.Name,
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
			add(schema, filteredSrc, filteredTgt)
		}
		if len(variants) > 0 {
			return variants
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
		names := make([]string, 0, len(mapFmt))
		for name := range mapFmt {
			names = append(names, name)
		}
		sort.Strings(names)

		variants := make([]relationshipSchemaVariant, 0, len(names))
		for _, name := range names {
			entry := mapFmt[name]
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
			variant := relationshipSchemaVariant{
				Schema:      schema,
				SourceTypes: filteredSrc,
				TargetTypes: filteredTgt,
			}
			for _, srcType := range wildcardOr(filteredSrc) {
				for _, tgtType := range wildcardOr(filteredTgt) {
					variant.addEndpoint(srcType, tgtType)
				}
			}
			variants = append(variants, variant)
		}
		if len(variants) > 0 {
			return variants
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
