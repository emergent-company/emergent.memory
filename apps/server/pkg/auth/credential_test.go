package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestCredentialForUser locks the principal → credential-kind mapping used by the
// write stamp (#2376). A wrong mapping here silently attributes writes to the
// wrong kind of credential in every graph mutation.
func TestCredentialForUser(t *testing.T) {
	tests := []struct {
		name string
		user *AuthUser
		want string
	}{
		{name: "nil user is no credential", user: nil, want: CredentialNone},
		{name: "API token", user: &AuthUser{ID: "u1", APITokenID: "0f2b6a2e-0000-0000-0000-000000000000"}, want: CredentialAPIKey},
		{name: "human session", user: &AuthUser{ID: "u1"}, want: CredentialSession},
		{
			// A per-agent MCP endpoint key: the credential an MCP client presents
			// when its tool calls write graph rows.
			name: "per-agent MCP endpoint key",
			user: &AuthUser{ID: "u1", APITokenID: "0f2b6a2e-0000-0000-0000-000000000000", Scopes: []string{agentCallScope, "projects:read"}},
			want: CredentialMCPLink,
		},
		{
			// A public agent-share chat link is an API token but is not MCP; the
			// marker scope must not be confused with mcp:agent-call.
			name: "agent-share chat link",
			user: &AuthUser{ID: "u1", APITokenID: "0f2b6a2e-0000-0000-0000-000000000000", Scopes: []string{shareAgentChatScope}},
			want: CredentialAPIKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CredentialForUser(tt.user); got != tt.want {
				t.Errorf("expected credential kind %q, got %q", tt.want, got)
			}
		})
	}
}

// TestInjectAuthContext_StampsCredential covers the single choke point where the
// request context is built: an emt_* token is recorded as an API-key credential
// with its token id, and a human session is recorded with no credential id.
func TestInjectAuthContext_StampsCredential(t *testing.T) {
	tokenID := uuid.New()

	ctx := InjectAuthContext(context.Background(), &AuthUser{
		ID:         "user-1",
		ProjectID:  uuid.NewString(),
		APITokenID: tokenID.String(),
	})

	kind, id, ok := CredentialFromContext(ctx)
	if !ok {
		t.Fatal("expected a stamped credential, got none")
	}
	if kind != CredentialAPIKey {
		t.Errorf("expected credential kind %q, got %q", CredentialAPIKey, kind)
	}
	if id == nil || *id != tokenID {
		t.Errorf("expected credential id %s, got %v", tokenID, id)
	}
}

// TestInjectAuthContext_SessionHasNoCredentialID covers a human session: the
// kind is recorded as session and no credential id is invented.
func TestInjectAuthContext_SessionHasNoCredentialID(t *testing.T) {
	ctx := InjectAuthContext(context.Background(), &AuthUser{ID: "user-1"})

	kind, id, ok := CredentialFromContext(ctx)
	if !ok {
		t.Fatal("expected a stamped credential, got none")
	}
	if kind != CredentialSession {
		t.Errorf("expected credential kind %q, got %q", CredentialSession, kind)
	}
	if id != nil {
		t.Errorf("expected no credential id for a session, got %v", id)
	}
}

// TestInjectAuthContext_MalformedTokenIDDoesNotFail covers a token id that is not
// a UUID: auth must not fail the request over a stamp it cannot complete, and the
// stamp must not carry a fabricated id.
func TestInjectAuthContext_MalformedTokenIDDoesNotFail(t *testing.T) {
	ctx := InjectAuthContext(context.Background(), &AuthUser{ID: "user-1", APITokenID: "not-a-uuid"})

	kind, id, _ := CredentialFromContext(ctx)
	if kind != CredentialAPIKey {
		t.Errorf("expected credential kind %q, got %q", CredentialAPIKey, kind)
	}
	if id != nil {
		t.Errorf("expected no credential id for a malformed token id, got %v", id)
	}
}

// TestCredentialFromContext_AbsentIsNone covers a background/internal context:
// nothing stamped resolves to CredentialNone, never to an empty string, so a
// write stamp can be stored as a NOT NULL column.
func TestCredentialFromContext_AbsentIsNone(t *testing.T) {
	kind, id, ok := CredentialFromContext(context.Background())
	if ok {
		t.Error("expected ok=false when nothing was stamped")
	}
	if kind != CredentialNone {
		t.Errorf("expected credential kind %q, got %q", CredentialNone, kind)
	}
	if id != nil {
		t.Errorf("expected nil credential id, got %v", id)
	}
}

// TestContextWithCredential_EmptyKindIsNone covers the normalisation that keeps
// the stored stamp non-empty.
func TestContextWithCredential_EmptyKindIsNone(t *testing.T) {
	ctx := ContextWithCredential(context.Background(), "", nil)

	kind, _, _ := CredentialFromContext(ctx)
	if kind != CredentialNone {
		t.Errorf("expected credential kind %q, got %q", CredentialNone, kind)
	}
}

// TestRunIDsRoundTrip covers the run half of the stamp.
func TestRunIDsRoundTrip(t *testing.T) {
	runID := uuid.New()
	rootRunID := uuid.New()
	ctx := ContextWithRunIDs(context.Background(), &runID, &rootRunID)

	gotRun, gotRoot := RunIDsFromContext(ctx)
	if gotRun == nil || *gotRun != runID {
		t.Errorf("expected run id %s, got %v", runID, gotRun)
	}
	if gotRoot == nil || *gotRoot != rootRunID {
		t.Errorf("expected root run id %s, got %v", rootRunID, gotRoot)
	}
}

// TestRunIDsAbsent covers a write outside any run.
func TestRunIDsAbsent(t *testing.T) {
	runID, rootRunID := RunIDsFromContext(context.Background())
	if runID != nil || rootRunID != nil {
		t.Errorf("expected no run ids, got %v/%v", runID, rootRunID)
	}
}
