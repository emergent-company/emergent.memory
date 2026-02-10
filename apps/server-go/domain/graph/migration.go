package graph

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/emergent/emergent-core/domain/extraction/agents"
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
	ObjectID      uuid.UUID        `json:"object_id"`
	FromVersion   string           `json:"from_version"`
	ToVersion     string           `json:"to_version"`
	Success       bool             `json:"success"`
	MigratedProps []string         `json:"migrated_props"`
	DroppedProps  []string         `json:"dropped_props"`
	AddedProps    []string         `json:"added_props"`
	CoercedProps  []string         `json:"coerced_props"`
	Issues        []MigrationIssue `json:"issues,omitempty"`
	NewProperties map[string]any   `json:"new_properties"`
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
			result.DroppedProps = append(result.DroppedProps, propName)
			result.Issues = append(result.Issues, MigrationIssue{
				Type:        IssueTypeFieldRemoved,
				Field:       propName,
				OldValue:    value,
				Description: fmt.Sprintf("Field %s exists in old schema but not in new schema", propName),
				Suggestion:  fmt.Sprintf("Review if data should be migrated to another field or can be safely dropped"),
				Severity:    "warning",
			})
		}
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
