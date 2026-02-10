package graph

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/emergent/emergent-core/domain/extraction/agents"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaMigration_SimpleFieldMigration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("compatible_fields_migrate_successfully", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
				"age":  {Type: "number", Description: "Age in years"},
			},
			Required: []string{"name"},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
				"age":  {Type: "number", Description: "Age in years"},
				"city": {Type: "string", Description: "City of residence"},
			},
			Required: []string{"name"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name": "John Doe",
				"age":  float64(30),
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success, "Migration should succeed")
		assert.Equal(t, 2, len(result.MigratedProps), "Should migrate 2 properties")
		assert.Contains(t, result.MigratedProps, "name")
		assert.Contains(t, result.MigratedProps, "age")
		assert.Equal(t, 1, len(result.AddedProps), "Should add 1 new optional property")
		assert.Contains(t, result.AddedProps, "city")
		assert.Equal(t, 0, len(result.DroppedProps), "No properties should be dropped")
		assert.Equal(t, 0, len(result.Issues), "No issues should be reported")
		assert.Equal(t, "John Doe", result.NewProperties["name"])
		assert.Equal(t, float64(30), result.NewProperties["age"])
	})
}

func TestSchemaMigration_FieldRemoval(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("removed_fields_are_flagged", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":      {Type: "string", Description: "Full name"},
				"age":       {Type: "number", Description: "Age in years"},
				"old_field": {Type: "string", Description: "Deprecated field"},
			},
			Required: []string{"name"},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
				"age":  {Type: "number", Description: "Age in years"},
			},
			Required: []string{"name"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name":      "John Doe",
				"age":       float64(30),
				"old_field": "deprecated value",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success, "Migration should succeed despite field removal")
		assert.Equal(t, 1, len(result.DroppedProps), "Should drop 1 property")
		assert.Contains(t, result.DroppedProps, "old_field")
		assert.Equal(t, 1, len(result.Issues), "Should report 1 issue")
		assert.Equal(t, IssueTypeFieldRemoved, result.Issues[0].Type)
		assert.Equal(t, "old_field", result.Issues[0].Field)
		assert.Equal(t, "warning", result.Issues[0].Severity)
		assert.Contains(t, result.Issues[0].Suggestion, "migrated to another field")
	})
}

func TestSchemaMigration_TypeCoercion(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("coercible_type_changes_succeed", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
				"age":  {Type: "string", Description: "Age as string"},
			},
			Required: []string{"name"},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
				"age":  {Type: "number", Description: "Age as number"},
			},
			Required: []string{"name"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name": "John Doe",
				"age":  "30",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success, "Migration should succeed with coercion")
		assert.Equal(t, 1, len(result.CoercedProps), "Should coerce 1 property")
		assert.Contains(t, result.CoercedProps, "age")
		assert.Equal(t, float64(30), result.NewProperties["age"])
		assert.Equal(t, 0, len(result.Issues), "No issues for successful coercion")
	})

	t.Run("incompatible_type_changes_fail", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
				"age":  {Type: "string", Description: "Age as string"},
			},
			Required: []string{"name"},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
				"age":  {Type: "number", Description: "Age as number"},
			},
			Required: []string{"name"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name": "John Doe",
				"age":  "not a number",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.False(t, result.Success, "Migration should fail due to coercion error")
		assert.Equal(t, 1, len(result.Issues), "Should report 1 issue")
		assert.Equal(t, IssueTypeCoercionFailed, result.Issues[0].Type)
		assert.Equal(t, "age", result.Issues[0].Field)
		assert.Equal(t, "error", result.Issues[0].Severity)
		assert.Contains(t, result.Issues[0].Suggestion, "Manually convert")
	})
}

func TestSchemaMigration_NewRequiredFields(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("new_optional_fields_are_added", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
			},
			Required: []string{"name"},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":  {Type: "string", Description: "Full name"},
				"email": {Type: "string", Description: "Email address"},
			},
			Required: []string{"name"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name": "John Doe",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success, "Migration should succeed with new optional field")
		assert.Equal(t, 1, len(result.AddedProps), "Should add 1 new property")
		assert.Contains(t, result.AddedProps, "email")
		assert.Equal(t, 0, len(result.Issues), "No issues for new optional field")
	})

	t.Run("new_required_fields_flag_error", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
			},
			Required: []string{"name"},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":  {Type: "string", Description: "Full name"},
				"email": {Type: "string", Description: "Email address"},
			},
			Required: []string{"name", "email"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name": "John Doe",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.False(t, result.Success, "Migration should fail due to new required field")
		requireIssueCount := 0
		for _, issue := range result.Issues {
			if issue.Type == IssueTypeNewRequiredField {
				requireIssueCount++
				assert.Equal(t, "email", issue.Field)
				assert.Equal(t, "error", issue.Severity)
				assert.Contains(t, issue.Suggestion, "Provide a default value")
			}
		}
		assert.Equal(t, 1, requireIssueCount, "Should report new required field issue")
	})
}

func TestSchemaMigration_ComplexScenario(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("complex_migration_with_multiple_changes", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":       {Type: "string", Description: "Full name"},
				"age":        {Type: "string", Description: "Age as string"},
				"old_field":  {Type: "string", Description: "To be removed"},
				"department": {Type: "string", Description: "Department"},
			},
			Required: []string{"name"},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":       {Type: "string", Description: "Full name"},
				"age":        {Type: "number", Description: "Age as number"},
				"department": {Type: "string", Description: "Department"},
				"email":      {Type: "string", Description: "Email address"},
				"hire_date":  {Type: "date", Description: "Hire date"},
			},
			Required: []string{"name", "email"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name":       "John Doe",
				"age":        "30",
				"old_field":  "deprecated",
				"department": "Engineering",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		require.NotNil(t, result)
		assert.False(t, result.Success, "Migration should fail due to new required field")

		assert.Contains(t, result.MigratedProps, "name", "name should migrate")
		assert.Contains(t, result.MigratedProps, "department", "department should migrate")
		assert.Contains(t, result.CoercedProps, "age", "age should be coerced")
		assert.Contains(t, result.DroppedProps, "old_field", "old_field should be dropped")
		assert.Contains(t, result.AddedProps, "hire_date", "hire_date should be added (optional)")

		hasNewRequiredFieldIssue := false
		hasFieldRemovedIssue := false

		for _, issue := range result.Issues {
			switch issue.Type {
			case IssueTypeNewRequiredField:
				assert.Equal(t, "email", issue.Field)
				hasNewRequiredFieldIssue = true
			case IssueTypeFieldRemoved:
				assert.Equal(t, "old_field", issue.Field)
				hasFieldRemovedIssue = true
			}
		}

		assert.True(t, hasNewRequiredFieldIssue, "Should flag new required field")
		assert.True(t, hasFieldRemovedIssue, "Should flag removed field")

		assert.Equal(t, "John Doe", result.NewProperties["name"])
		assert.Equal(t, float64(30), result.NewProperties["age"])
		assert.Equal(t, "Engineering", result.NewProperties["department"])
	})
}

func TestSchemaMigration_MultipleVersionCoexistence(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("objects_with_different_schema_versions_can_coexist", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string", Description: "Full name"},
				"age":  {Type: "number", Description: "Age"},
			},
			Required: []string{"name"},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":  {Type: "string", Description: "Full name"},
				"age":   {Type: "number", Description: "Age"},
				"email": {Type: "string", Description: "Email"},
			},
			Required: []string{"name"},
		}

		objV1 := &GraphObject{
			ID:            uuid.New(),
			Type:          "Person",
			SchemaVersion: stringPtr("1.0.0"),
			Properties: map[string]any{
				"name": "Alice",
				"age":  float64(25),
			},
		}

		objV2 := &GraphObject{
			ID:            uuid.New(),
			Type:          "Person",
			SchemaVersion: stringPtr("2.0.0"),
			Properties: map[string]any{
				"name":  "Bob",
				"age":   float64(30),
				"email": "bob@example.com",
			},
		}

		resultV1ToV2 := migrator.MigrateObject(ctx, objV1, v1Schema, v2Schema, "1.0.0", "2.0.0")
		assert.True(t, resultV1ToV2.Success, "v1 to v2 migration should succeed")
		assert.Equal(t, "Alice", resultV1ToV2.NewProperties["name"])
		assert.Equal(t, float64(25), resultV1ToV2.NewProperties["age"])

		resultV2ToV2 := migrator.MigrateObject(ctx, objV2, v2Schema, v2Schema, "2.0.0", "2.0.0")
		assert.True(t, resultV2ToV2.Success, "v2 to v2 migration (no-op) should succeed")
		assert.Equal(t, "Bob", resultV2ToV2.NewProperties["name"])
		assert.Equal(t, float64(30), resultV2ToV2.NewProperties["age"])
		assert.Equal(t, "bob@example.com", resultV2ToV2.NewProperties["email"])
	})
}

func stringPtr(s string) *string {
	return &s
}
