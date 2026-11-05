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

// TestPreviewMergeHandlerRejectsUnparseableObjectTypes drives the real handler
// through Echo's error handler. The strict objectTypes parse happens before any
// database access, so a nil DB is enough: the malformed payload must be a 400,
// not the old fail-open 200 "no conflicts".
func TestPreviewMergeHandlerRejectsUnparseableObjectTypes(t *testing.T) {
	log := slog.Default()
	repo := schemas.NewRepository(nil, log)
	svc := schemas.NewService(repo, nil, log, &config.Config{})
	h := schemas.NewHandler(svc)

	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	e.POST("/projects/:projectId/merge-preview", h.PreviewMerge)

	body := `{"packs":[{"name":"Broken","objectTypes":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-0000-0000-000000000000/merge-preview", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "bad_request", resp.Error.Code)
	for _, want := range []string{"objectTypes", "JSON array", "JSON object", `{"name":"Person"`} {
		assert.Contains(t, resp.Error.Message, want)
	}
	for _, unwanted := range []string{"Go value", "struct {", "cannot unmarshal"} {
		assert.NotContains(t, resp.Error.Message, unwanted)
	}
}
