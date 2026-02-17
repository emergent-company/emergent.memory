package graph

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent/domain/extraction/agents"
	"github.com/google/uuid"
)

type MigrationIssueType string

const (
	IssueTypeFieldRenamed     MigrationIssueType = "field_renamed"
	IssueTypeFieldRemoved     MigrationIssueType = "field_removed"
	IssueTypeFieldTypeChanged MigrationIssueType = "field_type_changed"
	IssueTypeNewRequiredField MigrationIssueType = "new_required_field"
	IssueTypeCoercionFailed   MigrationIssueType = "coercion_failed"
	IssueTypeValidationFailed MigrationIssueType = "validation_failed"
	IssueTypeIncompatibleType MigrationIssueType = "incompatible_type"
)

type MigrationRiskLevel string

const (
	RiskLevelSafe      MigrationRiskLevel = "safe"
	RiskLevelCautious  MigrationRiskLevel = "cautious"
	RiskLevelRisky     MigrationRiskLevel = "risky"
	RiskLevelDangerous MigrationRiskLevel = "dangerous"
)

type MigrationIssue struct {
	Type        MigrationIssueType `json:"type"`
	Field       string             `json:"field"`
	OldValue    any                `json:"old_value,omitempty"`
	NewType     string             `json:"new_type,omitempty"`
	Description string             `json:"description"`
	Suggestion  string             `json:"suggestion"`
	Severity    string             `json:"severity"`
}

type MigrationResult struct {
	ObjectID      uuid.UUID          `json:"object_id"`
	FromVersion   string             `json:"from_version"`
	ToVersion     string             `json:"to_version"`
	Success       bool               `json:"success"`
	RiskLevel     MigrationRiskLevel `json:"risk_level"`
	CanProceed    bool               `json:"can_proceed"`
	BlockReason   string             `json:"block_reason,omitempty"`
	MigratedProps []string           `json:"migrated_props"`
	DroppedProps  []string           `json:"dropped_props"`
	AddedProps    []string           `json:"added_props"`
	CoercedProps  []string           `json:"coerced_props"`
	Issues        []MigrationIssue   `json:"issues,omitempty"`
	NewProperties map[string]any     `json:"new_properties"`
}

type SchemaMigrator struct {
	validator *PropertyValidator
	logger    *slog.Logger
}

func NewSchemaMigrator(validator *PropertyValidator, logger *slog.Logger) *SchemaMigrator {
	return &SchemaMigrator{
		validator: validator,
		logger:    logger.With(slog.String("component", "schema_migrator")),
	}
}

func (m *SchemaMigrator) MigrateObject(
	ctx context.Context,
	obj *GraphObject,
	fromSchema *agents.ObjectSchema,
	toSchema *agents.ObjectSchema,
	fromVersion string,
	toVersion string,
) *MigrationResult {
	result := &MigrationResult{
		ObjectID:      obj.ID,
		FromVersion:   fromVersion,
		ToVersion:     toVersion,
		Success:       true,
		MigratedProps: []string{},
		DroppedProps:  []string{},
		AddedProps:    []string{},
		CoercedProps:  []string{},
		Issues:        []MigrationIssue{},
		NewProperties: make(map[string]any),
	}

	if obj.Properties == nil {
		obj.Properties = make(map[string]any)
	}

	fromProps := make(map[string]string)
	for propName, propDef := range fromSchema.Properties {
		fromProps[propName] = propDef.Type
	}

	toProps := make(map[string]agents.PropertyDef)
	for propName, propDef := range toSchema.Properties {
		toProps[propName] = propDef
	}

	droppedData := make(map[string]any)

	for propName, value := range obj.Properties {
		if newPropDef, existsInNew := toProps[propName]; existsInNew {
			oldType := fromProps[propName]
			newType := newPropDef.Type

			if oldType == newType {
				result.NewProperties[propName] = value
				result.MigratedProps = append(result.MigratedProps, propName)
			} else {
				coerced, err := m.validator.coerceValue(value, newType)
				if err != nil {
					result.Issues = append(result.Issues, MigrationIssue{
						Type:        IssueTypeCoercionFailed,
						Field:       propName,
						OldValue:    value,
						NewType:     newType,
						Description: fmt.Sprintf("Failed to coerce %s from %s to %s", propName, oldType, newType),
						Suggestion:  fmt.Sprintf("Manually convert value %v to type %s", value, newType),
						Severity:    "error",
					})
					result.Success = false
				} else {
					result.NewProperties[propName] = coerced
					result.CoercedProps = append(result.CoercedProps, propName)
				}
			}
		} else {
			droppedData[propName] = value
			result.DroppedProps = append(result.DroppedProps, propName)
			result.Issues = append(result.Issues, MigrationIssue{
				Type:        IssueTypeFieldRemoved,
				Field:       propName,
				OldValue:    value,
				Description: fmt.Sprintf("Field %s exists in old schema but not in new schema", propName),
				Suggestion:  fmt.Sprintf("Data will be archived in migration_archive field and can be restored"),
				Severity:    "warning",
			})
		}
	}

	if len(droppedData) > 0 {
		archiveEntry := map[string]any{
			"from_version": fromVersion,
			"to_version":   toVersion,
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
			"dropped_data": droppedData,
		}
		obj.MigrationArchive = append(obj.MigrationArchive, archiveEntry)

		m.logger.Info("Archived dropped fields",
			slog.String("object_id", obj.ID.String()),
			slog.Int("fields_archived", len(droppedData)),
			slog.Any("fields", getKeys(droppedData)))
	}

	for propName, propDef := range toProps {
		if _, existsInOld := obj.Properties[propName]; !existsInOld {
			isRequired := false
			for _, reqField := range toSchema.Required {
				if reqField == propName {
					isRequired = true
					break
				}
			}

			if isRequired {
				result.Issues = append(result.Issues, MigrationIssue{
					Type:        IssueTypeNewRequiredField,
					Field:       propName,
					NewType:     propDef.Type,
					Description: fmt.Sprintf("New required field %s added in schema version %s", propName, toVersion),
					Suggestion:  fmt.Sprintf("Provide a default value or manually populate %s for existing objects", propName),
					Severity:    "error",
				})
				result.Success = false
			} else {
				result.AddedProps = append(result.AddedProps, propName)
			}
		}
	}

	validationResult := m.validator.ValidateProperties(result.NewProperties, toSchema)
	if !validationResult.Valid {
		for _, err := range validationResult.Errors {
			result.Issues = append(result.Issues, MigrationIssue{
				Type:        IssueTypeValidationFailed,
				Field:       err.Field,
				Description: err.Message,
				Suggestion:  "Fix validation error before migration can complete",
				Severity:    "error",
			})
		}
		result.Success = false
	}

	m.assessRisk(result)

	return result
}

func (m *SchemaMigrator) MigrateRelationship(
	ctx context.Context,
	rel *GraphRelationship,
	fromSchema *agents.RelationshipSchema,
	toSchema *agents.RelationshipSchema,
	fromVersion string,
	toVersion string,
) *MigrationResult {
	result := &MigrationResult{
		ObjectID:      rel.ID,
		FromVersion:   fromVersion,
		ToVersion:     toVersion,
		Success:       true,
		MigratedProps: []string{},
		DroppedProps:  []string{},
		CoercedProps:  []string{},
		Issues:        []MigrationIssue{},
		NewProperties: make(map[string]any),
	}

	if rel.Properties == nil {
		rel.Properties = make(map[string]any)
	}

	for propName, value := range rel.Properties {
		result.NewProperties[propName] = value
		result.MigratedProps = append(result.MigratedProps, propName)
	}

	return result
}

// getKeys extracts keys from a map for logging purposes
func getKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func (m *SchemaMigrator) assessRisk(result *MigrationResult) {
	errorCount := 0
	warningCount := 0

	for _, issue := range result.Issues {
		if issue.Severity == "error" {
			errorCount++
		} else if issue.Severity == "warning" {
			warningCount++
		}
	}

	if errorCount > 0 {
		result.RiskLevel = RiskLevelDangerous
		result.CanProceed = false
		result.BlockReason = fmt.Sprintf("Migration has %d error(s) that must be resolved before proceeding", errorCount)
		return
	}

	droppedFieldCount := len(result.DroppedProps)

	if droppedFieldCount >= 3 {
		result.RiskLevel = RiskLevelDangerous
		result.CanProceed = false
		result.BlockReason = fmt.Sprintf("Dropping %d fields is too risky. Use --force --confirm-data-loss to proceed anyway", droppedFieldCount)
		return
	}

	if droppedFieldCount >= 1 {
		result.RiskLevel = RiskLevelRisky
		result.CanProceed = false
		result.BlockReason = fmt.Sprintf("Dropping %d field(s). Data will be archived. Use --force to proceed", droppedFieldCount)
		return
	}

	if len(result.CoercedProps) > 0 {
		result.RiskLevel = RiskLevelCautious
		result.CanProceed = true
		return
	}

	result.RiskLevel = RiskLevelSafe
	result.CanProceed = true
}

type RollbackResult struct {
	ObjectID      uuid.UUID `json:"object_id"`
	FromVersion   string    `json:"from_version"`
	ToVersion     string    `json:"to_version"`
	Success       bool      `json:"success"`
	RestoredProps []string  `json:"restored_props"`
	Error         string    `json:"error,omitempty"`
}

func (m *SchemaMigrator) RollbackObject(
	obj *GraphObject,
	toVersion string,
) *RollbackResult {
	fromVersion := ""
	if obj.SchemaVersion != nil {
		fromVersion = *obj.SchemaVersion
	}

	result := &RollbackResult{
		ObjectID:      obj.ID,
		FromVersion:   fromVersion,
		ToVersion:     toVersion,
		Success:       false,
		RestoredProps: []string{},
	}

	if len(obj.MigrationArchive) == 0 {
		result.Error = "No migration archive found - cannot rollback"
		return result
	}

	var targetArchive map[string]any
	var archiveIndex int = -1

	for i := len(obj.MigrationArchive) - 1; i >= 0; i-- {
		archive := obj.MigrationArchive[i]
		if archive["to_version"] == toVersion {
			targetArchive = archive
			archiveIndex = i
			break
		}
	}

	if targetArchive == nil {
		result.Error = fmt.Sprintf("No migration archive found for version %s", toVersion)
		return result
	}

	droppedData, ok := targetArchive["dropped_data"].(map[string]any)
	if !ok {
		result.Error = "Invalid archive format - dropped_data missing or malformed"
		return result
	}

	if obj.Properties == nil {
		obj.Properties = make(map[string]any)
	}

	for propName, value := range droppedData {
		obj.Properties[propName] = value
		result.RestoredProps = append(result.RestoredProps, propName)
	}

	obj.MigrationArchive = obj.MigrationArchive[:archiveIndex]

	result.Success = true
	result.ToVersion = targetArchive["from_version"].(string)

	m.logger.Info("Rolled back object",
		slog.String("object_id", obj.ID.String()),
		slog.String("from_version", result.FromVersion),
		slog.String("to_version", result.ToVersion),
		slog.Int("restored_fields", len(result.RestoredProps)),
		slog.Any("fields", result.RestoredProps))

	return result
}
