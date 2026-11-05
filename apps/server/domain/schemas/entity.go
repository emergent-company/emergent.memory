package schemas

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// ---------------------------------------------------------------------------
// Migration Hints
// ---------------------------------------------------------------------------

// SchemaMigrationHints describes how to upgrade from a previous schema version.
// When present in a schema definition, auto-migration is triggered on assign.
type SchemaMigrationHints struct {
	// FromVersion is the schema version this migration applies from (required when block is present).
	FromVersion string `json:"from_version" yaml:"from_version"`
	// TypeRenames lists object/relationship type renames to apply.
	TypeRenames []TypeRename `json:"type_renames,omitempty" yaml:"type_renames,omitempty"`
	// PropertyRenames lists property renames within types.
	PropertyRenames []PropertyRename `json:"property_renames,omitempty" yaml:"property_renames,omitempty"`
	// RemovedProperties lists properties that are intentionally dropped.
	// Their data will still be archived but no warning is emitted.
	RemovedProperties []RemovedProperty `json:"removed_properties,omitempty" yaml:"removed_properties,omitempty"`
}

// RemovedProperty identifies a property that is intentionally removed in a migration.
type RemovedProperty struct {
	TypeName string `json:"type_name" yaml:"type_name"`
	Name     string `json:"name" yaml:"name"`
}

// MigrationHop is a single step in a multi-hop migration chain.
type MigrationHop struct {
	FromSchemaID string                `json:"from_schema_id"`
	ToSchemaID   string                `json:"to_schema_id"`
	Hints        *SchemaMigrationHints `json:"hints,omitempty"`
}

// ---------------------------------------------------------------------------
// SchemaMigrationJob — Bun entity for kb.schema_migration_jobs
// ---------------------------------------------------------------------------

// SchemaMigrationJob tracks an async migration job.
type SchemaMigrationJob struct {
	bun.BaseModel `bun:"table:kb.schema_migration_jobs,alias:smj"`

	ID              string         `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID       string         `bun:"project_id,notnull,type:uuid" json:"project_id"`
	FromSchemaID    string         `bun:"from_schema_id,notnull,type:uuid" json:"from_schema_id"`
	ToSchemaID      string         `bun:"to_schema_id,notnull,type:uuid" json:"to_schema_id"`
	Chain           []MigrationHop `bun:"chain,type:jsonb" json:"chain"`
	Status          string         `bun:"status,notnull,default:'pending'" json:"status"`
	RiskLevel       string         `bun:"risk_level" json:"risk_level,omitempty"`
	ObjectsMigrated int            `bun:"objects_migrated,notnull,default:0" json:"objects_migrated"`
	ObjectsFailed   int            `bun:"objects_failed,notnull,default:0" json:"objects_failed"`
	Error           *string        `bun:"error" json:"error,omitempty"`
	AutoUninstall   bool           `bun:"auto_uninstall,notnull,default:false" json:"auto_uninstall,omitempty"`
	CreatedAt       time.Time      `bun:"created_at,notnull" json:"created_at"`
	StartedAt       *time.Time     `bun:"started_at" json:"started_at,omitempty"`
	CompletedAt     *time.Time     `bun:"completed_at" json:"completed_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Migration API request/response structs
// ---------------------------------------------------------------------------

// SchemaMigrationPreviewRequest is the request for a migration risk assessment.
type SchemaMigrationPreviewRequest struct {
	FromSchemaID string                `json:"from_schema_id"`
	ToSchemaID   string                `json:"to_schema_id"`
	Hints        *SchemaMigrationHints `json:"hints,omitempty"`
}

// SchemaMigrationPreviewResponse is the response from a migration preview.
type SchemaMigrationPreviewResponse struct {
	ProjectID      string                `json:"project_id"`
	FromSchemaID   string                `json:"from_schema_id"`
	ToSchemaID     string                `json:"to_schema_id"`
	OverallRisk    string                `json:"overall_risk_level"`
	CanProceed     bool                  `json:"can_proceed"`
	BlockReason    string                `json:"block_reason,omitempty"`
	TotalObjects   int                   `json:"total_objects"`
	Plan           *SchemaMigrationPlan  `json:"plan,omitempty"`
	PerTypeResults []MigrationTypeResult `json:"per_type_results,omitempty"`
}

// SchemaMigrationPlan is a structured dry-run report of what a migration will do.
// It lets a client render an exact preview: which types are renamed/added and
// which properties are renamed/dropped/added per type.
type SchemaMigrationPlan struct {
	// TypeRenames lists object/relationship type renames to apply (from hints).
	TypeRenames []TypeRename `json:"type_renames,omitempty"`
	// PropertyRenames lists property renames within types (from hints).
	PropertyRenames []PropertyRename `json:"property_renames,omitempty"`
	// RemovedProperties lists properties that are intentionally dropped (from hints).
	RemovedProperties []RemovedProperty `json:"removed_properties,omitempty"`
	// AddedTypes lists type names present in the target schema but not the source.
	AddedTypes []string `json:"added_types,omitempty"`
	// AddedProperties lists properties added to existing types (target prop not in source).
	AddedProperties []AddedProperty `json:"added_properties,omitempty"`
}

// AddedProperty identifies a property added to a type in a migration.
type AddedProperty struct {
	TypeName string `json:"type_name"`
	Name     string `json:"name"`
}

// MigrationTypeResult summarises migration risk for a single type.
type MigrationTypeResult struct {
	TypeName    string `json:"type_name"`
	ObjectCount int    `json:"object_count"`
	RiskLevel   string `json:"risk_level"`
	CanProceed  bool   `json:"can_proceed"`
	BlockReason string `json:"block_reason,omitempty"`
	// MigratedProps lists props carried over unchanged (union across objects, deduped).
	MigratedProps []string `json:"migrated_props,omitempty"`
	// DroppedProps lists props present in the old schema but absent from the new one.
	DroppedProps []string `json:"dropped_props,omitempty"`
	// AddedProps lists props present in the new schema but absent from existing objects.
	AddedProps []string `json:"added_props,omitempty"`
	// CoercedProps lists props whose values need type coercion.
	CoercedProps []string `json:"coerced_props,omitempty"`
}

// SchemaMigrationExecuteRequest is the request to execute a migration.
type SchemaMigrationExecuteRequest struct {
	FromSchemaID string                `json:"from_schema_id"`
	ToSchemaID   string                `json:"to_schema_id"`
	Hints        *SchemaMigrationHints `json:"hints,omitempty"`
	Force        bool                  `json:"force,omitempty"`
	MaxObjects   int                   `json:"max_objects,omitempty"`
}

// SchemaMigrationExecuteResponse is the response from executing a migration.
type SchemaMigrationExecuteResponse struct {
	ProjectID       string `json:"project_id"`
	FromSchemaID    string `json:"from_schema_id"`
	ToSchemaID      string `json:"to_schema_id"`
	ObjectsMigrated int    `json:"objects_migrated"`
	ObjectsFailed   int    `json:"objects_failed"`
	RiskLevel       string `json:"risk_level"`
}

// SchemaMigrationRollbackRequest is the request to roll back a migration.
type SchemaMigrationRollbackRequest struct {
	ToVersion           string `json:"to_version"`
	RestoreTypeRegistry bool   `json:"restore_type_registry,omitempty"`
	// MaxObjects caps the number of objects restored by a single rollback
	// (0 = unlimited). It bounds processing, not the scan; the scan itself is
	// bounded separately by OnlyWithMigrationArchive and the configurable
	// MigrationScanMaxObjects safety cap.
	MaxObjects int `json:"max_objects,omitempty"`
}

// SchemaMigrationRollbackResponse is the response from a rollback.
type SchemaMigrationRollbackResponse struct {
	ProjectID       string `json:"project_id"`
	ToVersion       string `json:"to_version"`
	ObjectsRestored int    `json:"objects_restored"`
	ObjectsFailed   int    `json:"objects_failed"`
}

// CommitMigrationArchiveRequest is the request to prune old migration archives.
type CommitMigrationArchiveRequest struct {
	ThroughVersion string `json:"through_version"`
}

// CommitMigrationArchiveResponse is the response from committing the archive.
type CommitMigrationArchiveResponse struct {
	ProjectID      string `json:"project_id"`
	ThroughVersion string `json:"through_version"`
	ObjectsUpdated int    `json:"objects_updated"`
	EntriesPruned  int    `json:"entries_pruned"`
}

// MigrationJobStatusResponse is the response from polling a migration job.
type MigrationJobStatusResponse struct {
	Job *SchemaMigrationJob `json:"job"`
}

// GraphMemorySchema represents a memory schema in kb.graph_schemas
type GraphMemorySchema struct {
	bun.BaseModel `bun:"table:kb.graph_schemas,alias:gtp"`

	ID                      string                `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	Name                    string                `bun:"name,notnull" json:"name"`
	Version                 string                `bun:"version,notnull" json:"version"`
	Description             *string               `bun:"description" json:"description,omitempty"`
	Author                  *string               `bun:"author" json:"author,omitempty"`
	Source                  *string               `bun:"source" json:"source,omitempty"`
	License                 *string               `bun:"license" json:"license,omitempty"`
	RepositoryURL           *string               `bun:"repository_url" json:"repositoryUrl,omitempty"`
	DocumentationURL        *string               `bun:"documentation_url" json:"documentationUrl,omitempty"`
	ObjectTypeSchemas       json.RawMessage       `bun:"object_type_schemas,type:jsonb" json:"objectTypeSchemas,omitempty"`
	RelationshipTypeSchemas json.RawMessage       `bun:"relationship_type_schemas,type:jsonb" json:"relationshipTypeSchemas,omitempty"`
	UIConfigs               json.RawMessage       `bun:"ui_configs,type:jsonb" json:"uiConfigs,omitempty"`
	ExtractionPrompts       json.RawMessage       `bun:"extraction_prompts,type:jsonb" json:"extractionPrompts,omitempty"`
	Migrations              *SchemaMigrationHints `bun:"migrations,type:jsonb" json:"migrations,omitempty"`
	Checksum                *string               `bun:"checksum" json:"checksum,omitempty"`
	ProjectID               *string               `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	// PendingReview marks discovery output that is awaiting an explicit
	// install. It is false for a pack created or imported any other way, so a
	// client can tell "proposed by a run" from "unassigned".
	PendingReview bool       `bun:"pending_review,default:false" json:"pendingReview"`
	Draft         bool       `bun:"draft,default:false" json:"draft"`
	PublishedAt   *time.Time `bun:"published_at" json:"publishedAt,omitempty"`
	DeprecatedAt  *time.Time `bun:"deprecated_at" json:"deprecatedAt,omitempty"`
	CreatedAt     time.Time  `bun:"created_at" json:"createdAt"`
	UpdatedAt     time.Time  `bun:"updated_at" json:"updatedAt"`
}

// ProjectMemorySchema represents a project's installed memory schema
type ProjectMemorySchema struct {
	bun.BaseModel `bun:"table:kb.project_schemas,alias:ptp"`

	ID          string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID   string     `bun:"project_id,notnull,type:uuid" json:"projectId"`
	SchemaID    string     `bun:"schema_id,notnull,type:uuid" json:"schemaId"`
	Active      bool       `bun:"active,notnull,default:true" json:"active"`
	InstalledAt time.Time  `bun:"installed_at" json:"installedAt"`
	RemovedAt   *time.Time `bun:"removed_at" json:"removedAt,omitempty"`
	// SourceBlueprintID records the blueprint that created this assignment
	// (NULL = assigned outside any blueprint). Unapply uses it to avoid removing
	// manually-installed or shared pack assignments.
	SourceBlueprintID *string   `bun:"source_blueprint_id,type:uuid" json:"sourceBlueprintId,omitempty"`
	CreatedAt         time.Time `bun:"created_at" json:"createdAt"`
	UpdatedAt         time.Time `bun:"updated_at" json:"updatedAt"`

	// Joined memory schema
	MemorySchema *GraphMemorySchema `bun:"rel:belongs-to,join:schema_id=id" json:"memorySchema,omitempty"`
}

// CompiledTypesResponse contains the compiled object and relationship types for a project
type CompiledTypesResponse struct {
	ObjectTypes       []ObjectTypeSchema       `json:"objectTypes"`
	RelationshipTypes []RelationshipTypeSchema `json:"relationshipTypes"`
}

// ObjectTypeSchema represents an object type definition
type ObjectTypeSchema struct {
	Name          string          `json:"name"`
	Label         string          `json:"label,omitempty"`
	Description   string          `json:"description,omitempty"`
	Properties    json.RawMessage `json:"properties,omitempty"`
	UI            json.RawMessage `json:"ui,omitempty"` // type-level ui block (e.g. {"icon":...,"color":...})
	SchemaID      string          `json:"schemaId,omitempty"`
	SchemaName    string          `json:"schemaName,omitempty"`
	SchemaVersion string          `json:"schemaVersion,omitempty"`
	Shadowed      bool            `json:"shadowed,omitempty"`
	// Object-driven work configuration (P4): board-enabled flag, allowed work
	// status values, and operational pipeline skip-flags.
	BoardEnabled      bool     `json:"boardEnabled,omitempty"`
	AllowedStatuses   []string `json:"allowedStatuses,omitempty"`
	SkipEmbeddings    bool     `json:"skipEmbeddings,omitempty"`
	SkipExtraction    bool     `json:"skipExtraction,omitempty"`
	ExcludeFromSearch bool     `json:"excludeFromSearch,omitempty"`
}

// RelationshipTypeSchema represents a relationship type definition
type RelationshipTypeSchema struct {
	Name          string `json:"name"`
	Label         string `json:"label,omitempty"`
	Description   string `json:"description,omitempty"`
	SourceType    string `json:"sourceType,omitempty"`
	TargetType    string `json:"targetType,omitempty"`
	SchemaID      string `json:"schemaId,omitempty"`
	SchemaName    string `json:"schemaName,omitempty"`
	SchemaVersion string `json:"schemaVersion,omitempty"`
	Shadowed      bool   `json:"shadowed,omitempty"`
}

// MemorySchemaListItem is a simplified schema for listing
type MemorySchemaListItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description *string `json:"description,omitempty"`
	Author      *string `json:"author,omitempty"`
}

// SchemaListInfo is one row of the schema catalog list (REST mirror of the MCP
// schema-list tool). Its field names, types and json tags are verbatim copies of
// the MCP tool's SchemaInfo (domain/mcp/schema_tools.go) so consumers can swap
// transports 1:1: visibility is always emitted ("" for global rows) while
// project_id and org_id are omitted for rows that have no project/org scope.
// Column values are scanned as strings to match the MCP tool's wire shape.
type SchemaListInfo struct {
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

// SchemaListResponse is the GET /api/schemas/projects/:projectId payload.
type SchemaListResponse struct {
	ProjectID string           `json:"project_id"`
	Schemas   []SchemaListInfo `json:"schemas"`
	Total     int              `json:"total"`
	Limit     int              `json:"limit"`
	Offset    int              `json:"offset"`
}

// InstalledSchemaItem represents an installed schema for a project
type InstalledSchemaItem struct {
	ID                string                 `json:"id"` // assignment ID
	SchemaID          string                 `json:"schemaId"`
	Name              string                 `json:"name"`
	Version           string                 `json:"version"`
	Description       *string                `json:"description,omitempty"`
	Active            bool                   `json:"active"`
	InstalledAt       time.Time              `json:"installedAt"`
	Customizations    map[string]interface{} `json:"customizations,omitempty"`
	ExtractionPrompts json.RawMessage        `json:"extractionPrompts,omitempty"`
}

// AssignPackRequest is the request to assign a memory schema to a project
type AssignPackRequest struct {
	SchemaID       string                 `json:"schema_id"`
	Customizations map[string]interface{} `json:"customizations"`
	// DryRun, when true, computes and returns the full conflict/merge preview
	// without making any database changes. The response uses HTTP 200.
	DryRun bool `json:"dry_run,omitempty"`
	// Merge, when true, additively merges incoming type schemas into existing
	// registered types rather than skipping conflicting type names.
	Merge bool `json:"merge,omitempty"`
	// Force, when true, allows migration to proceed even when the risk level is
	// dangerous. Has no effect if the schema has no migration hints.
	Force bool `json:"force,omitempty"`
	// AutoUninstall, when true, uninstalls the from_version schema after a
	// successful auto-migration.
	AutoUninstall bool `json:"auto_uninstall,omitempty"`
	// SourceBlueprintID, when set, records the blueprint that owns this
	// assignment. Used by blueprint unapply to avoid removing shared/manual
	// assignments. Nil for manual assigns.
	SourceBlueprintID *string `json:"sourceBlueprintId,omitempty"`
}

// PropertyConflict describes a single property-level conflict during a merge.
type PropertyConflict struct {
	Property    string          `json:"property"`
	ExistingDef json.RawMessage `json:"existing_def"`
	IncomingDef json.RawMessage `json:"incoming_def"`
	// Resolution is always "existing_wins" — existing properties are never overwritten.
	Resolution string `json:"resolution"`
}

// SchemaConflict describes a type-level conflict when assigning a pack whose
// type names overlap with types already registered in the project.
type SchemaConflict struct {
	TypeName              string             `json:"type_name"`
	ExistingSchema        json.RawMessage    `json:"existing_schema"`
	IncomingSchema        json.RawMessage    `json:"incoming_schema"`
	MergedSchema          json.RawMessage    `json:"merged_schema,omitempty"`
	AddedProperties       []string           `json:"added_properties,omitempty"`
	ConflictingProperties []PropertyConflict `json:"conflicting_properties,omitempty"`
}

// MergePreviewPack is one incoming pack in a merge preview request.
// ObjectTypes accepts the same array/map storage formats as a stored pack's
// object_type_schemas column: the array form is the canonical writer shape
// (blueprint apply/seeder and the HTTP request path), while the map form is the
// legacy stored shape and remains read-compatible.
//
// The object-type field accepts the same snake_case and camelCase spellings as
// the sibling pack request structs. Use GetObjectTypes() to retrieve the
// resolved value.
type MergePreviewPack struct {
	Name string `json:"name"`
	// ObjectTypes is a JSON array of {"name","label","properties",...} objects.
	// The map-of-type-name → schema stored form is also accepted here and is
	// read-compatible.
	ObjectTypes json.RawMessage `json:"objectTypes,omitempty" swaggertype:"array,object"`
	// ObjectTypeSchemasSnake mirrors the sibling pack request/storage spelling.
	ObjectTypeSchemasSnake json.RawMessage `json:"object_type_schemas,omitempty" swaggertype:"array,object"`
	// ObjectTypesSnake accepts the short snake_case spelling named in #163.
	ObjectTypesSnake json.RawMessage `json:"object_types,omitempty" swaggertype:"array,object"`
}

// UnmarshalJSON accepts the documented camelCase key and both snake_case
// aliases, while rejecting genuinely unknown pack keys. A typo such as
// "objectTypesTypo" must not bind to nothing and look like an absent
// objectTypes value: that is the fail-open the merge preview gate is meant to
// prevent. Other pack-level keys are not part of this endpoint's contract.
func (p *MergePreviewPack) UnmarshalJSON(data []byte) error {
	type packJSON MergePreviewPack
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var decoded packJSON
	if err := dec.Decode(&decoded); err != nil {
		return err
	}
	*p = MergePreviewPack(decoded)
	return nil
}

// GetObjectTypes returns the resolved object-type value, preferring snake_case
// over camelCase to match CreatePackRequest/UpdatePackRequest. The long
// storage-column spelling object_type_schemas wins over the short object_types
// alias when both are present.
func (p *MergePreviewPack) GetObjectTypes() json.RawMessage {
	if len(p.ObjectTypeSchemasSnake) > 0 {
		return p.ObjectTypeSchemasSnake
	}
	if len(p.ObjectTypesSnake) > 0 {
		return p.ObjectTypesSnake
	}
	return p.ObjectTypes
}

// MergePreviewConflict describes one type-name collision found by a merge
// preview. It is reported even when no properties are added or conflicting,
// because the type-name collision itself requires a merge decision.
type MergePreviewConflict struct {
	TypeName              string `json:"typeName"`
	IncomingPackName      string `json:"incomingPackName"`
	ExistingSchemaID      string `json:"existingSchemaId,omitempty"`
	ExistingSchemaName    string `json:"existingSchemaName,omitempty"`
	ExistingSchemaVersion string `json:"existingSchemaVersion,omitempty"`
	// ExistingSchema is the registered JSON Schema object for this type.
	ExistingSchema json.RawMessage `json:"existingSchema,omitempty" swaggertype:"object"`
	// IncomingSchema is the incoming JSON Schema object for this type.
	IncomingSchema json.RawMessage `json:"incomingSchema,omitempty" swaggertype:"object"`
	// MergedSchema is the preview of the merged JSON Schema object.
	MergedSchema          json.RawMessage    `json:"mergedSchema,omitempty" swaggertype:"object"`
	AddedProperties       []string           `json:"addedProperties,omitempty"`
	ConflictingProperties []PropertyConflict `json:"conflictingProperties,omitempty"`
}

// MergePreviewRequest is the body for POST
// /api/schemas/projects/:projectId/merge-preview.
type MergePreviewRequest struct {
	Packs []MergePreviewPack `json:"packs"`
}

// MergePreviewResult is the response from a merge preview. RequiresMerge is
// true whenever Conflicts is non-empty.
type MergePreviewResult struct {
	Conflicts     []MergePreviewConflict `json:"conflicts"`
	RequiresMerge bool                   `json:"requiresMerge"`
}

// AssignPackResult is the response from AssignPack / dry-run.
// Replaces the bare *ProjectMemorySchema return so callers get conflict details.
type AssignPackResult struct {
	// DryRun mirrors the request flag so callers can distinguish the response mode.
	DryRun bool `json:"dry_run"`
	// AssignmentID is empty for dry-run responses.
	AssignmentID string `json:"assignment_id,omitempty"`
	SchemaID     string `json:"schema_id"`
	SchemaName   string `json:"schema_name"`
	// InstalledTypes are type names newly written to the registry.
	InstalledTypes []string `json:"installed_types"`
	// SkippedTypes are type names that already existed and were not merged.
	SkippedTypes []string `json:"skipped_types,omitempty"`
	// MergedTypes are type names whose schemas were additively extended.
	MergedTypes []string `json:"merged_types,omitempty"`
	// Conflicts contains full diff detail for each conflicting type.
	Conflicts []SchemaConflict `json:"conflicts,omitempty"`
	// AlreadyInstalled is true when merge=true and the pack was already assigned.
	AlreadyInstalled bool `json:"already_installed,omitempty"`
	// MigrationJobID is set when an async migration job was enqueued.
	MigrationJobID *string `json:"migration_job_id,omitempty"`
	// MigrationStatus describes the auto-migration state:
	// "pending", "skipped", "blocked", "chain_unresolvable"
	MigrationStatus string `json:"migration_status,omitempty"`
	// MigrationBlockReason describes why migration was not enqueued (if blocked).
	MigrationBlockReason string `json:"migration_block_reason,omitempty"`
	// MigrationPreview is populated when dry_run=true and the schema has migration hints.
	MigrationPreview *SchemaMigrationPreviewResponse `json:"migration_preview,omitempty"`
}

// UpdateAssignmentRequest is the request to update a pack assignment
type UpdateAssignmentRequest struct {
	Active *bool `json:"active"`
}

// CreatePackRequest is the request to create a new schema.
// Both snake_case and camelCase field names are accepted so that files written
// with either convention (e.g. objectTypeSchemas or object_type_schemas) work
// without manual conversion.
type CreatePackRequest struct {
	Name             string  `json:"name"`
	Version          string  `json:"version"`
	Description      *string `json:"description,omitempty"`
	Author           *string `json:"author,omitempty"`
	License          *string `json:"license,omitempty"`
	RepositoryURL    *string `json:"repository_url,omitempty"`
	DocumentationURL *string `json:"documentation_url,omitempty"`

	// Migrations is the optional upgrade-path block for this schema version.
	Migrations *SchemaMigrationHints `json:"migrations,omitempty" yaml:"migrations,omitempty"`

	// ObjectTypeSchemas is the object-type definitions in the request shape: a
	// JSON array of {"name","label","properties",...} objects, e.g.
	// [{"name":"Note","properties":{...}}]. This is the shape the validator
	// accepts and the shape blueprint apply/seeder write. The map-of-type-name
	// → schema form stored in kb.graph_schemas.object_type_schemas (legacy
	// rows, the MCP create-schema tool, and the discovery path) is
	// read-compatible on later reads, but validateSchemaDefinitions does NOT
	// accept it on this request.
	// Accepts both snake_case and camelCase keys.
	// Use GetObjectTypeSchemas() to retrieve the resolved value.
	ObjectTypeSchemasSnake json.RawMessage `json:"object_type_schemas" swaggertype:"array,object"`
	ObjectTypeSchemasCamel json.RawMessage `json:"objectTypeSchemas" swaggertype:"array,object"`

	// RelationshipTypeSchemas is the optional relationship-type definitions in
	// the request shape: a JSON array of {"name","sourceType","targetType"}
	// objects, e.g. [{"name":"annotates","sourceType":"Note","targetType":"Note"}].
	// The map-of-rel-type → schema form stored in
	// kb.graph_schemas.relationship_type_schemas is the legacy storage shape and
	// is NOT accepted here.
	// Accepts both snake_case and camelCase keys.
	// Use GetRelationshipTypeSchemas() to retrieve the resolved value.
	RelationshipTypeSchemasSnake json.RawMessage `json:"relationship_type_schemas,omitempty" swaggertype:"array,object"`
	RelationshipTypeSchemasCamel json.RawMessage `json:"relationshipTypeSchemas,omitempty" swaggertype:"array,object"`

	// UIConfigs is a JSON object keyed by object type name, each value a UI
	// config such as {"icon":"file-text","color":"#3B82F6"}. This map form is
	// what the MCP create-schema tool writes (it rejects a non-object with
	// "must be an object, got %T"), the published API docs and SKILL.md show,
	// and the compiled-types reader unmarshals as map[string]json.RawMessage;
	// an array is not a supported shape. Blueprint manifests express UI per
	// type, inline, not through this pack-level map.
	// Accepts both snake_case and camelCase keys.
	// Use GetUIConfigs() to retrieve the resolved value.
	UIConfigsSnake json.RawMessage `json:"ui_configs,omitempty" swaggertype:"object"`
	UIConfigsCamel json.RawMessage `json:"uiConfigs,omitempty" swaggertype:"object"`

	// ExtractionPrompts is a JSON object keyed by object type name, each value
	// a prompt config such as {"system_prompt":"...","field_hints":{...}}.
	// This map form is what the MCP create-schema tool writes (it rejects a
	// non-object with "must be an object, got %T"), the published API docs and
	// SKILL.md show, and the assign/restore readers unmarshal as
	// map[string]json.RawMessage; no array form is read. Blueprint manifests do
	// not carry a pack-level extraction_prompts field.
	// Accepts both snake_case and camelCase keys.
	// Use GetExtractionPrompts() to retrieve the resolved value.
	ExtractionPromptsSnake json.RawMessage `json:"extraction_prompts,omitempty" swaggertype:"object"`
	ExtractionPromptsCamel json.RawMessage `json:"extractionPrompts,omitempty" swaggertype:"object"`
}

// ObjectTypeSchemas returns the ObjectTypeSchemas value, preferring snake_case over camelCase.
func (r *CreatePackRequest) GetObjectTypeSchemas() json.RawMessage {
	if len(r.ObjectTypeSchemasSnake) > 0 {
		return r.ObjectTypeSchemasSnake
	}
	return r.ObjectTypeSchemasCamel
}

// GetRelationshipTypeSchemas returns the RelationshipTypeSchemas value, preferring snake_case over camelCase.
func (r *CreatePackRequest) GetRelationshipTypeSchemas() json.RawMessage {
	if len(r.RelationshipTypeSchemasSnake) > 0 {
		return r.RelationshipTypeSchemasSnake
	}
	return r.RelationshipTypeSchemasCamel
}

// GetUIConfigs returns the UIConfigs value, preferring snake_case over camelCase.
func (r *CreatePackRequest) GetUIConfigs() json.RawMessage {
	if len(r.UIConfigsSnake) > 0 {
		return r.UIConfigsSnake
	}
	return r.UIConfigsCamel
}

// GetExtractionPrompts returns the ExtractionPrompts value, preferring snake_case over camelCase.
func (r *CreatePackRequest) GetExtractionPrompts() json.RawMessage {
	if len(r.ExtractionPromptsSnake) > 0 {
		return r.ExtractionPromptsSnake
	}
	return r.ExtractionPromptsCamel
}

// UpdatePackRequest is the request to update an existing schema (partial update — only non-nil fields are applied)
type UpdatePackRequest struct {
	Name             *string `json:"name,omitempty"`
	Version          *string `json:"version,omitempty"`
	Description      *string `json:"description,omitempty"`
	Author           *string `json:"author,omitempty"`
	License          *string `json:"license,omitempty"`
	RepositoryURL    *string `json:"repository_url,omitempty"`
	DocumentationURL *string `json:"documentation_url,omitempty"`
	// ObjectTypeSchemas is the request shape only: a JSON array of
	// {"name","label","properties",...} objects, which is also what blueprint
	// apply/seeder write. The map-of-type-name → schema stored form (legacy
	// rows, MCP create-schema, discovery) is read-compatible elsewhere but is
	// not accepted on update.
	ObjectTypeSchemas json.RawMessage `json:"object_type_schemas,omitempty" swaggertype:"array,object"`
	// RelationshipTypeSchemas is the request shape only: a JSON array of
	// {"name","sourceType","targetType"} objects. The stored map form is not
	// accepted on update.
	RelationshipTypeSchemas json.RawMessage `json:"relationship_type_schemas,omitempty" swaggertype:"array,object"`
	// UIConfigs is a JSON object keyed by object type name (type name → UI
	// config); an array is not a supported shape.
	UIConfigs json.RawMessage `json:"ui_configs,omitempty" swaggertype:"object"`
	// ExtractionPrompts is a JSON object keyed by object type name (type name →
	// prompt config); an array is not a supported shape.
	ExtractionPrompts json.RawMessage `json:"extraction_prompts,omitempty" swaggertype:"object"`
	// Migrations is the optional upgrade-path block for this schema version.
	Migrations *SchemaMigrationHints `json:"migrations,omitempty" yaml:"migrations,omitempty"`
}

// GetPackRequest is the request to get a schema by ID
type GetPackRequest struct {
	ID string `json:"id"`
}

// DeletePackRequest is the request to delete a schema
type DeletePackRequest struct {
	ID string `json:"id"`
}

// ValidateObjectsResponse is the response for GET /api/schemas/projects/:projectId/validate
type ValidateObjectsResponse struct {
	ProjectID    string                   `json:"project_id"`
	TotalObjects int                      `json:"total_objects"`
	StaleObjects int                      `json:"stale_objects"`
	Results      []ObjectValidationResult `json:"results"`
}

// ObjectValidationResult describes one object's validation state.
type ObjectValidationResult struct {
	EntityID      string   `json:"entity_id"`
	Type          string   `json:"type"`
	Key           *string  `json:"key,omitempty"`
	SchemaVersion *string  `json:"schema_version,omitempty"`
	Issues        []string `json:"issues"`
}

// SchemaHistoryItem represents a historical schema assignment entry (including removed ones)
type SchemaHistoryItem struct {
	ID          string     `json:"id"` // assignment ID
	SchemaID    string     `json:"schemaId"`
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Active      bool       `json:"active"`
	InstalledAt time.Time  `json:"installedAt"`
	RemovedAt   *time.Time `json:"removedAt,omitempty"`
}

// PackBlueprintClaim is a pack-centric view of blueprint pack provenance:
// one row per compiled schema pack, listing the applied blueprints that claim it.
// Returned by GET /api/schemas/projects/:projectId/pack-claims.
type PackBlueprintClaim struct {
	SchemaID   string              `json:"schemaId"`
	Name       string              `json:"name"`    // pack name (kb.graph_schemas.name)
	Version    string              `json:"version"` // pack version (kb.graph_schemas.version)
	Blueprints []ClaimingBlueprint `json:"blueprints"`
}

// ClaimingBlueprint identifies an applied blueprint that claims a schema pack.
type ClaimingBlueprint struct {
	BlueprintID string `json:"blueprintId"`
	Name        string `json:"name"`    // kb.blueprints.name
	Version     string `json:"version"` // applied version (kb.blueprint_applications.version)
}

// TypeRename specifies an object/edge type rename for migration.
type TypeRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// PropertyRename specifies a property rename within a type for migration.
type PropertyRename struct {
	TypeName string `json:"type_name"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// MigrateRequest is the request body for POST /api/schemas/projects/:projectId/migrate.
type MigrateRequest struct {
	TypeRenames     []TypeRename     `json:"type_renames,omitempty"`
	PropertyRenames []PropertyRename `json:"property_renames,omitempty"`
	DryRun          bool             `json:"dry_run,omitempty"`
}

// TypeRenameResult holds the outcome of a single type rename operation.
type TypeRenameResult struct {
	From            string `json:"from"`
	To              string `json:"to"`
	ObjectsAffected int    `json:"objects_affected"`
	EdgesAffected   int    `json:"edges_affected"`
}

// PropertyRenameResult holds the outcome of a single property rename operation.
type PropertyRenameResult struct {
	TypeName        string `json:"type_name"`
	From            string `json:"from"`
	To              string `json:"to"`
	ObjectsAffected int    `json:"objects_affected"`
}

// MigrateResponse is the response from the migrate endpoint.
type MigrateResponse struct {
	DryRun                bool                   `json:"dry_run"`
	TypeRenameResults     []TypeRenameResult     `json:"type_rename_results,omitempty"`
	PropertyRenameResults []PropertyRenameResult `json:"property_rename_results,omitempty"`
}
