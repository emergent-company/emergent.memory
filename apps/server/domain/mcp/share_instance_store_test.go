package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// requireShareInstanceTable skips the test when the dev/test database has not
// had migration 00144 applied. Tests here connect to the configured database
// directly (internal/testutil cannot be imported: it imports this package).
func requireShareInstanceTable(t *testing.T, db bun.IDB) {
	t.Helper()
	var exists bool
	err := db.NewRaw(`SELECT to_regclass('core.mcp_share_instances') IS NOT NULL`).Scan(context.Background(), &exists)
	if err != nil || !exists {
		t.Skip("core.mcp_share_instances not present; run migrations first")
	}
}

// seedShareUserAndToken inserts a user profile and a project-scoped API token,
// returning the token ID.
func seedShareUserAndToken(t *testing.T, db bun.IDB, projectID, name string) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
	`, userID, "zitadel-"+userID).Exec(ctx)
	require.NoError(t, err)

	tokenID := uuid.NewString()
	_, err = db.NewRaw(`
		INSERT INTO core.api_tokens (id, user_id, project_id, name, token_hash, token_prefix, scopes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, '{projects:read}'::text[], NOW())
	`, tokenID, userID, projectID, name, "hash-"+tokenID, "emt_"+tokenID[:8]).Exec(ctx)
	require.NoError(t, err)
	return tokenID
}

// TestShareInstanceStoreCRUD exercises the Bun store against the configured
// database. Skipped in -short mode.
func TestShareInstanceStoreCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	db := connectTestDB(t)
	requireShareInstanceTable(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	tokenID := seedShareUserAndToken(t, db, projectID, "bound-"+uuid.NewString())
	legacyTokenID := seedShareUserAndToken(t, db, projectID, "MCP Read-Only Share — "+uuid.NewString())

	store := newShareInstanceStore(db)

	inst := &MCPShareInstance{
		ID:            uuid.NewString(),
		ProjectID:     projectID,
		Name:          "Team A " + uuid.NewString(),
		TokenID:       tokenID,
		AllowedTools:  []string{"entity-search"},
		AllowedAgents: []uuid.UUID{uuid.MustParse(agentA)},
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	require.NoError(t, store.Create(ctx, inst))

	got, err := store.GetByID(ctx, projectID, inst.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, inst.Name, got.Name)
	assert.Equal(t, []string{"entity-search"}, got.AllowedTools)
	require.Len(t, got.AllowedAgents, 1)
	assert.Equal(t, uuid.MustParse(agentA), got.AllowedAgents[0])

	byToken, err := store.GetByTokenID(ctx, tokenID)
	require.NoError(t, err)
	require.NotNil(t, byToken)
	assert.Equal(t, inst.ID, byToken.ID)

	byName, err := store.FindByName(ctx, projectID, got.Name)
	require.NoError(t, err)
	require.NotNil(t, byName)

	list, err := store.ListByProject(ctx, projectID)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	legacy, err := store.ListLegacyTokens(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, legacy, 1)
	assert.Equal(t, legacyTokenID, legacy[0].ID)

	got.AllowedTools = []string{"schema-list"}
	got.AllowedAgents = []uuid.UUID{uuid.MustParse(agentA), uuid.MustParse(agentB)}
	got.UpdatedAt = time.Now().UTC()
	require.NoError(t, store.Update(ctx, got))
	reloaded, err := store.GetByID(ctx, projectID, inst.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"schema-list"}, reloaded.AllowedTools)
	require.Len(t, reloaded.AllowedAgents, 2)

	// Revoke is persisted via Update.
	revoked := time.Now().UTC()
	reloaded.RevokedAt = &revoked
	reloaded.UpdatedAt = revoked
	require.NoError(t, store.Update(ctx, reloaded))
	afterRevoke, err := store.GetByTokenID(ctx, tokenID)
	require.NoError(t, err)
	assert.Nil(t, afterRevoke, "revoked instance no longer resolves by token")
}

// TestListShareInstancesLegacyDB verifies bound instances surface through the
// service layer with the bound token ID excluded from legacy entries.
func TestListShareInstancesLegacyDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	db := connectTestDB(t)
	requireShareInstanceTable(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	tokenID := seedShareUserAndToken(t, db, projectID, "MCP Share: Team "+uuid.NewString())
	store := newShareInstanceStore(db)
	require.NoError(t, store.Create(ctx, &MCPShareInstance{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		Name:      "Team " + uuid.NewString(),
		TokenID:   tokenID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}))

	svc := &Service{shareInstances: store, shareTokens: &fakeTokenSvc{}}
	resp, err := svc.ListShareInstances(ctx, projectID, "user")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Instances)
	assert.Equal(t, 1, resp.Total)
	assert.False(t, resp.Instances[0].IsLegacy)
}
