package feedback

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func newTestHandler(t *testing.T, mutate func(*config.FeedbackConfig)) (*Handler, *fakeIssueCreator) {
	t.Helper()
	svc, fake := testService(t, mutate)
	return NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))), fake
}

func postFeedback(t *testing.T, h *Handler, body string, user *auth.AuthUser) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		status := http.StatusInternalServerError
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			status = appErr.HTTPStatus
		}
		_ = c.JSON(status, map[string]string{"message": err.Error()})
	}
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if user != nil {
				c.Set(string(auth.UserContextKey), user)
			}
			return next(c)
		}
	})
	e.POST("/api/feedback", h.Submit)

	req := httptest.NewRequest(http.MethodPost, "/api/feedback", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("User-Agent", "handler-test/1.0")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestHandlerSubmitReturnsCreated(t *testing.T) {
	h, fake := newTestHandler(t, nil)
	rec := postFeedback(t, h, `{"message":"the search tool returned 500","category":"bug","severity":"high"}`, &auth.AuthUser{
		ID:        "3f1c1a9e-1b6e-4f2a-9f0e-2b7c4d5e6f70",
		Email:     "ada@example.com",
		ProjectID: "2b7c4d5e-6f70-4a1b-8c2d-3e4f5a6b7c8d",
		OrgID:     "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d",
	})

	require.Equal(t, http.StatusCreated, rec.Code)
	var out SubmitResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, 42, out.IssueNumber)
	require.False(t, out.Duplicate)
	require.Len(t, fake.calls, 1)
	require.Contains(t, fake.calls[0].body, "ada@example.com")
	require.Contains(t, fake.calls[0].body, "2b7c4d5e-6f70-4a1b-8c2d-3e4f5a6b7c8d")
}

func TestHandlerDropsNonUUIDProjectContext(t *testing.T) {
	// RequireAuth copies the raw X-Project-ID header into user.ProjectID, so a
	// nonsense value must not reach the issue or the project lookup.
	h, fake := newTestHandler(t, nil)
	rec := postFeedback(t, h, `{"message":"the upload button does nothing"}`, &auth.AuthUser{
		ID: "3f1c1a9e-1b6e-4f2a-9f0e-2b7c4d5e6f70", Email: "ada@example.com", ProjectID: "project-1",
	})

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, fake.calls, 1)
	require.Contains(t, fake.calls[0].body, "none (account-level submission)")
	require.NotContains(t, fake.calls[0].body, "project-1")
}

func TestHandlerSubmitValidationError(t *testing.T) {
	h, _ := newTestHandler(t, nil)
	rec := postFeedback(t, h, `{"message":""}`, &auth.AuthUser{ID: "user-1"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlerSubmitRateLimited(t *testing.T) {
	h, _ := newTestHandler(t, func(c *config.FeedbackConfig) { c.RateLimitPerHour = 1 })
	user := &auth.AuthUser{ID: "user-1", Email: "ada@example.com"}

	first := postFeedback(t, h, `{"message":"first message"}`, user)
	require.Equal(t, http.StatusCreated, first.Code)

	second := postFeedback(t, h, `{"message":"a different message"}`, user)
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	require.NotEmpty(t, second.Header().Get("Retry-After"))
}

func TestHandlerSubmitNotConfigured(t *testing.T) {
	h, _ := newTestHandler(t, func(c *config.FeedbackConfig) { c.GitHubToken = "" })
	rec := postFeedback(t, h, `{"message":"hello"}`, &auth.AuthUser{ID: "user-1"})
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandlerMalformedJSON(t *testing.T) {
	h, _ := newTestHandler(t, nil)
	rec := postFeedback(t, h, `{"message":`, &auth.AuthUser{ID: "user-1"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandlerInProgressMapsToConflict keeps the REST answer in agreement with
// the MCP reply: 409 + Retry-After for "an identical submission is being filed".
func TestHandlerInProgressMapsToConflict(t *testing.T) {
	h, _ := newTestHandler(t, nil)
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/feedback", nil), rec)

	err := h.mapError(c, &InProgressError{RetryAfter: 7 * time.Second})
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusConflict, appErr.HTTPStatus)
	require.Equal(t, "feedback_in_progress", appErr.Code)
	require.Equal(t, "7", rec.Header().Get("Retry-After"))
}
