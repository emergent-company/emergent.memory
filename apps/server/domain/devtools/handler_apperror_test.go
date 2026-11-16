package devtools

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// newTestHandler builds a handler whose coverage directory is an empty temp dir,
// so the asset routes answer without touching the checkout.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	h := NewHandler(slog.Default(), &config.Config{})
	h.coverageDir = t.TempDir()
	h.docsDir = t.TempDir()
	return h
}

// callRoute invokes handler with requestPath bound to the route's "*" param.
func callRoute(t *testing.T, handler echo.HandlerFunc, method, requestPath string) error {
	t.Helper()
	c := echo.New().NewContext(
		httptest.NewRequest(method, "/coverage/"+requestPath, nil),
		httptest.NewRecorder(),
	)
	c.SetParamNames("*")
	c.SetParamValues(requestPath)
	return handler(c)
}

// TestCoverageAssetErrorsAreAppErrors pins the apperror shape of the devtools
// asset routes (#843): a traversal attempt is a coded 403 and a missing asset a
// coded 404, instead of the bare echo.HTTPError string body these returned
// before the conversion.
func TestCoverageAssetErrorsAreAppErrors(t *testing.T) {
	h := newTestHandler(t)

	t.Run("traversal is forbidden", func(t *testing.T) {
		err := callRoute(t, h.ServeCoverageFiles, http.MethodGet, "../../etc/passwd")
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T", err)
		require.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
		require.Equal(t, "forbidden", appErr.Code)
	})

	t.Run("missing asset is not found", func(t *testing.T) {
		err := callRoute(t, h.ServeCoverageFiles, http.MethodGet, "missing.css")
		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T", err)
		require.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
		require.Equal(t, "not_found", appErr.Code)
	})
}

// TestServeDocsMissingAssetIsAppError covers the sibling route: ServeDocs shares
// the same "File not found" answer and must not fall back to echo.HTTPError.
func TestServeDocsMissingAssetIsAppError(t *testing.T) {
	h := newTestHandler(t)

	err := callRoute(t, h.ServeDocs, http.MethodGet, "swagger.json")
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T", err)
	require.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	require.Equal(t, "not_found", appErr.Code)
}
