package blueprints

import (
	"encoding/json"
	"testing"
)

// TestPackSchemaJSONPromotesUIConfigs pins #291 on the server-side apply path:
// the registry stores appearance per type (graph_schemas.ui_configs[typeName]),
// so a pack that declares an inline `ui:` block and/or a pack-level uiConfigs
// block must have both promoted into that map. Before the fix only the inline
// block was considered and the top-level block was dropped at decode, leaving
// every registry row's ui_config as '{}'.
func TestPackSchemaJSONPromotesUIConfigs(t *testing.T) {
	p := PackManifest{
		Name: "p",
		ObjectTypes: []ObjectTypeDef{
			{Name: "Doc", UI: map[string]any{"icon": "inline-icon"}},
			{Name: "InlineOnly", UI: map[string]any{"icon": "only-inline"}},
			{Name: "Note"},
		},
		UIConfigs: map[string]json.RawMessage{
			"Doc":  json.RawMessage(`{"icon":"pack-icon"}`),
			"Note": json.RawMessage(`{"icon":"note-icon"}`),
		},
	}

	_, _, raw, err := packSchemaJSON(p)
	if err != nil {
		t.Fatalf("packSchemaJSON: %v", err)
	}

	var got map[string]map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal uiConfigs %s: %v", raw, err)
	}
	if got["Doc"]["icon"] != "inline-icon" {
		t.Fatalf("inline ui block must win over the pack-level entry: %v", got["Doc"])
	}
	if got["InlineOnly"]["icon"] != "only-inline" {
		t.Fatalf("inline-only appearance lost: %v", got["InlineOnly"])
	}
	if got["Note"]["icon"] != "note-icon" {
		t.Fatalf("pack-level appearance lost: %v", got["Note"])
	}
	if len(got) != 3 {
		t.Fatalf("expected exactly the three declared types, got %v", got)
	}
}

// TestPackSchemaJSONOmitsUIConfigsWhenAbsent keeps the payload unchanged for a
// pack that declares no appearance at all — the schema row must not grow an
// empty `{}` map where there was none.
func TestPackSchemaJSONOmitsUIConfigsWhenAbsent(t *testing.T) {
	p := PackManifest{
		Name:        "p",
		ObjectTypes: []ObjectTypeDef{{Name: "Doc"}},
	}

	_, _, raw, err := packSchemaJSON(p)
	if err != nil {
		t.Fatalf("packSchemaJSON: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("expected no uiConfigs payload, got %s", raw)
	}
}
