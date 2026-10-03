package schemas

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

// TestParseObjectTypeSchemas verifies that parseObjectTypeSchemas handles both
// the array format (user-uploaded YAML/JSON files) and the map format (blueprint
// seeds / epf-engine v3 schemas) without silently returning nil.
func TestParseObjectTypeSchemas(t *testing.T) {
	const packID = "pack-1"
	const packName = "test-schema"
	const packVersion = "1.0.0"

	t.Run("array format", func(t *testing.T) {
		data := json.RawMessage(`[
			{"name":"Belief","label":"Belief","description":"A belief","properties":{"text":{"type":"string"}}},
			{"name":"Person","label":"Person","ui":{"icon":"lucide--user","color":"#4F46E5"}}
		]`)

		got := parseObjectTypeSchemas(data, packID, packName, packVersion)
		if got == nil {
			t.Fatal("expected non-nil result for array format")
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 types, got %d", len(got))
		}
		names := map[string]bool{}
		for _, o := range got {
			names[o.Name] = true
			if o.SchemaID != packID {
				t.Errorf("expected SchemaID %q, got %q", packID, o.SchemaID)
			}
			if o.SchemaName != packName {
				t.Errorf("expected SchemaName %q, got %q", packName, o.SchemaName)
			}
			if o.SchemaVersion != packVersion {
				t.Errorf("expected SchemaVersion %q, got %q", packVersion, o.SchemaVersion)
			}
		}
		if !names["Belief"] || !names["Person"] {
			t.Errorf("expected type names Belief and Person, got %v", names)
		}
		// inline ui block is carried through to the compiled type
		for _, o := range got {
			if o.Name == "Person" && string(o.UI) != `{"icon":"lucide--user","color":"#4F46E5"}` {
				t.Errorf("expected ui block on Person, got %s", o.UI)
			}
		}
	})

	t.Run("map format (blueprint/epf-engine v3)", func(t *testing.T) {
		data := json.RawMessage(`{
			"Belief":  {"label":"Belief","description":"A belief","properties":{"text":{"type":"string"}}},
			"Person":  {"label":"Person","description":"A person","ui":{"icon":"lucide--user","color":"#4F46E5"}}
		}`)

		got := parseObjectTypeSchemas(data, packID, packName, packVersion)
		if got == nil {
			t.Fatal("expected non-nil result for map format")
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 types, got %d", len(got))
		}
		names := map[string]bool{}
		for _, o := range got {
			names[o.Name] = true
			if o.SchemaID != packID {
				t.Errorf("expected SchemaID %q, got %q", packID, o.SchemaID)
			}
		}
		if !names["Belief"] || !names["Person"] {
			t.Errorf("expected type names Belief and Person, got %v", names)
		}
		// inline ui block is carried through to the compiled type
		for _, o := range got {
			if o.Name == "Person" && string(o.UI) != `{"icon":"lucide--user","color":"#4F46E5"}` {
				t.Errorf("expected ui block on Person, got %s", o.UI)
			}
		}
	})

	t.Run("nil on empty data", func(t *testing.T) {
		got := parseObjectTypeSchemas(nil, packID, packName, packVersion)
		if got != nil {
			t.Errorf("expected nil for empty data, got %v", got)
		}
	})

	t.Run("nil on invalid json", func(t *testing.T) {
		got := parseObjectTypeSchemas(json.RawMessage(`not-json`), packID, packName, packVersion)
		if got != nil {
			t.Errorf("expected nil for invalid JSON, got %v", got)
		}
	})
}

// TestParseObjectTypeSchemasPreservesBoardWorkConfig pins the compiled-types
// contract for the blueprint/array storage format: the flagship
// `blueprints/task-board/schemas/task-board.yaml` is stored as a JSON array of
// object types, and the compiled-types path must carry `boardEnabled` /
// `allowedStatuses` (and the operational skip flags) through to the returned
// ObjectTypeSchema. Before this was fixed, parseObjectTypeSchemasToMap
// reconstructed each array entry with only properties/ui/scopeKey/label/
// description, so compiled types silently returned zero board config and the
// gateway fell back to canonical lanes.
func TestParseObjectTypeSchemasPreservesBoardWorkConfig(t *testing.T) {
	const packID = "pack-1"
	const packName = "task-board"
	const packVersion = "1.0.0"

	// Verbatim shape of blueprints/task-board/schemas/task-board.yaml objectTypes.
	data := json.RawMessage(`[
		{
			"name":"Task",
			"label":"Task",
			"description":"A single unit of work tracked on the Kanban board.",
			"boardEnabled":true,
			"allowedStatuses":["ready","in_progress","review","revision","blocked","done"],
			"skipEmbeddings":true,
			"skipExtraction":true,
			"excludeFromSearch":true,
			"properties":{"title":{"type":"string"}}
		}
	]`)

	got := parseObjectTypeSchemas(data, packID, packName, packVersion)
	if len(got) != 1 {
		t.Fatalf("expected 1 type, got %d", len(got))
	}
	task := got[0]
	if task.Name != "Task" {
		t.Fatalf("expected Task, got %q", task.Name)
	}
	if !task.BoardEnabled {
		t.Error("compiled Task.BoardEnabled = false, want true (array-form board config was dropped)")
	}
	want := []string{"ready", "in_progress", "review", "revision", "blocked", "done"}
	if !slices.Equal(task.AllowedStatuses, want) {
		t.Errorf("compiled Task.AllowedStatuses = %v, want %v (array-form board config was dropped)", task.AllowedStatuses, want)
	}
	if !task.SkipEmbeddings {
		t.Error("compiled Task.SkipEmbeddings = false, want true")
	}
	if !task.SkipExtraction {
		t.Error("compiled Task.SkipExtraction = false, want true")
	}
	if !task.ExcludeFromSearch {
		t.Error("compiled Task.ExcludeFromSearch = false, want true")
	}
	// The compiled JSON the gateway consumes must carry the board fields too.
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal compiled Task: %v", err)
	}
	for _, wantKey := range []string{`"boardEnabled":true`, `"allowedStatuses"`} {
		if !bytes.Contains(raw, []byte(wantKey)) {
			t.Errorf("compiled Task JSON missing %s: %s", wantKey, raw)
		}
	}
}

// TestParseObjectTypeSchemasPreservesBoardWorkConfigMapFormat is the map-format
// counterpart: the runtime extraction normalisation already preserves these
// fields for map-form storage, and the compiled path must agree.
func TestParseObjectTypeSchemasPreservesBoardWorkConfigMapFormat(t *testing.T) {
	data := json.RawMessage(`{
		"Task":{"boardEnabled":true,"allowedStatuses":["todo","doing"],"skipEmbeddings":true}
	}`)
	got := parseObjectTypeSchemas(data, "pack-1", "task-board", "1.0.0")
	if len(got) != 1 {
		t.Fatalf("expected 1 type, got %d", len(got))
	}
	task := got[0]
	if !task.BoardEnabled {
		t.Error("compiled Task.BoardEnabled = false, want true")
	}
	if !slices.Equal(task.AllowedStatuses, []string{"todo", "doing"}) {
		t.Errorf("compiled Task.AllowedStatuses = %v, want [todo doing]", task.AllowedStatuses)
	}
	if !task.SkipEmbeddings {
		t.Error("compiled Task.SkipEmbeddings = false, want true")
	}
}

// TestCompiledTypesUIResolution covers the type-level ui resolution used by the
// compiled-types path (GetCompiledTypesByProject): parseObjectTypeSchemas
// extracts any inline "ui" block, then applyUIConfigFallback fills it from the
// pack's top-level ui_configs map when absent. An explicit JSON null is treated
// as absent at both sources so it neither blocks the fallback nor leaks an
// "ui":null into the compiled output.
func TestCompiledTypesUIResolution(t *testing.T) {
	const packID = "pack-1"
	const packName = "test-schema"
	const packVersion = "1.0.0"

	inlineUI := `{"icon":"lucide--user","color":"#4F46E5"}`
	configUI := `{"icon":"lucide--folder","color":"#F59E0B"}`

	tests := []struct {
		name       string
		schemaData string // object_type_schemas raw (array or map storage format)
		uiConfigs  string // top-level ui_configs raw ("" = none)
		wantUI     string // expected ui bytes on the Person type ("" = absent)
	}{
		{
			name:       "inline ui present only",
			schemaData: `[{"name":"Person","label":"Person","ui":` + inlineUI + `}]`,
			wantUI:     inlineUI,
		},
		{
			name:       "ui_configs present only, fallback used",
			schemaData: `[{"name":"Person","label":"Person"}]`,
			uiConfigs:  `{"Person":` + configUI + `}`,
			wantUI:     configUI,
		},
		{
			name:       "inline ui present wins over ui_configs",
			schemaData: `[{"name":"Person","label":"Person","ui":` + inlineUI + `}]`,
			uiConfigs:  `{"Person":` + configUI + `}`,
			wantUI:     inlineUI,
		},
		{
			name:       "inline ui null falls back to ui_configs",
			schemaData: `[{"name":"Person","label":"Person","ui":null}]`,
			uiConfigs:  `{"Person":` + configUI + `}`,
			wantUI:     configUI,
		},
		{
			name:       "inline ui null with no ui_configs leaves ui absent",
			schemaData: `[{"name":"Person","label":"Person","ui":null}]`,
			wantUI:     "",
		},
		{
			name:       "null ui_configs entry skipped",
			schemaData: `[{"name":"Person","label":"Person"}]`,
			uiConfigs:  `{"Person":null}`,
			wantUI:     "",
		},
		{
			name:       "whitespace-padded ui null treated as absent",
			schemaData: `[{"name":"Person","label":"Person","ui": null }]`,
			uiConfigs:  `{"Person":` + configUI + `}`,
			wantUI:     configUI,
		},
		{
			name:       "map format inline ui null falls back to ui_configs",
			schemaData: `{"Person":{"label":"Person","ui":null}}`,
			uiConfigs:  `{"Person":` + configUI + `}`,
			wantUI:     configUI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectTypes := parseObjectTypeSchemas(json.RawMessage(tt.schemaData), packID, packName, packVersion)
			if objectTypes == nil {
				t.Fatal("expected non-nil parsed object types")
			}

			uiConfigs := map[string]json.RawMessage{}
			if tt.uiConfigs != "" {
				if err := json.Unmarshal([]byte(tt.uiConfigs), &uiConfigs); err != nil {
					t.Fatalf("bad test ui_configs fixture: %v", err)
				}
			}
			applyUIConfigFallback(objectTypes, uiConfigs)

			var person *ObjectTypeSchema
			for i := range objectTypes {
				if objectTypes[i].Name == "Person" {
					person = &objectTypes[i]
					break
				}
			}
			if person == nil {
				t.Fatal("expected a Person type in parsed schemas")
			}
			if got := string(person.UI); got != tt.wantUI {
				t.Errorf("Person ui = %q, want %q", got, tt.wantUI)
			}

			// The compiled JSON must never carry an explicit "ui":null and must
			// omit the key entirely when no ui metadata resolved.
			raw, err := json.Marshal(person)
			if err != nil {
				t.Fatalf("marshal compiled type: %v", err)
			}
			if bytes.Contains(raw, []byte(`"ui":null`)) {
				t.Errorf("compiled output contains \"ui\":null: %s", raw)
			}
			if tt.wantUI == "" && bytes.Contains(raw, []byte(`"ui":`)) {
				t.Errorf("compiled output contains \"ui\" when it should be absent: %s", raw)
			}
		})
	}
}
