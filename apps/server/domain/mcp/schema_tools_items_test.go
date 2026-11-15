package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// The schema-create tool writes kb.graph_schemas and the registry rows with its
// own INSERTs, so it never reached CreatePack's validateSchemaDefinitions guard:
// a bare scalar `items` (#726) was accepted here and then enforced nothing
// (#811). The validation runs before the transaction, so a nil db proves a
// rejected definition never reaches a write.
func TestExecuteCreateSchemaRejectsScalarItems(t *testing.T) {
	svc := &Service{log: slog.Default()}

	_, err := svc.executeCreateSchema(context.Background(), uuid.NewString(), map[string]any{
		"name":    "ui-patterns",
		"version": "1.0.0",
		"object_type_schemas": map[string]any{
			"UIPattern": map[string]any{
				"properties": map[string]any{
					"contract_aria": map[string]any{"type": "array", "items": "string"},
				},
			},
		},
	})

	require.Error(t, err, "schema-create must reject a bare scalar items declaration")
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr), "expected a 400-shaped apperror, got %T", err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	for _, want := range []string{
		`objectTypeSchemas[0] ("UIPattern")`,
		`property "contract_aria".items`,
		"must be an object",
		`"string"`,
	} {
		assert.Contains(t, appErr.Message, want)
	}
}

// A relationship property is validated by the same shared guard, including the
// plural endpoint spelling the docs use for MCP map-form relationship types.
func TestExecuteCreateSchemaRejectsScalarItemsOnRelationship(t *testing.T) {
	svc := &Service{log: slog.Default()}

	_, err := svc.executeCreateSchema(context.Background(), uuid.NewString(), map[string]any{
		"name":    "research",
		"version": "1.0.0",
		"object_type_schemas": map[string]any{
			"Person": map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}},
		},
		"relationship_type_schemas": map[string]any{
			"authored_by": map[string]any{
				"sourceTypes": []any{"Person"},
				"targetTypes": []any{"Person"},
				"properties": map[string]any{
					"roles": map[string]any{"type": "array", "items": "string"},
				},
			},
		},
	})

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), `property "roles".items`), "unexpected error: %v", err)
}

// CreatePack and the REST handler in front of it reject an empty definition set;
// the MCP path had no equivalent check and installed an empty pack (#811).
func TestExecuteCreateSchemaRejectsEmptyObjectTypes(t *testing.T) {
	svc := &Service{log: slog.Default()}

	_, err := svc.executeCreateSchema(context.Background(), uuid.NewString(), map[string]any{
		"name":                "empty",
		"version":             "1.0.0",
		"object_type_schemas": map[string]any{},
	})

	require.Error(t, err)
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr), "expected a 400-shaped apperror, got %T", err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "object_type_schemas is required")
}
