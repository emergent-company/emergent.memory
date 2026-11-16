package sources

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestSourceWebhookErrorEnvelope pins the #863 decision: the webhook ingress
// answers with the shared apperror envelope, not a plain-text body, so a sender
// can branch on `error.code`. A regression to c.String fails here and in the
// `plain-text error body` ratchet.
//
// The nil dispatcher is deliberate: a webhook key that is not a UUID is
// rejected before the dispatcher is consulted, which is the only path this unit
// test can exercise without a database. The dispatcher-backed statuses
// (invalid_signature, source_app_not_found) are asserted in the DB-backed
// dispatcher test.
func TestSourceWebhookErrorEnvelope(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterSourceEventRoutes(e, NewSourceEventHandler(nil))

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/source-apps/not-a-uuid", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Header().Get(echo.HeaderContentType), echo.MIMEApplicationJSON)
	require.JSONEq(t, `{"error":{"code":"source_app_not_found","message":"unknown app"}}`, rec.Body.String())
}
