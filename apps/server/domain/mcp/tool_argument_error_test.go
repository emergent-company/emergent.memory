package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/toolargscan"
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
// policy, end to end: a plain error from the tool layer must reach the caller as
// the collapsed sentence, never as its own text. session-get-messages with a
// well-formed session_id reaches the session service, which this Service does
// not have — a genuinely internal failure. (An unknown tool name is NOT this
// class: it is caller-addressable and stays public, see
// TestToolsCallTransportsShareTheErrorChannel.)
func TestHTTPToolsCallDriverFailureIsSanitised(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions()

	h := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)

	user := &auth.AuthUser{
		ID:        uuid.New().String(),
		Scopes:    []string{"graph:read"},
		ProjectID: uuid.New().String(),
	}
	token := "tok-driver"
	h.sessionsMu.Lock()
	h.sessions[token] = &Session{Initialized: true, ProjectID: user.ProjectID}
	h.sessionsMu.Unlock()

	c, _ := gateContext(user, token)
	req := newToolsCallRequest(t, "session-get-messages", map[string]any{"session_id": uuid.New().String()})

	resp := h.handleToolsCall(c, req, user)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInternalError, resp.Error.Code)
	assert.Equal(t, "Tool execution failed: An internal error occurred", resp.Error.Message)
	assert.NotContains(t, resp.Error.Message, "session service",
		"a driver message must not reach the caller")
}

// newToolsCallRequest builds one tools/call request.
func newToolsCallRequest(t *testing.T, name string, args map[string]any) *Request {
	t.Helper()
	raw, err := json.Marshal(ToolsCallParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: raw}
}

// TestToolsCallTransportsShareTheErrorChannel drives every tools/call transport
// — the legacy /api/mcp/rpc handler, the spec endpoint POST /api/mcp, and SSE —
// through the same three error classes. Issue #668's fix first landed only on
// the legacy handler, so the spec endpoint kept leaking err.Error() and
// re-tagging argument errors as internal faults. Each transport must:
//
//   - relay a tool argument error with its argument name, as invalid params;
//   - collapse a driver failure to the fixed sentence, as an internal error;
//   - relay an unknown tool name (caller-addressable, and the phrase the e2e
//     skip helper matches) as not-found.
func TestToolsCallTransportsShareTheErrorChannel(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	transports := []struct {
		name string
		call func(t *testing.T, svc *Service, user *auth.AuthUser, req *Request) *Response
	}{
		{
			name: "legacy /api/mcp/rpc",
			call: func(t *testing.T, svc *Service, user *auth.AuthUser, req *Request) *Response {
				h := NewHandler(svc, discard, nil)
				token := "tok-" + uuid.NewString()
				h.sessionsMu.Lock()
				h.sessions[token] = &Session{Initialized: true, ProjectID: user.ProjectID}
				h.sessionsMu.Unlock()
				c, _ := gateContext(user, token)
				return h.handleToolsCall(c, req, user)
			},
		},
		{
			name: "streamable POST /api/mcp",
			call: func(t *testing.T, svc *Service, user *auth.AuthUser, req *Request) *Response {
				h := NewStreamableHTTPHandler(svc, discard)
				c, _ := gateContext(user, "tok-"+uuid.NewString())
				session := &MCPSession{ID: uuid.NewString(), Initialized: true, ProjectID: user.ProjectID}
				return h.handleToolsCall(c, req, session, user)
			},
		},
		{
			name: "sse",
			call: func(t *testing.T, svc *Service, user *auth.AuthUser, req *Request) *Response {
				h := NewSSEHandler(svc, nil, discard)
				c, _ := gateContext(user, "tok-"+uuid.NewString())
				return h.handleToolsCall(c, req, user.ProjectID, user, "test-client")
			},
		},
	}

	for _, transport := range transports {
		t.Run(transport.name, func(t *testing.T) {
			newService := func() *Service {
				svc := &Service{}
				_ = svc.GetToolDefinitions()
				return svc
			}
			newUser := func() *auth.AuthUser {
				return &auth.AuthUser{
					ID:        uuid.NewString(),
					Scopes:    []string{"projects:write", "graph:read"},
					ProjectID: uuid.NewString(),
				}
			}

			t.Run("argument validation names the argument", func(t *testing.T) {
				resp := transport.call(t, newService(), newUser(),
					newToolsCallRequest(t, "token-create", map[string]any{}))

				require.NotNil(t, resp.Error)
				assert.Equal(t, ErrCodeInvalidParams, resp.Error.Code,
					"a missing tool argument is invalid params, not an internal error")
				assert.Contains(t, resp.Error.Message, "'name' is required",
					"got: %s", resp.Error.Message)
			})

			t.Run("driver failure is sanitised", func(t *testing.T) {
				resp := transport.call(t, newService(), newUser(),
					newToolsCallRequest(t, "session-get-messages", map[string]any{"session_id": uuid.NewString()}))

				require.NotNil(t, resp.Error)
				assert.Equal(t, ErrCodeInternalError, resp.Error.Code)
				assert.Equal(t, "Tool execution failed: An internal error occurred", resp.Error.Message)
				assert.NotContains(t, resp.Error.Message, "session service")
			})

			t.Run("unknown tool stays caller-addressable", func(t *testing.T) {
				resp := transport.call(t, newService(), newUser(),
					newToolsCallRequest(t, "list_traces", map[string]any{}))

				require.NotNil(t, resp.Error)
				assert.Equal(t, ErrCodeNotFound, resp.Error.Code)
				assert.Contains(t, strings.ToLower(resp.Error.Message), "tool not found",
					"the e2e skip helper matches this phrase; got: %s", resp.Error.Message)
			})
		})
	}
}

// TestAdminToolsCallSanitisesDriverFailure drives the operator transport
// /api/mcp/admin. It used to answer with RedactString(err.Error()) at -32603, so
// an operator client saw the raw authorization-lookup failure. The audit entry
// keeps the redacted detail; the client gets the same collapsed sentence as
// every other tools/call channel. A nil database makes the superadmin lookup
// fail, which is a genuine internal error.
func TestAdminToolsCallSanitisesDriverFailure(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc := &Service{}
	svc.log = discard
	h := NewAdminStreamableHTTPHandler(svc, discard)

	user := &auth.AuthUser{ID: uuid.NewString(), Scopes: []string{"admin:all"}}
	c, _ := gateContext(user, "tok-admin")
	session := &MCPSession{ID: uuid.NewString(), Initialized: true}

	resp := h.handleToolsCall(c, newToolsCallRequest(t, "user-list", map[string]any{}), session, user)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInternalError, resp.Error.Code)
	assert.Equal(t, "Tool execution failed: An internal error occurred", resp.Error.Message)
	assert.NotContains(t, resp.Error.Message, "no database available",
		"the authorization lookup detail must stay in the audit log")
	assert.NotContains(t, resp.Error.Message, "authorize")
}

// TestToolsCallPermissionRefusalIsForbidden pins the other mapping issue #668
// review found: a permission refusal reached the caller as an internal fault
// with its explanation thrown away. Both in-process refusal seams are covered —
// the share-instance allowlist and authz.AuthorizeTool's typed *Denial — because
// a transport maps a 403 to -32002, so the refusal must be an app error.
func TestToolsCallPermissionRefusalIsForbidden(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions()

	user := &auth.AuthUser{ID: uuid.NewString(), Scopes: []string{"projects:write"}}
	baseCtx := auth.InjectAuthContext(context.Background(), user)

	tests := []struct {
		name string
		ctx  context.Context
		tool string
		args map[string]any
		want string
	}{
		{
			name: "share-instance allowlist",
			ctx: WithInstanceScope(baseCtx, &InstanceScope{
				HasToolAllowlist: true,
				AllowedTools:     []string{"entity-query"},
			}),
			tool: "token-create",
			args: map[string]any{"name": "ci", "project_id": uuid.NewString()},
			want: "tool not allowed by MCP share instance",
		},
		{
			name: "agent-only denial from authz",
			ctx:  baseCtx,
			tool: "web-fetch",
			args: map[string]any{"url": "https://example.com"},
			want: "agent-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ExecuteTool(tt.ctx, "", tt.tool, tt.args)

			require.Error(t, err)
			appErr, ok := errors.AsType[*apperror.Error](err)
			require.True(t, ok, "a refusal must be an app error so every transport can map it, got %T", err)
			assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
			assert.Contains(t, appErr.Message, tt.want)
			assert.Equal(t, ErrCodeForbidden, jsonRPCErrorCodeFor(err),
				"a permission refusal is -32002, not an internal fault")

			resp := toolExecutionErrorResponse(json.RawMessage("1"), err)
			require.NotNil(t, resp.Error)
			assert.Contains(t, resp.Error.Message, tt.want,
				"the refusal text must survive the tools/call channel")
			assert.NotContains(t, resp.Error.Message, "An internal error occurred")
		})
	}
}

// TestNoPlainArgumentValidationErrors keeps the conversion from regressing. A
// new tool whose validator returns fmt.Errorf would compile, pass its handler
// test (the message is only hidden for a non-app error), and be discovered by
// an agent that could not see which argument was wrong — exactly the state
// #668 removed. The handler tests cannot cover a site that does not exist yet;
// this scan can.
//
// The scan is call-shaped (toolargscan), not line-shaped: it reads each
// fmt.Errorf/errors.New argument list, so a validator written with errors.New
// or split across lines is caught too. The sources domain shares the same
// scan (domain/sources/review_tools_scan_test.go) because its tool validators
// reach the same tools/call channel.
func TestNoPlainArgumentValidationErrors(t *testing.T) {
	findings, err := toolargscan.ScanDir(".", toolargscan.Exemptions{
		Files: map[string]string{
			// Serialises a cache key, not a tool argument: the error is dropped
			// by canonicalToolCacheKey and never reaches a caller.
			"tool_result_cache.go": "cache-key canonicalisation",
		},
		Messages: map[string]string{
			// The internal cause of an apperror.NewInternal: never rendered to
			// the caller (only logged), so it is not a public message.
			"auto-created branch has invalid UUID": "internal cause",
		},
	})
	require.NoError(t, err)

	offenders := make([]string, 0, len(findings))
	for _, finding := range findings {
		offenders = append(offenders, finding.String())
	}

	assert.Empty(t, offenders,
		"tool argument validation must return toolArgErrorf (issue #668) so the ExecuteTool channel can sanitise everything else")
}
