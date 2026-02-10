package graph

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/emergent/emergent-core/domain/extraction/agents"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestMigrationRiskAssessment_Safe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("no_changes_is_safe", func(t *testing.T) {
		schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string"},
				"age":  {Type: "number"},
			},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name": "Alice",
				"age":  float64(30),
			},
		}

		result := migrator.MigrateObject(ctx, obj, schema, schema, "1.0.0", "1.0.0")

		assert.True(t, result.Success)
		assert.Equal(t, RiskLevelSafe, result.RiskLevel)
		assert.True(t, result.CanProceed)
		assert.Empty(t, result.BlockReason)
	})

	t.Run("adding_optional_fields_is_safe", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string"},
			},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":  {Type: "string"},
				"email": {Type: "string"},
			},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name": "Alice",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success)
		assert.Equal(t, RiskLevelSafe, result.RiskLevel)
		assert.True(t, result.CanProceed)
	})
}

func TestMigrationRiskAssessment_Cautious(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("type_coercion_is_cautious", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"age": {Type: "string"},
			},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"age": {Type: "number"},
			},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"age": "30",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success)
		assert.Equal(t, RiskLevelCautious, result.RiskLevel)
		assert.True(t, result.CanProceed)
		assert.Contains(t, result.CoercedProps, "age")
	})
}

func TestMigrationRiskAssessment_Risky(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("dropping_one_field_is_risky", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":      {Type: "string"},
				"old_field": {Type: "string"},
			},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string"},
			},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name":      "Alice",
				"old_field": "important data",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success)
		assert.Equal(t, RiskLevelRisky, result.RiskLevel)
		assert.False(t, result.CanProceed)
		assert.Contains(t, result.BlockReason, "--force")
		assert.Equal(t, 1, len(result.DroppedProps))
	})

	t.Run("dropping_two_fields_is_risky", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":       {Type: "string"},
				"old_field1": {Type: "string"},
				"old_field2": {Type: "string"},
			},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string"},
			},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name":       "Alice",
				"old_field1": "data1",
				"old_field2": "data2",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success)
		assert.Equal(t, RiskLevelRisky, result.RiskLevel)
		assert.False(t, result.CanProceed)
		assert.Contains(t, result.BlockReason, "--force")
		assert.Equal(t, 2, len(result.DroppedProps))
	})
}

func TestMigrationRiskAssessment_Dangerous(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := NewSchemaMigrator(NewPropertyValidator(), logger)
	ctx := context.Background()

	t.Run("validation_errors_are_dangerous", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"age": {Type: "string"},
			},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"age": {Type: "number"},
			},
			Required: []string{"age"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"age": "not a number",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.False(t, result.Success)
		assert.Equal(t, RiskLevelDangerous, result.RiskLevel)
		assert.False(t, result.CanProceed)
		assert.Contains(t, result.BlockReason, "error")
	})

	t.Run("new_required_field_is_dangerous", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name": {Type: "string"},
			},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Person",
			Properties: map[string]agents.PropertyDef{
				"name":  {Type: "string"},
				"email": {Type: "string"},
			},
			Required: []string{"email"},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Person",
			Properties: map[string]any{
				"name": "Alice",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.False(t, result.Success)
		assert.Equal(t, RiskLevelDangerous, result.RiskLevel)
		assert.False(t, result.CanProceed)
		assert.Contains(t, result.BlockReason, "error")
	})

	t.Run("dropping_three_or_more_fields_is_dangerous", func(t *testing.T) {
		v1Schema := &agents.ObjectSchema{
			Name: "Document",
			Properties: map[string]agents.PropertyDef{
				"title":      {Type: "string"},
				"old_field1": {Type: "string"},
				"old_field2": {Type: "string"},
				"old_field3": {Type: "string"},
			},
		}

		v2Schema := &agents.ObjectSchema{
			Name: "Document",
			Properties: map[string]agents.PropertyDef{
				"title": {Type: "string"},
			},
		}

		obj := &GraphObject{
			ID:   uuid.New(),
			Type: "Document",
			Properties: map[string]any{
				"title":      "My Doc",
				"old_field1": "data1",
				"old_field2": "data2",
				"old_field3": "data3",
			},
		}

		result := migrator.MigrateObject(ctx, obj, v1Schema, v2Schema, "1.0.0", "2.0.0")

		assert.True(t, result.Success)
		assert.Equal(t, RiskLevelDangerous, result.RiskLevel)
		assert.False(t, result.CanProceed)
		assert.Contains(t, result.BlockReason, "--force --confirm-data-loss")
		assert.Equal(t, 3, len(result.DroppedProps))
	})
}
