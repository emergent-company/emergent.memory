package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v4"
)

func TestNormalizePathTemplate(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "uuid segment",
			path: "/api/chat/1d47dbf5-c4fd-4d28-8e3a-f9fa122d56bb/history",
			want: "/api/chat/{id}/history",
		},
		{
			name: "long hex segment",
			path: "/api/objects/0123456789abcdef0123",
			want: "/api/objects/{id}",
		},
		{
			name: "numeric segment",
			path: "/api/backups/42/download",
			want: "/api/backups/{id}/download",
		},
		{
			name: "mixed segments",
			path: "/api/chat/1d47dbf5-c4fd-4d28-8e3a-f9fa122d56bb/messages/7",
			want: "/api/chat/{id}/messages/{id}",
		},
		{
			name: "version segment with a letter is kept",
			path: "/api/v2",
			want: "/api/v2",
		},
		{
			name: "version segment path with a letter is kept",
			path: "/api/v2/objects",
			want: "/api/v2/objects",
		},
		{
			name: "all-digit segment is collapsed",
			path: "/api/2",
			want: "/api/{id}",
		},
		{
			name: "already templated",
			path: "/api/chat/{id}/history",
			want: "/api/chat/{id}/history",
		},
		{
			name: "plain path",
			path: "/api/health",
			want: "/api/health",
		},
		{
			name: "empty path",
			path: "",
			want: "",
		},
		{
			name: "root path",
			path: "/",
			want: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePathTemplate(tt.path); got != tt.want {
				t.Fatalf("normalizePathTemplate(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMemoryErrorDisposition(t *testing.T) {
	tests := []struct {
		status      int
		wantCapture bool
		wantLevel   sentry.Level
	}{
		{status: 0, wantCapture: true, wantLevel: sentry.LevelError},
		{status: http.StatusBadRequest, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusNotFound, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusConflict, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusUnprocessableEntity, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusTooManyRequests, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusUnauthorized, wantCapture: true, wantLevel: sentry.LevelWarning},
		{status: http.StatusForbidden, wantCapture: true, wantLevel: sentry.LevelWarning},
		{status: http.StatusInternalServerError, wantCapture: true, wantLevel: sentry.LevelError},
		{status: http.StatusBadGateway, wantCapture: false, wantLevel: sentry.LevelInfo},
		{status: http.StatusServiceUnavailable, wantCapture: true, wantLevel: sentry.LevelError},
		{status: http.StatusGatewayTimeout, wantCapture: true, wantLevel: sentry.LevelError},
	}

	for _, tt := range tests {
		name := http.StatusText(tt.status)
		if name == "" {
			name = "transport error"
		}
		t.Run(name, func(t *testing.T) {
			capture, level := memoryErrorDisposition(tt.status)
			if capture != tt.wantCapture {
				t.Fatalf("memoryErrorDisposition(%d) capture = %v, want %v", tt.status, capture, tt.wantCapture)
			}
			if level != tt.wantLevel {
				t.Fatalf("memoryErrorDisposition(%d) level = %v, want %v", tt.status, level, tt.wantLevel)
			}
		})
	}
}

// TestCaptureHelpersNoClientWithNoDSN is a smoke test: with no Sentry client
// configured both helpers must be safe no-ops (and never panic).
func TestCaptureHelpersNoClientWithNoDSN(t *testing.T) {
	sentry.CurrentHub().BindClient(nil)
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	captureError(errors.New("boom"))
	captureMemoryError(http.MethodGet, "/api/chat/123/history", http.StatusInternalServerError, errors.New("boom"))
	capturePollFailure(errors.New("boom"))
}

// TestShouldCaptureMemoryError asserts the reporting policy: a foreground 401
// is still captured, but the conversation poller's 401 is expected session
// expiry (the poller closes the stream instead) and must not flood Sentry.
// Other statuses keep their normal disposition in the background too.
func TestShouldCaptureMemoryError(t *testing.T) {
	bg := withBackgroundPoll(context.Background())

	tests := []struct {
		name   string
		ctx    context.Context
		status int
		want   bool
	}{
		{name: "foreground 401", ctx: context.Background(), status: http.StatusUnauthorized, want: true},
		{name: "background 401", ctx: bg, status: http.StatusUnauthorized, want: false},
		{name: "background 403", ctx: bg, status: http.StatusForbidden, want: true},
		{name: "background 502", ctx: bg, status: http.StatusBadGateway, want: false},
		{name: "background 5xx", ctx: bg, status: http.StatusServiceUnavailable, want: true},
		{name: "background 4xx", ctx: bg, status: http.StatusBadRequest, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCaptureMemoryError(tt.ctx, tt.status); got != tt.want {
				t.Fatalf("shouldCaptureMemoryError(%s, %d) = %v, want %v", tt.name, tt.status, got, tt.want)
			}
		})
	}
}

// recordingTransport captures Sentry events in-process so policy tests do not
// need a network or a DSN-backed project.
type recordingTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *recordingTransport) Flush(time.Duration) bool              { return true }
func (t *recordingTransport) FlushWithContext(context.Context) bool { return true }
func (t *recordingTransport) Configure(sentry.ClientOptions)        {}
func (t *recordingTransport) Close()                                {}
func (t *recordingTransport) SendEvent(e *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, e)
}
func (t *recordingTransport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.events)
}

// bindRecordingSentry binds an in-process Sentry client to the global hub for
// the duration of the test and returns its transport.
func bindRecordingSentry(t *testing.T) *recordingTransport {
	t.Helper()
	transport := &recordingTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://public@example.com/1",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("sentry.NewClient: %v", err)
	}
	prev := sentry.CurrentHub().Client()
	sentry.CurrentHub().BindClient(client)
	t.Cleanup(func() { sentry.CurrentHub().BindClient(prev) })
	return transport
}

// TestSentryErrorMiddlewareSkipsBadGateway guards the 502 policy: a 502 is an
// upstream restart/upgrade window (memory server or MCP registry), so it is
// returned to the client and left to the request log rather than captured. A
// real 500 is still captured.
func TestSentryErrorMiddlewareSkipsBadGateway(t *testing.T) {
	transport := bindRecordingSentry(t)

	call := func(status int) {
		e := echo.New()
		c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
		h := sentryErrorMiddleware()(func(echo.Context) error {
			return echo.NewHTTPError(status, "upstream")
		})
		if err := h(c); err == nil {
			t.Fatalf("status %d: middleware swallowed the handler error", status)
		}
	}

	call(http.StatusBadGateway)
	if got := transport.count(); got != 0 {
		t.Fatalf("502 must not be captured, got %d events", got)
	}
	call(http.StatusInternalServerError)
	if got := transport.count(); got != 1 {
		t.Fatalf("500 must be captured, got %d events", got)
	}
}

// TestCapturePollFailureSkipsBadGateway guards the same 502 policy for the
// sustained-poll reporter: an upgrade must not open an issue, while a real
// sustained upstream failure is still reported.
func TestCapturePollFailureSkipsBadGateway(t *testing.T) {
	transport := bindRecordingSentry(t)

	capturePollFailure(&memoryHTTPError{Status: http.StatusBadGateway, Message: "upstream"})
	if got := transport.count(); got != 0 {
		t.Fatalf("502 must not be captured, got %d events", got)
	}
	capturePollFailure(&memoryHTTPError{Status: http.StatusServiceUnavailable, Message: "upstream"})
	if got := transport.count(); got != 1 {
		t.Fatalf("503 must be captured, got %d events", got)
	}

	// A joined error that includes a real failure is still reported.
	capturePollFailure(errors.Join(
		&memoryHTTPError{Status: http.StatusBadGateway, Message: "upstream"},
		&memoryHTTPError{Status: http.StatusServiceUnavailable, Message: "upstream"},
	))
	if got := transport.count(); got != 2 {
		t.Fatalf("joined 502+503 must be captured, got %d events", got)
	}
	// A pure-502 joined error is still just an upgrade window.
	capturePollFailure(errors.Join(
		&memoryHTTPError{Status: http.StatusBadGateway, Message: "upstream"},
		&memoryHTTPError{Status: http.StatusBadGateway, Message: "upstream"},
	))
	if got := transport.count(); got != 2 {
		t.Fatalf("joined 502+502 must not be captured, got %d events", got)
	}
}

// TestCaptureErrorSkipsBadGateway guards the global policy: captureError drops
// a pure upstream 502, but still reports 503, transport failures, and a joined
// error that contains a non-502 failure.
func TestCaptureErrorSkipsBadGateway(t *testing.T) {
	transport := bindRecordingSentry(t)

	captureError(&memoryHTTPError{Status: http.StatusBadGateway, Message: "upstream"})
	if got := transport.count(); got != 0 {
		t.Fatalf("502 must not be captured, got %d events", got)
	}
	captureError(&memoryHTTPError{Status: http.StatusServiceUnavailable, Message: "upstream"})
	if got := transport.count(); got != 1 {
		t.Fatalf("503 must be captured, got %d events", got)
	}
	captureError(errors.New("dial tcp: connection refused"))
	if got := transport.count(); got != 2 {
		t.Fatalf("transport error must be captured, got %d events", got)
	}
	captureError(errors.Join(
		&memoryHTTPError{Status: http.StatusBadGateway, Message: "upstream"},
		&memoryHTTPError{Status: http.StatusServiceUnavailable, Message: "upstream"},
	))
	if got := transport.count(); got != 3 {
		t.Fatalf("joined 502+503 must be captured, got %d events", got)
	}
}
