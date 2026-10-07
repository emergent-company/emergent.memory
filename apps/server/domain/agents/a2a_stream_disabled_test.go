package agents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// =============================================================================
// Round 3: the A2A SSE surface (a2a_stream.go) must honour the kill switch the
// same way the JSON surface does. These tests pin:
//   - streamRun fails a refused run terminal and emits an SSE failure;
//   - streamNewTask refuses before creating a run row;
//   - streamResumeTask refuses BEFORE claiming the pending question.
// plus the TriggerAgent / RespondToQuestion 503 contract.
// =============================================================================

func disabledExecutor() *AgentExecutor {
	return &AgentExecutor{
		safeguards: config.AgentSafeguardsConfig{ExecutionEnabled: false},
		log:        testAgentLogger(),
	}
}

func newStreamContext(method, path string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// TestStreamRun_DisabledFailsRunTerminal_NewTask drives streamRun directly on the
// new-task path (resume=false). A disabled ExecuteWithRun must drive the run
// terminal (an UPDATE against kb.agent_runs) and the SSE stream must report
// failure rather than leaving the row `running`.
func TestStreamRun_DisabledFailsRunTerminal_NewTask(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	c, rec := newStreamContext(http.MethodPost, "/message:stream")
	run := &AgentRun{ID: "run-1", Status: RunStatusRunning}

	err := h.streamRun(c, "proj-1", "task-1", "ctx-1", run,
		ExecuteRequest{ProjectID: "proj-1", UserMessage: "hi"}, false)
	require.NoError(t, err)

	joined := strings.Join(rootDriver.queriesCopy(), "\n")
	assert.Contains(t, joined, `UPDATE "kb"."agent_runs"`,
		"the refused run must be failed terminal")
	assert.Contains(t, joined, `'run-1'`, "the refused run id must be written")
	assert.Contains(t, joined, ErrExecutionDisabled.Error(), "the disabled reason must be persisted")
	assert.Contains(t, rec.Body.String(), "TASK_STATE_FAILED", "the SSE stream must report failure")
}

// TestStreamRun_DisabledFailsRunTerminal_Resume is the resume-path variant.
func TestStreamRun_DisabledFailsRunTerminal_Resume(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	c, rec := newStreamContext(http.MethodPost, "/message:stream")
	run := &AgentRun{ID: "run-resume", Status: RunStatusPaused}

	err := h.streamRun(c, "proj-1", "task-1", "ctx-1", run,
		ExecuteRequest{ProjectID: "proj-1", UserMessage: "resume"}, true)
	require.NoError(t, err)

	joined := strings.Join(rootDriver.queriesCopy(), "\n")
	assert.Contains(t, joined, `UPDATE "kb"."agent_runs"`)
	assert.Contains(t, joined, `'run-resume'`)
	assert.Contains(t, rec.Body.String(), "TASK_STATE_FAILED")
}

// TestStreamNewTask_DisabledDoesNotCreateRun asserts the pre-check runs BEFORE
// CreateRunWithOptions on the SSE new-task path: no agent_runs INSERT, and the
// caller gets the disabled error.
func TestStreamNewTask_DisabledDoesNotCreateRun(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	c, rec := newStreamContext(http.MethodPost, "/message:stream")
	err := h.streamNewTask(c, "proj-1", "user-1", "hello", "", "")
	require.NoError(t, err)

	joined := strings.Join(rootDriver.queriesCopy(), "\n")
	assert.NotContains(t, joined, `INSERT INTO "kb"."agent_runs"`,
		"a disabled SSE new-task must not create a run row")
	assert.Contains(t, rec.Body.String(), ErrExecutionDisabled.Error())
}

// =============================================================================
// Row-returning driver for the resume path: FindRunByID (with the Agent
// relation), FindLatestRunInChain and FindPendingQuestionsByRunID must all
// resolve, so the test can prove the question is NOT claimed while disabled.
// =============================================================================

type a2aResumeDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *a2aResumeDriver) reset() {
	d.mu.Lock()
	d.queries = nil
	d.mu.Unlock()
}

func (d *a2aResumeDriver) record(q string) {
	d.mu.Lock()
	d.queries = append(d.queries, q)
	d.mu.Unlock()
}

func (d *a2aResumeDriver) queriesCopy() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.queries))
	copy(out, d.queries)
	return out
}

func (d *a2aResumeDriver) Open(string) (driver.Conn, error) { return &a2aResumeConn{d: d}, nil }

type a2aResumeConn struct{ d *a2aResumeDriver }

func (*a2aResumeConn) Prepare(string) (driver.Stmt, error) {
	return nil, io.EOF
}
func (*a2aResumeConn) Close() error              { return nil }
func (*a2aResumeConn) Begin() (driver.Tx, error) { return nil, io.EOF }

func (c *a2aResumeConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.d.record(q)
	return driver.RowsAffected(1), nil
}

func (c *a2aResumeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.d.record(q)
	switch {
	case strings.Contains(q, `FROM "kb"."agent_runs"`):
		return &a2aRunRows{}, nil
	case strings.Contains(q, `FROM "kb"."agent_questions"`):
		return &a2aQuestionRows{}, nil
	default:
		return rootEmptyRows{}, nil
	}
}

type a2aRunRows struct{ done bool }

// Columns mirror the exact select list bun emits for FindRunByID (ar.* plus the
// Agent relation aliased as agent__*).
func (*a2aRunRows) Columns() []string {
	return []string{
		"id", "agent_id", "status", "started_at", "last_step_at", "completed_at",
		"duration_ms", "summary", "error_message", "skip_reason", "created_at",
		"trigger_source", "trigger_metadata", "subject_object_id", "subject_object_type",
		"failure_class", "parent_run_id", "step_count", "max_steps", "resumed_from",
		"trigger_message", "model", "provider", "provider_slug", "dialect", "trace_id",
		"root_run_id", "session_id", "agent_definition_id", "trusted_internal", "tools",
		"suspend_context",
		"agent__id", "agent__project_id", "agent__name", "agent__strategy_type",
		"agent__prompt", "agent__cron_schedule", "agent__enabled", "agent__trigger_type",
		"agent__reaction_config", "agent__execution_mode", "agent__capabilities",
		"agent__config", "agent__description", "agent__last_run_at", "agent__last_run_status",
		"agent__consecutive_failures", "agent__disabled_reason", "agent__agent_definition_id",
		"agent__created_at", "agent__updated_at",
	}
}

func (*a2aRunRows) Close() error { return nil }

func (r *a2aRunRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	now := time.Now()
	dest[0] = "task-1"         // id
	dest[1] = "agent-1"        // agent_id
	dest[2] = "input-required" // status (paused)
	dest[3] = now              // started_at
	dest[4] = nil              // last_step_at
	dest[5] = nil              // completed_at
	dest[6] = nil              // duration_ms
	dest[7] = nil              // summary
	dest[8] = nil              // error_message
	dest[9] = nil              // skip_reason
	dest[10] = now             // created_at
	dest[11] = nil             // trigger_source
	dest[12] = nil             // trigger_metadata
	dest[13] = nil             // subject_object_id
	dest[14] = nil             // subject_object_type
	dest[15] = nil             // failure_class
	dest[16] = nil             // parent_run_id
	dest[17] = int64(1)        // step_count
	dest[18] = nil             // max_steps
	dest[19] = nil             // resumed_from
	dest[20] = nil             // trigger_message
	dest[21] = nil             // model
	dest[22] = nil             // provider
	dest[23] = nil             // provider_slug
	dest[24] = nil             // dialect
	dest[25] = nil             // trace_id
	dest[26] = nil             // root_run_id
	dest[27] = "sess-1"        // session_id
	dest[28] = nil             // agent_definition_id
	dest[29] = false           // trusted_internal
	dest[30] = nil             // tools
	dest[31] = nil             // suspend_context
	dest[32] = "agent-1"       // agent__id
	dest[33] = "proj-1"        // agent__project_id
	dest[34] = "resume-agent"  // agent__name
	dest[35] = "agent-def:x"   // agent__strategy_type
	dest[36] = nil             // agent__prompt
	dest[37] = nil             // agent__cron_schedule
	dest[38] = true            // agent__enabled
	dest[39] = "manual"        // agent__trigger_type
	dest[40] = nil             // agent__reaction_config
	dest[41] = nil             // agent__execution_mode
	dest[42] = nil             // agent__capabilities
	dest[43] = nil             // agent__config
	dest[44] = nil             // agent__description
	dest[45] = nil             // agent__last_run_at
	dest[46] = nil             // agent__last_run_status
	dest[47] = int64(0)        // agent__consecutive_failures
	dest[48] = nil             // agent__disabled_reason
	dest[49] = nil             // agent__agent_definition_id
	dest[50] = now             // agent__created_at
	dest[51] = now             // agent__updated_at
	return nil
}

type a2aQuestionRows struct{ done bool }

func (*a2aQuestionRows) Columns() []string {
	return []string{
		"id", "run_id", "agent_id", "project_id", "question", "proposal", "options",
		"interaction_type", "placeholder", "max_length", "response", "responded_by",
		"responded_at", "status", "notification_id", "created_at", "updated_at",
	}
}
func (*a2aQuestionRows) Close() error { return nil }
func (r *a2aQuestionRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	now := time.Now()
	dest[0] = "q-1"
	dest[1] = "task-1"
	dest[2] = "agent-1"
	dest[3] = "proj-1"
	dest[4] = "Approve?"
	dest[5] = nil
	dest[6] = []byte("[]")
	dest[7] = "buttons"
	dest[8] = ""
	dest[9] = int64(0)
	dest[10] = nil
	dest[11] = nil
	dest[12] = nil
	dest[13] = "pending"
	dest[14] = nil
	dest[15] = now
	dest[16] = now
	return nil
}

var (
	registerA2AResumeOnce sync.Once
	a2aResumeDriverGlobal = &a2aResumeDriver{}
)

func newA2AResumeRepository(t *testing.T) (*Repository, *a2aResumeDriver) {
	t.Helper()
	registerA2AResumeOnce.Do(func() { sql.Register("agents_a2a_resume", a2aResumeDriverGlobal) })
	sqldb, err := sql.Open("agents_a2a_resume", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	a2aResumeDriverGlobal.reset()
	return NewRepository(bun.NewDB(sqldb, pgdialect.New())), a2aResumeDriverGlobal
}

// TestStreamResumeTask_DisabledDoesNotConsumeQuestion asserts the SSE resume path
// refuses BEFORE AnswerQuestion: with the switch off the question stays pending
// (no UPDATE) so the task is resumable after re-enable.
func TestStreamResumeTask_DisabledDoesNotConsumeQuestion(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	c, rec := newStreamContext(http.MethodPost, "/message:stream")
	err := h.streamResumeTask(c, "proj-1", "user-1", "yes", "task-1", "")
	require.NoError(t, err)

	for _, q := range drv.queriesCopy() {
		assert.NotContains(t, q, "UPDATE",
			"a disabled SSE resume must not claim the pending question")
	}
	assert.Contains(t, rec.Body.String(), ErrExecutionDisabled.Error())
}

// =============================================================================
// 503 contract for the HTTP caller-side guards.
// =============================================================================

// TestRespondToQuestion_DisabledReturns503 asserts the respond entry point
// refuses with the stable 503 contract before any repository work.
func TestRespondToQuestion_DisabledReturns503(t *testing.T) {
	h := &Handler{executor: disabledExecutor()}

	_, err := h.RespondToQuestion(context.Background(), RespondParams{
		ProjectID:  "proj-1",
		QuestionID: "q-1",
		Response:   "yes",
	})
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
	assert.Equal(t, "agent_execution_disabled", appErr.Code)
	assert.Contains(t, appErr.Message, ErrExecutionDisabled.Error())
}

// TestTriggerAgent_DisabledReturns503 asserts the trigger entry point refuses
// with the stable 503 contract instead of returning a false success:true.
func TestTriggerAgent_DisabledReturns503(t *testing.T) {
	repo := newSingleRowRepository(t, "agent-1")
	h := &Handler{repo: repo, executor: disabledExecutor()}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/agents/agent-1/trigger", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u-1", ProjectID: "proj-test-id"})
	c.SetParamNames("id")
	c.SetParamValues("agent-1")

	err := h.TriggerAgent(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
	assert.Equal(t, "agent_execution_disabled", appErr.Code)
	assert.Contains(t, appErr.Message, ErrExecutionDisabled.Error())
}
