package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// --- blueprints / schemas ---

// AvailableSchemaItem is one installable pack, either from the memory schema
// registry (Source "registry") or the bundled blueprints/ directory (Source
// "bundled").
type AvailableSchemaItem struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Source      string   `json:"source"`
	Upgrade     bool     `json:"upgrade,omitempty"` // true when this is a newer version of an installed pack
	Agents      []string `json:"agents,omitempty"`  // agent names the blueprint defines (agent-only blueprints have no schema)
	HasSchema   bool     `json:"-"`                 // internal: true when the blueprint carries a schema pack
}

// CompiledType is one object or relationship type in the merged compiled view
// (GET /api/schemas/projects/:pid/compiled-types).
type CompiledType struct {
	Name          string          `json:"name"`
	Label         string          `json:"label"`
	Description   string          `json:"description"`
	Properties    json.RawMessage `json:"properties,omitempty"`
	UI            json.RawMessage `json:"ui,omitempty"` // optional type-level ui block ({"icon":...,"color":...})
	SourceType    string          `json:"sourceType,omitempty"`
	TargetType    string          `json:"targetType,omitempty"`
	SchemaID      string          `json:"schemaId,omitempty"`
	SchemaName    string          `json:"schemaName,omitempty"`
	SchemaVersion string          `json:"schemaVersion,omitempty"`
	Shadowed      bool            `json:"shadowed,omitempty"`
}

// CompiledSchemaTypes is the merged object + relationship types across active
// installs.
type CompiledSchemaTypes struct {
	ObjectTypes       []CompiledType `json:"objectTypes"`
	RelationshipTypes []CompiledType `json:"relationshipTypes"`
}

func (m *MemoryClient) GetCompiledTypes(ctx context.Context) (*CompiledSchemaTypes, error) {
	var wire struct {
		ObjectTypes []struct {
			Name          string          `json:"name"`
			Label         string          `json:"label"`
			Description   string          `json:"description"`
			Properties    json.RawMessage `json:"properties"`
			UI            json.RawMessage `json:"ui"`
			SchemaID      string          `json:"schemaId"`
			SchemaName    string          `json:"schemaName"`
			SchemaVersion string          `json:"schemaVersion"`
			Shadowed      bool            `json:"shadowed"`
		} `json:"objectTypes"`
		RelationshipTypes []struct {
			Name          string `json:"name"`
			Label         string `json:"label"`
			Description   string `json:"description"`
			SourceType    string `json:"sourceType"`
			TargetType    string `json:"targetType"`
			SchemaID      string `json:"schemaId"`
			SchemaName    string `json:"schemaName"`
			SchemaVersion string `json:"schemaVersion"`
			Shadowed      bool   `json:"shadowed"`
		} `json:"relationshipTypes"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/compiled-types", nil, &wire); err != nil {
		return nil, err
	}
	out := &CompiledSchemaTypes{
		ObjectTypes:       make([]CompiledType, 0, len(wire.ObjectTypes)),
		RelationshipTypes: make([]CompiledType, 0, len(wire.RelationshipTypes)),
	}
	for _, w := range wire.ObjectTypes {
		out.ObjectTypes = append(out.ObjectTypes, CompiledType{
			Name:          w.Name,
			Label:         w.Label,
			Description:   w.Description,
			Properties:    w.Properties,
			UI:            w.UI,
			SchemaID:      w.SchemaID,
			SchemaName:    w.SchemaName,
			SchemaVersion: w.SchemaVersion,
			Shadowed:      w.Shadowed,
		})
	}
	for _, w := range wire.RelationshipTypes {
		out.RelationshipTypes = append(out.RelationshipTypes, CompiledType{
			Name:          w.Name,
			Label:         w.Label,
			Description:   w.Description,
			SourceType:    w.SourceType,
			TargetType:    w.TargetType,
			SchemaID:      w.SchemaID,
			SchemaName:    w.SchemaName,
			SchemaVersion: w.SchemaVersion,
			Shadowed:      w.Shadowed,
		})
	}
	return out, nil
}

// SchemaInfo is one registered schema in memory's global registry (from the
// MCP schema-list tool). project_id is empty for global/built-in schemas.
type SchemaInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	ProjectID   string `json:"project_id"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// BlueprintSchema is the full schema definition returned by GET /api/schemas/:packId.
// ObjectTypeSchemas/RelationshipTypeSchemas are kept raw (array-or-map form).
type BlueprintSchema struct {
	ID                      string                `json:"id"`
	Name                    string                `json:"name"`
	Version                 string                `json:"version"`
	Description             string                `json:"description"`
	Author                  string                `json:"author"`
	Source                  string                `json:"source"`
	License                 string                `json:"license"`
	RepositoryURL           string                `json:"repositoryUrl"`
	DocumentationURL        string                `json:"documentationUrl"`
	ObjectTypeSchemas       json.RawMessage       `json:"objectTypeSchemas"`
	RelationshipTypeSchemas json.RawMessage       `json:"relationshipTypeSchemas"`
	Migrations              *SchemaMigrationHints `json:"migrations,omitempty"`
}

// ListAllSchemas lists the schema catalog visible to the current project
// (project-owned + global packs) via the REST schema-catalog endpoint — the
// REST mirror of the MCP schema-list tool (memory PR #389), which retires the
// MCP handshake + tools/call round-trip. Source for the built-in / registry
// blueprint catalog.
func (m *MemoryClient) ListAllSchemas(ctx context.Context) ([]SchemaInfo, error) {
	project := m.projectIDFor(ctx)
	if project == "" {
		return nil, nil
	}
	var out struct {
		Schemas []SchemaInfo `json:"schemas"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/schemas/projects/"+project+"?limit=100", nil, &out); err != nil {
		return nil, err
	}
	return out.Schemas, nil
}

// GetSchema fetches one full schema definition by pack id.
func (m *MemoryClient) GetSchema(ctx context.Context, packID string) (*BlueprintSchema, error) {
	var out BlueprintSchema
	if err := m.do(ctx, http.MethodGet, "/api/schemas/"+packID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- schema migrations ---

// SchemaHistoryItem is one install/uninstall record (GET .../history).
type SchemaHistoryItem struct {
	ID          string `json:"id"` // assignment id
	SchemaID    string `json:"schemaId"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Active      bool   `json:"active"`
	InstalledAt string `json:"installedAt"`
	RemovedAt   string `json:"removedAt"`
}

// MigrationPlan is the structured migration steps returned in the preview
// response. It drives the dropdown options and the plan summary in the UI.
type MigrationPlan struct {
	TypeRenames       []TypeRename      `json:"type_renames,omitempty"`
	PropertyRenames   []PropertyRename  `json:"property_renames,omitempty"`
	RemovedProperties []RemovedProperty `json:"removed_properties,omitempty"`
	AddedTypes        []string          `json:"added_types,omitempty"`
	AddedProperties   []AddedProperty   `json:"added_properties,omitempty"`
}

// AddedProperty is one property that the migration adds to a type.
type AddedProperty struct {
	TypeName string `json:"type_name"`
	Name     string `json:"name"`
}

// MigrationPreviewResult is the POST .../migrate/preview response.
type MigrationPreviewResult struct {
	ProjectID        string         `json:"project_id"`
	FromSchemaID     string         `json:"from_schema_id"`
	ToSchemaID       string         `json:"to_schema_id"`
	OverallRiskLevel string         `json:"overall_risk_level"`
	CanProceed       bool           `json:"can_proceed"`
	BlockReason      string         `json:"block_reason"`
	TotalObjects     int            `json:"total_objects"`
	Plan             *MigrationPlan `json:"plan,omitempty"`
	PerTypeResults   []struct {
		TypeName      string   `json:"type_name"`
		ObjectCount   int      `json:"object_count"`
		RiskLevel     string   `json:"risk_level"`
		CanProceed    bool     `json:"can_proceed"`
		BlockReason   string   `json:"block_reason"`
		MigratedProps []string `json:"migrated_props,omitempty"`
		DroppedProps  []string `json:"dropped_props,omitempty"`
		AddedProps    []string `json:"added_props,omitempty"`
		CoercedProps  []string `json:"coerced_props,omitempty"`
	} `json:"per_type_results"`
}

// MigrationExecuteResult is the POST .../migrate/execute response.
type MigrationExecuteResult struct {
	ProjectID       string `json:"project_id"`
	FromSchemaID    string `json:"from_schema_id"`
	ToSchemaID      string `json:"to_schema_id"`
	ObjectsMigrated int    `json:"objects_migrated"`
	ObjectsFailed   int    `json:"objects_failed"`
	RiskLevel       string `json:"risk_level"`
}

// MigrationRollbackResult is the POST .../migrate/rollback response.
type MigrationRollbackResult struct {
	ProjectID       string `json:"project_id"`
	ToVersion       string `json:"to_version"`
	ObjectsRestored int    `json:"objects_restored"`
	ObjectsFailed   int    `json:"objects_failed"`
}

// MigrationCommitResult is the POST .../migrate/commit response.
type MigrationCommitResult struct {
	ProjectID      string `json:"project_id"`
	ThroughVersion string `json:"through_version"`
	ObjectsUpdated int    `json:"objects_updated"`
	EntriesPruned  int    `json:"entries_pruned"`
}

// MigrationJob is the GET .../migration-jobs/:jobId response.
type MigrationJob struct {
	ID              string `json:"id"`
	ProjectID       string `json:"project_id"`
	FromSchemaID    string `json:"from_schema_id"`
	ToSchemaID      string `json:"to_schema_id"`
	Status          string `json:"status"`
	RiskLevel       string `json:"risk_level"`
	ObjectsMigrated int    `json:"objects_migrated"`
	ObjectsFailed   int    `json:"objects_failed"`
	Error           string `json:"error"`
	CreatedAt       string `json:"created_at"`
	StartedAt       string `json:"started_at"`
	CompletedAt     string `json:"completed_at"`
}

// SchemaValidationResult is the GET .../validate response (schema drift scan).
type SchemaValidationResult struct {
	ProjectID    string                   `json:"project_id"`
	TotalObjects int                      `json:"total_objects"`
	StaleObjects int                      `json:"stale_objects"`
	Results      []ObjectValidationResult `json:"results"`
}

// ObjectValidationResult describes one stale object in the drift scan: its
// entity id, compiled type, human key, stored schema_version, and the specific
// issues memory found. Key and SchemaVersion are nullable on the wire, so they
// decode to "" when memory sends null. The owning pack is NOT part of this
// response — callers map Type → pack via compiled-types' schemaName.
type ObjectValidationResult struct {
	EntityID      string   `json:"entity_id"`
	Type          string   `json:"type"`
	Key           string   `json:"key"`
	SchemaVersion string   `json:"schema_version"`
	Issues        []string `json:"issues"`
}

// TypeRename is one object/relationship type rename in a migration hint.
type TypeRename struct {
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
}

// PropertyRename is one property rename in a migration hint.
type PropertyRename struct {
	TypeName string `json:"type_name" yaml:"type_name"`
	From     string `json:"from" yaml:"from"`
	To       string `json:"to" yaml:"to"`
}

// RemovedProperty is one intentionally-dropped property in a migration hint.
type RemovedProperty struct {
	TypeName string `json:"type_name" yaml:"type_name"`
	Name     string `json:"name" yaml:"name"`
}

// SchemaMigrationHints describes how to upgrade from a previous schema version
// (the `migrations:` block in a pack YAML). Array-form type_renames, matching
// memory's domain entity (the SDK's map form is stale).
type SchemaMigrationHints struct {
	FromVersion       string            `json:"from_version" yaml:"from_version"`
	TypeRenames       []TypeRename      `json:"type_renames,omitempty" yaml:"type_renames,omitempty"`
	PropertyRenames   []PropertyRename  `json:"property_renames,omitempty" yaml:"property_renames,omitempty"`
	RemovedProperties []RemovedProperty `json:"removed_properties,omitempty" yaml:"removed_properties,omitempty"`
}

func (m *MemoryClient) GetSchemaHistory(ctx context.Context) ([]SchemaHistoryItem, error) {
	var out []SchemaHistoryItem
	if err := m.do(ctx, http.MethodGet, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/history", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *MemoryClient) PreviewMigration(ctx context.Context, fromSchemaID, toSchemaID string) (*MigrationPreviewResult, error) {
	var out MigrationPreviewResult
	body := map[string]any{"from_schema_id": fromSchemaID, "to_schema_id": toSchemaID}
	if err := m.do(ctx, http.MethodPost, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/migrate/preview", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) ExecuteMigration(ctx context.Context, fromSchemaID, toSchemaID string, force bool) (*MigrationExecuteResult, error) {
	var out MigrationExecuteResult
	body := map[string]any{"from_schema_id": fromSchemaID, "to_schema_id": toSchemaID, "force": force}
	if err := m.do(ctx, http.MethodPost, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/migrate/execute", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) RollbackMigration(ctx context.Context, toVersion string) (*MigrationRollbackResult, error) {
	var out MigrationRollbackResult
	body := map[string]any{"to_version": toVersion}
	if err := m.do(ctx, http.MethodPost, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/migrate/rollback", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) CommitMigrationArchive(ctx context.Context, throughVersion string) (*MigrationCommitResult, error) {
	var out MigrationCommitResult
	body := map[string]any{"through_version": throughVersion}
	if err := m.do(ctx, http.MethodPost, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/migrate/commit", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) GetMigrationJobStatus(ctx context.Context, jobID string) (*MigrationJob, error) {
	var out MigrationJob
	if err := m.do(ctx, http.MethodGet, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/migration-jobs/"+jobID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) ValidateSchemas(ctx context.Context) (*SchemaValidationResult, error) {
	var out SchemaValidationResult
	if err := m.do(ctx, http.MethodGet, "/api/schemas/projects/"+m.projectIDFor(ctx)+"/validate", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
