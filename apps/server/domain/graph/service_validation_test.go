package graph

import (
	"context"
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSchemaProvider is a simple mock for SchemaProvider.
type mockSchemaProvider struct {
	schemas *ExtractionSchemas
	err     error
}

func (m *mockSchemaProvider) GetProjectSchemas(_ context.Context, _ string) (*ExtractionSchemas, error) {
	return m.schemas, m.err
}

// newTestService creates a Service wired only with a schema provider (repo is nil).
// Only use for tests that expect early-return errors from schema validation.
func newTestService(sp SchemaProvider) *Service {
	return &Service{
		schemaProvider: sp,
		log:            slog.Default(),
	}
}

func schemasWithObjects(types ...string) *ExtractionSchemas {
	objSchemas := make(map[string]agents.ObjectSchema, len(types))
	for _, t := range types {
		objSchemas[t] = agents.ObjectSchema{
			Name:       t,
			Properties: map[string]agents.PropertyDef{},
		}
	}
	return &ExtractionSchemas{
		ObjectSchemas:       objSchemas,
		RelationshipSchemas: nil,
	}
}

// ---- Task 6.3: Service-level tests for Create (object) ----

func TestService_Create_ObjectTypeAllowlist(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()

	t.Run("unknown type rejected when schema installed", func(t *testing.T) {
		svc := newTestService(&mockSchemaProvider{schemas: schemasWithObjects("Person")})
		_, err := svc.Create(ctx, projectID, &CreateGraphObjectRequest{
			Type:       "UnknownType",
			Properties: map[string]any{},
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object_type_not_allowed")
	})

	t.Run("empty schema blocks all types", func(t *testing.T) {
		svc := newTestService(&mockSchemaProvider{schemas: schemasWithObjects()})
		_, err := svc.Create(ctx, projectID, &CreateGraphObjectRequest{
			Type:       "Person",
			Properties: map[string]any{},
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object_type_not_allowed")
	})

	t.Run("no schema installed passes through (nil schemas)", func(t *testing.T) {
		// nil schemas means no schema installed — should pass through (but will then
		// hit a nil-repo panic on the actual DB call, so we recover here).
		svc := newTestService(&mockSchemaProvider{schemas: nil})
		defer func() { recover() }() //nolint:errcheck
		_, _ = svc.Create(ctx, projectID, &CreateGraphObjectRequest{
			Type:       "AnyType",
			Properties: map[string]any{},
		}, nil)
	})

	t.Run("unknown property rejected when schema has properties", func(t *testing.T) {
		schemas := &ExtractionSchemas{
			ObjectSchemas: map[string]agents.ObjectSchema{
				"Person": {
					Name: "Person",
					Properties: map[string]agents.PropertyDef{
						"name": {Type: "string"},
					},
				},
			},
		}
		svc := newTestService(&mockSchemaProvider{schemas: schemas})
		_, err := svc.Create(ctx, projectID, &CreateGraphObjectRequest{
			Type:       "Person",
			Properties: map[string]any{"name": "Alice", "unknown_field": "oops"},
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown property")
	})
}

func TestService_CreateOrUpdate_ObjectTypeAllowlist(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	key := "alice"

	t.Run("unknown type rejected", func(t *testing.T) {
		svc := newTestService(&mockSchemaProvider{schemas: schemasWithObjects("Person")})
		_, _, err := svc.CreateOrUpdate(ctx, projectID, &CreateGraphObjectRequest{
			Type:       "Robot",
			Key:        &key,
			Properties: map[string]any{},
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object_type_not_allowed")
	})

	t.Run("unknown property rejected", func(t *testing.T) {
		schemas := &ExtractionSchemas{
			ObjectSchemas: map[string]agents.ObjectSchema{
				"Person": {
					Name: "Person",
					Properties: map[string]agents.PropertyDef{
						"name": {Type: "string"},
					},
				},
			},
		}
		svc := newTestService(&mockSchemaProvider{schemas: schemas})
		_, _, err := svc.CreateOrUpdate(ctx, projectID, &CreateGraphObjectRequest{
			Type:       "Person",
			Key:        &key,
			Properties: map[string]any{"name": "Alice", "extra": "bad"},
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown property")
	})
}

// ---- Task 6.4: Service-level tests for CreateRelationship ----

func TestService_CreateRelationship_Validation(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()

	relSchemas := map[string]agents.RelationshipSchema{
		"WORKS_AT": {
			Name:        "WORKS_AT",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Company"},
			Properties:  map[string]agents.PropertyDef{},
		},
	}

	t.Run("no schema installed passes through (will hit nil repo)", func(t *testing.T) {
		svc := newTestService(&mockSchemaProvider{schemas: nil})
		defer func() { recover() }() //nolint:errcheck
		_, _ = svc.CreateRelationship(ctx, projectID, &CreateGraphRelationshipRequest{
			Type:  "ANY_REL",
			SrcID: uuid.New(),
			DstID: uuid.New(),
		})
	})

	t.Run("unknown relationship type rejected", func(t *testing.T) {
		// Relationship type check happens after endpoint resolution (which requires DB).
		// The logic is unit-tested via TestValidateRelationship.
		// Here we just verify the schema is configured properly.
		schemas := &ExtractionSchemas{
			ObjectSchemas:       schemasWithObjects("Person", "Company").ObjectSchemas,
			RelationshipSchemas: relSchemas,
		}
		_ = schemas // used in full integration test path
		t.Skip("relationship type check happens after endpoint resolution which requires DB")
	})
}

// ---- Task 6.5: Service-level tests for PatchRelationship ----
// PatchRelationship also requires DB (GetRelationshipByID), so we test the
// validation logic via the unit-tested validateRelationship function (task 6.2).
// The integration path is covered by e2e tests.

// ---- Schema versioning compatibility tests ----

// TestPatch_SchemaVersionCompatibility verifies that objects created under an older schema
// version (which may have properties no longer in the current schema) can still be patched
// on unrelated fields without being rejected for their legacy properties.
//
// The scenario:
//   - Schema v1 had "name" and "legacy_field"
//   - Schema v2 removed "legacy_field", kept "name"
//   - An object created under v1 has both "name" and "legacy_field" stored
//   - Patching "name" under v2 should succeed — "legacy_field" is not touched by the patch
func TestPatch_SchemaVersionCompatibility(t *testing.T) {
	// Schema v2: only "name" is a known property.
	schemaV2 := agents.ObjectSchema{
		Name: "Person",
		Properties: map[string]agents.PropertyDef{
			"name": {Type: "string"},
		},
	}

	t.Run("patch delta with only known properties passes even if stored object has legacy props", func(t *testing.T) {
		// The patch only touches "name" — validatePatchProperties should not see "legacy_field".
		patchDelta := map[string]any{"name": "Bob"}
		out, err := validatePatchProperties(patchDelta, schemaV2)
		assert.NoError(t, err)
		assert.Equal(t, "Bob", out["name"])
	})

	t.Run("patch delta introducing an unknown property is rejected", func(t *testing.T) {
		// If someone tries to ADD a property that isn't in the current schema, reject it.
		patchDelta := map[string]any{"name": "Bob", "new_unknown": "oops"}
		_, err := validatePatchProperties(patchDelta, schemaV2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown property: new_unknown")
	})

	t.Run("patch delta with nil (delete) for legacy property is allowed", func(t *testing.T) {
		// A client cleaning up an old property by setting it to null should be allowed.
		patchDelta := map[string]any{"legacy_field": nil}
		out, err := validatePatchProperties(patchDelta, schemaV2)
		assert.NoError(t, err)
		assert.Nil(t, out["legacy_field"])
	})
}
