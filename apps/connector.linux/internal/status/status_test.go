package status

import (
	"strings"
	"testing"
)

func baseDoc(tools []string, state HubState, detail HubDetail) Document {
	return Document{
		SchemaVersion: SchemaVersion,
		InstanceID:    "mbp-1",
		Version:       "0.1.0",
		Tools:         tools,
		HubState:      state,
		HubDetail:     detail,
	}
}

func TestTextMatchesLegacyOutput(t *testing.T) {
	doc := baseDoc([]string{"a", "b"}, HubStateConnected, HubDetail{HubToolCount: 2, LocalToolCount: 2})
	want := "memory-connector status\n" +
		"instance: mbp-1\n" +
		"version: 0.1.0\n" +
		"tools (2): a, b\n" +
		"hub: connected — hub shows 2 tool(s), matches local set\n"
	if got := doc.Text(); got != want {
		t.Errorf("Text() =\n%q\nwant:\n%q", got, want)
	}
}

func TestTextPlatformNoteWhenNoTools(t *testing.T) {
	doc := baseDoc(nil, HubStateNotConnected, HubDetail{SessionCount: 3})
	doc.ToolsNote = "no osascript"
	want := "memory-connector status\n" +
		"instance: mbp-1\n" +
		"version: 0.1.0\n" +
		"tools: none (no osascript)\n" +
		"hub: not connected — instance not found among 3 hub session(s)\n"
	if got := doc.Text(); got != want {
		t.Errorf("Text() =\n%q\nwant:\n%q", got, want)
	}
}

func TestHubLinePerState(t *testing.T) {
	cases := []struct {
		name   string
		state  HubState
		detail HubDetail
		want   string
	}{
		{
			name:   "connected match",
			state:  HubStateConnected,
			detail: HubDetail{HubToolCount: 2, LocalToolCount: 2},
			want:   "connected — hub shows 2 tool(s), matches local set",
		},
		{
			name:   "connected mismatch",
			state:  HubStateConnected,
			detail: HubDetail{HubToolCount: 1, LocalToolCount: 2},
			want:   "connected — hub shows 1 tool(s) for this instance but 2 local (mismatch; restart 'relay' to re-register)",
		},
		{
			name:   "not connected",
			state:  HubStateNotConnected,
			detail: HubDetail{SessionCount: 4},
			want:   "not connected — instance not found among 4 hub session(s)",
		},
		{
			name:  "auth failed",
			state: HubStateAuthFailed,
			want:  "authentication failed — the server rejected the token",
		},
		{
			name:   "unreachable",
			state:  HubStateUnreachable,
			detail: HubDetail{Error: "dial tcp: connection refused"},
			want:   "unreachable — dial tcp: connection refused",
		},
		{
			name:  "missing config",
			state: HubStateMissingConfig,
			want:  "missing config — run 'memory-connector init' first",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := baseDoc([]string{"a"}, tc.state, tc.detail)
			if got := doc.hubLine(); got != tc.want {
				t.Errorf("hubLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJSONGolden(t *testing.T) {
	doc := baseDoc([]string{"a", "b"}, HubStateConnected, HubDetail{HubToolCount: 2, LocalToolCount: 2})
	got, err := doc.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	want := `{
  "schema_version": 1,
  "instance_id": "mbp-1",
  "version": "0.1.0",
  "tools": [
    "a",
    "b"
  ],
  "hub_state": "connected",
  "hub_detail": {
    "hub_tool_count": 2,
    "local_tool_count": 2,
    "session_count": 0
  }
}
`
	if string(got) != want {
		t.Errorf("JSON() =\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONEmptyToolsIsArray(t *testing.T) {
	doc := baseDoc(nil, HubStateMissingConfig, HubDetail{})
	got, err := doc.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(string(got), `"tools": []`) {
		t.Errorf("JSON() should encode an empty tool list as [], got:\n%s", got)
	}
	if !strings.Contains(string(got), `"hub_state": "missing_config"`) {
		t.Errorf("JSON() missing hub_state, got:\n%s", got)
	}
}

func TestJSONIncludesProjectWhenSet(t *testing.T) {
	doc := baseDoc([]string{"a"}, HubStateConnected, HubDetail{})
	doc.Project = &Project{ID: "p1", Name: "Project One"}
	got, err := doc.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(string(got), `"project": {`) || !strings.Contains(string(got), `"id": "p1"`) {
		t.Errorf("JSON() missing project, got:\n%s", got)
	}
}

func TestJSONOmitsProjectWhenUnset(t *testing.T) {
	doc := baseDoc([]string{"a"}, HubStateConnected, HubDetail{})
	got, err := doc.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if strings.Contains(string(got), `"project"`) {
		t.Errorf("JSON() should omit an unset project, got:\n%s", got)
	}
}

func TestTextIncludesDisabledTools(t *testing.T) {
	doc := baseDoc([]string{"linux-host-info"}, HubStateConnected, HubDetail{HubToolCount: 1, LocalToolCount: 1})
	doc.DisabledTools = []DisabledTool{
		{Name: "linux-fs-list", Reason: "no filesystem config"},
		{Name: "linux-fs-read", Reason: "no filesystem config"},
	}
	want := "memory-connector status\n" +
		"instance: mbp-1\n" +
		"version: 0.1.0\n" +
		"tools (1): linux-host-info\n" +
		"tools-disabled: linux-fs-list (no filesystem config), linux-fs-read (no filesystem config)\n" +
		"hub: connected — hub shows 1 tool(s), matches local set\n"
	if got := doc.Text(); got != want {
		t.Errorf("Text() =\n%q\nwant:\n%q", got, want)
	}
}

func TestTextOmitsDisabledLineWhenNone(t *testing.T) {
	doc := baseDoc([]string{"a"}, HubStateConnected, HubDetail{HubToolCount: 1, LocalToolCount: 1})
	if strings.Contains(doc.Text(), "tools-disabled") {
		t.Errorf("Text() should omit the disabled line when nothing is disabled:\n%s", doc.Text())
	}
}

func TestJSONIncludesDisabledTools(t *testing.T) {
	doc := baseDoc([]string{"linux-host-info"}, HubStateConnected, HubDetail{})
	doc.DisabledTools = []DisabledTool{{Name: "linux-fs-write", Reason: "no write-capable root configured"}}
	got, err := doc.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	for _, want := range []string{`"disabled_tools"`, `"name": "linux-fs-write"`, `"reason": "no write-capable root configured"`, `"schema_version": 1`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("JSON() missing %q:\n%s", want, got)
		}
	}
}
