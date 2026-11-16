package tracing

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// newTempoTestContext builds an Echo context for a trace route without a
// server: these tests exercise the Tempo mapping, which runs before auth.
func newTempoTestContext(t *testing.T, target string) echo.Context {
	t.Helper()
	return echo.New().NewContext(
		httptest.NewRequest(http.MethodGet, target, nil),
		httptest.NewRecorder(),
	)
}

// TestTempoStatusErrorMessage covers the mapping the trace routes apply to a
// non-200 Tempo response (#843): a 4xx keeps Tempo's status and message (it is
// the caller's own query error, capped so a proxy hiccup cannot inflate the
// envelope), while a 5xx becomes this API's own 503 with Tempo's body attached
// as the internal cause instead of relayed to the client.
func TestTempoStatusErrorMessage(t *testing.T) {
	t.Run("4xx keeps status and message", func(t *testing.T) {
		err := tempoStatusError(http.StatusBadRequest, []byte(`{"error":"parse error at line 1"}`))

		require.Equal(t, http.StatusBadRequest, err.HTTPStatus)
		require.Equal(t, "tempo_query_error", err.Code)
		require.JSONEq(t, `{"error":"parse error at line 1"}`, err.Message)
		require.NoError(t, err.Internal, "a caller's own query error needs no internal cause")
	})

	t.Run("4xx body is capped", func(t *testing.T) {
		err := tempoStatusError(http.StatusBadRequest, []byte(strings.Repeat("x", 4*maxErrorBody)))

		require.Equal(t, http.StatusBadRequest, err.HTTPStatus)
		require.Len(t, []rune(err.Message), maxErrorBody+1, "512 runes plus the ellipsis")
		require.True(t, strings.HasSuffix(err.Message, "…"))
	})

	t.Run("5xx becomes service_unavailable with the body as cause", func(t *testing.T) {
		body := "internal: shard kb-tempo-7 unavailable"
		err := tempoStatusError(http.StatusInternalServerError, []byte(body))

		require.Equal(t, http.StatusServiceUnavailable, err.HTTPStatus)
		require.Equal(t, "service_unavailable", err.Code)
		require.NotContains(t, err.Message, "shard", "the trace store's internals must not reach the client")
		require.Error(t, err.Internal)
		require.Equal(t, body, err.Internal.Error())
	})
}

// TestProxyMapsBackendFaultToAppError proves the streaming search route applies
// that mapping too, instead of relaying Tempo's 500 status and body.
func TestProxyMapsBackendFaultToAppError(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal: shard kb-tempo-7 unavailable")
	}))
	defer fake.Close()

	h := &Handler{tempoBaseURL: fake.URL, client: fake.Client()}

	err := h.proxy(newTempoTestContext(t, "/api/traces/search"), "/api/search", nil)

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "a Tempo 5xx must be an apperror, got %T", err)
	require.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
	require.Equal(t, "service_unavailable", appErr.Code)
	require.NotContains(t, appErr.Message, "shard")
	require.NotNil(t, appErr.Internal)
}

// TestTempoGetTransportFailureIsServiceUnavailable covers the unreachable case:
// the transport error names the Tempo URL and DNS/TLS state, so it must travel
// as the internal cause, not as the client-facing message.
func TestTempoGetTransportFailureIsServiceUnavailable(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	h := &Handler{tempoBaseURL: deadURL, client: http.DefaultClient}

	_, err := h.tempoGet(newTempoTestContext(t, "/api/traces/abc"), "/api/traces/abc")

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "an unreachable Tempo must be an apperror, got %T", err)
	require.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
	require.Equal(t, "service_unavailable", appErr.Code)
	require.NotContains(t, appErr.Message, "127.0.0.1", "the transport cause must not reach the client")
	require.Error(t, appErr.Internal)
}
