package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookScopesMatchCeiling(t *testing.T) {
	cases := []struct {
		name   string
		scopes []string
		want   bool
	}{
		{"exact ceiling matches", []string{"webhook:trigger", "agents:read", "agents:write", "data:read"}, true},
		{"reordered ceiling matches", []string{"data:read", "webhook:trigger", "agents:write", "agents:read"}, true},
		{"marker only is below ceiling", []string{"webhook:trigger"}, false},
		{"missing marker", []string{"agents:read", "agents:write", "data:read"}, false},
		{"extra write scope rejected", []string{"webhook:trigger", "agents:read", "agents:write", "data:read", "data:write"}, false},
		{"tampered to admin rejected", []string{"webhook:trigger", "admin:all"}, false},
		{"tampered marker with project write rejected", []string{"webhook:trigger", "agents:read", "agents:write", "projects:write"}, false},
		{"empty rejected", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, webhookScopesMatchCeiling(tc.scopes))
		})
	}
}

// A webhook:trigger token must be rejected on every server path outside the
// webhook trigger surface (the trigger route plus its query loopback). Mint,
// member, org, chat, graph-write and skills surfaces are out even though the
// exact-set ceiling also bounds the scopes.
func TestRejectWebhookTokenOutsideSurface(t *testing.T) {
	webhookScopes := []string{"webhook:trigger", "agents:read", "agents:write", "data:read"}

	cases := []struct {
		name    string
		method  string
		path    string
		scopes  []string
		wantErr bool
	}{
		{"trigger route allowed", "POST", "/api/projects/p1/agents/a1/trigger", webhookScopes, false},
		{"query loopback allowed", "POST", "/api/projects/p1/query", webhookScopes, false},

		{"token mint rejected", "POST", "/api/projects/p1/tokens", webhookScopes, true},
		{"device token mint rejected", "POST", "/api/projects/p1/device-tokens", webhookScopes, true},
		{"members rejected", "GET", "/api/projects/p1/members", webhookScopes, true},
		{"orgs rejected", "GET", "/api/orgs", webhookScopes, true},
		{"projects list rejected", "GET", "/api/projects", webhookScopes, true},
		{"agent create rejected", "POST", "/api/projects/p1/agents", webhookScopes, true},
		{"agent update rejected", "PATCH", "/api/projects/p1/agents/a1", webhookScopes, true},
		{"agent delete rejected", "DELETE", "/api/projects/p1/agents/a1", webhookScopes, true},
		{"skills write rejected", "POST", "/api/projects/p1/skills", webhookScopes, true},
		{"chat stream rejected", "POST", "/api/chat/stream", webhookScopes, true},
		{"chat admin rejected", "POST", "/api/chat/conversations", webhookScopes, true},
		{"graph object write rejected", "POST", "/api/graph/objects", webhookScopes, true},
		{"graph read rejected", "GET", "/api/graph/objects/search", webhookScopes, true},
		{"search unified rejected", "POST", "/api/search/unified", webhookScopes, true},

		{"normal token unaffected", "POST", "/api/projects/p1/tokens", []string{"projects:read"}, false},
		{"normal user token on members unaffected", "GET", "/api/projects/p1/members", []string{"data:read"}, false},
		{"device token unaffected by webhook guard", "POST", "/api/chat/stream", []string{"device:api", "agents:read", "data:read"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rejectWebhookTokenOutsideSurface(tc.method, tc.path, tc.scopes)
			if tc.wantErr {
				require.Error(t, err)
				assert.Equal(t, 403, apperrStatusAuth(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Effective-scope accuracy: the webhook credential's stored set includes two
// umbrella scopes (agents:write and data:read) whose ScopeImplies expansion the
// spec must not understate. This test pins the EFFECTIVE set so any future
// ScopeImplies change breaks loudly rather than silently widening/claiming a
// scope the credential does (or does not) carry.
func TestWebhookTriggerEffectiveScopeExpansion(t *testing.T) {
	eff := ExpandScopes(webhookTriggerScopes)

	// Stored ceiling members are always present.
	for _, want := range []string{"webhook:trigger", "agents:read", "agents:write", "data:read"} {
		if !eff[want] {
			t.Errorf("effective set missing stored scope %q", want)
		}
	}

	// agents:write umbrella implies chat:admin and skills:write — the webhook
	// credential's write surface. These are reachable ONLY through the review
	// agent's loopback because the surface guard confines the credential to the
	// trigger + query routes.
	for _, want := range []string{"chat:admin", "skills:write"} {
		if !eff[want] {
			t.Errorf("effective set missing agents:write umbrella %q", want)
		}
	}

	// agents:read umbrella implies chat:use and skills:read.
	for _, want := range []string{"chat:use", "skills:read"} {
		if !eff[want] {
			t.Errorf("effective set missing agents:read umbrella %q", want)
		}
	}

	// data:read umbrella implies the read family (notably search, graph:read,
	// schema:read, journal:read).
	for _, want := range []string{"search", "graph:read", "schema:read", "journal:read", "documents:read"} {
		if !eff[want] {
			t.Errorf("effective set missing data:read umbrella %q", want)
		}
	}

	// The webhook marker itself is a pure marker: it is not an umbrella and
	// implies nothing.
	if len(ScopeImplies[webhookTriggerScope]) != 0 {
		t.Errorf("webhook:trigger is a marker and must not imply any scope, got %v", ScopeImplies[webhookTriggerScope])
	}
}
