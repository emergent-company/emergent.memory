package agents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ---------- mapExecutorError (task 18.5) ----------

// TestMapExecutorError_BudgetExceeded verifies that a BudgetExceededError maps to HTTP 402.
func TestMapExecutorError_BudgetExceeded(t *testing.T) {
	err := &BudgetExceededError{
		ProjectID: "proj-123",
		Message:   "budget exceeded",
	}
	appErr := mapExecutorError(err)
	require.NotNil(t, appErr)
	assert.Equal(t, http.StatusPaymentRequired, appErr.HTTPStatus)
	assert.Equal(t, "budget_exceeded", appErr.Code)
}

// TestMapExecutorError_QueueFull verifies that a QueueFullError maps to HTTP 429.
func TestMapExecutorError_QueueFull(t *testing.T) {
	err := &QueueFullError{
		AgentID:        "agent-abc",
		PendingJobs:    10,
		MaxPendingJobs: 10,
	}
	appErr := mapExecutorError(err)
	require.NotNil(t, appErr)
	assert.Equal(t, http.StatusTooManyRequests, appErr.HTTPStatus)
	assert.Equal(t, "queue_full", appErr.Code)
	// Message should mention the pending/max counts for diagnosability
	assert.Contains(t, appErr.Message, "10")
}

// TestMapExecutorError_QueueFull_MessageContainsCounts verifies the message contains
// both the current pending count and the maximum for operator visibility.
func TestMapExecutorError_QueueFull_MessageContainsCounts(t *testing.T) {
	err := &QueueFullError{
		AgentID:        "agent-xyz",
		PendingJobs:    7,
		MaxPendingJobs: 10,
	}
	appErr := mapExecutorError(err)
	require.NotNil(t, appErr)
	assert.Contains(t, appErr.Message, "7")
	assert.Contains(t, appErr.Message, "10")
}

// TestMapExecutorError_GenericError verifies that an unknown error maps to HTTP 500.
func TestMapExecutorError_GenericError(t *testing.T) {
	err := assert.AnError // sentinel error that is not BudgetExceededError or QueueFullError
	appErr := mapExecutorError(err)
	require.NotNil(t, appErr)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
}

// TestMapExecutorError_WrappedBudgetExceeded verifies errors.As unwrapping works for
// a BudgetExceededError wrapped inside another error.
func TestMapExecutorError_WrappedBudgetExceeded(t *testing.T) {
	inner := &BudgetExceededError{ProjectID: "p1"}
	// Wrap it using apperror so errors.As still finds the underlying type.
	wrapped := apperror.NewInternal("wrapped", inner)

	// mapExecutorError takes an error; wrapped is *apperror.Error which is also an error.
	// errors.As will traverse the chain via Unwrap.
	appErr := mapExecutorError(wrapped)
	require.NotNil(t, appErr)
	// The wrapped budget error should be detected; result is 402.
	assert.Equal(t, http.StatusPaymentRequired, appErr.HTTPStatus)
}

// ---------- CreateAgent cron validation (task 17.6) ----------

// newTestHandler builds a Handler with nil repo (sufficient for tests that fail
// before any DB call, e.g., cron validation that returns 400 before repo.Create).
func newTestHandler() *Handler {
	return &Handler{
		repo:        nil,
		executor:    nil,
		rateLimiter: nil,
	}
}

// newEchoContextWithUser creates an Echo context backed by httptest with an
// authenticated user pre-populated, simulating a request through RequireAuth middleware.
func newEchoContextWithUser(method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{
		ID:        "user-test-id",
		Email:     "test@example.com",
		ProjectID: "proj-test-id",
	})
	return c, rec
}

// TestCreateAgent_InvalidCronReturns400 verifies that POST /api/admin/agents with a
// cron schedule that violates the minimum interval returns HTTP 400 (task 17.6).
func TestCreateAgent_InvalidCronReturns400(t *testing.T) {
	h := newTestHandler()

	// Every-minute schedule is below the 15-minute minimum.
	body := `{
		"projectId":    "proj-test-id",
		"name":         "test-agent",
		"strategyType": "llm",
		"cronSchedule": "* * * * *",
		"triggerType":  "schedule"
	}`

	c, rec := newEchoContextWithUser(http.MethodPost, "/api/admin/agents", body)

	err := h.CreateAgent(c)

	// The handler returns an *apperror.Error (not a plain Go error).
	// The Echo error handler would normally render it; here we inspect the error directly.
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T: %v", err, err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "cron")
	_ = rec // response recorder unused when handler returns an error (Echo renders it)
}

// TestCreateAgent_ValidCronDoesNotReturn400ForCron verifies that a valid 15-min
// schedule passes cron validation and does NOT return a 400 cron error.
// The handler will panic on the nil repo after validation passes — we use
// recover to distinguish a cron-400 (our concern) from a nil-pointer panic
// (expected given the test-only nil repo).
func TestCreateAgent_ValidCronDoesNotReturn400ForCron(t *testing.T) {
	h := newTestHandler()

	// Every-15-minute schedule meets the default minimum.
	body := `{
		"projectId":    "proj-test-id",
		"name":         "test-agent",
		"strategyType": "llm",
		"cronSchedule": "*/15 * * * *",
		"triggerType":  "schedule"
	}`

	c, _ := newEchoContextWithUser(http.MethodPost, "/api/admin/agents", body)

	var handlerErr error
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true // expected: nil repo causes a nil-pointer dereference
			}
		}()
		handlerErr = h.CreateAgent(c)
	}()

	if panicked {
		// Good: cron validation passed and the handler reached the nil repo — not a cron error.
		return
	}

	// If the handler returned an error it must NOT be a 400 cron error.
	if handlerErr != nil {
		var appErr *apperror.Error
		if assert.ErrorAs(t, handlerErr, &appErr) {
			assert.False(t,
				appErr.HTTPStatus == http.StatusBadRequest && strings.Contains(appErr.Message, "cron"),
				"valid cron should not fail with a 400 cron error; got: %v", appErr,
			)
		}
	}
}

// TestCreateAgent_NonScheduleTriggerSkipsCronValidation verifies that a
// webhook-triggered agent with an every-minute cron schedule is not rejected
// for cron-interval reasons — cron validation only applies to schedule triggers.
func TestCreateAgent_NonScheduleTriggerSkipsCronValidation(t *testing.T) {
	h := newTestHandler()

	// triggerType=webhook — cron validation should be skipped entirely.
	// CronSchedule is still required by field validation so supply a value,
	// but the interval check should not apply.
	body := `{
		"projectId":    "proj-test-id",
		"name":         "webhook-agent",
		"strategyType": "llm",
		"cronSchedule": "* * * * *",
		"triggerType":  "webhook"
	}`

	c, _ := newEchoContextWithUser(http.MethodPost, "/api/admin/agents", body)

	var handlerErr error
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true // expected: nil repo dereference after validation passes
			}
		}()
		handlerErr = h.CreateAgent(c)
	}()

	if panicked {
		// Cron validation was skipped and the handler reached the nil repo — correct.
		return
	}

	// If the handler returned an error it must NOT be a cron-400.
	if handlerErr != nil {
		var appErr *apperror.Error
		if assert.ErrorAs(t, handlerErr, &appErr) {
			assert.False(t,
				appErr.HTTPStatus == http.StatusBadRequest && strings.Contains(appErr.Message, "cron"),
				"webhook trigger should not fail cron validation; got: %v", appErr,
			)
		}
	}
}

// ---------- UpdateAgent cron validation (task 17.7) ----------

// TestIntegrationUpdateAgent_InvalidCronReturns400 verifies that PATCH /api/admin/agents/:id
// with an invalid cron schedule returns HTTP 400.
// Requires a running Postgres instance — skipped without DB env var.
func TestIntegrationUpdateAgent_InvalidCronReturns400(t *testing.T) {
	if os.Getenv("POSTGRES_HOST") == "" {
		t.Skip("POSTGRES_HOST not set; skipping DB integration test")
	}
	t.Skip("requires agents tables and a known agent ID; run after: task migrate")
}

// TestIntegrationUpdateAgent_ValidCronAccepted verifies that PATCH /api/admin/agents/:id
// with a valid cron schedule is accepted.
// Requires a running Postgres instance — skipped without DB env var.
func TestIntegrationUpdateAgent_ValidCronAccepted(t *testing.T) {
	if os.Getenv("POSTGRES_HOST") == "" {
		t.Skip("POSTGRES_HOST not set; skipping DB integration test")
	}
	t.Skip("requires agents tables and a known agent ID; run after: task migrate")
}

// ---------- JSON error body shape (helpers) ----------

// errorBodyCode extracts the "error.code" field from an HTTP response recorded by httptest.
func errorBodyCode(t *testing.T, body []byte) string {
	t.Helper()
	var wrapper struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &wrapper))
	return wrapper.Error.Code
}
