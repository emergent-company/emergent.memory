package agents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/domain/events"
)

// =============================================================================
// BuildAskUserTool tests (task 10.6)
// =============================================================================

func TestBuildAskUserTool_Success(t *testing.T) {
	deps := AskUserToolDeps{
		Repo:       nil, // Not called during build
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		ProjectID:  "project-1",
		AgentID:    "agent-1",
		RunID:      "run-1",
		PauseState: &AskPauseState{},
		UserID:     "user-1",
	}

	tool, err := BuildAskUserTool(deps)
	require.NoError(t, err)
	require.NotNil(t, tool)

	assert.Equal(t, ToolNameAskUser, tool.Name())
	assert.Equal(t, "ask_user", tool.Name())
	assert.NotEmpty(t, tool.Description())
	assert.Contains(t, tool.Description(), "question")
}

func TestBuildAskUserTool_NameIsConstant(t *testing.T) {
	deps := AskUserToolDeps{
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		PauseState: &AskPauseState{},
	}

	tool, err := BuildAskUserTool(deps)
	require.NoError(t, err)

	// Verify it matches the constant used for tool registration
	assert.Equal(t, "ask_user", tool.Name())
	assert.Equal(t, ToolNameAskUser, tool.Name())
}

// =============================================================================
// AskPauseState tests
// =============================================================================

func TestAskPauseState_InitialState(t *testing.T) {
	state := &AskPauseState{}

	assert.False(t, state.ShouldPause())
	assert.Equal(t, "", state.QuestionID())
}

func TestAskPauseState_RequestPause(t *testing.T) {
	state := &AskPauseState{}

	state.RequestPause("q-123")

	assert.True(t, state.ShouldPause())
	assert.Equal(t, "q-123", state.QuestionID())
}

func TestAskPauseState_MultiplePauses(t *testing.T) {
	state := &AskPauseState{}

	state.RequestPause("q-1")
	assert.True(t, state.ShouldPause())
	assert.Equal(t, "q-1", state.QuestionID())

	// Second pause overwrites the question ID
	state.RequestPause("q-2")
	assert.True(t, state.ShouldPause())
	assert.Equal(t, "q-2", state.QuestionID())
}

// =============================================================================
// parseQuestionOptions tests
// =============================================================================

func TestParseQuestionOptions_NoOptionsKey(t *testing.T) {
	args := map[string]any{
		"question": "What do you think?",
	}
	result := parseQuestionOptions(args)
	assert.Nil(t, result)
}

func TestParseQuestionOptions_EmptyArray(t *testing.T) {
	args := map[string]any{
		"options": []any{},
	}
	result := parseQuestionOptions(args)
	assert.Nil(t, result)
}

func TestParseQuestionOptions_WrongType(t *testing.T) {
	args := map[string]any{
		"options": "not an array",
	}
	result := parseQuestionOptions(args)
	assert.Nil(t, result)
}

func TestParseQuestionOptions_ValidOptions(t *testing.T) {
	args := map[string]any{
		"options": []any{
			map[string]any{"label": "Red", "value": "red"},
			map[string]any{"label": "Blue", "value": "blue", "description": "The color blue"},
		},
	}
	result := parseQuestionOptions(args)
	require.Len(t, result, 2)

	assert.Equal(t, "Red", result[0].Label)
	assert.Equal(t, "red", result[0].Value)
	assert.Empty(t, result[0].Description)

	assert.Equal(t, "Blue", result[1].Label)
	assert.Equal(t, "blue", result[1].Value)
	assert.Equal(t, "The color blue", result[1].Description)
}

func TestParseQuestionOptions_SkipsInvalidItems(t *testing.T) {
	args := map[string]any{
		"options": []any{
			map[string]any{"label": "Good", "value": "good"},
			"not a map",                              // skipped
			map[string]any{"label": "", "value": ""}, // skipped: empty
			map[string]any{"label": "Only label"},    // skipped: no value
			map[string]any{"value": "only_value"},    // skipped: no label
			map[string]any{"label": "Also Good", "value": "also_good"},
		},
	}
	result := parseQuestionOptions(args)
	require.Len(t, result, 2)

	assert.Equal(t, "Good", result[0].Label)
	assert.Equal(t, "Also Good", result[1].Label)
}

func TestParseQuestionOptions_SingleOption(t *testing.T) {
	args := map[string]any{
		"options": []any{
			map[string]any{"label": "Yes", "value": "yes", "description": "Confirm"},
		},
	}
	result := parseQuestionOptions(args)
	require.Len(t, result, 1)
	assert.Equal(t, "Yes", result[0].Label)
	assert.Equal(t, "yes", result[0].Value)
	assert.Equal(t, "Confirm", result[0].Description)
}

// =============================================================================
// parseProposal tests (agent-proposal-cards)
// =============================================================================

func TestParseProposal_ValidBlueprint(t *testing.T) {
	args := map[string]any{
		"proposal": map[string]any{
			"kind":    "blueprint",
			"summary": "Add two object types and one relationship type",
			"body": map[string]any{
				"objectTypes":       []any{},
				"relationshipTypes": []any{},
			},
		},
	}

	result := parseProposal(args)
	require.NotNil(t, result)
	assert.Equal(t, "blueprint", result["kind"])
	assert.Equal(t, "Add two object types and one relationship type", result["summary"])
	require.NotNil(t, result["body"])
	assert.IsType(t, map[string]any{}, result["body"])
}

func TestParseProposal_Absent(t *testing.T) {
	assert.Nil(t, parseProposal(map[string]any{"question": "hi"}))
	assert.Nil(t, parseProposal(map[string]any{"proposal": nil}))
}

func TestParseProposal_Invalid(t *testing.T) {
	cases := map[string]any{
		"not a map":         "blueprint",
		"missing kind":      map[string]any{"summary": "s", "body": map[string]any{}},
		"kind not a string": map[string]any{"kind": 123, "summary": "s", "body": map[string]any{}},
		"empty kind":        map[string]any{"kind": "", "summary": "s", "body": map[string]any{}},
		"missing summary":   map[string]any{"kind": "blueprint", "body": map[string]any{}},
		"empty summary":     map[string]any{"kind": "blueprint", "summary": "", "body": map[string]any{}},
		"missing body":      map[string]any{"kind": "blueprint", "summary": "s"},
		"body is array":     map[string]any{"kind": "blueprint", "summary": "s", "body": []any{}},
		"body is string":    map[string]any{"kind": "blueprint", "summary": "s", "body": "x"},
		"body is nil":       map[string]any{"kind": "blueprint", "summary": "s", "body": nil},
		"body is number":    map[string]any{"kind": "blueprint", "summary": "s", "body": 42},
	}
	for name, proposal := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, parseProposal(map[string]any{"proposal": proposal}), "invalid proposal must be dropped")
		})
	}
}

// =============================================================================
// AgentQuestion DTO tests (agent-proposal-cards)
// =============================================================================

func TestAgentQuestionDTO_CarriesProposal(t *testing.T) {
	proposal := map[string]any{
		"kind":    "blueprint",
		"summary": "Add types",
		"body":    map[string]any{"objectTypes": []any{}},
	}
	q := &AgentQuestion{
		ID:              "q-1",
		RunID:           "r-1",
		AgentID:         "a-1",
		ProjectID:       "p-1",
		Question:        "Approve this blueprint?",
		Proposal:        proposal,
		Options:         []AgentQuestionOption{},
		InteractionType: QuestionInteractionButtons,
		Status:          QuestionStatusPending,
	}

	dto := q.ToDTO()
	require.NotNil(t, dto.Proposal)
	assert.Equal(t, "blueprint", dto.Proposal["kind"])
	assert.Equal(t, "Add types", dto.Proposal["summary"])
}

func TestAgentQuestionDTO_NoProposal(t *testing.T) {
	q := &AgentQuestion{ID: "q-1"}
	assert.Nil(t, q.ToDTO().Proposal)
}

func TestAgentQuestionDTO_ProposalJSONFieldName(t *testing.T) {
	q := &AgentQuestion{
		ID:       "q-1",
		Proposal: map[string]any{"kind": "blueprint", "summary": "s", "body": map[string]any{}},
	}
	b, err := json.Marshal(q.ToDTO())
	require.NoError(t, err)
	assert.Contains(t, string(b), `"proposal":{`)

	noProposal := &AgentQuestion{ID: "q-2"}
	b2, err := json.Marshal(noProposal.ToDTO())
	require.NoError(t, err)
	assert.NotContains(t, string(b2), `"proposal"`)
}

// =============================================================================
// emitQuestionSSEEventDirect tests (agent-proposal-cards)
// =============================================================================

func newQuestionEventService(t *testing.T, projectID string, ch chan events.EntityEvent) *events.Service {
	t.Helper()
	svc := events.NewService(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	svc.Subscribe(projectID, func(e events.EntityEvent) { ch <- e })
	return svc
}

func TestEmitQuestionSSEEventDirect_IncludesProposal(t *testing.T) {
	ch := make(chan events.EntityEvent, 1)
	svc := newQuestionEventService(t, "p-1", ch)

	q := &AgentQuestion{
		ID:    "q-1",
		RunID: "r-1",
		Proposal: map[string]any{
			"kind":    "blueprint",
			"summary": "Add object types",
			"body":    map[string]any{"objectTypes": []any{}},
		},
	}

	emitQuestionSSEEventDirect(svc, "p-1", q)

	select {
	case e := <-ch:
		proposal, ok := e.Data["proposal"]
		require.True(t, ok, "SSE payload must include the proposal field")
		m, ok := proposal.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "blueprint", m["kind"])
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for question SSE event")
	}
}

func TestEmitQuestionSSEEventDirect_OmitsProposalWhenAbsent(t *testing.T) {
	ch := make(chan events.EntityEvent, 1)
	svc := newQuestionEventService(t, "p-1", ch)

	q := &AgentQuestion{ID: "q-1", RunID: "r-1"}

	emitQuestionSSEEventDirect(svc, "p-1", q)

	select {
	case e := <-ch:
		_, ok := e.Data["proposal"]
		assert.False(t, ok, "plain-text questions must not carry a proposal field")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for question SSE event")
	}
}

// =============================================================================
// CreateQuestion persistence test (agent-proposal-cards)
// =============================================================================

// captureExecDriver records the SQL text of the last ExecContext call so a
// test can assert that a repository write includes the proposal column.
type captureExecDriver struct {
	mu    sync.Mutex
	query string
	args  []driver.NamedValue
}

func (d *captureExecDriver) Open(string) (driver.Conn, error) { return &captureExecConn{d: d}, nil }

type captureExecConn struct{ d *captureExecDriver }

func (c *captureExecConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *captureExecConn) Close() error              { return nil }
func (c *captureExecConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (c *captureExecConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.d.mu.Lock()
	c.d.query = query
	c.d.mu.Unlock()
	return emptyRows{}, nil
}

// emptyRows is a no-op driver.Rows returned for QueryContext so Bun can iterate
// an empty result set when it routes an INSERT through the query path.
type emptyRows struct{}

func (emptyRows) Columns() []string         { return nil }
func (emptyRows) Close() error              { return nil }
func (emptyRows) Next([]driver.Value) error { return io.EOF }
func (c *captureExecConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	c.d.query = query
	c.d.args = args
	return driver.RowsAffected(1), nil
}

func (d *captureExecDriver) lastQuery() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.query
}

var (
	registerCaptureExecOnce sync.Once
	sharedCaptureDriver     = &captureExecDriver{}
)

func newCaptureExecRepository(t *testing.T) *Repository {
	t.Helper()
	registerCaptureExecOnce.Do(func() { sql.Register("agents_capture_exec", sharedCaptureDriver) })
	sqldb, err := sql.Open("agents_capture_exec", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New()))
}

func TestCreateQuestion_PersistsProposalColumn(t *testing.T) {
	sharedCaptureDriver.mu.Lock()
	sharedCaptureDriver.query = ""
	sharedCaptureDriver.args = nil
	sharedCaptureDriver.mu.Unlock()

	repo := newCaptureExecRepository(t)
	now := time.Now()
	q := &AgentQuestion{
		ID:        "q-1",
		RunID:     "r-1",
		AgentID:   "a-1",
		ProjectID: "p-1",
		Question:  "Approve this blueprint?",
		Proposal: map[string]any{
			"kind":    "blueprint",
			"summary": "Add object types",
			"body":    map[string]any{"objectTypes": []any{}},
		},
		// Populate every NOT NULL field so Bun emits a plain INSERT (no
		// RETURNING); the fake driver only implements ExecContext.
		Options:         []AgentQuestionOption{{Label: "Yes", Value: "yes"}},
		InteractionType: QuestionInteractionButtons,
		Status:          QuestionStatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	err := repo.CreateQuestion(context.Background(), q)
	require.NoError(t, err)

	query := sharedCaptureDriver.lastQuery()
	require.NotEmpty(t, query, "CreateQuestion must issue an INSERT")
	assert.True(t, strings.Contains(query, "proposal"), "INSERT must include the proposal column, got: %s", query)
}
