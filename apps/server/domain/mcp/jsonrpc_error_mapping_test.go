package mcp

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// The resources/read and prompts/get transports used to answer every service
// failure with internal-error and the collapsed "An internal error occurred",
// which turned a wrong URI or prompt name — the caller's own mistake, and the
// only thing it can correct — into an indistinguishable server fault. These
// tests pin both halves of the fix: the code the service's app error maps to, and
// that its message survives.

func TestJSONRPCErrorCodeFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"bad request", apperror.ErrBadRequest.WithMessage("invalid project_id"), ErrCodeInvalidParams},
		{"unprocessable", apperror.New(422, "unprocessable", "nope"), ErrCodeInvalidParams},
		{"not found", apperror.ErrNotFound.WithMessage("unknown prompt: x"), ErrCodeNotFound},
		{"forbidden", apperror.ErrForbidden.WithMessage("no"), ErrCodeForbidden},
		{"unauthorized", apperror.ErrUnauthorized.WithMessage("no"), ErrCodeUnauthorized},
		{"internal", apperror.ErrInternal, ErrCodeInternalError},
		{"conflict is not the client's params", apperror.ErrConflict.WithMessage("conflict"), ErrCodeInternalError},
		{"a plain error is internal", assert.AnError, ErrCodeInternalError},
		{"an app error behind a wrap", errWrap(apperror.ErrNotFound.WithMessage("gone")), ErrCodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, jsonRPCErrorCodeFor(tt.err))
		})
	}
}

func TestResourcesReadNamesTheUnknownURI(t *testing.T) {
	h := NewHandler(&Service{}, slog.Default(), nil)
	user := &auth.AuthUser{ID: "user-1", ProjectID: "11111111-1111-1111-1111-111111111111"}
	c, _ := newJSONContext(t, "POST", "", user, nil, nil)

	resp := h.handleResourcesRead(*c, &Request{
		JSONRPC: "2.0",
		Params:  []byte(`{"uri":"memory://nope"}`),
	}, user)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeNotFound, resp.Error.Code, "a URI the server does not serve is a miss, not a server fault")
	assert.Contains(t, resp.Error.Message, "unknown resource URI: memory://nope",
		"the caller must learn which URI to correct")
}

func TestResourcesReadNamesAnInvalidProjectID(t *testing.T) {
	h := NewHandler(&Service{}, slog.Default(), nil)
	user := &auth.AuthUser{ID: "user-1", ProjectID: "not-a-uuid"}
	c, _ := newJSONContext(t, "POST", "", user, nil, nil)

	resp := h.handleResourcesRead(*c, &Request{
		JSONRPC: "2.0",
		Params:  []byte(`{"uri":"memory://project/x/metadata"}`),
	}, user)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "invalid project_id",
		"a malformed project id is the caller's input, not an outage")
}

func TestPromptsGetNamesTheUnknownPrompt(t *testing.T) {
	h := NewHandler(&Service{}, slog.Default(), nil)
	user := &auth.AuthUser{ID: "user-1", ProjectID: "11111111-1111-1111-1111-111111111111"}
	c, _ := newJSONContext(t, "POST", "", user, nil, nil)

	resp := h.handlePromptsGet(*c, &Request{JSONRPC: "2.0", Params: []byte(`{"name":"nope"}`)}, user)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeNotFound, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "unknown prompt: nope")
}

// errWrap is fmt.Errorf("%w") without pulling fmt into this file's imports for
// one call.
func errWrap(err error) error {
	return wrappedError{err: err}
}

type wrappedError struct{ err error }

func (w wrappedError) Error() string { return "context: " + w.err.Error() }
func (w wrappedError) Unwrap() error { return w.err }
