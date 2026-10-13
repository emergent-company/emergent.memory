package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- MCP Registry page handlers (GET list + POST install PRG) ---

// mcpRegistryTestServer registers the registry page + install routes, mirroring
// main.go's wiring.
func mcpRegistryTestServer(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/settings/mcp-registry", s.uiMCPRegistry)
	e.POST("/settings/mcp-registry/install", s.uiMCPRegistryInstall)
	e.POST("/settings/mcp-registry/install-to-device", s.uiMCPRegistryInstallToDevice)
	return e
}

func mcpRegistryGet(e *echo.Echo, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func mcpRegistryPost(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

// mcpRegistryTestBackend is the registry test backend: an embedded fakeMemory
// with registry search/install semantics plus error injection.
type mcpRegistryTestBackend struct {
	*fakeMemory
	searchResult *MCPRegistrySearchResult
	searchErr    error
	lastQuery    string
	lastCursor   string

	installResult *MCPRegistryInstallResult
	installErr    error
	installCalls  int
	lastRegName   string
	lastVersion   string
	lastName      string

	deviceInstallResult *MCPRegistryInstallResult
	deviceInstallErr    error
	deviceInstallCalls  int
	lastDeviceRegName   string
	lastInstanceID      string
}

func newMCPRegistryTestBackend() *mcpRegistryTestBackend {
	return &mcpRegistryTestBackend{fakeMemory: &fakeMemory{}}
}

func (b *mcpRegistryTestBackend) SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*MCPRegistrySearchResult, error) {
	b.lastQuery = query
	b.lastCursor = cursor
	if b.searchErr != nil {
		return nil, b.searchErr
	}
	if b.searchResult != nil {
		return b.searchResult, nil
	}
	return &MCPRegistrySearchResult{}, nil
}

func (b *mcpRegistryTestBackend) InstallRegistryServer(ctx context.Context, registryName, version, name string) (*MCPRegistryInstallResult, error) {
	b.installCalls++
	b.lastRegName, b.lastVersion, b.lastName = registryName, version, name
	if b.installErr != nil {
		return nil, b.installErr
	}
	if b.installResult != nil {
		return b.installResult, nil
	}
	return &MCPRegistryInstallResult{}, nil
}

// InstallRegistryServerToDevice records the device-install call. It is separate
// from InstallRegistryServer so a test can assert which path the handler took.
func (b *mcpRegistryTestBackend) InstallRegistryServerToDevice(ctx context.Context, registryName, version, name, instanceID string) (*MCPRegistryInstallResult, error) {
	b.deviceInstallCalls++
	b.lastDeviceRegName, b.lastInstanceID = registryName, instanceID
	if b.deviceInstallErr != nil {
		return nil, b.deviceInstallErr
	}
	if b.deviceInstallResult != nil {
		return b.deviceInstallResult, nil
	}
	return &MCPRegistryInstallResult{}, nil
}

// mcpRegistryRemoteResult is a success install result with two required env vars.
func mcpRegistryRemoteResult() *MCPRegistryInstallResult {
	return &MCPRegistryInstallResult{
		Server: &MCPServer{ID: "srv-1", Name: "github-mcp-server", Type: "http", Enabled: true},
		RequiredEnvVars: []MCPRegistryEnvVar{
			{Name: "GITHUB_PERSONAL_ACCESS_TOKEN", IsRequired: true, IsSecret: true},
			{Name: "GITHUB_ORG", IsRequired: true},
		},
	}
}

// --- list page ---

func TestUIMCPRegistryListRoute(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.searchResult = &MCPRegistrySearchResult{Servers: mcpRegistryPageFixture(), Count: 2}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryGet(e, "/settings/mcp-registry")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/mcp-registry = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"MCP Registry", "2 results",
		`data-testid="mcp-registry-search"`,
		"GitHub", "Chrome DevTools",
		`action="/settings/mcp-registry/install"`,
		`data-testid="mcp-registry-install-to-device"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /settings/mcp-registry missing %q", want)
		}
	}
}

func TestUIMCPRegistrySearchForwardsQuery(t *testing.T) {
	f := newMCPRegistryTestBackend()
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryGet(e, "/settings/mcp-registry?q=chrome-devtools&cursor=c1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if f.lastQuery != "chrome-devtools" {
		t.Errorf("backend query = %q, want chrome-devtools", f.lastQuery)
	}
	if f.lastCursor != "c1" {
		t.Errorf("backend cursor = %q, want c1", f.lastCursor)
	}
}

func TestUIMCPRegistryListLoadError(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.searchErr = errTest
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryGet(e, "/settings/mcp-registry")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "MCP registry unavailable") {
		t.Error("load-error state missing")
	}
}

// --- install flow ---

func TestUIMCPRegistryInstallRemoteRedirectsWithEnv(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.installResult = mcpRegistryRemoteResult()
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install", "registryName=io.github.github%2Fgithub-mcp-server")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install = %d, want 303", rec.Code)
	}
	want := "/settings/mcp-servers?created=1&env=" + "GITHUB_PERSONAL_ACCESS_TOKEN%2C+GITHUB_ORG"
	if loc := rec.Header().Get("Location"); loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
	if f.installCalls != 1 || f.lastRegName != "io.github.github/github-mcp-server" {
		t.Errorf("install record = calls %d name %q", f.installCalls, f.lastRegName)
	}
}

func TestUIMCPRegistryInstallNoEnvRedirectsPlain(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.installResult = &MCPRegistryInstallResult{Server: &MCPServer{ID: "srv-1", Name: "remote"}}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install", "registryName=io.github.x%2Fy")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?created=1" {
		t.Fatalf("install = %d %q, want 303 /settings/mcp-servers?created=1", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIMCPRegistryInstallErrorRedirectsToRegistry(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.installErr = fmt.Errorf("memory 400 bad_request: only offers stdio-based packages")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install", "registryName=io.github.ChromeDevTools%2Fchrome-devtools-mcp")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/settings/mcp-registry?err=") {
		t.Errorf("Location = %q, want redirect back to the registry page with an error", loc)
	}
	if f.installCalls != 1 {
		t.Errorf("install calls = %d, want 1 (delegated to memory)", f.installCalls)
	}
}

func TestUIMCPRegistryInstallStdioOnlyNoPartialInstall(t *testing.T) {
	// The gateway delegates to memory; memory's remote-only policy rejects a
	// stdio-only install before any write. The handler must surface that error
	// (redirect to the registry page) and must not fabricate a created server.
	f := newMCPRegistryTestBackend()
	f.installErr = fmt.Errorf("memory 400 bad_request: server only offers stdio-based packages")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install", "registryName=io.github.ChromeDevTools%2Fchrome-devtools-mcp")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-registry?err=") {
		t.Fatalf("stdio-only install = %d %q, want error redirect to the registry page", rec.Code, rec.Header().Get("Location"))
	}
	// no created=1 redirect, i.e. the page never reports a successful install.
	if strings.Contains(rec.Header().Get("Location"), "created=1") {
		t.Error("stdio-only rejection must not surface a created=1 redirect")
	}
}

func TestUIMCPRegistryInstallMissingName(t *testing.T) {
	f := newMCPRegistryTestBackend()
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install", "registryName=")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-registry?err=") {
		t.Fatalf("missing-name install = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if f.installCalls != 0 {
		t.Error("missing registryName must not reach the backend")
	}
}

// --- device install flow ---

func TestUIMCPRegistryInstallToDeviceRedirectsToDevices(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.deviceInstallResult = &MCPRegistryInstallResult{
		Server: &MCPServer{ID: "srv-2", Name: "chrome-devtools-mcp", Type: "stdio", Enabled: true},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install-to-device",
		"registryName=io.github.ChromeDevTools%2Fchrome-devtools-mcp&targetInstanceId=dev-1")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("device install = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/devices?mcp_installed=1" {
		t.Errorf("Location = %q, want /settings/devices?mcp_installed=1", loc)
	}
	if f.deviceInstallCalls != 1 || f.lastInstanceID != "dev-1" {
		t.Errorf("device install record = calls %d instance %q", f.deviceInstallCalls, f.lastInstanceID)
	}
	if f.lastDeviceRegName != "io.github.ChromeDevTools/chrome-devtools-mcp" {
		t.Errorf("device install registry name = %q", f.lastDeviceRegName)
	}
	// The device path must not fall through to the outbound install.
	if f.installCalls != 0 {
		t.Error("device install must not call InstallRegistryServer")
	}
}

// TestUIMCPRegistryInstallToDeviceRequiredConfigGoesToConfigure asserts a device
// install that still needs credentials lands on the configure form, exactly like
// the outbound install, instead of the success redirect.
func TestUIMCPRegistryInstallToDeviceRequiredConfigGoesToConfigure(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.deviceInstallResult = &MCPRegistryInstallResult{
		Server:         &MCPServer{ID: "srv-3", Name: "thing", Type: "stdio"},
		RequiredConfig: []MCPRequiredConfigKey{{Name: "API_KEY", Kind: "env", IsRequired: true}},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install-to-device",
		"registryName=io.github.x%2Fy&targetInstanceId=dev-1")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("device install = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/mcp-servers/srv-3/edit?configure=1" {
		t.Errorf("Location = %q, want the configure form", loc)
	}
}

func TestUIMCPRegistryInstallToDeviceRequiresDevice(t *testing.T) {
	f := newMCPRegistryTestBackend()
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install-to-device",
		"registryName=io.github.x%2Fy&targetInstanceId=")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-registry?err=") {
		t.Fatalf("deviceless install = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if f.deviceInstallCalls != 0 {
		t.Error("a device install without an instance id must not reach the backend")
	}
}

func TestUIMCPRegistryInstallToDeviceRequiresRegistryName(t *testing.T) {
	f := newMCPRegistryTestBackend()
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install-to-device", "registryName=&targetInstanceId=dev-1")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-registry?err=") {
		t.Fatalf("nameless device install = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if f.deviceInstallCalls != 0 {
		t.Error("a device install without a registry name must not reach the backend")
	}
}

// TestUIMCPRegistryInstallToDeviceErrorRedirectsToRegistry asserts an
// unauthorized device target surfaces as an error on the registry page, with no
// success redirect (no partially-installed server is reported).
func TestUIMCPRegistryInstallToDeviceErrorRedirectsToRegistry(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.deviceInstallErr = fmt.Errorf("memory 403 forbidden: not authorized to target this device")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install-to-device",
		"registryName=io.github.x%2Fy&targetInstanceId=dev-1")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("device install = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/settings/mcp-registry?err=") {
		t.Fatalf("Location = %q, want error redirect to the registry page", loc)
	}
	if strings.Contains(loc, "mcp_installed") || strings.Contains(loc, "created=1") {
		t.Error("a rejected device install must not surface a success redirect")
	}
}
