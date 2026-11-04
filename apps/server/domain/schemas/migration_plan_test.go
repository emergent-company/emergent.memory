package schemas

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMigrationPlan(t *testing.T) {
	// Rename scenario: Contract → Agreement (via hints), plus brand-new Clause.
	fromRenamed := map[string]*agents.ObjectSchema{
		"Contract": {
			Name: "Contract",
			Properties: map[string]agents.PropertyDef{
				"title":      {Type: "string"},
				"signedDate": {Type: "string"},
				"legacyId":   {Type: "string"},
			},
		},
	}
	toWithRename := map[string]*agents.ObjectSchema{
		"Agreement": {
			Name: "Agreement",
			Properties: map[string]agents.PropertyDef{
				"title":        {Type: "string"},
				"signed_at":    {Type: "string"},
				"jurisdiction": {Type: "string"},
			},
		},
		"Clause": {
			Name: "Clause",
			Properties: map[string]agents.PropertyDef{
				"text": {Type: "string"},
			},
		},
	}

	// Same-name scenario for added-property diffing on an existing type.
	fromSameName := map[string]*agents.ObjectSchema{
		"Agreement": {
			Name: "Agreement",
			Properties: map[string]agents.PropertyDef{
				"title":      {Type: "string"},
				"signedDate": {Type: "string"},
				"legacyId":   {Type: "string"},
			},
		},
	}

	t.Run("populates_renames_and_removals_from_hints", func(t *testing.T) {
		hints := &SchemaMigrationHints{
			FromVersion: "1.0.0",
			TypeRenames: []TypeRename{{From: "Contract", To: "Agreement"}},
			PropertyRenames: []PropertyRename{
				{TypeName: "Agreement", From: "signedDate", To: "signed_at"},
			},
			RemovedProperties: []RemovedProperty{
				{TypeName: "Agreement", Name: "legacyId"},
			},
		}

		plan := buildMigrationPlan(fromRenamed, toWithRename, hints)
		require.NotNil(t, plan)
		assert.Equal(t, []TypeRename{{From: "Contract", To: "Agreement"}}, plan.TypeRenames)
		assert.Equal(t, []PropertyRename{{TypeName: "Agreement", From: "signedDate", To: "signed_at"}}, plan.PropertyRenames)
		assert.Equal(t, []RemovedProperty{{TypeName: "Agreement", Name: "legacyId"}}, plan.RemovedProperties)

		// Agreement arrives via the hint rename → excluded from added_types.
		assert.Equal(t, []string{"Clause"}, plan.AddedTypes)
		assert.Empty(t, plan.AddedProperties)
	})

	t.Run("computes_added_types_and_properties_from_schema_diff", func(t *testing.T) {
		plan := buildMigrationPlan(fromSameName, toWithRename, nil)
		require.NotNil(t, plan)
		assert.Empty(t, plan.TypeRenames)
		assert.Empty(t, plan.PropertyRenames)
		assert.Empty(t, plan.RemovedProperties)
		assert.Equal(t, []string{"Clause"}, plan.AddedTypes)
		assert.ElementsMatch(t, []AddedProperty{
			{TypeName: "Agreement", Name: "signed_at"},
			{TypeName: "Agreement", Name: "jurisdiction"},
		}, plan.AddedProperties)
	})

	t.Run("empty_schemas_yields_empty_plan", func(t *testing.T) {
		plan := buildMigrationPlan(map[string]*agents.ObjectSchema{}, map[string]*agents.ObjectSchema{}, nil)
		require.NotNil(t, plan)
		assert.Empty(t, plan.TypeRenames)
		assert.Empty(t, plan.PropertyRenames)
		assert.Empty(t, plan.RemovedProperties)
		assert.Empty(t, plan.AddedTypes)
		assert.Empty(t, plan.AddedProperties)
	})
}

func TestAggregateMigrationProps(t *testing.T) {
	results := []*graph.MigrationResult{
		{
			MigratedProps: []string{"title", "body"},
			DroppedProps:  []string{"legacyId"},
			AddedProps:    []string{"jurisdiction"},
			CoercedProps:  []string{"signedDate"},
		},
		{
			MigratedProps: []string{"title", "owner"},
			DroppedProps:  []string{"legacyId", "obsolete"},
			AddedProps:    []string{"jurisdiction"},
			CoercedProps:  []string{"signedDate", "partyCount"},
		},
	}

	typeResult := &MigrationTypeResult{TypeName: "Agreement"}
	aggregateMigrationProps(typeResult, results)

	assert.ElementsMatch(t, []string{"title", "body", "owner"}, typeResult.MigratedProps)
	assert.ElementsMatch(t, []string{"legacyId", "obsolete"}, typeResult.DroppedProps)
	assert.ElementsMatch(t, []string{"jurisdiction"}, typeResult.AddedProps)
	assert.ElementsMatch(t, []string{"signedDate", "partyCount"}, typeResult.CoercedProps)

	// Union is idempotent — re-aggregating must not duplicate entries.
	aggregateMigrationProps(typeResult, results)
	assert.Len(t, typeResult.MigratedProps, 3)
	assert.Len(t, typeResult.DroppedProps, 2)
	assert.Len(t, typeResult.AddedProps, 1)
	assert.Len(t, typeResult.CoercedProps, 2)
}

// TestPreviewResponseJSONContract locks the wire format of the preview response
// to the pinned contract field names.
func TestPreviewResponseJSONContract(t *testing.T) {
	resp := &SchemaMigrationPreviewResponse{
		ProjectID:    "proj-1",
		FromSchemaID: "schema-a",
		ToSchemaID:   "schema-b",
		OverallRisk:  "risky",
		CanProceed:   true,
		TotalObjects: 12,
		Plan: &SchemaMigrationPlan{
			TypeRenames:     []TypeRename{{From: "Contract", To: "Agreement"}},
			PropertyRenames: []PropertyRename{{TypeName: "Agreement", From: "signedDate", To: "signed_at"}},
			RemovedProperties: []RemovedProperty{
				{TypeName: "Agreement", Name: "legacyId"},
			},
			AddedTypes:      []string{"Clause"},
			AddedProperties: []AddedProperty{{TypeName: "Agreement", Name: "jurisdiction"}},
		},
		PerTypeResults: []MigrationTypeResult{
			{
				TypeName:      "Agreement",
				ObjectCount:   12,
				RiskLevel:     "risky",
				CanProceed:    true,
				MigratedProps: []string{"title"},
				DroppedProps:  []string{"legacyId"},
				AddedProps:    []string{"jurisdiction"},
				CoercedProps:  []string{"signedDate"},
			},
		},
	}

	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	// Top-level contract keys.
	for _, k := range []string{"project_id", "from_schema_id", "to_schema_id", "overall_risk_level", "can_proceed", "total_objects", "plan", "per_type_results"} {
		assert.Contains(t, m, k, "missing top-level key %q", k)
	}
	assert.NotContains(t, m, "suggested_hints_yaml")
	assert.NotContains(t, m, "block_reason", "empty block_reason should be omitted")

	plan, ok := m["plan"].(map[string]any)
	require.True(t, ok)
	for _, k := range []string{"type_renames", "property_renames", "removed_properties", "added_types", "added_properties"} {
		assert.Contains(t, plan, k, "missing plan key %q", k)
	}

	perType, ok := m["per_type_results"].([]any)
	require.True(t, ok)
	require.Len(t, perType, 1)
	typeResult, ok := perType[0].(map[string]any)
	require.True(t, ok)
	for _, k := range []string{"type_name", "object_count", "risk_level", "can_proceed", "migrated_props", "dropped_props", "added_props", "coerced_props"} {
		assert.Contains(t, typeResult, k, "missing per-type key %q", k)
	}
}

// TestPlanBuildsWithRealMigrator sanity-checks that per-object MigrationResult
// prop lists feed through to the per-type summary fields end to end.
func TestPerTypePropsFromMigrator(t *testing.T) {
	from := &agents.ObjectSchema{
		Name: "Agreement",
		Properties: map[string]agents.PropertyDef{
			"title":      {Type: "string"},
			"signedDate": {Type: "string"},
			"legacyId":   {Type: "string"},
		},
	}
	to := &agents.ObjectSchema{
		Name: "Agreement",
		Properties: map[string]agents.PropertyDef{
			"title":        {Type: "string"},
			"signed_at":    {Type: "string"},
			"jurisdiction": {Type: "string"},
		},
	}

	obj := &graph.GraphObject{
		ID:         uuid.New(),
		Type:       "Agreement",
		Properties: map[string]any{"title": "A", "signedDate": "2020-01-01", "legacyId": "x"},
	}

	obj.SchemaVersion = new(string)
	*obj.SchemaVersion = "1.0.0"

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	migrator := graph.NewSchemaMigrator(graph.NewPropertyValidator(), logger)
	result := migrator.MigrateObject(context.Background(), obj, from, to, "1.0.0", "2.0.0")

	typeResult := &MigrationTypeResult{TypeName: "Agreement"}
	aggregateMigrationProps(typeResult, []*graph.MigrationResult{result})

	assert.Contains(t, typeResult.MigratedProps, "title")
	assert.Contains(t, typeResult.DroppedProps, "legacyId")
	assert.Contains(t, typeResult.DroppedProps, "signedDate")
	assert.Contains(t, typeResult.AddedProps, "jurisdiction")
	assert.Contains(t, typeResult.AddedProps, "signed_at")
}
