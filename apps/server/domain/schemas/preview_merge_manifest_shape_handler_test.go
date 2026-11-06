package schemas_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestPreviewMergeHandlerManifestPackShape drives the real handler through Echo
// against a mocked registry. A pack shaped like a blueprint manifest must
// preview (200) and compute conflicts from its object types rather than failing
// on the manifest metadata fields (#167). The preview consumes object types
// only; the rest of the manifest pack shape is tolerated.
//
// sqlmock keeps this a real HTTP test without requiring a live database: the
// handler still decodes the body, the service still calls the repository, and
// the repository still parses the incoming object types and runs the conflict
// query.
func TestPreviewMergeHandlerManifestPackShape(t *testing.T) {
	sqldb, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { sqldb.Close() })
	db := bun.NewDB(sqldb, pgdialect.New())
	t.Cleanup(func() { db.Close() })

	const projectID = "00000000-0000-0000-0000-000000000167"

	log := slog.Default()
	repo := schemas.NewRepository(db, log)
	svc := schemas.NewService(repo, nil, log, &config.Config{})
	h := schemas.NewHandler(svc)

	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	e.POST("/projects/:projectId/merge-preview", h.PreviewMerge)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "canonical manifest pack with objectTypes",
			body: `{"packs":[{
				"name":"Manifest Pack",
				"version":"1.0.0",
				"description":"forwarded manifest pack",
				"author":"someone",
				"migrations":{"from_version":"1.0.0"},
				"objectTypes":{"Person":{"properties":{"email":{"type":"string"}}}},
				"relationshipTypes":[{"name":"knows","sourceType":"Person","targetType":"Person"}],
				"relationshipTypeSchemas":[{"name":"knows","sourceTypes":["Person"],"targetTypes":["Person"]}]
			}]}`,
		},
		{
			name: "manifest alias objectTypeSchemas",
			body: `{"packs":[{
				"name":"Manifest Alias Pack",
				"version":"1.0.0",
				"objectTypeSchemas":[{"name":"Person","properties":{"email":{"type":"string"}}}]
			}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock.ExpectQuery(`SELECT por\.json_schema`).
				WillReturnRows(sqlmock.NewRows([]string{"json_schema", "schema_id", "schema_name", "schema_version"}).
					AddRow(`{"properties":{"name":{"type":"string"}}}`, "00000000-0000-0000-0000-0000000000aa", "Existing Pack", "1.0.0"))

			req := httptest.NewRequest(http.MethodPost, "/projects/"+projectID+"/merge-preview", strings.NewReader(tc.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

			var result schemas.MergePreviewResult
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			require.Len(t, result.Conflicts, 1)
			assert.True(t, result.RequiresMerge)
			assert.Equal(t, "Person", result.Conflicts[0].TypeName)
			assert.Contains(t, result.Conflicts[0].AddedProperties, "email")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
