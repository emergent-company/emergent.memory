package mcp

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteCreateSchemaRejectsMalformedScopeKey pins the write-path fix for
// issue #1177: the schema-create MCP tool writes kb.project_object_schema_registry
// json_schema directly and does not go through CreatePack/UpdatePack, so it must
// validate the optional scopeKey declaration itself and reject a malformed one
// fail-closed instead of persisting it.
func TestExecuteCreateSchemaRejectsMalformedScopeKey(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db, log: slog.Default()}

	t.Run("unknown scope property is rejected", func(t *testing.T) {
		_, err := svc.executeCreateSchema(context.Background(), projectID, map[string]any{
			"name":    "law-bad",
			"version": "1.0.0",
			"object_type_schemas": map[string]any{
				"LegalParagraphBad": map[string]any{
					"properties": map[string]any{
						"chapter_id": map[string]any{"type": "string"},
					},
					"scopeKey": map[string]any{"property": "law_ref_id"},
				},
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `scopeKey.property "law_ref_id" is not a declared property`)

		// Fail-closed: the malformed declaration must not have been persisted.
		var n int
		require.NoError(t, db.NewSelect().
			Table("kb.project_object_schema_registry").
			Where("project_id = ?", projectID).
			Where("type_name = ?", "LegalParagraphBad").
			ColumnExpr("count(*)").
			Scan(context.Background(), &n))
		assert.Zero(t, n, "rejected schema-create must not insert a registry row")
	})

	t.Run("scope key naming the reference property is accepted", func(t *testing.T) {
		_, err := svc.executeCreateSchema(context.Background(), projectID, map[string]any{
			"name":    "law-good",
			"version": "1.0.0",
			"object_type_schemas": map[string]any{
				"LegalParagraphGood": map[string]any{
					"properties": map[string]any{
						"law_ref_id": map[string]any{"type": "string"},
						"chapter_id": map[string]any{"type": "string"},
					},
					"scopeKey": map[string]any{"property": "law_ref_id"},
				},
			},
		})
		require.NoError(t, err)

		var n int
		require.NoError(t, db.NewSelect().
			Table("kb.project_object_schema_registry").
			Where("project_id = ?", projectID).
			Where("type_name = ?", "LegalParagraphGood").
			ColumnExpr("count(*)").
			Scan(context.Background(), &n))
		assert.Equal(t, 1, n, "a valid declaration must persist")
	})
}
