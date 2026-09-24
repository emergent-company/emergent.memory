package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceScopesMatchCeiling(t *testing.T) {
	cases := []struct {
		name   string
		scopes []string
		want   bool
	}{
		{"exact ceiling matches", []string{"device:api", "agents:read", "data:read"}, true},
		{"reordered ceiling matches", []string{"data:read", "device:api", "agents:read"}, true},
		{"marker only is below ceiling", []string{"device:api"}, false},
		{"missing marker", []string{"agents:read", "data:read"}, false},
		{"extra write scope rejected", []string{"device:api", "agents:read", "data:read", "data:write"}, false},
		{"tampered to admin rejected", []string{"device:api", "admin:all"}, false},
		{"empty rejected", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, deviceScopesMatchCeiling(tc.scopes))
		})
	}
}

// A device:api token must be rejected on every server path outside the device
// surface (voice/read relay + self-introspection). Mint, member, org and write
// surfaces are out even though the exact-set ceiling also bounds the scopes.
func TestRejectDeviceTokenOutsideSurface(t *testing.T) {
	deviceScopes := []string{"device:api", "agents:read", "data:read"}

	cases := []struct {
		name    string
		method  string
		path    string
		scopes  []string
		wantErr bool
	}{
		{"introspection allowed", "GET", "/api/auth/me", deviceScopes, false},
		{"agent definitions allowed", "GET", "/api/projects/p1/agent-definitions", deviceScopes, false},
		{"agent definition detail allowed", "GET", "/api/projects/p1/agent-definitions/a1", deviceScopes, false},
		{"chat stream allowed", "POST", "/api/chat/stream", deviceScopes, false},
		{"conversation list allowed", "GET", "/api/chat/conversations", deviceScopes, false},
		{"conversation history allowed", "GET", "/api/chat/c1/history", deviceScopes, false},
		{"search allowed", "POST", "/api/search/unified", deviceScopes, false},
		{"graph objects search allowed", "GET", "/api/graph/objects/search", deviceScopes, false},

		{"token mint rejected", "POST", "/api/projects/p1/tokens", deviceScopes, true},
		{"device token mint rejected", "POST", "/api/projects/p1/device-tokens", deviceScopes, true},
		{"members rejected", "GET", "/api/projects/p1/members", deviceScopes, true},
		{"orgs rejected", "GET", "/api/orgs", deviceScopes, true},
		{"projects list rejected", "GET", "/api/projects", deviceScopes, true},
		{"agent create rejected", "POST", "/api/projects/p1/agents", deviceScopes, true},
		{"chat conversation create rejected", "POST", "/api/chat/conversations", deviceScopes, true},
		{"graph object write rejected", "POST", "/api/graph/objects", deviceScopes, true},

		{"normal token unaffected", "POST", "/api/projects/p1/tokens", []string{"projects:read"}, false},
		{"normal user token on members unaffected", "GET", "/api/projects/p1/members", []string{"data:read"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rejectDeviceTokenOutsideSurface(tc.method, tc.path, tc.scopes)
			if tc.wantErr {
				require.Error(t, err)
				assert.Equal(t, 403, apperrStatusAuth(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
