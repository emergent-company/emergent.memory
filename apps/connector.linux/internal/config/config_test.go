package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/emergent-company/memory.web-ui/connector/internal/mcphost"
)

const validYAML = "server_url: https://memory.example.test\n" +
	"token: emt_projecttoken\n" +
	"project_id: 9f1c1a4b-9d11-4a2e-9a6d-1234567890ab\n" +
	"instance_id: mbp-connector\n"

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

func TestDefaultConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".config", "memory-connector.yml")
	if got := DefaultConfigPath(); got != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestDefaultConfigPathXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	want := filepath.Join(xdg, "memory-connector.yml")
	if got := DefaultConfigPath(); got != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestDefaultInstanceID(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("Hostname: %v", err)
	}
	want := host + "-connector"
	if got := DefaultInstanceID(); got != want {
		t.Errorf("DefaultInstanceID() = %q, want %q", got, want)
	}
}

func TestLoadValid(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.yml", validYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ServerURL != "https://memory.example.test" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Token != "emt_projecttoken" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if cfg.ProjectID != "9f1c1a4b-9d11-4a2e-9a6d-1234567890ab" {
		t.Errorf("ProjectID = %q", cfg.ProjectID)
	}
	if cfg.InstanceID != "mbp-connector" {
		t.Errorf("InstanceID = %q", cfg.InstanceID)
	}
}

func TestLoadDefaultsInstanceID(t *testing.T) {
	body := "server_url: https://memory.example.test\n" +
		"token: emt_projecttoken\n"
	path := writeFile(t, t.TempDir(), "config.yml", body)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.InstanceID != DefaultInstanceID() {
		t.Errorf("InstanceID = %q, want default %q", cfg.InstanceID, DefaultInstanceID())
	}
	if cfg.ProjectID != "" {
		t.Errorf("ProjectID = %q, want empty", cfg.ProjectID)
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yml")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load: expected error for missing file, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load error = %v, want wrapped os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Load error %q should name the path", err)
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.yml", "server_url: [unclosed\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load: expected error for malformed YAML, got nil")
	}
}

func TestLoadValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "empty server_url", body: "token: emt_x\n", want: "server_url"},
		{name: "empty token", body: "server_url: https://x.test\n", want: "token"},
		{name: "both empty", body: "", want: "server_url"},
		{name: "blank whitespace token", body: "server_url: https://x.test\ntoken: '  '\n", want: "token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), "config.yml", tc.body)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load: expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load error %q should mention %q", err, tc.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	if err := (&Config{ServerURL: "https://x.test", Token: "emt_x"}).Validate(); err != nil {
		t.Errorf("Validate(valid) = %v, want nil", err)
	}
	if err := (&Config{Token: "emt_x"}).Validate(); err == nil {
		t.Error("Validate(missing server_url) = nil, want error")
	}
	if err := (&Config{ServerURL: "https://x.test"}).Validate(); err == nil {
		t.Error("Validate(missing token) = nil, want error")
	}
}

func TestSaveCreatesParentDirAndMode0600(t *testing.T) {
	cfg := &Config{
		ServerURL:  "https://memory.example.test",
		Token:      "emt_projecttoken",
		ProjectID:  "9f1c1a4b-9d11-4a2e-9a6d-1234567890ab",
		InstanceID: "mbp-connector",
	}
	path := filepath.Join(t.TempDir(), "nested", "deeper", "config.yml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat saved config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("saved config mode = %o, want 600", perm)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("round trip mismatch: got %+v, want %+v", got, cfg)
	}
}

func TestSaveDefaultPathWritesLane(t *testing.T) {
	// Ensure Save works when filepath.Dir resolves to "." (no explicit parent).
	dir := t.TempDir()
	cfg := &Config{ServerURL: "https://x.test", Token: "emt_x"}
	path := filepath.Join(dir, "config.yml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

func TestLoadDisabledToolsPresent(t *testing.T) {
	body := "server_url: https://memory.example.test\n" +
		"token: emt_projecttoken\n" +
		"disabled_tools:\n" +
		"  - reminders_list\n" +
		"  - notes_search\n"
	path := writeFile(t, t.TempDir(), "config.yml", body)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"reminders_list", "notes_search"}
	if len(cfg.DisabledTools) != 2 || cfg.DisabledTools[0] != want[0] || cfg.DisabledTools[1] != want[1] {
		t.Errorf("DisabledTools = %v, want %v", cfg.DisabledTools, want)
	}
}

func TestLoadDisabledToolsAbsent(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.yml", validYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.DisabledTools) != 0 {
		t.Errorf("DisabledTools = %v, want empty when key absent", cfg.DisabledTools)
	}
}

func TestSaveLoadDisabledToolsRoundTrip(t *testing.T) {
	cfg := &Config{
		ServerURL:     "https://memory.example.test",
		Token:         "emt_projecttoken",
		InstanceID:    "mbp-connector",
		DisabledTools: []string{"notes_create", "reminders_add"},
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "disabled_tools:") {
		t.Errorf("saved YAML missing disabled_tools:\n%s", raw)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.DisabledTools, cfg.DisabledTools) {
		t.Errorf("DisabledTools round trip = %v, want %v", got.DisabledTools, cfg.DisabledTools)
	}
}

func TestSaveOmitsDisabledToolsWhenEmpty(t *testing.T) {
	cfg := &Config{ServerURL: "https://memory.example.test", Token: "emt_x"}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "disabled_tools") {
		t.Errorf("empty disabled_tools must be omitted from YAML:\n%s", raw)
	}
}

func TestLoadIgnoresUnknownDisabledToolNames(t *testing.T) {
	// Unknown names must not fail config load; they are simply ignored at
	// filter time.
	body := "server_url: https://memory.example.test\n" +
		"token: emt_x\n" +
		"disabled_tools:\n" +
		"  - no_such_tool\n"
	path := writeFile(t, t.TempDir(), "config.yml", body)
	if _, err := Load(path); err != nil {
		t.Fatalf("Load with unknown disabled tool: %v", err)
	}
}

func TestMaterializeDefaultsInstanceID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yml")
	if err := Materialize(path, "https://srv.test", "emt_tok", "p1"); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Materialize: %v", err)
	}
	if cfg.ServerURL != "https://srv.test" || cfg.Token != "emt_tok" || cfg.ProjectID != "p1" {
		t.Errorf("materialized config = %+v", cfg)
	}
	if cfg.InstanceID != DefaultInstanceID() {
		t.Errorf("InstanceID = %q, want default %q", cfg.InstanceID, DefaultInstanceID())
	}
}

func TestMaterializePreservesInstanceIDAndDisabledTools(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.yml",
		"server_url: https://old.test\n"+
			"token: old-token\n"+
			"instance_id: my-stable-id\n"+
			"disabled_tools:\n"+
			"  - notes_create\n"+
			"  - reminders_add\n")
	if err := Materialize(path, "https://new.test", "emt_new", "p-new"); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Materialize: %v", err)
	}
	if cfg.ServerURL != "https://new.test" || cfg.Token != "emt_new" || cfg.ProjectID != "p-new" {
		t.Errorf("server/token/project = %q/%q/%q", cfg.ServerURL, cfg.Token, cfg.ProjectID)
	}
	if cfg.InstanceID != "my-stable-id" {
		t.Errorf("InstanceID = %q, want preserved my-stable-id", cfg.InstanceID)
	}
	if !reflect.DeepEqual(cfg.DisabledTools, []string{"notes_create", "reminders_add"}) {
		t.Errorf("DisabledTools = %v, want preserved list", cfg.DisabledTools)
	}
}

func TestLoadToolsSection(t *testing.T) {
	body := validYAML +
		"tools:\n" +
		"  linux:\n" +
		"    host_info:\n" +
		"      enabled: true\n" +
		"    filesystem:\n" +
		"      enabled: true\n" +
		"      roots:\n" +
		"        - path: /srv/scratch\n" +
		"          read: true\n" +
		"          write: true\n" +
		"          delete: true\n" +
		"      max_read_bytes: 1048576\n" +
		"      max_write_bytes: 2097152\n" +
		"      allow_permanent_delete: true\n"
	cfg, err := Load(writeFile(t, t.TempDir(), "config.yml", body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	lc := cfg.Tools.Linux
	if lc.HostInfo == nil || lc.HostInfo.Enabled == nil || !*lc.HostInfo.Enabled {
		t.Errorf("HostInfo = %+v, want enabled=true", lc.HostInfo)
	}
	fs := lc.Filesystem
	if fs == nil {
		t.Fatal("Filesystem = nil, want parsed")
	}
	if fs.Enabled == nil || !*fs.Enabled {
		t.Errorf("Filesystem.Enabled = %v, want true", fs.Enabled)
	}
	wantRoots := []RootConfig{{Path: "/srv/scratch", Read: true, Write: true, Delete: true}}
	if !reflect.DeepEqual(fs.Roots, wantRoots) {
		t.Errorf("Roots = %+v, want %+v", fs.Roots, wantRoots)
	}
	if fs.MaxReadBytes != 1048576 || fs.MaxWriteBytes != 2097152 {
		t.Errorf("max bytes = (%d, %d), want (1048576, 2097152)", fs.MaxReadBytes, fs.MaxWriteBytes)
	}
	if fs.AllowPermanentDelete == nil || !*fs.AllowPermanentDelete {
		t.Errorf("AllowPermanentDelete = %v, want true", fs.AllowPermanentDelete)
	}
}

func TestLoadWithoutToolsLeavesDefaults(t *testing.T) {
	cfg, err := Load(writeFile(t, t.TempDir(), "config.yml", validYAML))
	if err != nil {
		t.Fatalf("Load (old config without tools): %v", err)
	}
	if cfg.Tools.Linux.HostInfo != nil {
		t.Errorf("HostInfo = %+v, want nil (enabled by default)", cfg.Tools.Linux.HostInfo)
	}
	if cfg.Tools.Linux.Filesystem != nil {
		t.Errorf("Filesystem = %+v, want nil (no filesystem tools)", cfg.Tools.Linux.Filesystem)
	}
}

func TestToolsConfigRoundTrip(t *testing.T) {
	enabled, allow := true, false
	cfg := &Config{
		ServerURL:  "https://srv.test",
		Token:      "emt_x",
		InstanceID: "inst-1",
		Tools: ToolsConfig{Linux: LinuxToolsConfig{
			HostInfo: &HostInfoConfig{Enabled: &enabled},
			Filesystem: &FilesystemConfig{
				Enabled:              &enabled,
				Roots:                []RootConfig{{Path: "/data", Read: true, Write: true}},
				MaxReadBytes:         1024,
				MaxWriteBytes:        2048,
				AllowPermanentDelete: &allow,
			},
		}},
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.Tools, cfg.Tools) {
		t.Errorf("Tools round-trip = %+v, want %+v", got.Tools, cfg.Tools)
	}
}

func TestSaveOmitsEmptyToolsSection(t *testing.T) {
	cfg := &Config{ServerURL: "https://srv.test", Token: "emt_x", InstanceID: "inst-1"}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "tools") {
		t.Errorf("saved config should omit an empty tools section:\n%s", raw)
	}
}

func TestLoadMCPServersParses(t *testing.T) {
	body := validYAML +
		"mcp_servers:\n" +
		"  - name: github\n" +
		"    transport: stdio\n" +
		"    command: github-mcp\n" +
		"    args: [\"--stdio\"]\n" +
		"    env:\n" +
		"      GITHUB_TOKEN: secret\n" +
		"    disabled_tools:\n" +
		"      - delete_repo\n" +
		"  - name: home\n" +
		"    transport: http\n" +
		"    url: https://home.example.test/mcp\n" +
		"    headers:\n" +
		"      Authorization: Bearer abc\n" +
		"  - name: legacy\n" +
		"    transport: sse\n" +
		"    url: https://legacy.example.test/sse\n" +
		"    enabled: false\n"
	path := writeFile(t, t.TempDir(), "config.yml", body)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.MCPServers) != 3 {
		t.Fatalf("MCPServers length = %d, want 3", len(cfg.MCPServers))
	}

	gh := cfg.MCPServers[0]
	if gh.Name != "github" || gh.Transport != mcphost.TransportStdio {
		t.Errorf("server[0] = %+v, want stdio github", gh)
	}
	if gh.Command != "github-mcp" || len(gh.Args) != 1 || gh.Args[0] != "--stdio" {
		t.Errorf("server[0] command/args = %q/%v", gh.Command, gh.Args)
	}
	if gh.Env["GITHUB_TOKEN"] != "secret" {
		t.Errorf("server[0] env = %v", gh.Env)
	}
	if len(gh.DisabledTools) != 1 || gh.DisabledTools[0] != "delete_repo" {
		t.Errorf("server[0] disabled_tools = %v", gh.DisabledTools)
	}
	if !gh.IsEnabled() {
		t.Error("server[0] enabled must default to true")
	}

	home := cfg.MCPServers[1]
	if home.Transport != mcphost.TransportHTTP || home.URL != "https://home.example.test/mcp" {
		t.Errorf("server[1] = %+v", home)
	}
	if home.Headers["Authorization"] != "Bearer abc" {
		t.Errorf("server[1] headers = %v", home.Headers)
	}

	legacy := cfg.MCPServers[2]
	if legacy.IsEnabled() {
		t.Error("server[2] enabled: false must be honored")
	}
}

func TestLoadMCPServersValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown transport",
			body: "  - name: x\n    transport: carrier-pigeon\n",
			want: "unknown transport",
		},
		{
			name: "missing transport",
			body: "  - name: x\n    command: y\n",
			want: "transport is required",
		},
		{
			name: "stdio missing command",
			body: "  - name: x\n    transport: stdio\n",
			want: "command is required",
		},
		{
			name: "http missing url",
			body: "  - name: x\n    transport: http\n",
			want: "url is required",
		},
		{
			name: "sse missing url",
			body: "  - name: x\n    transport: sse\n",
			want: "url is required",
		},
		{
			name: "duplicate names",
			body: "  - name: dup\n    transport: stdio\n    command: a\n" +
				"  - name: dup\n    transport: stdio\n    command: b\n",
			want: "duplicate server name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), "config.yml", validYAML+"mcp_servers:\n"+tc.body)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load: expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load error %q should mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadMCPServersDisabledIncompleteAllowed(t *testing.T) {
	// A disabled server is kept but not validated, so an incomplete entry
	// never blocks startup.
	body := validYAML +
		"mcp_servers:\n" +
		"  - name: off\n" +
		"    transport: stdio\n" +
		"    enabled: false\n"
	path := writeFile(t, t.TempDir(), "config.yml", body)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].IsEnabled() {
		t.Errorf("MCPServers = %+v, want one disabled server", cfg.MCPServers)
	}
}

func TestSaveLoadMCPServersRoundTrip(t *testing.T) {
	cfg := &Config{
		ServerURL:  "https://memory.example.test",
		Token:      "emt_projecttoken",
		InstanceID: "mbp-connector",
		MCPServers: []mcphost.ServerConfig{
			{Name: "github", Transport: mcphost.TransportStdio, Command: "github-mcp", Args: []string{"--stdio"}},
		},
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "mcp_servers:") {
		t.Errorf("saved YAML missing mcp_servers:\n%s", raw)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.MCPServers) != 1 || got.MCPServers[0].Name != "github" {
		t.Errorf("MCPServers round trip = %+v", got.MCPServers)
	}
}

func TestSaveOmitsMCPServersWhenEmpty(t *testing.T) {
	cfg := &Config{ServerURL: "https://memory.example.test", Token: "emt_x"}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "mcp_servers") {
		t.Errorf("empty mcp_servers must be omitted from YAML:\n%s", raw)
	}
}
