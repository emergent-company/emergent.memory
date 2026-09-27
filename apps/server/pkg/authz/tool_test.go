package authz

import (
	"strings"
	"testing"
)

// TestAuthorizeToolDecisionMatrix locks the fail-closed decision for every
// combination of tool authority and principal the in-process and transport
// paths produce. Any row that flips unexpectedly is a security regression.
func TestAuthorizeToolDecisionMatrix(t *testing.T) {
	super := ToolAuthority{Name: "trace-list", SuperadminOnly: true}
	agentOnly := ToolAuthority{Name: "web-fetch", AgentOnly: true}
	scoped := ToolAuthority{Name: "entity-query", RequiredScope: "graph:read"}
	adminScoped := ToolAuthority{Name: "provider-models-list", RequiredScope: "admin"}
	plain := ToolAuthority{Name: "project-get", RequiredScope: "graph:read"}

	untrusted := Principal{}
	trusted := Principal{Trusted: true}
	transport := Principal{TransportEnforced: true}
	platform := Principal{Platform: true}
	platformUntrusted := Principal{Platform: true, TransportEnforced: true}

	tests := []struct {
		name string
		auth ToolAuthority
		p    Principal
		want string // "" == allow; otherwise the expected denial substring
	}{
		{name: "superadmin tool denied without platform", auth: super, p: untrusted, want: "superadmin"},
		{name: "superadmin tool denied for trusted non-platform", auth: super, p: trusted, want: "superadmin"},
		{name: "superadmin tool denied for transport non-platform", auth: super, p: transport, want: "superadmin"},
		{name: "superadmin tool allowed with platform", auth: super, p: platform},
		{name: "agent-only denied untrusted", auth: agentOnly, p: untrusted, want: "agent-only"},
		{name: "agent-only allowed trusted", auth: agentOnly, p: trusted},
		{name: "agent-only transport-enforced treated as authorized", auth: agentOnly, p: transport},
		{name: "scoped denied untrusted", auth: scoped, p: untrusted, want: "graph:read"},
		{name: "scoped allowed trusted", auth: scoped, p: trusted},
		{name: "scoped allowed transport", auth: scoped, p: transport},
		{name: "admin scoped denied untrusted", auth: adminScoped, p: untrusted, want: "admin authority"},
		{name: "admin scoped allowed trusted", auth: adminScoped, p: trusted},
		{name: "plain scoped denied untrusted (the mechanism-7 gap)", auth: plain, p: untrusted, want: "graph:read"},
		{name: "transport-enforced bypasses scope", auth: adminScoped, p: platformUntrusted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AuthorizeTool(tt.auth, tt.p)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("AuthorizeTool(%+v, %+v) = %v, want nil", tt.auth, tt.p, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("AuthorizeTool(%+v, %+v) = nil, want denial containing %q", tt.auth, tt.p, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("AuthorizeTool error %q does not contain %q", err.Error(), tt.want)
			}
		})
	}
}
