package apitoken

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// seedProjectForWebhook creates an org + project so the webhook token's
// project_id foreign key is satisfiable.
func seedProjectForWebhook(t *testing.T, db bun.IDB) string {
	t.Helper()
	ctx := context.Background()
	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Org "+orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "Project "+projectID)
	require.NoError(t, err)
	return projectID
}

func newWebhookTestService(t *testing.T, db *bun.DB) *Service {
	t.Helper()
	repo := NewRepository(db, slog.Default())
	return NewService(db, repo, nil, slog.Default())
}

// CreateWebhookTriggerToken must mint a project-scoped credential with the
// exact hardcoded ceiling, a NULL user_id, the integration name, and the
// operator expiry — and no caller-supplied scopes are accepted.
func TestCreateWebhookTriggerTokenMintsExactCeiling(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	svc := newWebhookTestService(t, db)

	projectID := seedProjectForWebhook(t, db)
	name := "github-pr-review-webhook"
	expiresAt := time.Now().Add(24 * time.Hour)

	dto, err := svc.CreateWebhookTriggerToken(ctx, projectID, name, &expiresAt)
	require.NoError(t, err)
	require.NotNil(t, dto)
	require.NotEmpty(t, dto.Token, "the raw token is returned exactly once at mint")

	// The stored scopes are EXACTLY the ceiling (same members, same cardinality).
	require.ElementsMatch(t, webhookTriggerScopes, dto.Scopes)
	require.Equal(t, name, dto.Name)
	require.Equal(t, projectID, *dto.ProjectID)

	// user_id must be NULL: an integration is not a user.
	row, err := svc.repo.FindByHash(ctx, hashToken(dto.Token))
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Nil(t, row.UserID, "webhook credential must have user_id NULL")
	require.NotNil(t, row.ExpiresAt, "webhook credential must carry an expiry")
}

// Per-integration identity: each integration has its own credential row. A
// second integration's credential must not collide with the first, and
// revoking one must not affect the other.
func TestWebhookTriggerPerIntegrationIsolation(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	svc := newWebhookTestService(t, db)

	projectID := seedProjectForWebhook(t, db)
	expiresAt := time.Now().Add(24 * time.Hour)

	x, err := svc.CreateWebhookTriggerToken(ctx, projectID, "integration-x", &expiresAt)
	require.NoError(t, err)
	y, err := svc.CreateWebhookTriggerToken(ctx, projectID, "integration-y", &expiresAt)
	require.NoError(t, err)

	require.NotEqual(t, x.Token, y.Token, "distinct integrations must get distinct credentials")
	require.NotEqual(t, x.ID, y.ID)

	// Revoke X only.
	require.NoError(t, svc.Revoke(ctx, x.ID, projectID, ""))

	// X is now gone (revoked); Y is untouched.
	rowX, err := svc.repo.FindByHash(ctx, hashToken(x.Token))
	require.NoError(t, err)
	require.NotNil(t, rowX)
	require.NotNil(t, rowX.RevokedAt, "integration X must be revoked")

	rowY, err := svc.repo.FindByHash(ctx, hashToken(y.Token))
	require.NoError(t, err)
	require.NotNil(t, rowY)
	require.Nil(t, rowY.RevokedAt, "integration Y must remain active")
}

// Rotation reuses the existing Regenerate path: regenerating a webhook
// credential atomically revokes the old token and mints a new one with the
// same scopes and name.
func TestWebhookTriggerRegeneratePreservesCeiling(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	svc := newWebhookTestService(t, db)

	projectID := seedProjectForWebhook(t, db)
	expiresAt := time.Now().Add(24 * time.Hour)

	dto, err := svc.CreateWebhookTriggerToken(ctx, projectID, "github-pr-review-webhook", &expiresAt)
	require.NoError(t, err)

	regenerated, err := svc.Regenerate(ctx, dto.ID, projectID, "")
	require.NoError(t, err)
	require.NotEmpty(t, regenerated.Token)
	require.NotEqual(t, dto.Token, regenerated.Token)
	require.ElementsMatch(t, webhookTriggerScopes, regenerated.Scopes)
	require.Equal(t, "github-pr-review-webhook", regenerated.Name)

	// The old token is revoked.
	old, err := svc.repo.FindByHash(ctx, hashToken(dto.Token))
	require.NoError(t, err)
	require.NotNil(t, old.RevokedAt, "the pre-rotation token must be revoked")
}
