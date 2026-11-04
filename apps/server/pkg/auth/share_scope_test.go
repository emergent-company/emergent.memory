package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func apperrStatusAuth(err error) int {
	if e, ok := err.(*apperror.Error); ok {
		return e.HTTPStatus
	}
	return 0
}

// A share:agent-chat token must be rejected on every path outside
// /api/share/agent (defense in depth — the key also carries no other scopes).
func TestRejectShareTokenOutsideSurface(t *testing.T) {
	shareScopes := []string{shareAgentChatScope}

	cases := []struct {
		name    string
		path    string
		scopes  []string
		wantErr bool
	}{
		{"projects list rejected", "/api/projects", shareScopes, true},
		{"project members rejected", "/api/projects/abc-123/members", shareScopes, true},
		{"graph read rejected", "/api/graph/objects", shareScopes, true},
		{"share surface allowed", "/api/share/agent", shareScopes, false},
		{"share sessions allowed", "/api/share/agent/sessions", shareScopes, false},
		{"normal token unaffected", "/api/projects", []string{"projects:read"}, false},
		{"normal token share path unaffected", "/api/projects/abc/members", []string{"data:read"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rejectShareTokenOutsideSurface(tc.path, tc.scopes)
			if tc.wantErr {
				require.Error(t, err)
				assert.Equal(t, 403, apperrStatusAuth(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
