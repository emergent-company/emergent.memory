package a2ui

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, msgs []Message) string {
	t.Helper()
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("\n")
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal message %d: %v", i, err)
		}
		b.Write(data)
	}
	return b.String()
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		msgs    []Message
		wantErr bool
	}{
		{
			name: "valid createSurface",
			msgs: []Message{
				{CreateSurface: &CreateSurface{SurfaceID: "s1", CatalogID: "basic"}},
			},
			wantErr: false,
		},
		{
			name: "valid updateComponents with catalog component",
			msgs: []Message{
				{UpdateComponents: &UpdateComponents{
					SurfaceID: "s1",
					Components: []Component{
						{ID: "p1", Component: "proposal", Props: map[string]any{"kind": "change", "summary": "hi"}},
					},
				}},
			},
			wantErr: false,
		},
		{
			name: "valid updateComponents with sources component",
			msgs: []Message{
				{UpdateComponents: &UpdateComponents{
					SurfaceID: "s1",
					Components: []Component{
						{ID: "s1", Component: "sources", Props: map[string]any{
							"items": []any{
								map[string]any{"id": "11111111-1111-1111-1111-111111111111", "type": "Company", "label": "Acme Corp"},
							},
						}},
					},
				}},
			},
			wantErr: false,
		},
		{
			name: "valid updateDataModel",
			msgs: []Message{
				{UpdateDataModel: &UpdateDataModel{SurfaceID: "s1", Path: "/x", Value: 1}},
			},
			wantErr: false,
		},
		{
			name: "valid deleteSurface",
			msgs: []Message{
				{DeleteSurface: &DeleteSurface{SurfaceID: "s1"}},
			},
			wantErr: false,
		},
		{
			name:    "empty message set",
			msgs:    []Message{{}},
			wantErr: true,
		},
		{
			name: "two members set",
			msgs: []Message{{
				CreateSurface: &CreateSurface{SurfaceID: "s1", CatalogID: "basic"},
				DeleteSurface: &DeleteSurface{SurfaceID: "s1"},
			}},
			wantErr: true,
		},
		{
			name: "createSurface missing surfaceId",
			msgs: []Message{
				{CreateSurface: &CreateSurface{SurfaceID: "", CatalogID: "basic"}},
			},
			wantErr: true,
		},
		{
			name: "createSurface missing catalogId",
			msgs: []Message{
				{CreateSurface: &CreateSurface{SurfaceID: "s1", CatalogID: ""}},
			},
			wantErr: true,
		},
		{
			name: "updateComponents missing surfaceId",
			msgs: []Message{
				{UpdateComponents: &UpdateComponents{
					SurfaceID: "",
					Components: []Component{
						{ID: "p1", Component: "proposal"},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "updateComponents unknown component",
			msgs: []Message{
				{UpdateComponents: &UpdateComponents{
					SurfaceID: "s1",
					Components: []Component{
						{ID: "p1", Component: "not-a-real-component"},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "updateComponents empty component id",
			msgs: []Message{
				{UpdateComponents: &UpdateComponents{
					SurfaceID: "s1",
					Components: []Component{
						{ID: "", Component: "proposal"},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "updateDataModel missing surfaceId",
			msgs: []Message{
				{UpdateDataModel: &UpdateDataModel{SurfaceID: "", Path: "/x", Value: 1}},
			},
			wantErr: true,
		},
		{
			name: "deleteSurface missing surfaceId",
			msgs: []Message{
				{DeleteSurface: &DeleteSurface{SurfaceID: ""}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.msgs)
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestExtractFromText(t *testing.T) {
	validBlock := "```a2ui\n" +
		"{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"basic\"}}\n" +
		"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\",\"summary\":\"hi\"}]}}\n" +
		"```"

	tests := []struct {
		name      string
		text      string
		wantFound bool
		wantMsgN  int
		wantRest  string // exact expected text after stripping
	}{
		{
			name:      "valid fence extracts messages and strips block",
			text:      "before\n" + validBlock + "\nafter",
			wantFound: true,
			wantMsgN:  2,
			wantRest:  "before\nafter",
		},
		{
			name:      "invalid fence is stripped and yields no messages",
			text:      "before\n```a2ui\nnot json\n```\nafter",
			wantFound: false,
			wantMsgN:  0,
			wantRest:  "before\nafter",
		},
		{
			name:      "no fence returns unchanged",
			text:      "plain text\nno fences",
			wantFound: false,
			wantMsgN:  0,
			wantRest:  "plain text\nno fences",
		},
		{
			name:      "unknown component fence is stripped",
			text:      "```a2ui\n{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"x\",\"component\":\"bogus\"}]}}\n```",
			wantFound: false,
			wantMsgN:  0,
			wantRest:  "",
		},
		{
			name:      "empty fence block is stripped",
			text:      "```a2ui\n```",
			wantFound: false,
			wantMsgN:  0,
			wantRest:  "",
		},
		{
			name:      "unterminated fence is stripped to end of text",
			text:      "before\n```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"basic\"}}",
			wantFound: false,
			wantMsgN:  0,
			wantRest:  "before",
		},
		{
			name:      "one bad block does not hide a valid one",
			text:      "```a2ui\n{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"x\",\"component\":\"bogus\"}]}}\n```\n" + validBlock,
			wantFound: true,
			wantMsgN:  2,
			wantRest:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, rest, found := ExtractFromText(tt.text)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if len(msgs) != tt.wantMsgN {
				t.Fatalf("len(msgs) = %d, want %d", len(msgs), tt.wantMsgN)
			}
			if rest != tt.wantRest {
				t.Errorf("rest = %q, want %q", rest, tt.wantRest)
			}
			if strings.Contains(rest, "```a2ui") {
				t.Errorf("rest = %q, should not contain the fence", rest)
			}
		})
	}
}

func TestComponentRoundTrip(t *testing.T) {
	raw := `{"id":"p1","component":"proposal","kind":"change","summary":"hi"}`
	var c Component
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.ID != "p1" || c.Component != "proposal" {
		t.Errorf("id/component = %q/%q", c.ID, c.Component)
	}
	if c.Props["kind"] != "change" || c.Props["summary"] != "hi" {
		t.Errorf("props = %v", c.Props)
	}
	back := mustJSON(t, nil) // placeholder to keep helper used; real round-trip below
	_ = back

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"kind":"change"`) {
		t.Errorf("marshaled component missing flattened prop: %s", data)
	}
}

func TestValidateCatalogProps(t *testing.T) {
	// Every catalog id, carrying only the props the catalog declares required.
	valid := map[string]map[string]any{
		"proposal":    {"kind": "change", "summary": "rename the field"},
		"approval":    {"tool": "graph_delete_object"},
		"question":    {"prompt": "Which branch?"},
		"code":        {"code": "func main() {}"},
		"entity":      {"type": "Company"},
		"object-form": {"fields": []any{map[string]any{"name": "title", "type": "string"}}},
		"todo":        {"items": []any{"write the test", map[string]any{"label": "ship it", "done": false}}},
		"result":      {"rows": []any{map[string]any{"label": "count", "value": 3}}},
		"sources":     {"items": []any{map[string]any{"id": "11111111-1111-1111-1111-111111111111", "type": "Company", "label": "Acme"}}},
	}

	for id, props := range valid {
		if !BasicCatalog.Contains(id) {
			t.Fatalf("test data names unknown catalog id %q", id)
		}
		t.Run("valid/"+id, func(t *testing.T) {
			msgs := []Message{
				{CreateSurface: &CreateSurface{SurfaceID: "s1", CatalogID: CatalogID}},
				{UpdateComponents: &UpdateComponents{
					SurfaceID:  "s1",
					Components: []Component{{ID: "c1", Component: id, Props: props}},
				}},
			}
			if err := Validate(msgs); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}

	tests := []struct {
		name     string
		kind     string
		props    map[string]any
		wantText string
	}{
		{
			name:     "proposal missing summary",
			kind:     "proposal",
			props:    map[string]any{"kind": "change"},
			wantText: `component "c1" (proposal): missing required prop "summary"`,
		},
		{
			name:     "proposal wrong-typed summary",
			kind:     "proposal",
			props:    map[string]any{"kind": "change", "summary": 42},
			wantText: `component "c1" (proposal): expected string, got number`,
		},
		{
			name:     "question enum rejected",
			kind:     "question",
			props:    map[string]any{"prompt": "q", "interactionType": "slider"},
			wantText: `component "c1" (question): expected one of text, choice, multi, bool, got "slider"`,
		},
		{
			name:     "code empty string",
			kind:     "code",
			props:    map[string]any{"code": ""},
			wantText: `component "c1" (code): must be a non-empty string`,
		},
		{
			name:     "result rows wrong element",
			kind:     "result",
			props:    map[string]any{"rows": []any{"not an object"}},
			wantText: `component "c1" (result): item 0: expected object, got string`,
		},
		{
			name:     "todo items not a list",
			kind:     "todo",
			props:    map[string]any{"items": "one"},
			wantText: `component "c1" (todo): expected array, got string`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := []Message{
				{CreateSurface: &CreateSurface{SurfaceID: "s1", CatalogID: CatalogID}},
				{UpdateComponents: &UpdateComponents{
					SurfaceID:  "s1",
					Components: []Component{{ID: "c1", Component: tt.kind, Props: tt.props}},
				}},
			}
			err := Validate(msgs)
			if err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantText)
			}
		})
	}
}

func TestValidateHostLimits(t *testing.T) {
	// Too many rows is a host-limit rejection, not a renderer degradation.
	rows := make([]any, MaxListItems+1)
	for i := range rows {
		rows[i] = map[string]any{"label": "row", "value": i}
	}
	msgs := []Message{{UpdateComponents: &UpdateComponents{
		SurfaceID:  "s1",
		Components: []Component{{ID: "r1", Component: "result", Props: map[string]any{"rows": rows}}},
	}}}
	err := Validate(msgs)
	if err == nil || !strings.Contains(err.Error(), "exceeds the host limit") {
		t.Fatalf("Validate() = %v, want a host-limit error", err)
	}

	// More than one surface in a single message set is rejected so
	// SurfaceIDOf's "first createSurface wins" rule never silently drops one.
	msgs = []Message{
		{CreateSurface: &CreateSurface{SurfaceID: "s1", CatalogID: CatalogID}},
		{CreateSurface: &CreateSurface{SurfaceID: "s2", CatalogID: CatalogID}},
	}
	err = Validate(msgs)
	if err == nil || !strings.Contains(err.Error(), "one surface only") {
		t.Fatalf("Validate() = %v, want a one-surface error", err)
	}
}

func TestValidateAllReportsEveryIssue(t *testing.T) {
	msgs := []Message{{UpdateComponents: &UpdateComponents{
		SurfaceID: "s1",
		Components: []Component{
			{ID: "p1", Component: "proposal", Props: map[string]any{"kind": "change"}},
			{ID: "p2", Component: "proposal", Props: map[string]any{"kind": "change", "summary": 7}},
		},
	}}}
	issues := ValidateAll(msgs)
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d, want 2 (%v)", len(issues), issues)
	}
	if issues[0].Component != "p1" || issues[0].Prop != "summary" {
		t.Errorf("issue[0] = %+v, want p1/summary", issues[0])
	}
}

func TestInstructionRendersPropTypes(t *testing.T) {
	inst := BasicCatalog.Instruction()
	for _, want := range []string{
		"proposal",
		"required: kind string, summary string",
		"interactionType (text|choice|multi|bool)",
	} {
		if !strings.Contains(inst, want) {
			t.Errorf("Instruction() missing %q:\n%s", want, inst)
		}
	}
	// The catalog id is advertised even though the instruction is prose.
	if !strings.Contains(inst, CatalogID) {
		t.Errorf("Instruction() missing catalog id %q", CatalogID)
	}
}

func TestSummaryText(t *testing.T) {
	one := []Message{{UpdateComponents: &UpdateComponents{SurfaceID: "s1", Components: []Component{
		{ID: "c1", Component: "proposal", Props: map[string]any{"summary": "Ship it"}},
	}}}}
	if got := SummaryText(one); got != "Rendered a structured card (proposal): Ship it" {
		t.Errorf("SummaryText(one) = %q", got)
	}

	bare := []Message{{UpdateComponents: &UpdateComponents{SurfaceID: "s1", Components: []Component{
		{ID: "c1", Component: "code", Props: map[string]any{"code": "secret()"}},
	}}}}
	if got := SummaryText(bare); got != "Rendered a structured card (code)." {
		t.Errorf("SummaryText(bare) = %q", got)
	}

	many := []Message{{UpdateComponents: &UpdateComponents{SurfaceID: "s1", Components: []Component{
		{ID: "c1", Component: "proposal", Props: map[string]any{"summary": "a"}},
		{ID: "c2", Component: "todo", Props: map[string]any{"items": []any{"x"}}},
	}}}}
	if got := SummaryText(many); got != "Rendered 2 structured cards (proposal, todo)." {
		t.Errorf("SummaryText(many) = %q", got)
	}

	if got := SummaryText(nil); got != "Rendered a structured card." {
		t.Errorf("SummaryText(nil) = %q", got)
	}
}

func TestMessageVersionDefaults(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Version != DefaultVersion {
		t.Errorf("Version = %q, want %q", m.Version, DefaultVersion)
	}
}
