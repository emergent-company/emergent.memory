package routeguard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDerive(t *testing.T) {
	cases := []struct {
		name  string
		mw    []middleware
		level Level
	}{
		{"public", nil, LevelPublic},
		{"decorative only", []middleware{{"a2aErrorEnvelopeMiddleware", ""}}, LevelPublic},
		{"auth", []middleware{{"RequireAuth", LevelAuth}}, LevelAuth},
		{"auth + scope", []middleware{{"RequireAuth", LevelAuth}, {"RequireAPITokenScopes(projects:read)", ""}}, LevelAuth},
		{"project token", []middleware{{"RequireAuth", LevelAuth}, {"RequireProjectTokenScope", LevelProjectToken}}, LevelProjectToken},
		{"project member", []middleware{{"RequireAuth", LevelAuth}, {"RequireProjectTokenScope", LevelProjectToken}, {"RequireProjectMember", LevelProjectMember}}, LevelProjectMember},
		{"member without token", []middleware{{"RequireAuth", LevelAuth}, {"RequireProjectMember", LevelProjectMember}}, LevelProjectMember},
		{"superadmin wins", []middleware{{"RequireAuth", LevelAuth}, {"RequireProjectMember", LevelProjectMember}, {"RequireSuperadminFull", LevelSuperadminFull}}, LevelSuperadminFull},
		{"a2a streaming implies auth", []middleware{{"a2aStreamingAuthMiddleware(agents:write)", LevelAuth}}, LevelAuth},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := derive(c.mw)
			if got != c.level {
				t.Fatalf("derive(%v) = %s, want %s", c.mw, got, c.level)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	derived := []Route{
		{Method: "GET", Path: "/health", Level: LevelPublic},
		{Method: "GET", Path: "/api/projects", Level: LevelAuth},
		{Method: "POST", Path: "/api/projects", Level: LevelProjectMember},
	}

	t.Run("clean", func(t *testing.T) {
		declared := &Table{Routes: []Route{
			{Method: "GET", Path: "/health", Level: LevelPublic},
			{Method: "GET", Path: "/api/projects", Level: LevelAuth},
			{Method: "POST", Path: "/api/projects", Level: LevelProjectMember},
		}}
		v, _ := Compare(derived, declared)
		if len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("undeclared", func(t *testing.T) {
		declared := &Table{Routes: []Route{{Method: "GET", Path: "/health", Level: LevelPublic}}}
		v, _ := Compare(derived, declared)
		if len(v) != 2 {
			t.Fatalf("expected 2 undeclared violations, got %v", v)
		}
		for _, x := range v {
			if x.Kind != "undeclared" {
				t.Errorf("expected undeclared, got %s", x.Kind)
			}
		}
	})

	t.Run("vanished", func(t *testing.T) {
		declared := &Table{Routes: []Route{
			{Method: "GET", Path: "/health", Level: LevelPublic},
			{Method: "DELETE", Path: "/gone", Level: LevelAuth},
		}}
		v, _ := Compare(derived, declared)
		var vanished bool
		for _, x := range v {
			if x.Kind == "vanished" && x.Path == "/gone" {
				vanished = true
			}
		}
		if !vanished {
			t.Fatalf("expected vanished violation for /gone, got %v", v)
		}
	})

	t.Run("level mismatch", func(t *testing.T) {
		declared := &Table{Routes: []Route{
			{Method: "GET", Path: "/health", Level: LevelPublic},
			{Method: "GET", Path: "/api/projects", Level: LevelProjectMember}, // wrong
			{Method: "POST", Path: "/api/projects", Level: LevelProjectMember},
		}}
		v, _ := Compare(derived, declared)
		if len(v) != 1 || v[0].Kind != "level-mismatch" {
			t.Fatalf("expected one level-mismatch, got %v", v)
		}
	})
}

func TestExtractSynthetic(t *testing.T) {
	dir := t.TempDir()
	domainDir := filepath.Join(dir, "domain", "demo")
	if err := os.MkdirAll(domainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package demo

import (
	"github.com/labstack/echo/v4"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	e.GET("/health", h.Health)

	public := e.Group("/api/demo")
	public.GET("", h.List)

	g := e.Group("/api/projects/:projectId/demo")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())
	g.GET("", h.List)
	g.POST("", h.Create, authMiddleware.RequireAPITokenScopes("demo:write"))

	admin := e.Group("/api/superadmin/demo")
	admin.Use(authMiddleware.RequireAuth())
	admin.Use(authMiddleware.RequireSuperadminFull())
	admin.DELETE("/:id", h.Delete)

	m := e.Group("/api/multi")
	m.Use(authMiddleware.RequireAuth())
	m.Match([]string{"GET", "POST"}, "", h.Unified)
}
`
	if err := os.WriteFile(filepath.Join(domainDir, "routes.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Extract(dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Unclassified) != 0 {
		t.Fatalf("unexpected unclassified: %v", res.Unclassified)
	}

	want := map[string]Level{
		"GET /health":                        LevelPublic,
		"GET /api/demo":                      LevelPublic,
		"GET /api/projects/:projectId/demo":  LevelProjectMember,
		"POST /api/projects/:projectId/demo": LevelProjectMember,
		"DELETE /api/superadmin/demo/:id":    LevelSuperadminFull,
		"GET /api/multi":                     LevelAuth,
		"POST /api/multi":                    LevelAuth,
	}
	got := map[string]Level{}
	for _, r := range res.Routes {
		got[r.Method+" "+r.Path] = r.Level
	}
	if len(got) != len(want) {
		t.Fatalf("route count = %d, want %d\n got=%v", len(got), len(want), got)
	}
	for k, wlvl := range want {
		if got[k] != wlvl {
			t.Errorf("%s = %s, want %s", k, got[k], wlvl)
		}
	}
}

// TestExtractRealTree ensures the extractor can classify every registration
// pattern currently in apps/server/domain (fail-closed smoke test): if a future
// pattern is added, this test surfaces it as an unclassified entry before CI.
func TestExtractRealTree(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/routeguard -> apps/server
	serverDir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if _, err := os.Stat(filepath.Join(serverDir, "domain")); err != nil {
		t.Skipf("domain tree not available at %s: %v", serverDir, err)
	}

	res, err := Extract(serverDir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Unclassified) != 0 {
		t.Fatalf("unclassified registration patterns:\n%s", strings.Join(res.Unclassified, "\n"))
	}
	if len(res.Routes) == 0 {
		t.Fatal("Extract returned zero routes; expected a populated table")
	}
	t.Logf("extracted %d routes from the real tree", len(res.Routes))
}
