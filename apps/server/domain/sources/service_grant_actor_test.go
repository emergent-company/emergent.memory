package sources

import (
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestCreateGrant_RecordsActorPair pins the granting-actor pair written when the
// source-create path grants the creating credential (issue #280). A grant minted
// by a resolved user is ('user', <user_profiles.id>); an ownerless caller — an
// ephemeral sandbox token that carries an api_key credential but no user profile
// — must not record a 'user' actor that names nobody: it records the principal
// actually responsible, ('system', NULL), distinguishable from a user grant and
// from the 00266 backfill ('system'/NULL, credential_kind='project').
func TestCreateGrant_RecordsActorPair(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcegrantactor")
	t.Cleanup(tdb.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	svc := NewService(NewRepository(tdb.DB, log), cfg, log, NewDefaultRegistry())

	projectID := uuid.New()

	driveCfg := map[string]any{
		"folder_id":     "folder-a",
		"refresh_token": "refresh-a",
	}

	// User-minted api_key: the grant records the authenticated user.
	userID := uuid.New()
	keyID := uuid.New()
	userCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: userID.String()})
	userCtx = auth.ContextWithCredential(userCtx, auth.CredentialAPIKey, &keyID)

	created, err := svc.Create(userCtx, projectID, &CreateIntegrationRequest{
		Name:         "user key drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config:       driveCfg,
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	// Ownerless sandbox api_key: no user in the context, credential id present.
	sandboxKeyID := uuid.New()
	sandboxCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &sandboxKeyID)

	sandboxCreated, err := svc.Create(sandboxCtx, projectID, &CreateIntegrationRequest{
		Name:         "sandbox drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config:       driveCfg,
	})
	require.NoError(t, err)
	require.NotEmpty(t, sandboxCreated.ID)

	var userGrant struct {
		GrantedByType string     `bun:"granted_by_type"`
		GrantedByID   *uuid.UUID `bun:"granted_by_id"`
	}
	require.NoError(t, tdb.DB.NewRaw(`
		SELECT granted_by_type, granted_by_id FROM kb.source_grants
		WHERE project_id = ? AND source_id = ? AND credential_kind = 'api_key' AND credential_id = ?`,
		projectID, uuid.MustParse(created.ID), keyID,
	).Scan(ctx, &userGrant.GrantedByType, &userGrant.GrantedByID))
	require.Equal(t, "user", userGrant.GrantedByType, "a user-minted api_key grant records 'user'")
	require.NotNil(t, userGrant.GrantedByID)
	require.Equal(t, userID, *userGrant.GrantedByID, "a user grant must name the resolved user")

	var sandboxGrant struct {
		GrantedByType string     `bun:"granted_by_type"`
		GrantedByID   *uuid.UUID `bun:"granted_by_id"`
	}
	require.NoError(t, tdb.DB.NewRaw(`
		SELECT granted_by_type, granted_by_id FROM kb.source_grants
		WHERE project_id = ? AND source_id = ? AND credential_kind = 'api_key' AND credential_id = ?`,
		projectID, uuid.MustParse(sandboxCreated.ID), sandboxKeyID,
	).Scan(ctx, &sandboxGrant.GrantedByType, &sandboxGrant.GrantedByID))
	require.Equal(t, "system", sandboxGrant.GrantedByType, "an ownerless caller records 'system', not 'user'")
	require.Nil(t, sandboxGrant.GrantedByID, "an ownerless caller records no user id")
}
