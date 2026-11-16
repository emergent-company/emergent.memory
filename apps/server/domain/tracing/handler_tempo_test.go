package tracing

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
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
// non-2xx Tempo response (#843). Statuses split by who can act on them: a
// rejected query (400/409/422) keeps Tempo's status and a capped copy of its
// diagnostic, an unknown trace (404) is this API's not_found, and anything the
// caller cannot act on (401/403/408/429/5xx) becomes this API's own 503 with
// Tempo's body attached as the internal cause instead of being relayed.
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

	// Statuses the caller cannot act on must not be relayed as if they were this
	// API's verdict on the caller's credential or query.
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusGatewayTimeout,
	} {
		t.Run(fmt.Sprintf("%d becomes service_unavailable", status), func(t *testing.T) {
			err := tempoStatusError(status, []byte("upstream said no"))

			require.Equal(t, http.StatusServiceUnavailable, err.HTTPStatus)
			require.Equal(t, "service_unavailable", err.Code)
			require.NotContains(t, err.Message, "upstream said no")
			require.Error(t, err.Internal)
		})
	}

	t.Run("missing upstream resource is not_found", func(t *testing.T) {
		err := tempoStatusError(http.StatusNotFound, []byte("not here"))

		require.Equal(t, http.StatusNotFound, err.HTTPStatus)
		require.Equal(t, "not_found", err.Code)
		require.NoError(t, err.Internal, "a 404 carries no internal cause to log")
	})

	// Rejected queries keep their status, and the cap must not be applied to
	// bytes: a body of multi-byte runes has to stay valid UTF-8 after trimming.
	t.Run("4xx does not split a multi-byte rune when capping", func(t *testing.T) {
		err := tempoStatusError(http.StatusUnprocessableEntity, []byte(strings.Repeat("é", maxErrorBody+16)))

		require.Equal(t, http.StatusUnprocessableEntity, err.HTTPStatus)
		require.True(t, utf8.ValidString(err.Message), "capping must not cut a UTF-8 sequence")
		require.Len(t, []rune(err.Message), maxErrorBody+1)
	})

	// The caller reads a bounded byte prefix, so truncateErrorBody can be handed
	// a body whose last rune was cut in half. The partial rune must be dropped,
	// not rendered as U+FFFD.
	t.Run("a body cut mid-rune stays valid UTF-8", func(t *testing.T) {
		full := []byte(strings.Repeat("é", 8))
		cut := full[:len(full)-1] // "é" is 2 bytes: drop one, leave a stray lead byte

		err := tempoStatusError(http.StatusBadRequest, cut)

		require.True(t, utf8.ValidString(err.Message), "a split rune must not survive as an invalid byte")
		require.NotContains(t, err.Message, string(utf8.RuneError))
		require.Equal(t, strings.Repeat("é", 7), err.Message)
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

// TestProxyMapsNon2xxBackendAnswer covers the rest of the mapping on the search
// route: every non-2xx answer is buffered and re-issued as this API's own code,
// so a Tempo credential failure can never surface as if the caller's token were
// rejected, and a Tempo 404 is not a pass-through body.
func TestProxyMapsNon2xxBackendAnswer(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantStatus int
		wantCode   string
	}{
		{"unauthorized is a backend fault", http.StatusUnauthorized, http.StatusServiceUnavailable, "service_unavailable"},
		{"forbidden is a backend fault", http.StatusForbidden, http.StatusServiceUnavailable, "service_unavailable"},
		{"timeout is retryable", http.StatusRequestTimeout, http.StatusServiceUnavailable, "service_unavailable"},
		{"rate limited is retryable", http.StatusTooManyRequests, http.StatusServiceUnavailable, "service_unavailable"},
		{"bad query keeps its status", http.StatusBadRequest, http.StatusBadRequest, "tempo_query_error"},
		{"unprocessable query keeps its status", http.StatusUnprocessableEntity, http.StatusUnprocessableEntity, "tempo_query_error"},
		{"missing resource is not_found", http.StatusNotFound, http.StatusNotFound, "not_found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"error":"parse error at line 1"}`)
			}))
			defer fake.Close()

			h := &Handler{tempoBaseURL: fake.URL, client: fake.Client()}

			err := h.proxy(newTempoTestContext(t, "/api/traces/search"), "/api/search", nil)

			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr, "a Tempo %d must be an apperror, got %T", tc.status, err)
			require.Equal(t, tc.wantStatus, appErr.HTTPStatus)
			require.Equal(t, tc.wantCode, appErr.Code)
		})
	}
}

// newTempoTraceContext binds a trace id and a project-scoped user so GetTrace
// reaches the Tempo call without a database-backed superadmin lookup.
func newTempoTraceContext(t *testing.T, traceID string) echo.Context {
	t.Helper()
	c := newTempoTestContext(t, "/api/traces/"+traceID)
	c.SetParamNames("id")
	c.SetParamValues(traceID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ProjectID: "proj-1"})
	return c
}

// TestGetTraceMapsUpstreamErrors covers GetTrace's error path: an unknown trace
// is reported as this API's own not_found (carrying the id the caller asked
// for), a Tempo credential rejection is a backend fault, and an oversized
// upstream error body is capped rather than relayed.
func TestGetTraceMapsUpstreamErrors(t *testing.T) {
	t.Run("unknown trace is not_found", func(t *testing.T) {
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "trace not found")
		}))
		defer fake.Close()

		h := &Handler{tempoBaseURL: fake.URL, client: fake.Client()}

		err := h.GetTrace(newTempoTraceContext(t, "abc123"))

		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr, "got %T", err)
		require.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
		require.Equal(t, "not_found", appErr.Code)
		require.Contains(t, appErr.Message, "abc123")
	})

	t.Run("tempo credential rejection is service_unavailable", func(t *testing.T) {
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "tenant token rejected")
		}))
		defer fake.Close()

		h := &Handler{tempoBaseURL: fake.URL, client: fake.Client()}

		err := h.GetTrace(newTempoTraceContext(t, "abc123"))

		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr, "got %T", err)
		require.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
		require.Equal(t, "service_unavailable", appErr.Code)
		require.NotContains(t, appErr.Message, "tenant", "upstream detail must stay in the cause")
	})

	t.Run("oversized error body is capped", func(t *testing.T) {
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, strings.Repeat("x", 4*maxErrorBody*2))
		}))
		defer fake.Close()

		h := &Handler{tempoBaseURL: fake.URL, client: fake.Client()}

		err := h.GetTrace(newTempoTraceContext(t, "abc123"))

		var appErr *apperror.Error
		require.ErrorAs(t, err, &appErr, "got %T", err)
		require.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
		require.Error(t, appErr.Internal)
		require.Len(t, []rune(appErr.Internal.Error()), maxErrorBody+1, "the cause is capped too")
	})
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
