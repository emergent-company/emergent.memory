package tracing_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// newFakeTempo returns an httptest server mimicking the Tempo query surface.
// GET /api/search evaluates the TraceQL `q` (a parser-aware subset: &&, ||, =,
// !=, parentheses, dotted attribute refs, strings) against the two candidate
// traces and returns the matching ones; GET /api/traces/<id> returns the trace
// by id.
func newFakeTempo() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		candidates := []struct {
			id    string
			attrs map[string]string
		}{
			{traceA, map[string]string{"memory.project.id": projectA, "rootName": "agent.run"}},
			{traceB, map[string]string{"memory.project.id": projectB, "rootName": "agent.run"}},
		}
		traces := []map[string]any{}
		for _, c := range candidates {
			if traceqlEval(q, c.attrs) {
				traces = append(traces, map[string]any{"traceID": c.id, "rootTraceName": "agent.run", "durationMs": 1.0})
			}
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

// traceqlEval evaluates a minimal TraceQL condition block against a trace's
// attribute map. Supports && (AND), || (OR, lower precedence), =, !=,
// parentheses, dotted attribute refs and double-quoted strings. A missing
// attribute matches != and fails =.
func traceqlEval(q string, attrs map[string]string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return true
	}
	q = strings.TrimPrefix(q, "{")
	if i := strings.LastIndex(q, "}"); i >= 0 {
		q = q[:i]
	}
	p := &tqParser{s: q, attrs: attrs}
	return p.parseOr()
}

type tqParser struct {
	s     string
	pos   int
	attrs map[string]string
}

func (p *tqParser) skipWS() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t' || p.s[p.pos] == '\n') {
		p.pos++
	}
}

func (p *tqParser) consume(s string) bool {
	if strings.HasPrefix(p.s[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *tqParser) parseOr() bool {
	v := p.parseAnd()
	for {
		p.skipWS()
		if !p.consume("||") {
			return v
		}
		// Evaluate the RHS first so the input is always consumed — Go's `||`
		// short-circuits, which would otherwise skip the RHS parse entirely.
		rhs := p.parseAnd()
		v = v || rhs
	}
}

func (p *tqParser) parseAnd() bool {
	v := p.parseUnary()
	for {
		p.skipWS()
		if !p.consume("&&") {
			return v
		}
		rhs := p.parseUnary()
		v = v && rhs
	}
}

func (p *tqParser) parseUnary() bool {
	p.skipWS()
	if p.consume("(") {
		v := p.parseOr()
		p.skipWS()
		p.consume(")")
		return v
	}
	return p.parseComparison()
}

func (p *tqParser) parseComparison() bool {
	p.skipWS()
	attr := p.parseAttr()
	p.skipWS()
	op := ""
	switch {
	case p.consume("!="):
		op = "!="
	case p.consume("="):
		op = "="
	default:
		return false
	}
	p.skipWS()
	val := p.parseString()
	// TraceQL ".memory.project.id" refers to the span attribute "memory.project.id";
	// the leading dot is the span-attribute shortcut. Intrinsics like "rootName"
	// carry no dot and are looked up verbatim.
	attr = strings.TrimPrefix(attr, ".")
	got, ok := p.attrs[attr]
	if op == "=" {
		return ok && got == val
	}
	// != : an absent attribute is "not equal" to any value.
	return !ok || got != val
}

func (p *tqParser) parseAttr() string {
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' {
			p.pos++
		} else {
			break
		}
	}
	return p.s[start:p.pos]
}

func (p *tqParser) parseString() string {
	p.skipWS()
	if p.pos >= len(p.s) || p.s[p.pos] != '"' {
		return ""
	}
	p.pos++
	var b strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '\\' {
			p.pos++
			if p.pos < len(p.s) {
				b.WriteByte(p.s[p.pos])
				p.pos++
			}
			continue
		}
		if c == '"' {
			p.pos++
			break
		}
		b.WriteByte(c)
		p.pos++
	}
	return b.String()
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

	// Injection vectors: a caller must not be able to weaken the forced project
	// predicate via a crafted `q`. The parser-aware fake evaluates the composed
	// TraceQL (including != and ||), so these cases prove the override is caught
	// either by rejection (400) or by returning only the caller's project.
	search := func(q string) *testutil.HTTPResponse {
		return client.GET("/api/traces/search?q="+url.QueryEscape(q),
			testutil.WithAuth(adminAllTok), testutil.WithProjectID(projectA))
	}

	t.Run("paren-escape injection rejected -> 400", func(t *testing.T) {
		resp := search(`{ .foo = "bar") || .memory.project.id != "` + projectA + `" }`)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"paren-escape must be 400; got %d body=%s", resp.StatusCode, resp.String())
	})

	t.Run("empty-block OR foreign rejected -> 400", func(t *testing.T) {
		resp := search(`{ } || .memory.project.id = "` + projectB + `"`)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"empty-block OR foreign must be 400; got %d body=%s", resp.StatusCode, resp.String())
	})

	t.Run("OR into pipeline targeting foreign rejected -> 400", func(t *testing.T) {
		resp := search(`{ rootName = "agent.run" } | select(span.memory.agent.run_id) || .memory.project.id = "` + projectB + `"`)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"OR into pipeline must be 400; got %d body=%s", resp.StatusCode, resp.String())
	})

	t.Run("balanced OR targeting foreign rejected -> 400", func(t *testing.T) {
		resp := search(`{ .memory.project.id = "` + projectA + `" || .memory.project.id = "` + projectB + `" }`)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"balanced OR must be rejected (|| not allowed); got %d body=%s", resp.StatusCode, resp.String())
	})

	t.Run("well-formed AND targeting foreign yields empty", func(t *testing.T) {
		resp := search(`{ .memory.project.id = "` + projectB + `" }`)
		require.Equal(t, http.StatusOK, resp.StatusCode, "got %d body=%s", resp.StatusCode, resp.String())
		require.NotContains(t, resp.String(), traceB, "foreign target must not surface foreign trace")
		require.NotContains(t, resp.String(), traceA, "foreign-targeting AND must not surface own trace either")
	})

	t.Run("legitimate scoped search still works -> only own project", func(t *testing.T) {
		resp := search(`{ rootName = "agent.run" }`)
		require.Equal(t, http.StatusOK, resp.StatusCode, "got %d body=%s", resp.StatusCode, resp.String())
		require.Contains(t, resp.String(), traceA, "legitimate scoped search must surface own trace")
		require.NotContains(t, resp.String(), traceB, "legitimate scoped search must not surface foreign trace")
	})
}
