package mcp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// requireAgentMCPEndpointTables skips the test when the dev/test database has
// not had migration 00152 applied.
func requireAgentMCPEndpointTables(t *testing.T, db bun.IDB) {
	t.Helper()
	var exists bool
	err := db.NewRaw(`SELECT to_regclass('core.agent_mcp_endpoints') IS NOT NULL
		AND to_regclass('core.agent_mcp_keys') IS NOT NULL
		AND to_regclass('core.agent_mcp_sessions') IS NOT NULL`).Scan(context.Background(), &exists)
	if err != nil || !exists {
		t.Skip("core.agent_mcp_* tables not present; run migrations first")
	}
}

// seedAgent inserts a minimal kb.agents row (agent_mcp_endpoints.agent_id has an
// FK to it) and returns the agent ID.
func seedAgent(t *testing.T, db bun.IDB, projectID string) string {
	t.Helper()
	ctx := context.Background()
	agentID := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, trigger_type, enabled)
		VALUES (?, ?, ?, 'manual', '0 0 * * *', 'manual', true)
	`, agentID, projectID, "agent-"+agentID).Exec(ctx)
	require.NoError(t, err)
	return agentID
}

// TestAgentMCPEndpointStoreCRUD exercises the endpoint store against the
// configured database. Skipped in -short mode and when no database is reachable.
func TestAgentMCPEndpointStoreCRUD(t *testing.T) {
	db := connectTestDB(t)
	requireAgentMCPEndpointTables(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	agentID := seedAgent(t, db, projectID)

	store := newAgentMCPEndpointStore(db)

	ep := &AgentMCPEndpoint{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		AgentID:   agentID,
	}
	require.NoError(t, store.CreateEndpoint(ctx, ep))

	got, err := store.GetEndpointByID(ctx, ep.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, agentID, got.AgentID)
	assert.Nil(t, got.RevokedAt)

	active, err := store.GetActiveEndpointByAgentID(ctx, projectID, agentID)
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.Equal(t, ep.ID, active.ID)

	// A second active endpoint for the same agent is rejected by the partial
	// unique index.
	dupErr := store.CreateEndpoint(ctx, &AgentMCPEndpoint{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		AgentID:   agentID,
	})
	require.Error(t, dupErr)
	var appErr *apperror.Error
	require.True(t, errors.As(dupErr, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)

	// Touch bumps updated_at.
	touchedAt := time.Now().UTC().Add(2 * time.Second)
	require.NoError(t, store.TouchEndpoint(ctx, ep.ID, touchedAt))
	reloaded, err := store.GetEndpointByID(ctx, ep.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.WithinDuration(t, touchedAt, reloaded.UpdatedAt, time.Second)

	// Revoke removes it from the active lookup and is idempotent.
	revokedAt := time.Now().UTC()
	require.NoError(t, store.RevokeEndpoint(ctx, ep.ID, revokedAt))
	require.NoError(t, store.RevokeEndpoint(ctx, ep.ID, revokedAt), "revoke is idempotent")

	afterRevoke, err := store.GetActiveEndpointByAgentID(ctx, projectID, agentID)
	require.NoError(t, err)
	assert.Nil(t, afterRevoke, "revoked endpoint no longer active")

	// GetEndpointByID still returns the revoked row.
	stillThere, err := store.GetEndpointByID(ctx, ep.ID)
	require.NoError(t, err)
	require.NotNil(t, stillThere)
	assert.NotNil(t, stillThere.RevokedAt)
}

// TestAgentMCPKeyStoreCRUD exercises the key store, including active-only lookup,
// per-endpoint label uniqueness, and rotation preserving the key id.
func TestAgentMCPKeyStoreCRUD(t *testing.T) {
	db := connectTestDB(t)
	requireAgentMCPEndpointTables(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	agentID := seedAgent(t, db, projectID)

	epStore := newAgentMCPEndpointStore(db)
	keyStore := newAgentMCPKeyStore(db)

	ep := &AgentMCPEndpoint{ID: uuid.NewString(), ProjectID: projectID, AgentID: agentID}
	require.NoError(t, epStore.CreateEndpoint(ctx, ep))

	tokenID := seedShareUserAndToken(t, db, projectID, "key-"+uuid.NewString())
	createdBy := uuid.NewString()
	key := &AgentMCPKey{
		ID:         uuid.NewString(),
		EndpointID: ep.ID,
		TokenID:    tokenID,
		Label:      "laptop-" + uuid.NewString(),
		CreatedBy:  &createdBy,
	}
	require.NoError(t, keyStore.CreateKey(ctx, key))

	byToken, err := keyStore.GetActiveKeyByTokenID(ctx, tokenID)
	require.NoError(t, err)
	require.NotNil(t, byToken)
	assert.Equal(t, key.ID, byToken.ID)
	assert.Equal(t, ep.ID, byToken.EndpointID)
	assert.Equal(t, key.Label, byToken.Label)

	// Read model joins endpoint identity and token timestamps.
	list, err := keyStore.ListKeysByEndpoint(ctx, ep.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, key.ID, list[0].ID)
	assert.Equal(t, key.Label, list[0].Label)
	assert.Equal(t, agentID, list[0].EndpointAgentID)
	assert.Equal(t, projectID, list[0].EndpointProjectID)

	// A second key with a distinct label is allowed on the same endpoint.
	secondTokenID := seedShareUserAndToken(t, db, projectID, "key2-"+uuid.NewString())
	secondKey := &AgentMCPKey{
		ID:         uuid.NewString(),
		EndpointID: ep.ID,
		TokenID:    secondTokenID,
		Label:      "tablet-" + uuid.NewString(),
	}
	require.NoError(t, keyStore.CreateKey(ctx, secondKey))

	// Case-insensitive duplicate label on the same endpoint is rejected.
	dupTokenID := seedShareUserAndToken(t, db, projectID, "key4-"+uuid.NewString())
	dupErr := keyStore.CreateKey(ctx, &AgentMCPKey{
		ID:         uuid.NewString(),
		EndpointID: ep.ID,
		TokenID:    dupTokenID,
		Label:      strings.ToUpper(key.Label),
	})
	require.Error(t, dupErr)
	var appErr *apperror.Error
	require.True(t, errors.As(dupErr, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)

	// Rotation repoints token_id but keeps key.id.
	rotatedTokenID := seedShareUserAndToken(t, db, projectID, "key3-"+uuid.NewString())
	require.NoError(t, keyStore.SetKeyToken(ctx, key.ID, rotatedTokenID, time.Now().UTC()))

	rotated, err := keyStore.GetActiveKeyByTokenID(ctx, rotatedTokenID)
	require.NoError(t, err)
	require.NotNil(t, rotated)
	assert.Equal(t, key.ID, rotated.ID, "rotation preserves the key id")
	assert.Equal(t, rotatedTokenID, rotated.TokenID)

	oldToken, err := keyStore.GetActiveKeyByTokenID(ctx, tokenID)
	require.NoError(t, err)
	assert.Nil(t, oldToken, "old credential no longer resolves the key")

	// Revocation makes active lookup return not-found and is idempotent.
	revokedAt := time.Now().UTC()
	require.NoError(t, keyStore.RevokeKey(ctx, key.ID, revokedAt))
	require.NoError(t, keyStore.RevokeKey(ctx, key.ID, revokedAt), "revoke is idempotent")

	afterRevoke, err := keyStore.GetActiveKeyByTokenID(ctx, rotatedTokenID)
	require.NoError(t, err)
	assert.Nil(t, afterRevoke, "revoked key no longer resolves by token")

	// Both keys remain listable: the revoked one with its label, the active one
	// still resolvable.
	all, err := keyStore.ListKeysByEndpoint(ctx, ep.ID)
	require.NoError(t, err)
	require.Len(t, all, 2, "revoked key remains listable")
	activeSecond, err := keyStore.GetActiveKeyByTokenID(ctx, secondTokenID)
	require.NoError(t, err)
	require.NotNil(t, activeSecond)
	assert.Equal(t, secondKey.ID, activeSecond.ID)
}

// TestAgentMCPSessionStoreCRUD exercises the session store: create/get-by-ref,
// list-by-key, status transitions, counter bumps, and key-scoped queryability
// after the owning key is revoked.
func TestAgentMCPSessionStoreCRUD(t *testing.T) {
	db := connectTestDB(t)
	requireAgentMCPEndpointTables(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	agentID := seedAgent(t, db, projectID)

	epStore := newAgentMCPEndpointStore(db)
	keyStore := newAgentMCPKeyStore(db)
	sessStore := newAgentMCPSessionStore(db)

	ep := &AgentMCPEndpoint{ID: uuid.NewString(), ProjectID: projectID, AgentID: agentID}
	require.NoError(t, epStore.CreateEndpoint(ctx, ep))
	tokenID := seedShareUserAndToken(t, db, projectID, "sess-"+uuid.NewString())
	key := &AgentMCPKey{ID: uuid.NewString(), EndpointID: ep.ID, TokenID: tokenID, Label: "session-key"}
	require.NoError(t, keyStore.CreateKey(ctx, key))

	sessionRef := uuid.NewString()
	sess := &AgentMCPSession{
		ID:         uuid.NewString(),
		EndpointID: ep.ID,
		KeyID:      key.ID,
		SessionRef: sessionRef,
	}
	require.NoError(t, sessStore.CreateSession(ctx, sess))
	assert.Equal(t, AgentMCPSessionStatusActive, sess.Status)

	got, err := sessStore.GetSessionByRef(ctx, sessionRef)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, key.ID, got.KeyID, "session carries key_id for key-scoped auth")
	assert.Equal(t, ep.ID, got.EndpointID)

	list, err := sessStore.ListSessionsByKey(ctx, key.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, sessionRef, list[0].SessionRef)

	// Status transitions.
	require.NoError(t, sessStore.SetSessionStatus(ctx, sess.ID, "running"))
	got, err = sessStore.GetSessionByRef(ctx, sessionRef)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "running", got.Status)

	// Touch increments turn_count/total_steps and records the run.
	runID := uuid.NewString()
	at := time.Now().UTC()
	require.NoError(t, sessStore.TouchSession(ctx, sess.ID, 4, &runID, at))
	got, err = sessStore.GetSessionByRef(ctx, sessionRef)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, got.TurnCount)
	assert.Equal(t, 4, got.TotalSteps)
	require.NotNil(t, got.LastRunID)
	assert.Equal(t, runID, *got.LastRunID)

	require.NoError(t, sessStore.TouchSession(ctx, sess.ID, 3, &runID, at))
	got, err = sessStore.GetSessionByRef(ctx, sessionRef)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 2, got.TurnCount)
	assert.Equal(t, 7, got.TotalSteps)

	// Revoking the owning key does not make its sessions unqueryable by key_id.
	require.NoError(t, keyStore.RevokeKey(ctx, key.ID, time.Now().UTC()))
	afterRevoke, err := sessStore.GetSessionByRef(ctx, sessionRef)
	require.NoError(t, err)
	require.NotNil(t, afterRevoke)
	assert.Equal(t, key.ID, afterRevoke.KeyID)

	byKey, err := sessStore.ListSessionsByKey(ctx, key.ID)
	require.NoError(t, err)
	assert.Len(t, byKey, 1)
}

// TestAgentMCPKeyStoreRejectsDuplicateTokenBinding proves the global
// core.agent_mcp_keys.token_id uniqueness: one credential cannot be bound to a
// second key, on the same or a different endpoint.
func TestAgentMCPKeyStoreRejectsDuplicateTokenBinding(t *testing.T) {
	db := connectTestDB(t)
	requireAgentMCPEndpointTables(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	epStore := newAgentMCPEndpointStore(db)
	keyStore := newAgentMCPKeyStore(db)

	ep1 := &AgentMCPEndpoint{ID: uuid.NewString(), ProjectID: projectID, AgentID: seedAgent(t, db, projectID)}
	require.NoError(t, epStore.CreateEndpoint(ctx, ep1))
	ep2 := &AgentMCPEndpoint{ID: uuid.NewString(), ProjectID: projectID, AgentID: seedAgent(t, db, projectID)}
	require.NoError(t, epStore.CreateEndpoint(ctx, ep2))

	tokenID := seedShareUserAndToken(t, db, projectID, "bind-"+uuid.NewString())
	require.NoError(t, keyStore.CreateKey(ctx, &AgentMCPKey{
		ID: uuid.NewString(), EndpointID: ep1.ID, TokenID: tokenID, Label: "first-" + uuid.NewString(),
	}))

	// Same credential, different endpoint, different label: token_id UNIQUE wins.
	dupErr := keyStore.CreateKey(ctx, &AgentMCPKey{
		ID: uuid.NewString(), EndpointID: ep2.ID, TokenID: tokenID, Label: "second-" + uuid.NewString(),
	})
	require.Error(t, dupErr)
	var appErr *apperror.Error
	require.True(t, errors.As(dupErr, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)
	assert.Equal(t, "agent_mcp_key_token_exists", appErr.Code)
}

// seedProjectAdmin inserts a user_profile plus a kb.project_memberships
// project_admin row, matching what apitoken.Repository.GetUserProjectRole reads.
func seedProjectAdmin(t *testing.T, db bun.IDB, projectID string) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
	`, userID, "zitadel-"+userID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`
		INSERT INTO kb.project_memberships (project_id, user_id, role)
		VALUES (?, ?, 'project_admin')
	`, projectID, userID).Exec(ctx)
	require.NoError(t, err)
	return userID
}

// TestRotateAgentKeyRealRegenerate drives key rotation through the real
// apitoken.Service.RegenerateWith path (not the SetKeyToken store shortcut):
// key.id is preserved, the previous credential stops authenticating the
// endpoint, and the replacement credential authenticates it.
func TestRotateAgentKeyRealRegenerate(t *testing.T) {
	db := connectTestDB(t)
	requireAgentMCPEndpointTables(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	adminUserID := seedProjectAdmin(t, db, projectID)
	agentID := seedAgent(t, db, projectID)

	epStore := newAgentMCPEndpointStore(db)
	keyStore := newAgentMCPKeyStore(db)
	ep := &AgentMCPEndpoint{ID: uuid.NewString(), ProjectID: projectID, AgentID: agentID}
	require.NoError(t, epStore.CreateEndpoint(ctx, ep))

	oldTokenID := seedShareUserAndToken(t, db, projectID, "rotate-"+uuid.NewString())
	key := &AgentMCPKey{
		ID:         uuid.NewString(),
		EndpointID: ep.ID,
		TokenID:    oldTokenID,
		Label:      "rotate-" + uuid.NewString(),
	}
	require.NoError(t, keyStore.CreateKey(ctx, key))

	realTokenSvc := apitoken.NewService(db, apitoken.NewRepository(db, slog.Default()), nil, slog.Default())
	svc := &Service{
		db:             db,
		agentEndpoints: epStore,
		agentKeys:      keyStore,
		shareTokens:    realTokenSvc,
	}

	// The old credential authenticates before rotation.
	_, _, err := svc.AuthorizeAgentEndpoint(ctx, oldTokenID, agentID)
	require.NoError(t, err)

	resp, err := svc.RotateAgentKey(ctx, projectID, adminUserID, "", key.ID)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, key.ID, resp.ID, "rotation preserves the key id")
	require.NotEmpty(t, resp.Token, "rotation returns the new raw secret once")

	detail, err := keyStore.GetKeyByID(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, key.ID, detail.ID, "key identity preserved")
	require.NotEqual(t, oldTokenID, detail.TokenID, "token_id repointed")

	// Old credential revoked in core.api_tokens.
	var oldRevokedAt *time.Time
	require.NoError(t, db.NewRaw(`SELECT revoked_at FROM core.api_tokens WHERE id = ?`, oldTokenID).Scan(ctx, &oldRevokedAt))
	require.NotNil(t, oldRevokedAt, "old token is revoked")

	// New credential active, old one rejected by the endpoint authorizer.
	var newRevokedAt *time.Time
	require.NoError(t, db.NewRaw(`SELECT revoked_at FROM core.api_tokens WHERE id = ?`, detail.TokenID).Scan(ctx, &newRevokedAt))
	assert.Nil(t, newRevokedAt, "replacement token is active")

	_, _, err = svc.AuthorizeAgentEndpoint(ctx, detail.TokenID, agentID)
	require.NoError(t, err, "rotated credential authorizes the endpoint")
	_, _, err = svc.AuthorizeAgentEndpoint(ctx, oldTokenID, agentID)
	assertAppError(t, err, 403)
}

// TestAgentMCPSessionStoreTableDriven covers simple lookup branches in a table.
func TestAgentMCPSessionStoreTableDriven(t *testing.T) {
	db := connectTestDB(t)
	requireAgentMCPEndpointTables(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)
	agentID := seedAgent(t, db, projectID)

	epStore := newAgentMCPEndpointStore(db)
	keyStore := newAgentMCPKeyStore(db)
	sessStore := newAgentMCPSessionStore(db)
	ep := &AgentMCPEndpoint{ID: uuid.NewString(), ProjectID: projectID, AgentID: agentID}
	require.NoError(t, epStore.CreateEndpoint(ctx, ep))
	tokenID := seedShareUserAndToken(t, db, projectID, "td-"+uuid.NewString())
	key := &AgentMCPKey{ID: uuid.NewString(), EndpointID: ep.ID, TokenID: tokenID, Label: "td-key"}
	require.NoError(t, keyStore.CreateKey(ctx, key))

	cases := []struct {
		name      string
		ref       string
		wantFound bool
	}{
		{name: "unknown ref", ref: uuid.NewString(), wantFound: false},
		{name: "known ref", ref: uuid.NewString(), wantFound: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantFound {
				require.NoError(t, sessStore.CreateSession(ctx, &AgentMCPSession{
					ID:         uuid.NewString(),
					EndpointID: ep.ID,
					KeyID:      key.ID,
					SessionRef: tc.ref,
				}))
			}
			got, err := sessStore.GetSessionByRef(ctx, tc.ref)
			require.NoError(t, err)
			if tc.wantFound {
				require.NotNil(t, got)
				assert.Equal(t, tc.ref, got.SessionRef)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}
