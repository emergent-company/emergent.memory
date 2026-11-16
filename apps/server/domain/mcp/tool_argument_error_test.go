package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Issue #668: the tools/call channel used to prepend err.Error() into its
// JSON-RPC error, because a tool's argument validation was a plain fmt.Errorf
// and the handler could not tell it apart from a driver failure. These tests
// pin both halves after the split: argument validation is an app error whose
// message names the argument, and everything else is collapsed.

func TestToolArgErrorIsAnAppError(t *testing.T) {
	err := toolArgErrorf("list_agent_questions: %q is required", "run_id")

	appErr, ok := errors.AsType[*apperror.Error](err)
	require.True(t, ok, "argument validation must be distinguishable from a plain error")
	assert.Equal(t, http.StatusUnprocessableEntity, appErr.HTTPStatus)
	assert.Equal(t, "list_agent_questions: \"run_id\" is required", apperror.PublicMessage(err),
		"the message that names the argument must survive PublicMessage")
	assert.Equal(t, ErrCodeInvalidParams, jsonRPCErrorCodeFor(err),
		"a bad tool argument is the caller's params, not a server fault")
}

func TestToolExecutionErrorResponseKeepsTheArgumentToFix(t *testing.T) {
	resp := toolExecutionErrorResponse(json.RawMessage("1"), toolArgErrorf("create_project: %q is required", "name"))

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInvalidParams, resp.Error.Code)
	assert.Equal(t, `Tool execution failed: create_project: "name" is required`, resp.Error.Message,
		"the caller must learn which argument to fix, behind the channel's stable prefix")
}

func TestToolExecutionErrorResponseKeepsADomainPublicMessage(t *testing.T) {
	// A domain failure is an app error too, and its client-facing message is
	// already written to be shown; wrapping it must not lose it.
	domainErr := fmt.Errorf("entity-get: %w",
		apperror.New(http.StatusNotFound, "not_found", "entity 'abc' not found"))

	resp := toolExecutionErrorResponse(json.RawMessage("1"), domainErr)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeNotFound, resp.Error.Code)
	assert.Equal(t, "Tool execution failed: entity 'abc' not found", resp.Error.Message)
}

// TestToolExecutionErrorResponseSanitisesADriverFailure is the acceptance test
// for the sanitisation itself: a driver failure inside a tool (not an app
// error) must not put its detail in the caller's JSON-RPC message.
func TestToolExecutionErrorResponseSanitisesADriverFailure(t *testing.T) {
	driverErr := errors.New(`pq: relation "kb.graph_objects" does not exist (host=db.internal user=emergent)`)
	wrapped := fmt.Errorf("entity-query: %w", driverErr)

	resp := toolExecutionErrorResponse(json.RawMessage("1"), wrapped)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInternalError, resp.Error.Code)
	assert.Equal(t, "Tool execution failed: An internal error occurred", resp.Error.Message)
	assert.NotContains(t, resp.Error.Message, "kb.graph_objects")
	assert.NotContains(t, resp.Error.Message, "db.internal")
}

// TestHTTPToolsCallArgumentValidationReachesTheCaller drives the real transport
// path (handleToolsCall → ExecuteTool → the tool's own validator) and checks
// the caller sees the argument name rather than a collapsed server fault. The
// token-create tool validates 'name' before it touches the database, so this
// needs no DB.
func TestHTTPToolsCallArgumentValidationReachesTheCaller(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions()

	h := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)

	user := &auth.AuthUser{
		ID:        uuid.New().String(),
		Scopes:    []string{"projects:write"},
		ProjectID: uuid.New().String(),
	}
	token := "tok-validation"
	h.sessionsMu.Lock()
	h.sessions[token] = &Session{Initialized: true, ProjectID: user.ProjectID}
	h.sessionsMu.Unlock()

	c, _ := gateContext(user, token)
	params, err := json.Marshal(ToolsCallParams{Name: "token-create", Arguments: map[string]any{}})
	require.NoError(t, err)
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}

	resp := h.handleToolsCall(c, req, user)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInvalidParams, resp.Error.Code,
		"a missing tool argument is invalid params, not an internal error")
	assert.Contains(t, resp.Error.Message, "Tool execution failed: ")
	assert.Contains(t, resp.Error.Message, "'name' is required",
		"got: %s", resp.Error.Message)
}

// TestHTTPToolsCallDriverFailureIsSanitised is the other half of the channel
// policy, end to end: a plain error from the tool layer (here the dispatcher's
// unresolvable-name branch, which touches no database) must reach the caller as
// the collapsed sentence, never as its own text.
func TestHTTPToolsCallDriverFailureIsSanitised(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions()

	h := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)

	user := &auth.AuthUser{
		ID:        uuid.New().String(),
		Scopes:    []string{"projects:write"},
		ProjectID: uuid.New().String(),
	}
	token := "tok-driver"
	h.sessionsMu.Lock()
	h.sessions[token] = &Session{Initialized: true, ProjectID: user.ProjectID}
	h.sessionsMu.Unlock()

	c, _ := gateContext(user, token)
	params, err := json.Marshal(ToolsCallParams{Name: "no-such-tool", Arguments: map[string]any{}})
	require.NoError(t, err)
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}

	resp := h.handleToolsCall(c, req, user)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInternalError, resp.Error.Code)
	assert.Equal(t, "Tool execution failed: An internal error occurred", resp.Error.Message)
	assert.NotContains(t, resp.Error.Message, "no-such-tool",
		"a plain error's text must not reach the caller")
}

// argumentValidationGrep is the call shape from issue #668: a tool's own
// argument check returned as a plain error, which the ExecuteTool channel must
// either relay unsanitised or collapse. The keywords are the families the
// conversion found — the issue's original scan plus the sibling shapes
// ("are required", "missing required parameter", "cannot be combined", …) that
// the same defect covered.
var argumentValidationGrep = regexp.MustCompile(
	`fmt\.Errorf\("[^"]*(is required|are required|required parameter|required argument|must be|must not|invalid |unsupported|not supported|unknown |missing|cannot be combined|no valid |exceeds )`)

// TestNoPlainArgumentValidationErrors keeps the conversion from regressing. A
// new tool whose validator returns fmt.Errorf would compile, pass its handler
// test (the message is only hidden for a non-app error), and be discovered by
// an agent that could not see which argument was wrong — exactly the state
// #668 removed. The handler tests cannot cover a site that does not exist yet;
// this scan can.
func TestNoPlainArgumentValidationErrors(t *testing.T) {
	exempt := map[string]string{
		// Serialises a cache key, not a tool argument: the error is dropped by
		// canonicalToolCacheKey and never reaches a caller.
		"tool_result_cache.go": "cache-key canonicalisation",
		// The size limit is on the fetched upstream response, not on anything
		// the caller passed.
		"webfetch.go": "upstream response size",
	}

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, ok := exempt[name]; ok {
			continue
		}
		src, readErr := os.ReadFile(name)
		require.NoError(t, readErr)
		for i, line := range strings.Split(string(src), "\n") {
			if argumentValidationGrep.MatchString(line) {
				offenders = append(offenders, fmt.Sprintf("%s:%d: %s", name, i+1, strings.TrimSpace(line)))
			}
		}
	}

	assert.Empty(t, offenders,
		"tool argument validation must return toolArgErrorf (issue #668) so the ExecuteTool channel can sanitise everything else")
}
