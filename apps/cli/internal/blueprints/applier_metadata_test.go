package blueprints

import (
	"encoding/json"
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

	if string(uiCfgs) != string(p.UIConfigs.Raw()) {
		t.Fatalf("uiConfigs not preserved: got %s want %s", uiCfgs, p.UIConfigs.Raw())
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
