package mcp

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

// These tests pin issue #811: the schema-create MCP tool writes
// kb.graph_schemas and both registry tables with a direct INSERT, so it must run
// the same definition guard as the REST CreatePack/UpdatePack path. Before this
// fix it only ran the scope-key check, which let a bare scalar `items`
// ("items": "string") install silently on this second entry point — #726 over
// REST, still live over MCP.
//
// Every rejection happens before the handler touches s.db, so these run without
// a database; the nil db is also proof the call was refused on the way in
// rather than after a partial write.
const validationTestProjectID = "11111111-1111-1111-1111-111111111111"

func TestExecuteCreateSchemaRunsSharedDefinitionGuard(t *testing.T) {
	svc := &Service{log: slog.Default()}
	ctx := context.Background()

	objectTypes := func(props map[string]any) map[string]any {
		return map[string]any{
			"Contract": map[string]any{"properties": props},
		}
	}

	t.Run("map form object type rejects a bare scalar items", func(t *testing.T) {
		_, err := svc.executeCreateSchema(ctx, validationTestProjectID, map[string]any{
			"name": "scalar-items",
			"object_type_schemas": objectTypes(map[string]any{
				"tags": map[string]any{"type": "array", "items": "string"},
			}),
		})
		if err == nil {
			t.Fatal("a scalar items declaration must be rejected, not installed silently")
		}
		msg := err.Error()
		for _, want := range []string{
			"invalid schema definitions",
			`objectTypeSchemas[0] ("Contract")`,
			`property "tags".items`,
			"must be an object",
			`{"type": "string"}`,
		} {
			if !strings.Contains(msg, want) {
				t.Fatalf("expected error to contain %q, got %q", want, msg)
			}
		}
	})

	t.Run("legacy object_types string form runs the same guard", func(t *testing.T) {
		// This form used to be silently dropped when it named its type with
		// "name" (the REST key) instead of "type_name"; the guard now names the
		// type and the offending property instead of creating an empty pack.
		_, err := svc.executeCreateSchema(ctx, validationTestProjectID, map[string]any{
			"name":         "legacy-scalar-items",
			"object_types": `[{"name":"Contract","properties":{"tags":{"type":"array","items":"string"}}}]`,
		})
		if err == nil {
			t.Fatal("a scalar items declaration must be rejected through the legacy form too")
		}
		msg := err.Error()
		for _, want := range []string{`("Contract")`, `property "tags".items`, "must be an object"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("expected error to contain %q, got %q", want, msg)
			}
		}
	})

	t.Run("relationship type scalar items is rejected too", func(t *testing.T) {
		_, err := svc.executeCreateSchema(ctx, validationTestProjectID, map[string]any{
			"name": "rel-scalar-items",
			"object_type_schemas": objectTypes(map[string]any{
				"title": map[string]any{"type": "string"},
			}),
			"relationship_type_schemas": map[string]any{
				"MEMBER_OF": map[string]any{
					"sourceType": "Contract",
					"targetType": "Contract",
					"properties": map[string]any{
						"roles": map[string]any{"type": "array", "items": "string"},
					},
				},
			},
		})
		if err == nil {
			t.Fatal("a relationship property with scalar items must be rejected")
		}
		msg := err.Error()
		for _, want := range []string{"relationshipTypeSchemas[0]", `property "roles".items`} {
			if !strings.Contains(msg, want) {
				t.Fatalf("expected error to contain %q, got %q", want, msg)
			}
		}
	})

	t.Run("the rest of the guard runs too, not just items", func(t *testing.T) {
		_, err := svc.executeCreateSchema(ctx, validationTestProjectID, map[string]any{
			"name": "bad-enum",
			"object_type_schemas": objectTypes(map[string]any{
				"status": map[string]any{"type": "string", "enum": []any{}},
			}),
		})
		if err == nil {
			t.Fatal("an empty enum must be rejected by the shared guard")
		}
		if msg := err.Error(); !strings.Contains(msg, `property "status".enum must be a non-empty list`) {
			t.Fatalf("expected the enum guard to run, got %q", msg)
		}
	})

	t.Run("a missing type name is refused instead of silently dropped", func(t *testing.T) {
		_, err := svc.executeCreateSchema(ctx, validationTestProjectID, map[string]any{
			"name":         "nameless",
			"object_types": `[{"properties":{"tags":{"type":"array","items":{"type":"string"}}}}]`,
		})
		if err == nil {
			t.Fatal("an entry with no type name must be refused, not silently dropped")
		}
		if msg := err.Error(); !strings.Contains(msg, "missing required field 'name'") {
			t.Fatalf("expected a missing-name error, got %q", msg)
		}
	})

	t.Run("empty object types are refused, matching CreatePack", func(t *testing.T) {
		_, err := svc.executeCreateSchema(ctx, validationTestProjectID, map[string]any{
			"name":                "empty",
			"object_type_schemas": map[string]any{},
		})
		if err == nil {
			t.Fatal("a pack with no object types must be refused")
		}
		if msg := err.Error(); !strings.Contains(msg, "objectTypeSchemas array is empty") {
			t.Fatalf("expected the empty-array guard, got %q", msg)
		}
	})
}
