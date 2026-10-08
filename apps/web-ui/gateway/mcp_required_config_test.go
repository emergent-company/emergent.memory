package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// Tests for the mcp-required-config UI surface: the list's needs-config badge +
// Configure link, the edit form's required-row markers + configure banner, the
// install redirect to the edit page, the env-name join (now including header
// names), and the save-less test-connection handler.

// mcpNeedsConfigServer is an http server whose required Authorization header is
// still blank (needs configuration). Its required key carries the registry's
// description but, like GitHub's real entry, no value template.
func mcpNeedsConfigServer() MCPServer {
	return MCPServer{
		ID: "srv-http", Name: "github-mcp", Type: "http", Enabled: false,
		URL: "https://github.example.com/mcp",
		RequiredConfig: []MCPRequiredConfigKey{
			{
				Name: "Authorization", Kind: "header", IsSecret: true, IsRequired: true,
				Description: "Authorization header with authentication token (PAT or App token)",
			},
		},
		NeedsConfiguration: true,
		MissingConfigKeys:  []string{"Authorization"},
	}
}

// TestRenderMCPServersPageNeedsConfig asserts the list row for a needs-config
// server shows the warning badge (with the data-mcp-needs-config anchor), the
// expanded-body alert naming the missing keys, and a Configure link to the
// edit page in configure mode.
func TestRenderMCPServersPageNeedsConfig(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: []MCPServer{mcpNeedsConfigServer()}}))
	for _, want := range []string{
		"Needs configuration",
		`data-mcp-needs-config="srv-http"`,
		`data-mcp-needs-config-alert="srv-http"`,
		"Missing configuration: Authorization.",
		`data-mcp-configure-link="srv-http"`,
		`href="/settings/mcp-servers/srv-http/edit?configure=1"`,
		"Configure",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("list needs-config missing %q", want)
		}
	}
}

// TestRenderMCPServersPageNoNeedsConfig asserts a fully-configured server does
// not render the needs-config badge or Configure link.
func TestRenderMCPServersPageNoNeedsConfig(t *testing.T) {
	srv := mcpNeedsConfigServer()
	srv.NeedsConfiguration = false
	srv.MissingConfigKeys = nil
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: []MCPServer{srv}}))
	for _, want := range []string{
		"Needs configuration",
		`data-mcp-needs-config="srv-http"`,
		`data-mcp-configure-link="srv-http"`,
	} {
		if strings.Contains(html, want) {
			t.Errorf("configured server must not render %q", want)
		}
	}
}

// TestRenderMCPServerEditPageRequiredConfig asserts the edit form (configure
// mode) marks the required row, prompts for the missing value, and shows the
// configure banner naming the missing keys.
func TestRenderMCPServerEditPageRequiredConfig(t *testing.T) {
	s := MCPServer{
		ID: "srv-http", Name: "github-mcp", Type: "http",
		URL: "https://github.example.com/mcp",
		RequiredConfig: []MCPRequiredConfigKey{
			{
				Name: "Authorization", Kind: "header", IsSecret: true, IsRequired: true,
				Description: "Authorization header with authentication token (PAT or App token)",
			},
		},
		MissingConfigKeys: []string{"Authorization"},
		SecretHeadersKeys: []string{"Authorization"},
	}
	data := mcpServerFormDataFromServerConfigure(s, true)
	html := renderHTML(t, MCPServerEditPage(data))
	for _, want := range []string{
		`data-mcp-configure-banner="srv-http"`,
		"This server needs configuration before it can be enabled. Missing: Authorization.",
		`data-mcp-kv-required`,
		`data-mcp-kv-missing`,
		"Required — enter a value.",
		`placeholder="Enter value"`,
		// the registry's own guidance for the required key is shown under the row
		`data-mcp-kv-hint`,
		"Authorization header with authentication token (PAT or App token)",
		// hidden serverId + test button are present on the edit form
		`name="serverId" value="srv-http"`,
		"data-mcp-test",
		"Test connection",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("edit required-config form missing %q", want)
		}
	}
	// the missing secret row must not advertise the leave-blank contract
	if strings.Contains(html, "Leave blank to keep the stored value.") {
		t.Error("missing required secret row must prompt for a value, not keep-blank")
	}
}

// TestRenderMCPServerEditPageRequiredConfigValueHint asserts a required header
// whose registry entry declares a value template renders that template as the
// value input's placeholder (never prefilled), alongside the description hint.
func TestRenderMCPServerEditPageRequiredConfigValueHint(t *testing.T) {
	s := MCPServer{
		ID: "srv-http", Name: "netdata-mcp", Type: "http",
		URL: "https://app.netdata.cloud/mcp",
		RequiredConfig: []MCPRequiredConfigKey{
			{
				Name: "Authorization", Kind: "header", IsSecret: true, IsRequired: true,
				Description: "Netdata Cloud API token with scope:mcp",
				ValueHint:   "Bearer {NETDATA_CLOUD_API_TOKEN}",
			},
		},
		MissingConfigKeys: []string{"Authorization"},
		SecretHeadersKeys: []string{"Authorization"},
	}
	data := mcpServerFormDataFromServerConfigure(s, true)
	html := renderHTML(t, MCPServerEditPage(data))
	for _, want := range []string{
		`data-mcp-kv-hint`,
		"Netdata Cloud API token with scope:mcp",
		`placeholder="Bearer {NETDATA_CLOUD_API_TOKEN}"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("edit required-config value-hint form missing %q", want)
		}
	}
	// the template must be a hint only — the input value stays blank
	if strings.Contains(html, `value="Bearer {NETDATA_CLOUD_API_TOKEN}"`) {
		t.Error("registry value template must be a placeholder, never prefilled")
	}
}

// TestRenderMCPServerEditPageNoConfigureBanner asserts the configure banner is
// absent when the page is not opened in configure mode.
func TestRenderMCPServerEditPageNoConfigureBanner(t *testing.T) {
	s := mcpNeedsConfigServer()
	data := mcpServerFormDataFromServerConfigure(s, false)
	html := renderHTML(t, MCPServerEditPage(data))
	if strings.Contains(html, "data-mcp-configure-banner") {
		t.Error("configure banner must not render without ?configure=1")
	}
}

// TestRenderMCPServerNewPageTestButton asserts the create form also offers the
// save-less Test connection button.
func TestRenderMCPServerNewPageTestButton(t *testing.T) {
	html := renderHTML(t, MCPServerNewPage(mcpServerFormData{Type: "stdio", Enabled: true}))
	for _, want := range []string{"data-mcp-test", "Test connection", `data-mcp-test-result`} {
		if !strings.Contains(html, want) {
			t.Errorf("create form missing %q", want)
		}
	}
	if strings.Contains(html, `name="serverId"`) {
		t.Error("create form must not render a serverId hidden input")
	}
}

// TestUIMCPRegistryInstallRequiredConfigRedirectsToEdit asserts an install that
// declares required config redirects straight to the server's edit page in
// configure mode rather than the list.
func TestUIMCPRegistryInstallRequiredConfigRedirectsToEdit(t *testing.T) {
	f := newMCPRegistryTestBackend()
	f.installResult = &MCPRegistryInstallResult{
		Server: &MCPServer{ID: "srv-1", Name: "github-mcp-server", Type: "http"},
		RequiredConfig: []MCPRequiredConfigKey{
			{Name: "Authorization", Kind: "header", IsSecret: true, IsRequired: true},
		},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpRegistryTestServer(s)

	rec := mcpRegistryPost(e, "/settings/mcp-registry/install", "registryName=io.github.github%2Fgithub-mcp-server")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/mcp-servers/srv-1/edit?configure=1" {
		t.Errorf("Location = %q, want edit page with configure=1", loc)
	}
}

// TestMCPRegistryRequiredEnvNamesIncludesHeaders asserts the env-name join now
// appends required header names from RequiredConfig (deduped against env names).
func TestMCPRegistryRequiredEnvNamesIncludesHeaders(t *testing.T) {
	res := &MCPRegistryInstallResult{
		RequiredEnvVars: []MCPRegistryEnvVar{{Name: "GITHUB_TOKEN"}},
		RequiredConfig: []MCPRequiredConfigKey{
			{Name: "Authorization", Kind: "header"},
			{Name: "GITHUB_TOKEN", Kind: "env"}, // duplicate of an env name → dropped
		},
	}
	if got := mcpRegistryRequiredEnvNames(res); got != "GITHUB_TOKEN, Authorization" {
		t.Errorf("env names = %q, want GITHUB_TOKEN, Authorization", got)
	}
}

// mcpTestConfigBackend records the TestMCPServer probe inputs (draft server +
// serverId) for the handler proxy test.
type mcpTestConfigBackend struct {
	*fakeMemory
	testResult   *MCPInspectResult
	testErr      error
	lastIn       *MCPServer
	lastServerID string
}

func (b *mcpTestConfigBackend) TestMCPServer(ctx context.Context, in *MCPServer, serverID string) (*MCPInspectResult, error) {
	b.lastIn = in
	b.lastServerID = serverID
	if b.testErr != nil {
		return nil, b.testErr
	}
	if b.testResult != nil {
		return b.testResult, nil
	}
	return &MCPInspectResult{ServerID: serverID, Status: "ok"}, nil
}

// TestUIMCPServersTestConnectionProxies asserts the /api/mcp-servers/test
// handler parses the live form into a draft, forwards it plus the serverId to
// memory, and returns the inspect envelope.
func TestUIMCPServersTestConnectionProxies(t *testing.T) {
	b := &mcpTestConfigBackend{
		fakeMemory: &fakeMemory{},
		testResult: &MCPInspectResult{ServerID: "srv-1", ServerName: "probe", Status: "ok"},
	}
	s := &Server{cfg: Config{}, memory: b}
	e := echo.New()
	e.POST("/api/mcp-servers/test", s.testMCPServer)

	rec := mcpServerPost(e, "/api/mcp-servers/test",
		"name=gh&type=http&url=https%3A%2F%2Fmcp.example.com&headers.key=Authorization&headers.value=Bearer+abc&serverId=srv-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("test = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if b.lastServerID != "srv-1" {
		t.Errorf("serverID = %q, want srv-1", b.lastServerID)
	}
	if b.lastIn == nil {
		t.Fatal("draft not forwarded to memory")
	}
	if b.lastIn.Name != "gh" || b.lastIn.Type != "http" || b.lastIn.URL != "https://mcp.example.com" {
		t.Errorf("draft = %+v, want name/type/url parsed", b.lastIn)
	}
	if b.lastIn.Headers["Authorization"] != "Bearer abc" {
		t.Errorf("headers = %v, want Authorization forwarded", b.lastIn.Headers)
	}
	body := rec.Body.String()
	for _, want := range []string{`"status":"ok"`, `"serverName":"probe"`} {
		if !strings.Contains(body, want) {
			t.Errorf("test body missing %q (%s)", want, body)
		}
	}
}

// TestUIMCPServersTestConnectionErrorIsJSON asserts a memory failure surfaces as
// a 502 JSON error (the page surfaces the message).
func TestUIMCPServersTestConnectionErrorIsJSON(t *testing.T) {
	b := &mcpTestConfigBackend{fakeMemory: &fakeMemory{}}
	b.testErr = &memoryHTTPError{Status: http.StatusBadGateway, Code: "mcp_connection_error", Message: "connection refused"}
	s := &Server{cfg: Config{}, memory: b}
	e := echo.New()
	e.POST("/api/mcp-servers/test", s.testMCPServer)

	rec := mcpServerPost(e, "/api/mcp-servers/test", "name=gh&type=http&url=https%3A%2F%2Fmcp.example.com")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("test failure = %d %s, want 502 with the memory error", rec.Code, rec.Body.String())
	}
}
