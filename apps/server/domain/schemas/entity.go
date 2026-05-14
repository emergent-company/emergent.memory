package schemas

import (
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
	AutoUninstall   bool           `bun:"-" json:"auto_uninstall,omitempty"` // runtime-only flag
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
	PerTypeResults []MigrationTypeResult `json:"per_type_results,omitempty"`
	SuggestedHints string                `json:"suggested_hints_yaml,omitempty"`
}

// MigrationTypeResult summarises migration risk for a single type.
type MigrationTypeResult struct {
	TypeName    string `json:"type_name"`
	ObjectCount int    `json:"object_count"`
	RiskLevel   string `json:"risk_level"`
	CanProceed  bool   `json:"can_proceed"`
	BlockReason string `json:"block_reason,omitempty"`
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
	OrgID                   *string               `bun:"org_id,type:uuid" json:"orgId,omitempty"`
	Visibility              string                `bun:"visibility,notnull,default:'project'" json:"visibility"`
	Draft                   bool                  `bun:"draft,notnull,default:false" json:"draft"`
	PublishedAt             *time.Time            `bun:"published_at" json:"publishedAt,omitempty"`
	DeprecatedAt            *time.Time            `bun:"deprecated_at" json:"deprecatedAt,omitempty"`
	CreatedAt               time.Time             `bun:"created_at" json:"createdAt"`
	UpdatedAt               time.Time             `bun:"updated_at" json:"updatedAt"`
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
	CreatedAt   time.Time  `bun:"created_at" json:"createdAt"`
	UpdatedAt   time.Time  `bun:"updated_at" json:"updatedAt"`

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
	SchemaID      string          `json:"schemaId,omitempty"`
	SchemaName    string          `json:"schemaName,omitempty"`
	SchemaVersion string          `json:"schemaVersion,omitempty"`
	Shadowed      bool            `json:"shadowed,omitempty"`
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
	Visibility  string  `json:"visibility,omitempty"`
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
	// Visibility controls schema scope: "project" (default) or "organization".
	Visibility string `json:"visibility,omitempty"`

	// Migrations is the optional upgrade-path block for this schema version.
	Migrations *SchemaMigrationHints `json:"migrations,omitempty" yaml:"migrations,omitempty"`

	// ObjectTypeSchemas accepts both snake_case and camelCase keys.
	// Use GetObjectTypeSchemas() to retrieve the resolved value.
	ObjectTypeSchemasSnake json.RawMessage `json:"object_type_schemas"`
	ObjectTypeSchemasCamel json.RawMessage `json:"objectTypeSchemas"`

	// RelationshipTypeSchemas accepts both snake_case and camelCase keys.
	// Use GetRelationshipTypeSchemas() to retrieve the resolved value.
	RelationshipTypeSchemasSnake json.RawMessage `json:"relationship_type_schemas,omitempty"`
	RelationshipTypeSchemasCamel json.RawMessage `json:"relationshipTypeSchemas,omitempty"`

	// UIConfigs accepts both snake_case and camelCase keys.
	UIConfigsSnake json.RawMessage `json:"ui_configs,omitempty"`
	UIConfigsCamel json.RawMessage `json:"uiConfigs,omitempty"`

	// ExtractionPrompts accepts both snake_case and camelCase keys.
	ExtractionPromptsSnake json.RawMessage `json:"extraction_prompts,omitempty"`
	ExtractionPromptsCamel json.RawMessage `json:"extractionPrompts,omitempty"`
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
	Name                    *string         `json:"name,omitempty"`
	Version                 *string         `json:"version,omitempty"`
	Description             *string         `json:"description,omitempty"`
	Author                  *string         `json:"author,omitempty"`
	License                 *string         `json:"license,omitempty"`
	RepositoryURL           *string         `json:"repository_url,omitempty"`
	DocumentationURL        *string         `json:"documentation_url,omitempty"`
	ObjectTypeSchemas       json.RawMessage `json:"object_type_schemas,omitempty"`
	RelationshipTypeSchemas json.RawMessage `json:"relationship_type_schemas,omitempty"`
	UIConfigs               json.RawMessage `json:"ui_configs,omitempty"`
	ExtractionPrompts       json.RawMessage `json:"extraction_prompts,omitempty"`
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
