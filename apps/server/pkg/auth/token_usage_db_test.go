package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// newDBMiddleware builds the middleware through the production constructor so
// the last_used_at tracker is wired exactly as it is in the server, against a
// throwaway test database.
func newDBMiddleware(t *testing.T, db *bun.DB) *Middleware {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg, err := config.NewConfig(log)
	if err != nil {
		t.Fatalf("config.NewConfig: %v", err)
	}
	return NewMiddleware(MiddlewareParams{
		DB:  db,
		Cfg: cfg,
		Log: log,
	})
}

// seedAPIToken inserts a non-revoked, non-expired emt_* token with user_id NULL
// (ephemeral/sandbox class) and the given SQL scope literal, returning the
// token id and the raw token value to present to validateAPIToken.
func seedAPIToken(t *testing.T, ctx context.Context, db *bun.DB, scopesLiteral string) (id, token string) {
	t.Helper()
	token = "emt_" + uuid.NewString()
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	id = uuid.NewString()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO core.api_tokens (id, user_id, project_id, name, token_hash, token_prefix, scopes)
		VALUES (?, NULL, NULL, ?, ?, ?, `+scopesLiteral+`)`,
		id, "audit-"+id, tokenHash, "emt_test"); err != nil {
		t.Fatalf("seed api token: %v", err)
	}
	return id, token
}

func lastUsedAt(t *testing.T, ctx context.Context, db *bun.DB, id string) *time.Time {
	t.Helper()
	var v *time.Time
	if err := db.NewSelect().Table("core.api_tokens").Column("last_used_at").Where("id = ?", id).Scan(ctx, &v); err != nil {
		t.Fatalf("read last_used_at: %v", err)
	}
	return v
}

// waitLastUsed polls (bounded) for the asynchronous flush to land: the touch is
// fire-and-forget, so the assertion waits rather than reading immediately.
func waitLastUsed(t *testing.T, ctx context.Context, db *bun.DB, id string) *time.Time {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if v := lastUsedAt(t, ctx, db, id); v != nil {
			return v
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil
}

// Fail-first guard for issue #845: before the generalized touch, a successful
// validation of a NON-device emt_* token left last_used_at NULL. After the
// change it must be set. (This is the assertion that fails on the pre-change
// code, which only touched device credentials.)
func TestValidateAPITokenSetsLastUsedAt(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	id, token := seedAPIToken(t, ctx, db, "'{}'")

	m := newDBMiddleware(t, db)

	got, err := m.validateAPIToken(ctx, token)
	require.NoError(t, err, "validation must succeed")
	require.NotNil(t, got)

	if v := waitLastUsed(t, ctx, db, id); v == nil {
		t.Fatal("last_used_at was not set after a successful validation of a non-device token")
	}
}

// The generalized touch must not regress the #857 device-credential path: a
// device:api token at the exact ceiling still gets last_used_at.
func TestValidateAPITokenDeviceCredentialStillTouched(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	id, token := seedAPIToken(t, ctx, db, "ARRAY['device:api','agents:read','data:read']")

	m := newDBMiddleware(t, db)

	got, err := m.validateAPIToken(ctx, token)
	require.NoError(t, err, "device credential at the exact ceiling must validate")
	require.NotNil(t, got)

	if v := waitLastUsed(t, ctx, db, id); v == nil {
		t.Fatal("device credential last_used_at was not set; the #857 device path regressed")
	}
}

// A store error in the touch path must never fail the request: validation
// returns success even when the flush fails.
func TestValidateAPITokenTouchFailureDoesNotFail(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	_, token := seedAPIToken(t, ctx, db, "'{}'")

	m := newDBMiddleware(t, db)
	m.tokenUsage.flushFn = func(_ context.Context, _ string) error {
		return errors.New("injected store failure")
	}

	got, err := m.validateAPIToken(ctx, token)
	require.NoError(t, err, "the touch failure must not fail validation")
	require.NotNil(t, got)
}
