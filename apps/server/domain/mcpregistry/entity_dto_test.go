package mcpregistry

import (
	"encoding/json"
	"strings"
	"testing"
)

// toolWithSchemas builds one cached tool carrying everything a full row can
// carry, so a summary projection that forgets a field is visible.
func toolWithSchemas() *MCPServerTool {
	desc := "Search the web"
	return &MCPServerTool{
		ID:           "tool-1",
		ServerID:     "srv-1",
		ToolName:     "web_search",
		Description:  &desc,
		InputSchema:  map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
		OutputSchema: map[string]any{"type": "object"},
		Enabled:      true,
		Config:       map[string]any{"api_key": "x"},
		ConfigKeys:   []string{"api_key"},
	}
}

// TestMCPServerToDTO_OmitsToolSchemas pins #210: the list payload is a
// catalogue. Tool schemas are the bulk of its bytes (57 kB of input schemas for
// the 141 builtin tools on production) and no list consumer reads them, so
// ToDTO() must project the summary shape while ToDetailDTO() keeps the full
// rows for the per-server endpoints.
func TestMCPServerToDTO_OmitsToolSchemas(t *testing.T) {
	srv := &MCPServer{
		ID:        "srv-1",
		ProjectID: "proj-1",
		Name:      "builtin",
		Type:      ServerTypeBuiltin,
		Enabled:   true,
		Tools:     []*MCPServerTool{toolWithSchemas()},
	}

	list := srv.ToDTO()
	if list.ToolCount != 1 {
		t.Errorf("ToolCount = %d, want 1", list.ToolCount)
	}
	if len(list.Tools) != 1 {
		t.Fatalf("list Tools = %d, want 1", len(list.Tools))
	}
	got := list.Tools[0]
	if got.ToolName != "web_search" {
		t.Errorf("ToolName = %q, want web_search", got.ToolName)
	}
	if got.Description == nil || *got.Description != "Search the web" {
		t.Errorf("Description = %v, want the cached description", got.Description)
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}
	if got.InputSchema != nil || got.OutputSchema != nil || got.Config != nil || len(got.ConfigKeys) != 0 {
		t.Errorf("list tool carries detail-only fields: schema=%v/%v config=%v keys=%v",
			got.InputSchema != nil, got.OutputSchema != nil, got.Config != nil, got.ConfigKeys)
	}

	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{"inputSchema", "outputSchema", "configKeys"} {
		if strings.Contains(string(raw), field) {
			t.Errorf("list JSON must not carry %q: %s", field, raw)
		}
	}

	detail := srv.ToDetailDTO()
	if len(detail.Tools) != 1 {
		t.Fatalf("detail Tools = %d, want 1", len(detail.Tools))
	}
	if detail.Tools[0].InputSchema == nil {
		t.Error("detail tools must keep their input schema — it is the reason the detail endpoint exists")
	}
	if detail.Tools[0].OutputSchema == nil {
		t.Error("detail tools must keep their output schema")
	}
}
