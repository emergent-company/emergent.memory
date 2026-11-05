package schemas_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestPreviewMergeHandlerValidation verifies the handler rejects a missing
// projectId and an empty packs list before reaching the service layer.
func TestPreviewMergeHandlerValidation(t *testing.T) {
	newContext := func(t *testing.T, projectID, body string) echo.Context {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(body)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("projectId")
		c.SetParamValues(projectID)
		return c
	}

	h := schemas.NewHandler(nil) // svc never reached for validation failures

	t.Run("missing projectId", func(t *testing.T) {
		err := h.PreviewMerge(newContext(t, "", `{"packs":[{"name":"p"}]}`))
		require.Error(t, err)
		var aerr *apperror.Error
		require.ErrorAs(t, err, &aerr)
		assert.Equal(t, http.StatusBadRequest, aerr.HTTPStatus)
		assert.Equal(t, "projectId is required", aerr.Message)
	})

	t.Run("empty packs", func(t *testing.T) {
		err := h.PreviewMerge(newContext(t, "00000000-0000-0000-0000-000000000000", `{"packs":[]}`))
		require.Error(t, err)
		var aerr *apperror.Error
		require.ErrorAs(t, err, &aerr)
		assert.Equal(t, http.StatusBadRequest, aerr.HTTPStatus)
		assert.Equal(t, "at least one pack is required", aerr.Message)
	})

	t.Run("invalid body", func(t *testing.T) {
		err := h.PreviewMerge(newContext(t, "00000000-0000-0000-0000-000000000000", `not-json`))
		require.Error(t, err)
		var aerr *apperror.Error
		require.ErrorAs(t, err, &aerr)
		assert.Equal(t, http.StatusBadRequest, aerr.HTTPStatus)
		assert.Equal(t, "invalid request body", aerr.Message)
	})
}
