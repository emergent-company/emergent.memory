package agents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	stdErrors "errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// =============================================================================
// Adversarial review (R2) follow-ups: caller-side kill-switch guarantees.
//
// The in-executor guard alone left two gaps:
//   1. callers pre-create run rows, which stayed `running` on a disabled refusal;
//   2. responders consumed a pending question before Resume refused, stranding
//      the run paused with no question.
// These tests pin the caller-side fixes.
//
// The rootCapture driver (agent_run_root_test.go) inlines literals into the SQL
// text rather than passing bind args, so assertions read the query string.
// =============================================================================

// TestFailRunIfExecutionDisabled_MarksRunTerminal pins the mechanism used by the
// HTTP trigger and resume callers: a pre-created run refused by the kill switch
// is driven to a terminal state (an UPDATE against kb.agent_runs) rather than
// being left `running` for the stale-run reaper.
func TestFailRunIfExecutionDisabled_MarksRunTerminal(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	run := &AgentRun{ID: "pre-created-run", StepCount: 4}

	failRunIfExecutionDisabled(context.Background(), repo, run, ErrExecutionDisabled, testAgentLogger())

	queries := rootDriver.queriesCopy()
	require.NotEmpty(t, queries, "the disabled pre-created run must be marked terminal")
	joined := strings.Join(queries, "\n")
	assert.Contains(t, joined, `UPDATE "kb"."agent_runs"`, "terminal transition must target the run row")
	assert.Contains(t, joined, `'pre-created-run'`, "the pre-created run id must be written")
	assert.Contains(t, joined, ErrExecutionDisabled.Error(), "the disabled reason must be persisted")
}

// TestFailRunIfExecutionDisabled_NoopForOtherErrors asserts the helper does not
// fail a run for ordinary errors (the executor owns those transitions).
func TestFailRunIfExecutionDisabled_NoopForOtherErrors(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)

	failRunIfExecutionDisabled(context.Background(), repo, &AgentRun{ID: "run-x"},
		stdErrors.New("some other failure"), testAgentLogger())

	assert.Empty(t, rootDriver.queriesCopy(), "a non-kill-switch error must not mark the run terminal")
}

// TestFailRunIfExecutionDisabled_NoopForNilRun asserts a nil run is tolerated.
func TestFailRunIfExecutionDisabled_NoopForNilRun(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)

	failRunIfExecutionDisabled(context.Background(), repo, nil, ErrExecutionDisabled, testAgentLogger())

	assert.Empty(t, rootDriver.queriesCopy())
}

// TestResumeQuestionRun_PreCreatedRunFailsWhenDisabled covers the response path:
// resumeQuestionRun pre-creates the resume run BEFORE calling Resume, so a
// disabled Resume must leave no orphaned `running` row. The goroutine signals
// completion via OnRunSettled.
func TestResumeQuestionRun_PreCreatedRunFailsWhenDisabled(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	h := &Handler{
		repo:     repo,
		executor: &AgentExecutor{safeguards: config.AgentSafeguardsConfig{ExecutionEnabled: false}, log: testAgentLogger()},
	}

	run := &AgentRun{ID: "prior-run", Status: RunStatusPaused, AgentID: "agent-1", StepCount: 2}
	agent := &Agent{ID: "agent-1", ProjectID: "proj-1"}

	settled := make(chan struct{}, 1)
	p := RespondParams{
		OrgID:        "org-1",
		OnRunSettled: func(*ExecuteResult) { settled <- struct{}{} },
	}

	preCreatedRun, err := h.resumeQuestionRun(context.Background(), p, run, agent, nil, "user answered", "")
	require.NoError(t, err)
	require.NotNil(t, preCreatedRun)

	select {
	case <-settled:
	case <-time.After(5 * time.Second):
		t.Fatal("resume goroutine did not settle")
	}

	queries := rootDriver.queriesCopy()
	joined := strings.Join(queries, "\n")
	// Two terminal transitions: the prior paused run (existing behaviour) AND the
	// pre-created resume run (the R2 fix). The pre-created run's in-memory ID is
	// empty (uuid is a DB default), so prove the second write by counting.
	assert.Equal(t, 2, strings.Count(joined, `UPDATE "kb"."agent_runs"`),
		"both the prior run and the pre-created resume run must be failed terminal")
	assert.Contains(t, joined, ErrExecutionDisabled.Error(),
		"the disabled reason must be recorded on the pre-created run")
}

// =============================================================================
// Question-consuming responder: the question must NOT be consumed while disabled.
// =============================================================================

// questionRowDriver returns exactly one pending AgentQuestion row for any SELECT
// against kb.agent_questions and records every statement, so a test can assert
// that no UPDATE (the AnswerQuestion claim) was issued.
type questionRowDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *questionRowDriver) reset() {
	d.mu.Lock()
	d.queries = nil
	d.mu.Unlock()
}

func (d *questionRowDriver) record(q string) {
	d.mu.Lock()
	d.queries = append(d.queries, q)
	d.mu.Unlock()
}

func (d *questionRowDriver) queriesCopy() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.queries))
	copy(out, d.queries)
	return out
}

func (d *questionRowDriver) Open(string) (driver.Conn, error) { return &questionRowConn{d: d}, nil }

type questionRowConn struct{ d *questionRowDriver }

func (*questionRowConn) Prepare(string) (driver.Stmt, error) {
	return nil, stdErrors.New("not implemented")
}
func (*questionRowConn) Close() error              { return nil }
func (*questionRowConn) Begin() (driver.Tx, error) { return nil, stdErrors.New("not implemented") }

func (c *questionRowConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.d.record(q)
	return driver.RowsAffected(1), nil
}

func (c *questionRowConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.d.record(q)
	if strings.Contains(q, "agent_questions") {
		return &questionRowRows{}, nil
	}
	return rootEmptyRows{}, nil
}

type questionRowRows struct{ done bool }

func (*questionRowRows) Columns() []string {
	return []string{
		"id", "run_id", "agent_id", "project_id", "question", "proposal", "options",
		"interaction_type", "placeholder", "max_length", "response", "responded_by",
		"responded_at", "status", "notification_id", "created_at", "updated_at",
	}
}
func (*questionRowRows) Close() error { return nil }
func (r *questionRowRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	now := time.Now()
	dest[0] = "q-1"        // id
	dest[1] = "run-1"      // run_id
	dest[2] = "agent-1"    // agent_id
	dest[3] = "proj-1"     // project_id
	dest[4] = "Continue?"  // question
	dest[5] = nil          // proposal
	dest[6] = []byte("[]") // options
	dest[7] = "buttons"    // interaction_type
	dest[8] = ""           // placeholder
	dest[9] = int64(0)     // max_length
	dest[10] = nil         // response
	dest[11] = nil         // responded_by
	dest[12] = nil         // responded_at
	dest[13] = "pending"   // status
	dest[14] = nil         // notification_id
	dest[15] = now         // created_at
	dest[16] = now         // updated_at
	return nil
}

var (
	registerQuestionRowOnce sync.Once
	questionRowDriverGlobal = &questionRowDriver{}
)

func newQuestionRowRepository(t *testing.T) (*Repository, *questionRowDriver) {
	t.Helper()
	registerQuestionRowOnce.Do(func() { sql.Register("agents_question_row", questionRowDriverGlobal) })
	sqldb, err := sql.Open("agents_question_row", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	questionRowDriverGlobal.reset()
	return NewRepository(bun.NewDB(sqldb, pgdialect.New())), questionRowDriverGlobal
}

// TestExecuteRespondToAgentQuestion_DisabledDoesNotConsumeQuestion asserts the
// pre-check runs BEFORE the AnswerQuestion claim: with the kill switch off the
// tool returns the disabled error and issues no UPDATE, so the question stays
// pending and the run remains resumable after re-enable.
func TestExecuteRespondToAgentQuestion_DisabledDoesNotConsumeQuestion(t *testing.T) {
	repo, drv := newQuestionRowRepository(t)
	h := &MCPToolHandler{
		repo:     repo,
		executor: &AgentExecutor{safeguards: config.AgentSafeguardsConfig{ExecutionEnabled: false}, log: testAgentLogger()},
		log:      testAgentLogger(),
	}

	result, err := h.ExecuteRespondToAgentQuestion(context.Background(), "proj-1", map[string]any{
		"question_id": "q-1",
		"response":    "yes",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	assert.Contains(t, result.Content[0].Text, ErrExecutionDisabled.Error())

	for _, q := range drv.queriesCopy() {
		assert.NotContains(t, q, "UPDATE",
			"a disabled respond must not claim the pending question (no UPDATE)")
	}
}
