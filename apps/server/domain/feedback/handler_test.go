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
		ID: "user-1", Email: "ada@example.com", ProjectID: "project-1", OrgID: "org-1",
	})

	require.Equal(t, http.StatusCreated, rec.Code)
	var out SubmitResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, 42, out.IssueNumber)
	require.False(t, out.Duplicate)
	require.Len(t, fake.calls, 1)
	require.Contains(t, fake.calls[0].body, "ada@example.com")
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
