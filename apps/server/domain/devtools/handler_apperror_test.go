package devtools

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
// coded 404. The status was already right before the conversion, but the message
// came from echo.HTTPError and the code was inferred from the status; the
// handler now owns both, and the lint-ratchet forbids going back.
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
// the same "File not found" answer and must express it as an apperror rather than
// an echo.HTTPError.
func TestServeDocsMissingAssetIsAppError(t *testing.T) {
	h := newTestHandler(t)

	err := callRoute(t, h.ServeDocs, http.MethodGet, "swagger.json")
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T", err)
	require.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	require.Equal(t, "not_found", appErr.Code)
}

// callRouteAt is callRoute for a route whose wildcard prefix is not /coverage.
func callRouteAt(t *testing.T, handler echo.HandlerFunc, prefix, requestPath string) error {
	t.Helper()
	c := echo.New().NewContext(
		httptest.NewRequest(http.MethodGet, prefix+requestPath, nil),
		httptest.NewRecorder(),
	)
	c.SetParamNames("*")
	c.SetParamValues(requestPath)
	return handler(c)
}

// TestAssetRoutesRejectTraversal pins the directory-traversal guard on both
// asset routes. ServeDocs had no guard at all before #843's review round 2, and
// ServeCoverageFiles compared with strings.HasPrefix on the absolute path, which
// accepts any sibling directory sharing the prefix ("/srv/coverage-notes" for
// "/srv/coverage").
func TestAssetRoutesRejectTraversal(t *testing.T) {
	// Directories that share each asset directory's name as a prefix, holding a
	// file the routes must not serve.
	h := newTestHandler(t)
	coverageSibling := h.coverageDir + "-sibling"
	docsSibling := h.docsDir + "-sibling"
	for _, dir := range []string{coverageSibling, docsSibling} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644))
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}

	cases := []struct {
		name    string
		handler echo.HandlerFunc
		prefix  string
		path    string
	}{
		{"coverage dot-dot", h.ServeCoverageFiles, "/coverage/", "../../etc/passwd"},
		{"coverage sibling prefix", h.ServeCoverageFiles, "/coverage/", "../" + filepath.Base(coverageSibling) + "/secret.txt"},
		{"docs dot-dot", h.ServeDocs, "/docs/", "../../etc/passwd"},
		{"docs sibling prefix", h.ServeDocs, "/docs/", "../" + filepath.Base(docsSibling) + "/secret.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := callRouteAt(t, tc.handler, tc.prefix, tc.path)
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T", err)
			require.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
			require.Equal(t, "forbidden", appErr.Code)
		})
	}
}

// TestResolveAssetAllowsNestedFile is the positive control for the guard: a
// clean path below the asset directory still resolves.
func TestResolveAssetAllowsNestedFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets", "css"), 0o755))

	got, err := resolveAsset(dir, "assets/css/app.css")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "assets", "css", "app.css"), got)
}
