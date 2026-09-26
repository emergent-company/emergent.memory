package tracing_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/tracing"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

const (
	orgA          = "aaaaaaaa-0000-0000-0000-000000000001"
	projectA      = "aaaaaaaa-0000-0000-0000-000000000002"
	memberAID     = "aaaaaaaa-0000-0000-0000-000000000003"
	orgB          = "bbbbbbbb-0000-0000-0000-000000000001"
	projectB      = "bbbbbbbb-0000-0000-0000-000000000002"
	memberBID     = "bbbbbbbb-0000-0000-0000-000000000003"
	traceA        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	traceB        = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	adminAllTok   = "emt_trace_admin_all_a"
	memberBTok    = "emt_trace_member_b"
	superadminTok = "e2e-test-user" // maps to testutil.AdminUser (superadmin_full below)
)

// newFakeTempo returns an httptest server mimicking the Tempo query surface:
// GET /api/search returns a result set scoped by the project predicate in `q`
// (no predicate → all traces); GET /api/traces/<id> returns the trace by id.
func newFakeTempo() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		traces := []map[string]any{{"traceID": traceA, "rootTraceName": "agent.run", "durationMs": 1.0}}
		if q == "" || strings.Contains(q, projectB) {
			traces = append(traces, map[string]any{"traceID": traceB, "rootTraceName": "agent.run", "durationMs": 1.0})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"traces": traces})
	})
	mux.HandleFunc("/api/traces/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/traces/")
		switch id {
		case traceA:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(traceBody(projectA))
		case traceB:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(traceBody(projectB))
		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux)
}

// traceBody builds a minimal OTLP JSON trace with a single span carrying the
// memory.project.id attribute, matching Tempo's protobuf-JSON shape.
func traceBody(projectID string) map[string]any {
	return map[string]any{
		"batches": []any{
			map[string]any{
				"resource": map[string]any{"attributes": []any{}},
				"scopeSpans": []any{
					map[string]any{
						"spans": []any{
							map[string]any{
								"traceId": "0123456789abcdef0123456789abcdef",
								"spanId":  "1111111111111111",
								"name":    "agent.run",
								"attributes": []any{
									map[string]any{"key": "memory.project.id", "value": map[string]any{"stringValue": projectID}},
								},
							},
						},
					},
				},
			},
		},
	}
}

// newTracesEcho wires a minimal Echo instance with only the tracing routes, the
// real auth middleware + handler, and a fake Tempo upstream.
func newTracesEcho(t *testing.T, testDB *testutil.TestDB) (*testutil.HTTPClient, *httptest.Server) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dbc := testDB.GetDB()

	userSvc := auth.NewUserProfileService(dbc, log)
	am := auth.NewMiddleware(auth.MiddlewareParams{DB: dbc, Cfg: testDB.Config, Log: log, UserSvc: userSvc})

	tempo := newFakeTempo()
	cfg := *testDB.Config
	cfg.Otel.ExporterEndpoint = tempo.URL
	h := tracing.NewHandler(&cfg, dbc)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	tracing.RegisterRoutes(e, h, am)

	return testutil.NewHTTPClient(e), tempo
}

// TestTracesAuthz is the caller-class matrix for the /api/traces tenant-scoping
// fix (issue #994 mechanism 1/5). It proves that a trace's owning project is
// the tenant boundary, that a project member can read only their own project's
// traces, that a foreign/unknown trace id is masked as 404, that an admin:all
// scope does not grant cross-tenant or aggregate access, and that only
// superadmin_full reaches the instance-wide aggregate.
func TestTracesAuthz(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "traces_authz")
	defer testDB.Close()

	dbc := testDB.GetDB()
	require.NoError(t, testutil.SetupTestFixtures(ctx, dbc))

	// superadmin_full principal: AdminUser holds an active superadmin_full grant.
	_, err := dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, testutil.AdminUser.ID).Exec(ctx)
	require.NoError(t, err)

	// Project A + member A (admin:all account token — the escalation path).
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID: memberAID, ZitadelUserID: "trace-member-a", Email: "a@test.local",
	}))
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, orgA, "Org A"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{
		ID: projectA, OrgID: orgA, Name: "Project A",
	}, memberAID))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgA, memberAID, "member"))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, memberAID, adminAllTok, []string{"admin:all"}))

	// Project B + member B.
	require.NoError(t, testutil.CreateTestUser(ctx, dbc, testutil.TestUser{
		ID: memberBID, ZitadelUserID: "trace-member-b", Email: "b@test.local",
	}))
	require.NoError(t, testutil.CreateTestOrganization(ctx, dbc, orgB, "Org B"))
	require.NoError(t, testutil.CreateTestProject(ctx, dbc, testutil.TestProject{
		ID: projectB, OrgID: orgB, Name: "Project B",
	}, memberBID))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, dbc, orgB, memberBID, "member"))
	require.NoError(t, testutil.CreateTestAccountAPIToken(ctx, dbc, memberBID, memberBTok, []string{"admin:all"}))

	client, tempo := newTracesEcho(t, testDB)
	defer tempo.Close()

	t.Run("unauthenticated -> 401", func(t *testing.T) {
		for _, path := range []string{"/api/traces/search", "/api/traces/" + traceA} {
			require.Equal(t, http.StatusUnauthorized, client.GET(path).StatusCode,
				"unauthenticated %s must be 401", path)
		}
	})

	t.Run("member A reads own project trace by id -> 200", func(t *testing.T) {
		resp := client.GET("/api/traces/"+traceA,
			testutil.WithAuth(adminAllTok), testutil.WithProjectID(projectA))
		require.Equal(t, http.StatusOK, resp.StatusCode, "got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("member A reading foreign project trace by id -> 404", func(t *testing.T) {
		resp := client.GET("/api/traces/"+traceB,
			testutil.WithAuth(adminAllTok), testutil.WithProjectID(projectA))
		require.Equal(t, http.StatusNotFound, resp.StatusCode,
			"foreign trace by id must be 404 (no existence oracle); got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("member A search is scoped to own project", func(t *testing.T) {
		resp := client.GET("/api/traces/search",
			testutil.WithAuth(adminAllTok), testutil.WithProjectID(projectA))
		require.Equal(t, http.StatusOK, resp.StatusCode, "got %d: %s", resp.StatusCode, resp.String())
		require.NotContains(t, resp.String(), traceB, "search must not surface foreign trace")
		require.Contains(t, resp.String(), traceA, "search must surface own trace")
	})

	t.Run("member A addressing project B -> 403", func(t *testing.T) {
		resp := client.GET("/api/traces/search",
			testutil.WithAuth(adminAllTok), testutil.WithProjectID(projectB))
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"addressing a foreign project must be 403; got %d", resp.StatusCode)
	})

	t.Run("admin:all holder with no project context -> 403", func(t *testing.T) {
		for _, path := range []string{"/api/traces/search", "/api/traces/" + traceA} {
			resp := client.GET(path, testutil.WithAuth(adminAllTok))
			require.Equal(t, http.StatusForbidden, resp.StatusCode,
				"admin:all without project context must be 403 (aggregate is superadmin_full only); %s got %d", path, resp.StatusCode)
		}
	})

	t.Run("superadmin_full instance-wide search -> 200 (both tenants)", func(t *testing.T) {
		resp := client.GET("/api/traces/search", testutil.WithAuth(superadminTok))
		require.Equal(t, http.StatusOK, resp.StatusCode, "got %d: %s", resp.StatusCode, resp.String())
		require.Contains(t, resp.String(), traceA)
		require.Contains(t, resp.String(), traceB)
	})

	t.Run("superadmin_full reads any trace by id -> 200", func(t *testing.T) {
		resp := client.GET("/api/traces/"+traceB, testutil.WithAuth(superadminTok))
		require.Equal(t, http.StatusOK, resp.StatusCode, "got %d: %s", resp.StatusCode, resp.String())
	})

	t.Run("member B reads own project trace -> 200", func(t *testing.T) {
		resp := client.GET("/api/traces/"+traceB,
			testutil.WithAuth(memberBTok), testutil.WithProjectID(projectB))
		require.Equal(t, http.StatusOK, resp.StatusCode, "got %d: %s", resp.StatusCode, resp.String())
	})
}
