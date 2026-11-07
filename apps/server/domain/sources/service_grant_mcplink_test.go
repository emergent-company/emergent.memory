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

// TestCreateGrant_MCPLinkCredentialGetsExactGrant pins the mcp_link half of
// issue #253: before this fix the create path granted only api_key, so a source
// created by an mcp_link credential was unpushable by that link (owner_conflict
// on every changeset). An mcp_link carries a stable id, so it must receive the
// exact grant that lets it push as the source it created.
func TestCreateGrant_MCPLinkCredentialGetsExactGrant(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcegrantmcplink")
	t.Cleanup(tdb.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	svc := NewService(NewRepository(tdb.DB, log), cfg, log, NewDefaultRegistry())

	projectID := uuid.New()

	linkID := uuid.New()
	userID := uuid.New()
	linkCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: userID.String()})
	linkCtx = auth.ContextWithCredential(linkCtx, auth.CredentialMCPLink, &linkID)

	created, err := svc.Create(linkCtx, projectID, &CreateIntegrationRequest{
		Name:         "mcp link drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder-a",
			"refresh_token": "refresh-a",
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	var grant struct {
		CredentialKind string     `bun:"credential_kind"`
		CredentialID   *uuid.UUID `bun:"credential_id"`
		GrantedByType  string     `bun:"granted_by_type"`
		GrantedByID    *uuid.UUID `bun:"granted_by_id"`
	}
	require.NoError(t, tdb.DB.NewRaw(`
		SELECT credential_kind, credential_id, granted_by_type, granted_by_id
		FROM kb.source_grants WHERE project_id = ? AND source_id = ?`,
		projectID, uuid.MustParse(created.ID),
	).Scan(ctx, &grant.CredentialKind, &grant.CredentialID, &grant.GrantedByType, &grant.GrantedByID))

	require.Equal(t, auth.CredentialMCPLink, grant.CredentialKind,
		"an mcp_link creator must receive an mcp_link grant, not be left ungranted")
	require.NotNil(t, grant.CredentialID)
	require.Equal(t, linkID, *grant.CredentialID, "the grant must name the creating link")
	require.Equal(t, "user", grant.GrantedByType)
	require.NotNil(t, grant.GrantedByID)
	require.Equal(t, userID, *grant.GrantedByID)
}
