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
		name        string
		text        string
		wantFound   bool
		wantMsgN    int
		wantRestSub string // substring expected in rest, or "" for exact-unchanged
		wantUnchg   bool   // rest must equal text
	}{
		{
			name:        "valid fence extracts messages and strips block",
			text:        "before\n" + validBlock + "\nafter",
			wantFound:   true,
			wantMsgN:    2,
			wantRestSub: "before",
		},
		{
			name:      "invalid fence returns unchanged",
			text:      "before\n```a2ui\nnot json\n```\nafter",
			wantFound: false,
			wantMsgN:  0,
			wantUnchg: true,
		},
		{
			name:      "no fence returns unchanged",
			text:      "plain text\nno fences",
			wantFound: false,
			wantMsgN:  0,
			wantUnchg: true,
		},
		{
			name:      "unknown component fence is invalid",
			text:      "```a2ui\n{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"x\",\"component\":\"bogus\"}]}}\n```",
			wantFound: false,
			wantMsgN:  0,
			wantUnchg: true,
		},
		{
			name:      "empty fence block is invalid",
			text:      "```a2ui\n```",
			wantFound: false,
			wantMsgN:  0,
			wantUnchg: true,
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
			if tt.wantUnchg {
				if rest != tt.text {
					t.Errorf("rest = %q, want unchanged %q", rest, tt.text)
				}
				return
			}
			if !strings.Contains(rest, tt.wantRestSub) {
				t.Errorf("rest = %q, want to contain %q", rest, tt.wantRestSub)
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

func TestMessageVersionDefaults(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Version != DefaultVersion {
		t.Errorf("Version = %q, want %q", m.Version, DefaultVersion)
	}
}
