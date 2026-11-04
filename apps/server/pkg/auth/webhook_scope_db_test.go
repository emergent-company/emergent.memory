package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// webhookCeilingLiteral is the exact ceiling as a SQL array literal, matching
// the server's mint-time set.
const webhookCeilingLiteral = "ARRAY['webhook:trigger','agents:read','agents:write','data:read']"

// A webhook:trigger credential at the exact ceiling validates (use path).
func TestValidateAPITokenWebhookAtCeilingSucceeds(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	_, token := seedAPIToken(t, ctx, db, webhookCeilingLiteral)

	m := newDBMiddleware(t, db)
	got, err := m.validateAPIToken(ctx, token)
	require.NoError(t, err, "a webhook credential at the exact ceiling must validate")
	require.NotNil(t, got)
	require.ElementsMatch(t, webhookTriggerScopes, got.Scopes)
}

// A DB-tampered webhook:trigger credential whose scope set was widened beyond
// the ceiling must be rejected fail-closed, never trusted.
func TestValidateAPITokenWebhookTamperedCeilingRejected(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	// The marker plus an extra write scope: outside the exact ceiling.
	_, token := seedAPIToken(t, ctx, db, "ARRAY['webhook:trigger','agents:read','agents:write','data:read','data:write']")

	m := newDBMiddleware(t, db)
	got, err := m.validateAPIToken(ctx, token)
	require.Error(t, err, "a tampered webhook credential must be rejected")
	require.Nil(t, got)
	if apperrStatusAuth(err) != 403 {
		t.Fatalf("tampered credential error = %v, want 403", err)
	}
}

// A webhook:trigger credential carrying an admin scope is rejected even when
// the marker is present (tampered to full write).
func TestValidateAPITokenWebhookAdminScopeRejected(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	_, token := seedAPIToken(t, ctx, db, "ARRAY['webhook:trigger','admin:all']")

	m := newDBMiddleware(t, db)
	_, err := m.validateAPIToken(ctx, token)
	require.Error(t, err, "a webhook credential tampered to admin:all must be rejected")
}

// Revocation cuts access: a revoked webhook credential is rejected.
func TestValidateAPITokenWebhookRevokedRejected(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	id, token := seedAPIToken(t, ctx, db, webhookCeilingLiteral)
	if _, err := db.ExecContext(ctx, `UPDATE core.api_tokens SET revoked_at = NOW() WHERE id = ?`, id); err != nil {
		t.Fatalf("revoke token: %v", err)
	}

	m := newDBMiddleware(t, db)
	_, err := m.validateAPIToken(ctx, token)
	require.Error(t, err, "a revoked webhook credential must be rejected")
}

// Expiry cuts access: an expired webhook credential is rejected.
func TestValidateAPITokenWebhookExpiredRejected(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	id, token := seedAPIToken(t, ctx, db, webhookCeilingLiteral)
	if _, err := db.ExecContext(ctx, `UPDATE core.api_tokens SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = ?`, id); err != nil {
		t.Fatalf("expire token: %v", err)
	}

	m := newDBMiddleware(t, db)
	_, err := m.validateAPIToken(ctx, token)
	require.Error(t, err, "an expired webhook credential must be rejected")
}

// An absent (unknown) credential is denied: validateAPIToken fails closed with
// an invalid-token error, never a pass-through.
func TestValidateAPITokenWebhookAbsentRejected(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	m := newDBMiddleware(t, db)
	_, err := m.validateAPIToken(ctx, "emt_"+uuid.NewString())
	require.Error(t, err, "an unknown credential must be denied")
	if apperrStatusAuth(err) != 401 {
		t.Fatalf("unknown credential error = %v, want 401", err)
	}
}

// Per-integration isolation at the validation layer: revoking integration X's
// credential does not affect integration Y's.
func TestValidateAPITokenWebhookIsolation(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	idX, tokenX := seedAPIToken(t, ctx, db, webhookCeilingLiteral)
	_, tokenY := seedAPIToken(t, ctx, db, webhookCeilingLiteral)

	if _, err := db.ExecContext(ctx, `UPDATE core.api_tokens SET revoked_at = NOW() WHERE id = ?`, idX); err != nil {
		t.Fatalf("revoke X: %v", err)
	}

	m := newDBMiddleware(t, db)
	if _, err := m.validateAPIToken(ctx, tokenX); err == nil {
		t.Fatal("revoked integration X credential must be rejected")
	}
	if got, err := m.validateAPIToken(ctx, tokenY); err != nil || got == nil {
		t.Fatalf("integration Y credential must remain valid after X is revoked: %v", err)
	}
}

// Store-unavailable must deny, never pass through: when the backing token
// store cannot be queried, validation returns an error rather than a user.
func TestValidateAPITokenWebhookStoreUnavailableDenies(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	m := newDBMiddleware(t, db)

	// Drop the token table so the lookup errors rather than returning no rows.
	if _, err := db.ExecContext(ctx, `DROP TABLE core.api_tokens`); err != nil {
		t.Fatalf("drop api_tokens: %v", err)
	}

	_, err := m.validateAPIToken(ctx, "emt_"+uuid.NewString())
	require.Error(t, err, "a store failure must deny, never pass through")
}
