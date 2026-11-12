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

// TestPackSchemaJSONKeepsNestedUIView covers the `ui.view` half of the same
// promotion path: a type's ui block may carry a presentation plan beside its
// icon/colour, and the plan is nested (sections hold relation lists). Promotion
// must forward the nested shape verbatim — a flatten-to-known-fields step here
// would silently strip every declared section, and a pack's view would only
// ever work when it was written into the registry by hand.
func TestPackSchemaJSONKeepsNestedUIView(t *testing.T) {
	p := PackManifest{
		Name: "p",
		ObjectTypes: []ObjectTypeDef{{
			Name: "BugReport",
			UI: map[string]any{
				"icon": "lucide--bug",
				"view": map[string]any{
					"inline": []any{"severity", "source"},
					"sections": []any{
						map[string]any{"title": "Affects", "relation": "affects", "dir": "out"},
						map[string]any{"title": "Fixed by", "relation": []any{"fixes"}, "limit": 4},
					},
				},
			},
		}},
	}

	_, _, raw, err := packSchemaJSON(p)
	if err != nil {
		t.Fatalf("packSchemaJSON: %v", err)
	}
	var got map[string]struct {
		Icon string `json:"icon"`
		View struct {
			Inline   []string `json:"inline"`
			Sections []struct {
				Title    string `json:"title"`
				Relation any    `json:"relation"`
				Dir      string `json:"dir"`
				Limit    int    `json:"limit"`
			} `json:"sections"`
		} `json:"view"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal uiConfigs %s: %v", raw, err)
	}
	view := got["BugReport"].View
	if got["BugReport"].Icon != "lucide--bug" {
		t.Fatalf("icon lost beside the view: %v", got["BugReport"])
	}
	if len(view.Inline) != 2 || view.Inline[0] != "severity" {
		t.Fatalf("inline = %v, want [severity source]", view.Inline)
	}
	if len(view.Sections) != 2 {
		t.Fatalf("sections = %d, want 2 (the plan must survive promotion)", len(view.Sections))
	}
	if view.Sections[0].Title != "Affects" || view.Sections[0].Relation != "affects" || view.Sections[0].Dir != "out" {
		t.Fatalf("first section = %+v, want the bare-string relation form preserved", view.Sections[0])
	}
	if view.Sections[1].Limit != 4 {
		t.Fatalf("second section = %+v, want limit 4", view.Sections[1])
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
