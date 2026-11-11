package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
)

func writeImportConfig(t *testing.T) (dir, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	cfgPath = filepath.Join(dir, "config.yml")
	body := "server_url: https://memory.example.test\ntoken: emt_secret\ninstance_id: test-connector\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir, cfgPath
}

func writeClaudeDoc(t *testing.T, dir, doc string) string {
	t.Helper()
	path := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write claude: %v", err)
	}
	return path
}

func TestRunMCPImportWritesConfig(t *testing.T) {
	dir, cfgPath := writeImportConfig(t)
	source := writeClaudeDoc(t, dir, `{"mcpServers":{
		"local":{"command":"npx","args":["-y","pkg"]},
		"remote":{"type":"http","url":"https://example.test/mcp"}
	}}`)

	var out, errBuf bytes.Buffer
	code := runMCPImport([]string{"--config", cfgPath, "--from-claude", source}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "2 created") {
		t.Errorf("stdout = %q", out.String())
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("servers = %d, want 2", len(cfg.MCPServers))
	}
}

func TestRunMCPImportDryRunDoesNotWrite(t *testing.T) {
	dir, cfgPath := writeImportConfig(t)
	before, _ := os.ReadFile(cfgPath)
	source := writeClaudeDoc(t, dir, `{"mcpServers":{"local":{"command":"npx"}}}`)

	var out, errBuf bytes.Buffer
	code := runMCPImport([]string{"--config", cfgPath, "--from-claude", source, "--dry-run"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "[dry run]") {
		t.Errorf("stdout = %q", out.String())
	}
	after, _ := os.ReadFile(cfgPath)
	if !bytes.Equal(before, after) {
		t.Error("dry run modified the config file")
	}
}

func TestRunMCPImportSkipsUnlessOverwrite(t *testing.T) {
	dir, cfgPath := writeImportConfig(t)
	source := writeClaudeDoc(t, dir, `{"mcpServers":{"local":{"command":"new"}}}`)

	var out, errBuf bytes.Buffer
	if code := runMCPImport([]string{"--config", cfgPath, "--from-claude", source}, &out, &errBuf); code != 0 {
		t.Fatalf("seed code = %d: %s", code, errBuf.String())
	}
	out.Reset()
	errBuf.Reset()
	if code := runMCPImport([]string{"--config", cfgPath, "--from-claude", source}, &out, &errBuf); code != 0 {
		t.Fatalf("rerun code = %d: %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "1 skipped") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestRunMCPImportRequiresSource(t *testing.T) {
	_, cfgPath := writeImportConfig(t)
	var out, errBuf bytes.Buffer
	code := runMCPImport([]string{"--config", cfgPath}, &out, &errBuf)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "--from-claude is required") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

func TestRunMCPImportRejectsMalformedJSON(t *testing.T) {
	dir, cfgPath := writeImportConfig(t)
	source := writeClaudeDoc(t, dir, `not json`)
	var out, errBuf bytes.Buffer
	code := runMCPImport([]string{"--config", cfgPath, "--from-claude", source}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
}

func TestRunMCPImportRefusesWhenLocked(t *testing.T) {
	_, cfgPath := writeImportConfig(t)
	release, err := acquireLock(cfgPath + ".lock")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	var out, errBuf bytes.Buffer
	code := runMCPImport([]string{"--config", cfgPath, "--from-claude", "-"}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errBuf.String(), "connector is running") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}
