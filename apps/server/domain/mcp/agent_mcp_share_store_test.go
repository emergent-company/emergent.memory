package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// requireAgentShareTable skips the test when the dev/test database has not had
// migration 00145 applied.
func requireAgentShareTable(t *testing.T, db bun.IDB) {
	t.Helper()
	var exists bool
	err := db.NewRaw(`SELECT to_regclass('core.agent_mcp_shares') IS NOT NULL`).Scan(context.Background(), &exists)
	if err != nil || !exists {
		t.Skip("core.agent_mcp_shares not present; run migrations first")
	}
}

// TestAgentMCPShareStoreCRUD exercises the Bun store against the configured
// database. Skipped in -short mode and when no database is reachable.
func TestAgentMCPShareStoreCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	db := connectTestDB(t)
	requireAgentShareTable(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	tokenID := seedShareUserAndToken(t, db, projectID, "agent-share-"+uuid.NewString())
	agentID := uuid.NewString()
	store := newAgentMCPShareStore(db)

	share := &AgentMCPShare{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		AgentID:   agentID,
		TokenID:   tokenID,
		Name:      "Agent Share " + uuid.NewString(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Create(ctx, share))

	got, err := store.GetByID(ctx, projectID, share.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, agentID, got.AgentID)

	byToken, err := store.GetByTokenID(ctx, tokenID)
	require.NoError(t, err)
	require.NotNil(t, byToken)
	assert.Equal(t, share.ID, byToken.ID)

	byName, err := store.FindByName(ctx, projectID, got.Name)
	require.NoError(t, err)
	require.NotNil(t, byName)

	byAgent, err := store.ListByAgent(ctx, projectID, agentID)
	require.NoError(t, err)
	assert.Len(t, byAgent, 1)

	all, err := store.ListByProject(ctx, projectID)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	// Duplicate active name is rejected by the partial unique index.
	dup := &AgentMCPShare{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		AgentID:   agentID,
		TokenID:   tokenID,
		Name:      got.Name,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	dupErr := store.Create(ctx, dup)
	require.Error(t, dupErr)
	var appErr *apperror.Error
	require.True(t, errors.As(dupErr, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)

	// Revoke is persisted via Update and frees the name.
	revoked := time.Now().UTC()
	got.RevokedAt = &revoked
	got.UpdatedAt = revoked
	require.NoError(t, store.Update(ctx, got))
	afterRevoke, err := store.GetByTokenID(ctx, tokenID)
	require.NoError(t, err)
	assert.Nil(t, afterRevoke, "revoked share no longer resolves by token")
}
