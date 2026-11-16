package tracing_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/tracing"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestDisabledBackendReturnsAppError pins the wire shape of the "tracing not
// enabled" answer (#843). Before the conversion it was an echo.HTTPError carrying
// only a fixed string: the shared handler derives a code from the status, and it
// has no case for 503, so the client saw `internal_error` — the same code a Tempo
// outage produced. The route now states its own code, and the ratchet in
// scripts/lint-ratchet.sh forbids a non-test handler from constructing an
// echo.HTTPError again.
func TestDisabledBackendReturnsAppError(t *testing.T) {
	h := tracing.NewHandler(&config.Config{}, nil)

	c := echo.New().NewContext(
		httptest.NewRequest(http.MethodGet, "/api/traces", nil),
		httptest.NewRecorder(),
	)

	for name, handler := range map[string]echo.HandlerFunc{
		"search":    h.Search,
		"get trace": h.GetTrace,
	} {
		t.Run(name, func(t *testing.T) {
			err := handler(c)
			require.Error(t, err)

			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr,
				"a disabled tracing backend must answer with *apperror.Error, got %T", err)
			require.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
			// A disabled backend has its own code so a client can tell it from a
			// Tempo outage, which is also a 503.
			require.Equal(t, "tracing_disabled", appErr.Code)
		})
	}
}
