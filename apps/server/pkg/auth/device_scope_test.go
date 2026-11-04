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
		{"question respond allowed", "POST", "/api/projects/p1/agent-questions/q1/respond", deviceScopes, false},
		{"question cancel allowed", "POST", "/api/projects/p1/agent-questions/q1/cancel", deviceScopes, false},
		{"question respond extra segment rejected", "POST", "/api/projects/p1/agent-questions/q1/respond/x", deviceScopes, true},
		{"question respond missing question rejected", "POST", "/api/projects/p1/agent-questions//respond", deviceScopes, true},
		{"scheduled agents allowed", "GET", "/api/projects/p1/agents", deviceScopes, false},
		{"scheduled runs allowed", "GET", "/api/projects/p1/agents/a1/runs", deviceScopes, false},
		{"tool approvals allowed", "GET", "/api/projects/p1/agent-approvals", deviceScopes, false},
		{"work items allowed", "GET", "/api/projects/p1/work-items", deviceScopes, false},
		{"board runs allowed", "GET", "/api/projects/p1/work-items/runs", deviceScopes, false},
		{"agent detail rejected", "GET", "/api/projects/p1/agents/a1", deviceScopes, true},
		{"agent pending events rejected", "GET", "/api/projects/p1/agents/a1/pending-events", deviceScopes, true},
		{"work item detail rejected", "GET", "/api/projects/p1/work-items/c1", deviceScopes, true},
		{"approval extra segment rejected", "GET", "/api/projects/p1/agent-approvals/x", deviceScopes, true},
		{"search allowed", "POST", "/api/search/unified", deviceScopes, false},
		{"graph objects search allowed", "GET", "/api/graph/objects/search", deviceScopes, false},
		{"voice enablement setting allowed", "GET", "/api/projects/p1/settings/voice/enabled", deviceScopes, false},

		{"conversation archive allowed", "POST", "/api/chat/c1/archive", deviceScopes, false},
		{"conversation unarchive allowed", "POST", "/api/chat/c1/unarchive", deviceScopes, false},
		{"conversation delete allowed", "DELETE", "/api/chat/c1", deviceScopes, false},
		{"conversation archive extra segment rejected", "POST", "/api/chat/c1/archive/extra", deviceScopes, true},
		{"conversation unarchive extra segment rejected", "POST", "/api/chat/c1/unarchive/extra", deviceScopes, true},
		{"conversation archive nested id rejected", "POST", "/api/chat/c1/nested/archive", deviceScopes, true},
		{"conversation archive missing id rejected", "POST", "/api/chat//archive", deviceScopes, true},
		{"conversation lifecycle unknown action rejected", "POST", "/api/chat/c1/delete", deviceScopes, true},
		{"conversation delete wrong method rejected", "POST", "/api/chat/c1", deviceScopes, true},
		{"conversation delete nested rejected", "DELETE", "/api/chat/c1/nested", deviceScopes, true},
		{"conversation message write rejected", "POST", "/api/chat/c1/messages", deviceScopes, true},
		{"conversation delete archive path rejected", "DELETE", "/api/chat/c1/archive", deviceScopes, true},

		{"other voice setting rejected", "GET", "/api/projects/p1/settings/voice/goodbyeText", deviceScopes, true},
		{"non-voice setting rejected", "GET", "/api/projects/p1/settings/budget/limit", deviceScopes, true},
		{"voice setting write rejected", "PUT", "/api/projects/p1/settings/voice/enabled", deviceScopes, true},
		{"voice setting deeper path rejected", "GET", "/api/projects/p1/settings/voice/enabled/extra", deviceScopes, true},
		{"voice setting without project rejected", "GET", "/api/projects//settings/voice/enabled", deviceScopes, true},

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
