package schemas_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestPreviewMergeHandlerObjectTypes drives the real handler through Echo's
// error handler. The strict objectTypes parse happens before any database
// access, so a nil DB is enough: malformed payloads must be a 400, never the
// old fail-open 200 "no conflicts", and accepted snake_case aliases must still
// bind.
func TestPreviewMergeHandlerObjectTypes(t *testing.T) {
	log := slog.Default()
	repo := schemas.NewRepository(nil, log)
	svc := schemas.NewService(repo, nil, log, &config.Config{})
	h := schemas.NewHandler(svc)

	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	e.POST("/projects/:projectId/merge-preview", h.PreviewMerge)

	tests := []struct {
		name         string
		body         string
		wantStatus   int
		wantContains []string
		wantMessage  string
	}{
		{
			name:         "camelCase malformed value",
			body:         `{"packs":[{"name":"Broken","objectTypes":"hello"}]}`,
			wantStatus:   http.StatusBadRequest,
			wantContains: []string{"objectTypes", "JSON array", "JSON object", `{"name":"Person"`},
		},
		{
			name:         "object_type_schemas malformed value",
			body:         `{"packs":[{"name":"Broken","object_type_schemas":"hello"}]}`,
			wantStatus:   http.StatusBadRequest,
			wantContains: []string{"objectTypes", "JSON array", "JSON object", `{"name":"Person"`},
		},
		{
			name:         "object_types malformed value",
			body:         `{"packs":[{"name":"Broken","object_types":"hello"}]}`,
			wantStatus:   http.StatusBadRequest,
			wantContains: []string{"objectTypes", "JSON array", "JSON object", `{"name":"Person"`},
		},
		{
			name:        "unknown pack key is rejected",
			body:        `{"packs":[{"name":"Broken","objectTypesTypo":[{"name":"Typo"}]}]}`,
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid request body",
		},
		{
			name:       "object_type_schemas empty array is accepted",
			body:       `{"packs":[{"name":"Empty","object_type_schemas":[]}]}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "object_types empty object is accepted",
			body:       `{"packs":[{"name":"Empty","object_types":{}}]}`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-0000-0000-000000000000/merge-preview", strings.NewReader(tc.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code, "body: %s", rec.Body.String())

			if tc.wantStatus == http.StatusOK {
				var result schemas.MergePreviewResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Empty(t, result.Conflicts)
				assert.False(t, result.RequiresMerge)
				return
			}

			var resp struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, "bad_request", resp.Error.Code)
			if tc.wantMessage != "" {
				assert.Equal(t, tc.wantMessage, resp.Error.Message)
			}
			for _, want := range tc.wantContains {
				assert.Contains(t, resp.Error.Message, want)
			}
			for _, unwanted := range []string{"Go value", "struct {", "cannot unmarshal"} {
				assert.NotContains(t, resp.Error.Message, unwanted)
			}
		})
	}
}
