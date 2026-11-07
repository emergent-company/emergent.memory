package blueprints

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestMarshalPackSchemas_PreservesFullMetadata pins the install/update payload:
// labels, required, ui, embedding, extraction, relationship properties, and
// pack-level uiConfigs must all survive the typed→raw marshalling step.
func TestMarshalPackSchemas_PreservesFullMetadata(t *testing.T) {
	p := PackFile{
		Name:    "p",
		Version: "1.0.0",
		ObjectTypes: []ObjectTypeDef{
			{
				Name:        "Doc",
				Label:       "Document",
				Labels:      []string{"doc", "content"},
				Description: "d",
				Properties: map[string]any{
					"title": map[string]any{"type": "string", "x-embedding-target": true},
				},
				Required:   []string{"title"},
				UI:         map[string]any{"icon": "file-text", "color": "#3B82F6"},
				Embedding:  map[string]any{"mode": "property", "field": "title"},
				Extraction: map[string]any{"enabled": true},
			},
		},
		RelationshipTypes: []RelationshipTypeDef{
			{
				Name: "links", Label: "Links", SourceType: "Doc", TargetType: "Doc",
				Properties: map[string]any{"weight": map[string]any{"type": "number"}},
				Required:   []string{"weight"},
			},
		},
		UIConfigs: JSONBlob(`{"Doc":{"icon":"file-text","color":"#3B82F6"}}`),
	}

	objSchemas, relSchemas, uiCfgs, _, err := marshalPackSchemas(p)
	if err != nil {
		t.Fatalf("marshalPackSchemas: %v", err)
	}

	var objs []map[string]any
	if err := json.Unmarshal(objSchemas, &objs); err != nil {
		t.Fatalf("unmarshal object schemas: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("expected 1 object schema, got %d", len(objs))
	}
	obj := objs[0]
	assertStringSlice(t, obj, "labels", []string{"doc", "content"})
	assertStringSlice(t, obj, "required", []string{"title"})
	assertMapValue(t, obj, "ui", "icon", "file-text")
	assertMapValue(t, obj, "embedding", "mode", "property")
	assertMapValue(t, obj, "extraction", "enabled", true)

	var rels []map[string]any
	if err := json.Unmarshal(relSchemas, &rels); err != nil {
		t.Fatalf("unmarshal relationship schemas: %v", err)
	}
	if len(rels) != 1 {
		t.Fatalf("expected 1 relationship schema, got %d", len(rels))
	}
	props, ok := rels[0]["properties"].(map[string]any)
	if !ok {
		t.Fatalf("relationship properties missing from payload: %v", rels[0])
	}
	if _, ok := props["weight"]; !ok {
		t.Fatalf("relationship properties lost weight: %v", props)
	}
	assertStringSlice(t, rels[0], "required", []string{"weight"})

	if string(uiCfgs) != string(p.UIConfigs.Raw()) {
		// The map is rebuilt so inline `ui:` blocks can win, so compare
		// semantically rather than byte-for-byte.
		var got, want map[string]any
		if err := json.Unmarshal(uiCfgs, &got); err != nil {
			t.Fatalf("unmarshal uiConfigs: %v", err)
		}
		if err := json.Unmarshal(p.UIConfigs.Raw(), &want); err != nil {
			t.Fatalf("unmarshal expected uiConfigs: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("uiConfigs not preserved: got %v want %v", got, want)
		}
	}
}

// TestMergePackUIConfigs_PromotesInlineUI pins #291 on the CLI install path:
// `memory blueprints install` marshals the pack client-side and sends it to
// POST /api/schemas, so a pack that declares appearance only as an inline `ui:`
// block must still produce a type-keyed uiConfigs map — otherwise the registry
// row lands with ui_config='{}' and the UI renders no icon/colour.
func TestMergePackUIConfigs_PromotesInlineUI(t *testing.T) {
	p := PackFile{
		Name: "p",
		ObjectTypes: []ObjectTypeDef{
			{Name: "Doc", UI: map[string]any{"icon": "file-text", "color": "#3B82F6"}},
			{Name: "Note"}, // no inline ui, no pack-level entry
		},
	}

	raw, err := mergePackUIConfigs(p)
	if err != nil {
		t.Fatalf("mergePackUIConfigs: %v", err)
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal uiConfigs %s: %v", raw, err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only the type with inline ui, got %v", got)
	}
	if got["Doc"]["icon"] != "file-text" || got["Doc"]["color"] != "#3B82F6" {
		t.Fatalf("inline ui block not promoted: %v", got)
	}
	if _, ok := got["Note"]; ok {
		t.Fatalf("type without ui must not appear: %v", got)
	}
}

// TestMergePackUIConfigs_InlineWinsOverPackLevel pins the documented precedence
// on ObjectTypeDef.UI: the inline block is used when present, the pack-level
// uiConfigs entry is the fallback.
func TestMergePackUIConfigs_InlineWinsOverPackLevel(t *testing.T) {
	p := PackFile{
		Name: "p",
		ObjectTypes: []ObjectTypeDef{
			{Name: "Doc", UI: map[string]any{"icon": "inline-icon"}},
			{Name: "Note"},
		},
		UIConfigs: JSONBlob(`{"Doc":{"icon":"pack-icon"},"Note":{"icon":"note-icon"}}`),
	}

	raw, err := mergePackUIConfigs(p)
	if err != nil {
		t.Fatalf("mergePackUIConfigs: %v", err)
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal uiConfigs %s: %v", raw, err)
	}
	if got["Doc"]["icon"] != "inline-icon" {
		t.Fatalf("inline ui must win over pack-level: %v", got["Doc"])
	}
	if got["Note"]["icon"] != "note-icon" {
		t.Fatalf("pack-level fallback lost for a type without inline ui: %v", got["Note"])
	}
}

func assertStringSlice(t *testing.T, m map[string]any, key string, want []string) {
	t.Helper()
	raw, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s missing or not a list: %v", key, m[key])
	}
	got := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s[%d] is not a string: %v", key, i, v)
		}
		got[i] = s
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}

func assertMapValue(t *testing.T, m map[string]any, block, key string, want any) {
	t.Helper()
	nested, ok := m[block].(map[string]any)
	if !ok {
		t.Fatalf("%s missing or not a mapping: %v", block, m[block])
	}
	if nested[key] != want {
		t.Fatalf("%s.%s = %v, want %v", block, key, nested[key], want)
	}
}

func TestExpandRelationshipTypes_CrossProduct(t *testing.T) {
	defs := []RelationshipTypeDef{
		{
			Name:        "links",
			Label:       "Links",
			SourceTypes: []string{"A", "B"},
			TargetTypes: []string{"C", "D"},
			Properties:  map[string]any{"weight": map[string]any{"type": "number"}},
			Required:    []string{"weight"},
		},
	}
	got, err := expandRelationshipTypes(defs)
	if err != nil {
		t.Fatalf("expandRelationshipTypes: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 expanded entries, got %d: %+v", len(got), got)
	}
	want := [][2]string{{"A", "C"}, {"A", "D"}, {"B", "C"}, {"B", "D"}}
	for i, pair := range want {
		if got[i].SourceType != pair[0] || got[i].TargetType != pair[1] {
			t.Fatalf("entry %d = %s→%s, want %s→%s", i, got[i].SourceType, got[i].TargetType, pair[0], pair[1])
		}
		if got[i].Name != "links" || got[i].Label != "Links" {
			t.Fatalf("entry %d lost name/label: %+v", i, got[i])
		}
		if len(got[i].Properties) == 0 {
			t.Fatalf("entry %d lost properties", i)
		}
		if len(got[i].Required) != 1 || got[i].Required[0] != "weight" {
			t.Fatalf("entry %d lost required: %+v", i, got[i].Required)
		}
	}
}

func TestExpandRelationshipTypes_SingularPassthrough(t *testing.T) {
	defs := []RelationshipTypeDef{{Name: "links", SourceType: "A", TargetType: "B"}}
	got, err := expandRelationshipTypes(defs)
	if err != nil {
		t.Fatalf("expandRelationshipTypes: %v", err)
	}
	if len(got) != 1 || got[0].SourceType != "A" || got[0].TargetType != "B" {
		t.Fatalf("expected singular passthrough, got: %+v", got)
	}
}

func TestExpandRelationshipTypes_CapBeforeAllocation(t *testing.T) {
	src := make([]string, 101)
	tgt := make([]string, 100)
	for i := range src {
		src[i] = "S"
	}
	for i := range tgt {
		tgt[i] = "T"
	}
	defs := []RelationshipTypeDef{{Name: "huge", SourceTypes: src, TargetTypes: tgt}}

	got, err := expandRelationshipTypes(defs)
	if err == nil {
		t.Fatalf("expected expansion over the cap to fail, got %d entries", len(got))
	}
	if got != nil {
		t.Fatalf("expected no allocation on failure, got %d entries", len(got))
	}
	msg := err.Error()
	if !strings.Contains(msg, "exceeding the limit of 10000") || !strings.Contains(msg, `"huge"`) {
		t.Fatalf("expected a clear cap error naming the definition, got: %v", err)
	}
}
