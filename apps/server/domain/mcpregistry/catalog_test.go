package mcpregistry

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestLoadCatalog(t *testing.T) {
	cat, err := loadCatalog()
	if err != nil {
		t.Fatalf("loadCatalog: %v", err)
	}
	if len(cat.servers) != 29 {
		t.Fatalf("len(catalog) = %d, want 29", len(cat.servers))
	}
	seen := make(map[string]bool, len(cat.servers))
	for i, s := range cat.servers {
		if s.Name == "" {
			t.Fatalf("entry %d has empty name", i)
		}
		if seen[s.Name] {
			t.Fatalf("duplicate catalog name %q", s.Name)
		}
		seen[s.Name] = true
	}
}

func TestCatalogSearch(t *testing.T) {
	cat := catalog{servers: []RegistryServer{
		{Name: "alpha", Description: "unrelated"},
		{Name: "zzz", Description: "needle in desc"},
		{Name: "needle-server"},
		{Name: "aaa", Title: "needle tool"},
	}}

	t.Run("ranks name before title before description", func(t *testing.T) {
		got := cat.search("needle", 0)
		want := []string{"needle-server", "aaa", "zzz"}
		assertNames(t, got, want)
	})

	t.Run("case-insensitive", func(t *testing.T) {
		got := cat.search("NEEDLE", 0)
		want := []string{"needle-server", "aaa", "zzz"}
		assertNames(t, got, want)
	})

	t.Run("case-insensitive over the name and title fields", func(t *testing.T) {
		c := catalog{servers: []RegistryServer{
			{Name: "Chrome-DevTools"},
			{Name: "x", Title: "Playwright MCP"},
		}}
		assertNames(t, c.search("chrome", 0), []string{"Chrome-DevTools"})
		assertNames(t, c.search("playwright", 0), []string{"x"})
	})

	t.Run("respects limit", func(t *testing.T) {
		got := cat.search("needle", 1)
		assertNames(t, got, []string{"needle-server"})
	})

	t.Run("empty query lists all in file order", func(t *testing.T) {
		got := cat.search("", 0)
		assertNames(t, got, []string{"alpha", "zzz", "needle-server", "aaa"})
	})

	t.Run("empty query respects limit", func(t *testing.T) {
		got := cat.search("", 2)
		assertNames(t, got, []string{"alpha", "zzz"})
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got := cat.search("does-not-exist", 0)
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})
}

func TestCatalogGet(t *testing.T) {
	cat := catalog{servers: []RegistryServer{
		{Name: "io.github.github/github-mcp-server", Title: "GitHub"},
		{Name: "microsoft/playwright-mcp"},
	}}

	if got, ok := cat.get("microsoft/playwright-mcp"); !ok || got.Name != "microsoft/playwright-mcp" {
		t.Fatalf("get(playwright) = (%+v, %v), want found", got, ok)
	}
	if _, ok := cat.get("does-not-exist"); ok {
		t.Fatalf("get(does-not-exist) = true, want false")
	}
}

func assertNames(t *testing.T, got []RegistryServer, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), names(got))
	}
	for i := range got {
		if got[i].Name != want[i] {
			t.Fatalf("got[%d].Name = %q, want %q (full: %v)", i, got[i].Name, want[i], names(got))
		}
	}
}

func names(servers []RegistryServer) []string {
	out := make([]string, len(servers))
	for i, s := range servers {
		out[i] = s.Name
	}
	return out
}

// TestServiceSearchRegistryServesFromCatalog proves SearchRegistry is served
// entirely from the embedded catalog — no HTTP client is constructed or called.
func TestServiceSearchRegistryServesFromCatalog(t *testing.T) {
	svc, err := NewService(nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	res, err := svc.SearchRegistry(context.Background(), "github", 0, "")
	if err != nil {
		t.Fatalf("SearchRegistry: %v", err)
	}
	if res.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty (single embedded page)", res.NextCursor)
	}
	if len(res.Servers) == 0 {
		t.Fatalf("no results for 'github', want at least one")
	}
	found := false
	for _, s := range res.Servers {
		if strings.Contains(strings.ToLower(s.Name), "github") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a name matching 'github', got %v", names2(res.Servers))
	}
	if res.Count != len(res.Servers) {
		t.Errorf("Count = %d, want %d", res.Count, len(res.Servers))
	}
}

// TestServiceSearchRegistryEmptyQueryReturnsWholeCatalog guards the browse
// default: the gateway and API callers omit `limit` (0), browse is unpaginated
// (cursor ignored, no next page), so an empty query must return every embedded
// entry rather than a page-sized default that silently hides the tail.
func TestServiceSearchRegistryEmptyQueryReturnsWholeCatalog(t *testing.T) {
	cat, err := loadCatalog()
	if err != nil {
		t.Fatalf("loadCatalog: %v", err)
	}
	svc, err := NewService(nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	res, err := svc.SearchRegistry(context.Background(), "", 0, "")
	if err != nil {
		t.Fatalf("SearchRegistry: %v", err)
	}
	if len(res.Servers) != len(cat.servers) {
		t.Fatalf("empty-query browse returned %d of %d catalog entries", len(res.Servers), len(cat.servers))
	}
	if res.Count != len(cat.servers) {
		t.Errorf("Count = %d, want %d", res.Count, len(cat.servers))
	}
	if res.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty", res.NextCursor)
	}

	// An explicit positive limit still caps a search result.
	capped, err := svc.SearchRegistry(context.Background(), "", 5, "")
	if err != nil {
		t.Fatalf("SearchRegistry(limit=5): %v", err)
	}
	if len(capped.Servers) != 5 {
		t.Errorf("limit=5 returned %d entries, want 5", len(capped.Servers))
	}
}

// TestServiceGetRegistryServerServesFromCatalog proves GetRegistryServer is
// served from the embedded catalog and returns ErrRegistryServerNotFound for an
// unknown name (no upstream lookup).
func TestServiceGetRegistryServerServesFromCatalog(t *testing.T) {
	svc, err := NewService(nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	dto, err := svc.GetRegistryServer(context.Background(), "io.github.github/github-mcp-server")
	if err != nil {
		t.Fatalf("GetRegistryServer: %v", err)
	}
	if dto.Name != "io.github.github/github-mcp-server" {
		t.Errorf("Name = %q", dto.Name)
	}

	_, err = svc.GetRegistryServer(context.Background(), "io.github.github/does-not-exist")
	if !errors.Is(err, ErrRegistryServerNotFound) {
		t.Fatalf("err = %v, want ErrRegistryServerNotFound", err)
	}
}

// TestInstallFromRegistryResolvesCatalogEntry proves InstallFromRegistry
// resolves the entry from the embedded catalog: an unknown name fails before
// any write, and a stdio-only entry resolves far enough to hit transport
// selection (no network or DB needed).
func TestInstallFromRegistryResolvesCatalogEntry(t *testing.T) {
	svc, err := NewService(nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	t.Run("unknown entry is not found", func(t *testing.T) {
		_, err := svc.InstallFromRegistry(context.Background(), "project-id", &InstallFromRegistryDTO{
			RegistryName: "io.github.github/does-not-exist",
		})
		if !errors.Is(err, ErrRegistryServerNotFound) {
			t.Fatalf("err = %v, want ErrRegistryServerNotFound", err)
		}
	})

	t.Run("stdio-only entry is rejected at transport selection", func(t *testing.T) {
		_, err := svc.InstallFromRegistry(context.Background(), "project-id", &InstallFromRegistryDTO{
			RegistryName: "microsoft/playwright-mcp",
		})
		if !errors.Is(err, ErrUnsupportedRegistryTransport) {
			t.Fatalf("err = %v, want ErrUnsupportedRegistryTransport", err)
		}
	})
}

func names2(servers []RegistryServerDTO) []string {
	out := make([]string, len(servers))
	for i, s := range servers {
		out[i] = s.Name
	}
	return out
}
