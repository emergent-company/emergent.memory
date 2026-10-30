package extraction

import (
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestParsingBulkRetryRouteIsSibling pins the public path the gateway calls.
// Parsing jobs are registered under /api/admin/parsing-jobs, not nested under
// /api/admin/extraction-jobs; a nesting mistake makes "Retry all conversions"
// a 404 in production while every unit test still builds.
func TestParsingBulkRetryRouteIsSibling(t *testing.T) {
	e := echo.New()
	RegisterAdminRoutes(e, &AdminHandler{}, &auth.Middleware{})

	const want = "/api/admin/parsing-jobs/projects/:projectId/bulk-retry"
	for _, r := range e.Routes() {
		if r.Method == http.MethodPost && r.Path == want {
			return
		}
	}
	t.Fatalf("route %s not registered", want)
}
