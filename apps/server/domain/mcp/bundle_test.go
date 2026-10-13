package mcp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// ============================================================================
// authorizeAgentBundle — unit tests (fake stores, no DB)
// ============================================================================

// TestAuthorizeAgentBundle pins the 401/403 mapping of the bundle-download
// authorization: unbound/revoked keys are 401, while a key bound to a different
// agent, a revoked endpoint, or an unavailable agent are 403.
func TestAuthorizeAgentBundle(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name     string
		mutate   func(f *endpointTestFixture)
		token    string
		agentID  string
		wantErr  bool
		wantCode int
		wantEP   string
	}{
		{
			name:    "bound key accepted",
			mutate:  func(f *endpointTestFixture) {},
			token:   "tok-1",
			agentID: agentA,
			wantEP:  "ep-1",
		},
		{
			name:     "unbound credential rejected as 401",
			mutate:   func(f *endpointTestFixture) {},
			token:    "other",
			agentID:  agentA,
			wantErr:  true,
			wantCode: 401,
		},
		{
			name: "revoked key rejected as 401",
			mutate: func(f *endpointTestFixture) {
				revoked := now
				f.keys.byID["key-1"].RevokedAt = &revoked
			},
			token:    "tok-1",
			agentID:  agentA,
			wantErr:  true,
			wantCode: 401,
		},
		{
			name:     "key bound to different agent rejected as 403",
			mutate:   func(f *endpointTestFixture) {},
			token:    "tok-1",
			agentID:  agentB,
			wantErr:  true,
			wantCode: 403,
		},
		{
			name: "revoked endpoint rejected as 403",
			mutate: func(f *endpointTestFixture) {
				revoked := now
				f.endpoints.byID["ep-1"].RevokedAt = &revoked
			},
			token:    "tok-1",
			agentID:  agentA,
			wantErr:  true,
			wantCode: 403,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEndpointTestFixture()
			tt.mutate(f)
			endpoint, err := f.svc.authorizeAgentBundle(context.Background(), tt.token, tt.agentID)
			if tt.wantErr {
				require.Error(t, err)
				assertAppError(t, err, tt.wantCode)
				assert.Nil(t, endpoint)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, endpoint)
			assert.Equal(t, tt.wantEP, endpoint.ID)
		})
	}
}

// ============================================================================
// Handler — missing token
// ============================================================================

func TestHandleDownloadAgentMCPBundleMissingToken(t *testing.T) {
	h := NewHandler(&Service{}, slog.Default(), nil)
	c, _ := newAgentBundleContext(t, agentA, "")
	err := h.HandleDownloadAgentMCPBundle(c)
	assertAppError(t, err, 400)
}

// newAgentBundleContext builds an echo context for the agent bundle route with
// an optional token query parameter.
func newAgentBundleContext(t *testing.T, agentID, token string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	target := "/api/mcp/agents/" + agentID + "/bundle"
	if token != "" {
		target += "?token=" + token
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("agentId")
	c.SetParamValues(agentID)
	return c, rec
}

// ============================================================================
// GenerateAgentMCPBundle — DB-backed integration (skipped when no DB)
// ============================================================================

// seedAgentBundleToken inserts a project-scoped API token whose token_hash is
// the real SHA-256 of rawToken, returning the token id. revokedAt/expiresAt may
// be nil.
func seedAgentBundleToken(t *testing.T, db bun.IDB, projectID, rawToken string, revokedAt, expiresAt *time.Time) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
	`, userID, "zitadel-"+userID).Exec(ctx)
	require.NoError(t, err)

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])
	tokenID := uuid.NewString()
	_, err = db.NewRaw(`
		INSERT INTO core.api_tokens (id, user_id, project_id, name, token_hash, token_prefix, scopes, revoked_at, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, '{projects:read}'::text[], ?, ?, NOW())
	`, tokenID, userID, projectID, "agent-bundle-"+tokenID, tokenHash, "emt_"+tokenID[:8], revokedAt, expiresAt).Exec(ctx)
	require.NoError(t, err)
	return tokenID
}

// newAgentBundleService wires a real DB (token/project lookups) with the fake
// endpoint/key/directory stores for authorization.
func newAgentBundleService(t *testing.T, db bun.IDB, projectID, tokenID string) (*Service, *fakeEndpointStore, *fakeKeyStore) {
	t.Helper()
	endpoints := newFakeEndpointStore()
	keys := newFakeKeyStore()
	endpoints.byID["ep-1"] = &AgentMCPEndpoint{
		ID: "ep-1", ProjectID: projectID, AgentID: agentA, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	keys.endpoints["ep-1"] = endpoints.byID["ep-1"]
	keys.byID["key-1"] = &AgentMCPKey{
		ID: "key-1", EndpointID: "ep-1", TokenID: tokenID, Label: "default",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	dir := &fakeAgentDir{agents: []AgentRef{{ID: agentA, Name: "Alpha", Enabled: true}}}
	svc := &Service{db: db, agentEndpoints: endpoints, agentKeys: keys, agentDir: dir}
	return svc, endpoints, keys
}

func TestGenerateAgentMCPBundle(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	const rawToken = "emt_agent_bundle_token"
	tokenID := seedAgentBundleToken(t, db, projectID, rawToken, nil, nil)
	svc, _, _ := newAgentBundleService(t, db, projectID, tokenID)

	data, filename, err := svc.GenerateAgentMCPBundle(ctx, rawToken, "https://example.com", agentA)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.True(t, strings.HasSuffix(filename, ".mcpb"), "filename = %q", filename)

	// The bundle is a ZIP with a manifest whose MCP URL targets the per-agent
	// endpoint and whose auth header carries the raw token.
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	var manifest mcpbManifest
	found := false
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		require.NoError(t, err)
		raw, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		require.NoError(t, json.Unmarshal(raw, &manifest))
		found = true
		break
	}
	require.True(t, found, "manifest.json missing from bundle")
	args := manifest.Server.MCPConfig.Args
	assert.Contains(t, args, "https://example.com/api/mcp/agents/"+agentA)
	assert.Contains(t, args, "Authorization: Bearer "+rawToken)
}

func TestGenerateAgentMCPBundleTokenStates(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	now := time.Now().UTC()
	past := now.Add(-time.Hour)

	seedValid := func(raw string) string {
		return seedAgentBundleToken(t, db, projectID, raw, nil, nil)
	}

	t.Run("unknown token is 401", func(t *testing.T) {
		tokenID := seedValid("emt_unknown_anchor")
		svc, _, _ := newAgentBundleService(t, db, projectID, tokenID)
		_, _, err := svc.GenerateAgentMCPBundle(ctx, "emt_never_seeded", "https://example.com", agentA)
		assertAppError(t, err, 401)
	})

	t.Run("revoked token is 401", func(t *testing.T) {
		tokenID := seedAgentBundleToken(t, db, projectID, "emt_revoked_token", &now, nil)
		svc, _, _ := newAgentBundleService(t, db, projectID, tokenID)
		_, _, err := svc.GenerateAgentMCPBundle(ctx, "emt_revoked_token", "https://example.com", agentA)
		assertAppError(t, err, 401)
	})

	t.Run("expired token is 401", func(t *testing.T) {
		tokenID := seedAgentBundleToken(t, db, projectID, "emt_expired_token", nil, &past)
		svc, _, _ := newAgentBundleService(t, db, projectID, tokenID)
		_, _, err := svc.GenerateAgentMCPBundle(ctx, "emt_expired_token", "https://example.com", agentA)
		assertAppError(t, err, 401)
	})
}

func TestHandleDownloadAgentMCPBundleIntegration(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	const rawToken = "emt_handler_bundle_token"
	tokenID := seedAgentBundleToken(t, db, projectID, rawToken, nil, nil)
	svc, _, _ := newAgentBundleService(t, db, projectID, tokenID)
	h := NewHandler(svc, slog.Default(), nil)

	c, rec := newAgentBundleContext(t, agentA, rawToken)
	require.NoError(t, h.HandleDownloadAgentMCPBundle(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.NotEmpty(t, rec.Body.Bytes())
}
