package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// sseRecorder is a concurrency-safe http.ResponseWriter/Flusher for driving the
// SSE handlers from a test. sendEvent writes from Emit's subscriber goroutines,
// so a plain httptest.ResponseRecorder (which is not safe for concurrent use)
// would race with the test's reads.
type sseRecorder struct {
	mu   sync.Mutex
	buf  strings.Builder
	hdr  http.Header
	code int
}

func newSSERecorder() *sseRecorder {
	return &sseRecorder{hdr: make(http.Header), code: http.StatusOK}
}

func (r *sseRecorder) Header() http.Header { return r.hdr }

func (r *sseRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.code == http.StatusOK {
		r.code = code
	}
}

func (r *sseRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *sseRecorder) Flush() {}

func (r *sseRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func (r *sseRecorder) contains(s string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Contains(r.buf.String(), s)
}

// startAccountStream drives HandleUserStream on a request whose context is
// cancelled by the caller, waits until the "connected" event flushes (meaning
// the subscription is live), and returns the recorder plus the cancel func.
func startAccountStream(t *testing.T, svc *Service, userID string) (*sseRecorder, context.CancelFunc) {
	t.Helper()

	h := NewHandler(svc, newTestLogger(), nil) // HandleUserStream never uses h.auth
	t.Cleanup(h.Stop)

	e := echo.New()
	rec := newSSERecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/events/stream/account", nil)
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: userID})

	ctx, cancel := context.WithCancel(req.Context())
	c.SetRequest(req.WithContext(ctx))

	done := make(chan error, 1)
	go func() {
		done <- h.HandleUserStream(c)
	}()
	t.Cleanup(func() { cancel(); <-done })

	require.Eventually(t, func() bool { return rec.contains("event: connected") }, 2*time.Second, 5*time.Millisecond,
		"account stream must establish (connected event flushed)")

	return rec, cancel
}

// TestHandleUserStream_AccountScopeFiltering drives the fail-closed filters in
// the account-scope stream: only the caller's own account-scope notification is
// forwarded; another user's notification, a non-notification entity, and a
// project-scope notification for the same user are all dropped.
func TestHandleUserStream_AccountScopeFiltering(t *testing.T) {
	const userID = "user-123"

	svc := NewService(newTestLogger())
	rec, _ := startAccountStream(t, svc, userID)

	emit := func(entity EntityType, id, projectID string, data map[string]any) {
		svc.EmitCreated(context.Background(), entity, id, projectID, &EmitOptions{Data: data})
	}

	// (c) caller's own account-scope notification -> forwarded.
	emit(EntityNotification, "n-own-account", "", map[string]any{
		"userId": userID, "scope": "account",
	})
	// (a) another user's notification -> dropped.
	emit(EntityNotification, "n-other-user", "", map[string]any{
		"userId": "someone-else", "scope": "account",
	})
	// (b) non-notification entity -> dropped.
	emit(EntityDocument, "doc-non-notification", "proj-1", map[string]any{
		"userId": userID, "scope": "account",
	})
	// (d) project-scope notification for the same user -> dropped.
	emit(EntityNotification, "n-project-scope", "proj-1", map[string]any{
		"userId": userID, "scope": "project",
	})

	require.Eventually(t, func() bool {
		return rec.contains(`"id":"n-own-account"`)
	}, 2*time.Second, 5*time.Millisecond, "caller's own account notification must be forwarded")

	body := rec.String()
	require.Contains(t, body, `"entity":"notification"`, "forwarded payload must carry the notification entity")
	require.NotContains(t, body, "n-other-user", "another user's notification must be dropped")
	require.NotContains(t, body, "doc-non-notification", "non-notification entity must be dropped")
	require.NotContains(t, body, "n-project-scope", "project-scope notification must be dropped by the account stream")
}
