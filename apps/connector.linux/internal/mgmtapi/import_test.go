package mgmtapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcpimport"
)

func importBody(config string, overwrite, dryRun bool) map[string]any {
	return map[string]any{"config": json.RawMessage(config), "overwrite": overwrite, "dryRun": dryRun}
}

const sampleClaude = `{"mcpServers":{
	"local":{"command":"npx","args":["-y","pkg"],"env":{"TOKEN":"x"}},
	"remote":{"type":"http","url":"https://example.test/mcp","headers":{"Authorization":"Bearer y"}}
}}`

func TestImportCreatesServersAndReloads(t *testing.T) {
	h := newHarness(t)

	code, data := h.do(t, http.MethodPost, "/api/mcp-servers/import", importBody(sampleClaude, false, false))
	h.requireStatus(t, code, http.StatusOK)
	result := decodeInto[mcpimport.Result](t, data)
	if len(result.Created) != 2 || len(result.Failed) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if h.reloader.callCount() != 1 {
		t.Fatalf("reload calls = %d, want 1", h.reloader.callCount())
	}

	code, data = h.do(t, http.MethodGet, "/api/mcp-servers", nil)
	h.requireStatus(t, code, http.StatusOK)
	list := decodeInto[[]serverSummary](t, data)
	if len(list) != 2 {
		t.Fatalf("server count = %d, want 2", len(list))
	}

	if !strings.Contains(string(h.rawConfig(t)), "local") || !strings.Contains(string(h.rawConfig(t)), "remote") {
		t.Fatalf("imported servers not persisted: %s", h.rawConfig(t))
	}
}

func TestImportSkipsAndOverwrites(t *testing.T) {
	h := newHarness(t)

	// Seed an existing local server with a disabled_tools list and disabled state.
	code, _ := h.do(t, http.MethodPost, "/api/mcp-servers", map[string]any{
		"name": "local", "transport": "stdio", "command": "old", "enabled": false,
		"disabled_tools": []string{"danger"},
	})
	h.requireStatus(t, code, http.StatusCreated)

	doc := `{"mcpServers":{"local":{"command":"new"}}}`
	code, data := h.do(t, http.MethodPost, "/api/mcp-servers/import", importBody(doc, false, false))
	h.requireStatus(t, code, http.StatusOK)
	result := decodeInto[mcpimport.Result](t, data)
	if len(result.Skipped) != 1 || len(result.Updated) != 0 {
		t.Fatalf("skip result = %+v", result)
	}

	code, data = h.do(t, http.MethodPost, "/api/mcp-servers/import", importBody(doc, true, false))
	h.requireStatus(t, code, http.StatusOK)
	result = decodeInto[mcpimport.Result](t, data)
	if len(result.Updated) != 1 {
		t.Fatalf("overwrite result = %+v", result)
	}

	code, data = h.do(t, http.MethodGet, "/api/mcp-servers/local", nil)
	h.requireStatus(t, code, http.StatusOK)
	detail := decodeInto[map[string]any](t, data)
	if detail["command"] != "new" {
		t.Fatalf("command = %v, want new", detail["command"])
	}
	if detail["enabled"] != false {
		t.Fatalf("enabled = %v, want false (preserved)", detail["enabled"])
	}
	if tools, ok := detail["disabled_tools"].([]any); !ok || len(tools) != 1 || tools[0] != "danger" {
		t.Fatalf("disabled_tools = %v, want [danger]", detail["disabled_tools"])
	}
}

func TestImportDryRunDoesNotWrite(t *testing.T) {
	h := newHarness(t)
	before := string(h.rawConfig(t))

	code, data := h.do(t, http.MethodPost, "/api/mcp-servers/import", importBody(sampleClaude, false, true))
	h.requireStatus(t, code, http.StatusOK)
	result := decodeInto[mcpimport.Result](t, data)
	if !result.DryRun || len(result.Created) != 2 {
		t.Fatalf("dry-run result = %+v", result)
	}
	if h.reloader.callCount() != 0 {
		t.Fatalf("reload calls = %d, want 0", h.reloader.callCount())
	}
	if string(h.rawConfig(t)) != before {
		t.Fatal("dry-run must not modify the config file")
	}
}

func TestImportSkipsManagedServers(t *testing.T) {
	h := newHarness(t)
	h.server.SetManagedServers(func() []mcphost.ServerConfig {
		return []mcphost.ServerConfig{{Name: "local", Transport: mcphost.TransportStdio, Command: "memory-owned"}}
	})

	code, data := h.do(t, http.MethodPost, "/api/mcp-servers/import", importBody(sampleClaude, true, false))
	h.requireStatus(t, code, http.StatusOK)
	result := decodeInto[mcpimport.Result](t, data)
	if len(result.Skipped) != 1 || result.Skipped[0].Name != "local" {
		t.Fatalf("managed skip result = %+v", result)
	}
	if len(result.Created) != 1 || result.Created[0] != "remote" {
		t.Fatalf("created = %v, want [remote]", result.Created)
	}
}

func TestImportReportsBadEntriesAndImportsGoodOnes(t *testing.T) {
	h := newHarness(t)
	doc := `{"mcpServers":{
		"good":{"command":"npx"},
		"both":{"command":"npx","url":"https://x"},
		"unknown":{"type":"carrier-pigeon","url":"https://x"}
	}}`

	code, data := h.do(t, http.MethodPost, "/api/mcp-servers/import", importBody(doc, false, false))
	h.requireStatus(t, code, http.StatusOK)
	result := decodeInto[mcpimport.Result](t, data)
	if len(result.Created) != 1 || result.Created[0] != "good" {
		t.Fatalf("created = %v", result.Created)
	}
	if len(result.Failed) != 2 {
		t.Fatalf("failed = %+v, want 2", result.Failed)
	}
}

func TestImportRejectsMissingOrMalformedConfig(t *testing.T) {
	h := newHarness(t)

	code, _ := h.do(t, http.MethodPost, "/api/mcp-servers/import", map[string]any{})
	h.requireStatus(t, code, http.StatusBadRequest)

	code, _ = h.do(t, http.MethodPost, "/api/mcp-servers/import", map[string]any{"config": "not json"})
	h.requireStatus(t, code, http.StatusBadRequest)
}

func TestImportReloadFailureRollsBack(t *testing.T) {
	h := newHarness(t)
	h.reloader.setError(errors.New("boom"))

	code, _ := h.do(t, http.MethodPost, "/api/mcp-servers/import", importBody(sampleClaude, false, false))
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
	cfg, err := config.Load(h.config)
	if err != nil {
		t.Fatalf("load rolled-back config: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Fatalf("servers in rolled-back config = %d, want 0", len(cfg.MCPServers))
	}

	code, data := h.do(t, http.MethodGet, "/api/mcp-servers", nil)
	h.requireStatus(t, code, http.StatusOK)
	list := decodeInto[[]serverSummary](t, data)
	if len(list) != 0 {
		t.Fatalf("servers after failed reload = %d, want 0", len(list))
	}
}
