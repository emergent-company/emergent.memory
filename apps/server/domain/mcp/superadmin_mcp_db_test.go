package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// standaloneFixture seeds a throwaway DB with two standalone users
// (zitadel_user_id 'standalone' and 'standalone-2') and returns the DB, their
// IDs, and a config with standalone auth enabled for both API keys.
type standaloneFixture struct {
	db  *bun.DB
	id1 string
	id2 string
	cfg *config.Config
}

func newStandaloneFixture(t *testing.T) *standaloneFixture {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mcp_superadmin_admin_endpoint")
	t.Cleanup(tdb.Close)
	dbc := tdb.DB

	id1 := uuid.New().String()
	_, err := dbc.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, 'standalone')`, id1).Exec(ctx)
	require.NoError(t, err)
	id2 := uuid.New().String()
	_, err = dbc.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, 'standalone-2')`, id2).Exec(ctx)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.Standalone.Enabled = true
	cfg.Standalone.APIKey = "testkey"
	cfg.Standalone.APIKey2 = "testkey2"
	cfg.Standalone.UserEmail = "admin@localhost"
	cfg.Standalone.UserEmail2 = "admin2@localhost"
	return &standaloneFixture{db: dbc, id1: id1, id2: id2, cfg: cfg}
}

func adminEndpointEcho(t *testing.T, dbc *bun.DB, cfg *config.Config) *echo.Echo {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := &Service{db: dbc, log: log}
	_ = svc.GetToolDefinitions()

	h := NewHandler(svc, log, nil)
	sse := NewSSEHandler(svc, h, log)
	stream := NewStreamableHTTPHandler(svc, log)
	adminStream := NewAdminStreamableHTTPHandler(svc, log)
	ep := NewAgentEndpointHandler(svc, log)
	mw := auth.NewMiddleware(auth.MiddlewareParams{DB: dbc, Cfg: cfg, Log: log})
	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	RegisterRoutes(e, h, sse, stream, adminStream, ep, mw)
	return e
}

func adminRequest(e *echo.Echo, apiKey, sessionID, body string) (*httptest.ResponseRecorder, http.Header) {
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/admin", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec, rec.Header().Clone()
}

const adminInitBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"t","version":"1"}}}`
const adminListBody = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`

// TestAdminEndpointRouteGatingDB proves the dedicated endpoint's route gate:
// no grant → 403, superadmin_readonly → 403, superadmin_full → 200 with an
// operator catalog and a session that carries no project id.
func TestAdminEndpointRouteGatingDB(t *testing.T) {
	f := newStandaloneFixture(t)
	ctx := context.Background()

	t.Run("no grant is 403", func(t *testing.T) {
		e := adminEndpointEcho(t, f.db, f.cfg)
		rec, _ := adminRequest(e, "testkey", "", adminInitBody)
		require.Equal(t, http.StatusForbidden, rec.Code, "non-superadmin must be 403 at the route")
	})

	t.Run("superadmin_readonly is 403", func(t *testing.T) {
		_, err := f.db.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_readonly')`, f.id2).Exec(ctx)
		require.NoError(t, err)
		e := adminEndpointEcho(t, f.db, f.cfg)
		rec, _ := adminRequest(e, "testkey2", "", adminInitBody)
		require.Equal(t, http.StatusForbidden, rec.Code, "superadmin_readonly must be 403 (full-only)")
	})

	t.Run("superadmin_full initializes and lists catalog", func(t *testing.T) {
		_, err := f.db.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, f.id1).Exec(ctx)
		require.NoError(t, err)

		e := adminEndpointEcho(t, f.db, f.cfg)
		rec, headers := adminRequest(e, "testkey", "", adminInitBody)
		require.Equal(t, http.StatusOK, rec.Code, "superadmin_full must initialize, got %d: %s", rec.Code, rec.Body.String())
		sessionID := headers.Get("Mcp-Session-Id")
		require.NotEmpty(t, sessionID, "initialize must set Mcp-Session-Id")

		rec, _ = adminRequest(e, "testkey", sessionID, adminListBody)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp struct {
			Result struct {
				Tools []ToolDefinition `json:"tools"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		names := map[string]bool{}
		for _, def := range resp.Result.Tools {
			names[def.Name] = true
			if !def.SuperadminOnly {
				t.Errorf("admin catalog tool %q must be SuperadminOnly", def.Name)
			}
		}
		for _, want := range []string{"user-list", "project-delete", "embedding-pause", "trace-list"} {
			if !names[want] {
				t.Errorf("admin catalog missing operator tool %q", want)
			}
		}
		if names["entity-query"] || names["schema-list"] {
			t.Errorf("admin catalog must not contain project-scoped tools")
		}
	})
}

// TestSuperadminAuditDBWrite proves a redacted audit row is inserted into
// kb.audit_log with the expected event_type, action, endpoint, redacted details,
// and the persisted client fields (ip_address, user_agent, duration_ms).
func TestSuperadminAuditDBWrite(t *testing.T) {
	f := newStandaloneFixture(t)
	ctx := context.Background()

	w := NewSuperadminAuditWriter(f.db, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	err := w.Write(ctx, SuperadminAuditEntry{
		EventType:    "mcp.superadmin.tool_call",
		Outcome:      "success",
		UserID:       uuid.New().String(),
		UserEmail:    "op@example.com",
		ResourceType: "tool",
		ResourceID:   "user-list",
		Action:       "user-list",
		Endpoint:     "/api/mcp/admin",
		HTTPMethod:   http.MethodPost,
		StatusCode:   200,
		IPAddress:    "1.2.3.4",
		UserAgent:    "test-agent/1.0",
		RequestID:    "sess-1",
		Details:      map[string]any{"api_key": "sk-secret", "org_id": "o1"},
		Duration:     123 * time.Millisecond,
	})
	require.NoError(t, err)

	var row struct {
		EventType string         `bun:"event_type"`
		Outcome   string         `bun:"outcome"`
		Action    string         `bun:"action"`
		Endpoint  string         `bun:"endpoint"`
		IPAddress string         `bun:"ip_address"`
		UserAgent string         `bun:"user_agent"`
		RequestID string         `bun:"request_id"`
		Details   map[string]any `bun:"details"`
	}
	err = f.db.NewSelect().
		Table("kb.audit_log").
		Column("event_type", "outcome", "action", "endpoint", "ip_address", "user_agent", "request_id", "details").
		Where("request_id = 'sess-1'").
		Scan(ctx, &row)
	require.NoError(t, err)
	require.Equal(t, "mcp.superadmin.tool_call", row.EventType)
	require.Equal(t, "success", row.Outcome)
	require.Equal(t, "user-list", row.Action)
	require.Equal(t, "/api/mcp/admin", row.Endpoint)
	require.Equal(t, "1.2.3.4", row.IPAddress, "ip_address must be persisted")
	require.Equal(t, "test-agent/1.0", row.UserAgent, "user_agent must be persisted")
	require.Equal(t, "sess-1", row.RequestID)
	require.Equal(t, "[REDACTED]", row.Details["api_key"], "details must be redacted")
	require.Equal(t, "o1", row.Details["org_id"])
	require.Equal(t, float64(123), row.Details["duration_ms"], "duration_ms must be persisted in details")
}

// TestSuperadminPreExistingMutatorsConfirmGuardDB proves the confirm guard now
// covers the pre-existing SuperadminOnly mutators on the admin endpoint:
// unconfirmed returns a dry-run and performs no mutation; confirmed executes.
func TestSuperadminPreExistingMutatorsConfirmGuardDB(t *testing.T) {
	dbc, _, superID := setupGateDB(t)
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: superID, Scopes: []string{"admin"}})

	t.Run("embedding-pause", func(t *testing.T) {
		ctl := &fakeEmbeddingCtl{status: EmbeddingStatusSnapshot{}}
		svc := &Service{db: dbc, embeddingCtl: ctl}

		res, err := svc.ExecuteSuperadminTool(ctx, "embedding-pause", map[string]any{})
		require.NoError(t, err)
		require.Equal(t, true, res.StructuredContent["dry_run"], "unconfirmed embedding-pause must be a dry-run")
		require.False(t, ctl.paused, "unconfirmed embedding-pause must not mutate")

		_, err = svc.ExecuteSuperadminTool(ctx, "embedding-pause", map[string]any{"confirm": true})
		require.NoError(t, err)
		require.True(t, ctl.paused, "confirmed embedding-pause must execute")
	})

	t.Run("embedding-resume", func(t *testing.T) {
		ctl := &fakeEmbeddingCtl{status: EmbeddingStatusSnapshot{}, paused: true}
		svc := &Service{db: dbc, embeddingCtl: ctl}

		res, err := svc.ExecuteSuperadminTool(ctx, "embedding-resume", map[string]any{})
		require.NoError(t, err)
		require.Equal(t, true, res.StructuredContent["dry_run"])
		require.True(t, ctl.paused, "unconfirmed embedding-resume must not mutate")

		_, err = svc.ExecuteSuperadminTool(ctx, "embedding-resume", map[string]any{"confirm": true})
		require.NoError(t, err)
		require.False(t, ctl.paused, "confirmed embedding-resume must execute")
	})

	t.Run("embedding-config-update", func(t *testing.T) {
		ctl := &fakeEmbeddingCtl{status: EmbeddingStatusSnapshot{}}
		svc := &Service{db: dbc, embeddingCtl: ctl}

		res, err := svc.ExecuteSuperadminTool(ctx, "embedding-config-update", map[string]any{"batch_size": 5})
		require.NoError(t, err)
		require.Equal(t, true, res.StructuredContent["dry_run"])

		_, err = svc.ExecuteSuperadminTool(ctx, "embedding-config-update", map[string]any{"batch_size": 5, "confirm": true})
		require.NoError(t, err, "confirmed embedding-config-update must execute")
	})

	t.Run("provider-configure-org", func(t *testing.T) {
		svc := &Service{db: dbc} // providerCredSvc nil — the guard must short-circuit before any write

		res, err := svc.ExecuteSuperadminTool(ctx, "provider-configure-org", map[string]any{"provider_name": "google", "api_key": "sk-secret"})
		require.NoError(t, err)
		require.Equal(t, true, res.StructuredContent["dry_run"], "unconfirmed provider-configure-org must be a dry-run (no write)")
		require.Equal(t, "configure organization provider", res.StructuredContent["action"])

		// Confirmed reaches the real handler; with no org_id it errors on the
		// missing identifier (proving the guard was bypassed, not a dry-run).
		_, err = svc.ExecuteSuperadminTool(ctx, "provider-configure-org", map[string]any{"provider_name": "google", "confirm": true})
		require.Error(t, err)
		require.Contains(t, err.Error(), "org_id")
	})
}
