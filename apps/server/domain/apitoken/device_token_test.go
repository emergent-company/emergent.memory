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

func newDeviceTestService(t *testing.T, db *bun.DB) *Service {
	t.Helper()
	repo := NewRepository(db, slog.Default())
	return NewService(db, repo, nil, slog.Default())
}

// CreateDeviceToken must mint a project-scoped credential owned by the
// provisioning user so the device can read that user's private conversations.
// The stored scope set stays exactly the hardcoded device ceiling.
func TestCreateDeviceTokenPersistsProvisionerAndCeiling(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	svc := newDeviceTestService(t, db)

	projectID := seedProjectForWebhook(t, db)
	ownerID := uuid.NewString()
	seedUser(t, db, ownerID)

	expiresAt := time.Now().Add(24 * time.Hour)
	dto, err := svc.CreateDeviceToken(ctx, projectID, ownerID, "my-iphone", &expiresAt)
	require.NoError(t, err)
	require.NotNil(t, dto)
	require.NotEmpty(t, dto.Token, "the raw token is returned exactly once at mint")

	require.ElementsMatch(t, []string{deviceAPIScope, "agents:read", "data:read"}, dto.Scopes)
	require.Equal(t, "my-iphone", dto.Name)
	require.Equal(t, projectID, *dto.ProjectID)

	row, err := svc.repo.FindByHash(ctx, hashToken(dto.Token))
	require.NoError(t, err)
	require.NotNil(t, row)
	require.NotNil(t, row.UserID, "device credential must be owned by the provisioner")
	require.Equal(t, ownerID, *row.UserID)
	require.ElementsMatch(t, []string{deviceAPIScope, "agents:read", "data:read"}, row.Scopes)
	require.NotNil(t, row.ExpiresAt)
}
