package appletools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// lookupTool finds tool by name from a provider-built set.
func lookupTool(t *testing.T, tools []toolreg.Tool, name string) toolreg.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not in provider set", name)
	return toolreg.Tool{}
}

func jsonArgs(t *testing.T, raw string) map[string]any {
	t.Helper()
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("jsonArgs(%s): %v", raw, err)
	}
	return args
}

func newNotesTools(t *testing.T) (*fakeRunner, toolreg.Tool, toolreg.Tool) {
	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	return f, lookupTool(t, tools, "notes_search"), lookupTool(t, tools, "notes_create")
}

func TestNotesSearchSuccess(t *testing.T) {
	f, search, _ := newNotesTools(t)
	f.out = `[{"name":"Groceries","snippet":"Milk, eggs"},{"name":"Grocery list","snippet":"Butter"}]`

	result, err := search.Handler(context.Background(), jsonArgs(t, `{"query":"grocery"}`))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	notes, ok := result["notes"].([]map[string]any)
	if !ok {
		t.Fatalf("result[\"notes\"] type = %T, want []map[string]any", result["notes"])
	}
	if len(notes) != 2 {
		t.Fatalf("notes length = %d, want 2", len(notes))
	}
	if notes[0]["name"] != "Groceries" || notes[0]["snippet"] != "Milk, eggs" {
		t.Errorf("notes[0] = %v", notes[0])
	}

	if len(f.gotScripts) != 1 {
		t.Fatalf("runner invocations = %d, want 1", len(f.gotScripts))
	}
	script := f.gotScripts[0]
	for _, want := range []string{`set q to "grocery"`, `set f to ""`, "set mLimit to 20", `return "[" & rows & "]"`} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
	if !strings.Contains(script, "escapeForJSON") {
		t.Error("script should include the JSON-escape helpers")
	}
}

func TestNotesSearchDefaultsAndOptions(t *testing.T) {
	f, search, _ := newNotesTools(t)
	f.out = `[]`

	args := jsonArgs(t, `{"query":"standup","folder":"Work","limit":3}`)
	if _, err := search.Handler(context.Background(), args); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	script := f.gotScripts[0]
	for _, want := range []string{`set f to "Work"`, "set mLimit to 3"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}

	// limit arrives as float64 after a JSON round trip: exercise that path.
	f.gotScripts = nil
	if _, err := search.Handler(context.Background(), jsonArgs(t, `{"query":"x","limit":1.0}`)); err != nil {
		t.Fatalf("Handler(float limit): %v", err)
	}
	if !strings.Contains(f.gotScripts[0], "set mLimit to 1") {
		t.Errorf("float limit not honored: %s", f.gotScripts[0])
	}
}

func TestNotesSearchValidation(t *testing.T) {
	_, search, _ := newNotesTools(t)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing query", args: map[string]any{}, want: "missing required argument"},
		{name: "blank query", args: map[string]any{"query": " "}, want: "not be empty"},
		{name: "zero limit", args: map[string]any{"query": "x", "limit": float64(0)}, want: "between 1 and"},
		{name: "limit too large", args: map[string]any{"query": "x", "limit": float64(1000)}, want: "between 1 and"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := search.Handler(context.Background(), tc.args)
			if err == nil {
				t.Fatal("Handler: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Handler error %q should contain %q", err, tc.want)
			}
		})
	}
}

func TestNotesSearchUnparsableOutput(t *testing.T) {
	f, search, _ := newNotesTools(t)
	f.out = "not json at all"
	_, err := search.Handler(context.Background(), jsonArgs(t, `{"query":"x"}`))
	if err == nil {
		t.Fatal("Handler: expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse AppleScript output") {
		t.Errorf("Handler error %q should mention output parsing", err)
	}
}

func TestNotesSearchNotAuthorizedMapping(t *testing.T) {
	f, search, _ := newNotesTools(t)
	f.err = ErrNotAuthorized
	_, err := search.Handler(context.Background(), jsonArgs(t, `{"query":"x"}`))
	if !errors.Is(err, ErrNotAuthorized) {
		t.Errorf("Handler error = %v, want wrapped ErrNotAuthorized", err)
	}
	if !strings.Contains(err.Error(), "System Settings") || !strings.Contains(err.Error(), "Privacy & Security") {
		t.Errorf("Handler error %q should advise granting Automation permission", err)
	}
}

func TestNotesSearchRunnerScriptError(t *testing.T) {
	f, search, _ := newNotesTools(t)
	f.err = errors.New("osascript: exit status 1: Can't get folder \"Nope\" (-1728)")
	_, err := search.Handler(context.Background(), jsonArgs(t, `{"query":"x","folder":"Nope"}`))
	if err == nil {
		t.Fatal("Handler: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "(-1728)") {
		t.Errorf("Handler error %q should carry the osascript stderr detail", err)
	}
	if errors.Is(err, ErrNotAuthorized) {
		t.Error("folder error must not map to ErrNotAuthorized")
	}
}

func TestNotesCreateSuccess(t *testing.T) {
	f, _, create := newNotesTools(t)
	f.out = `{"title":"Meeting notes","created":true}`

	result, err := create.Handler(context.Background(), jsonArgs(t, `{"title":"Meeting notes","body":"Discuss roadmap"}`))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result["title"] != "Meeting notes" || result["created"] != true {
		t.Errorf("result = %v", result)
	}
	script := f.gotScripts[0]
	if !strings.Contains(script, `set t to "Meeting notes"`) || !strings.Contains(script, `set b to "Discuss roadmap"`) {
		t.Errorf("script should embed title and body:\n%s", script)
	}
	if !strings.Contains(script, "make new note with properties {name:t, body:b}") {
		t.Errorf("script should make the note:\n%s", script)
	}
}

func TestNotesCreateMultilineBodyIsSpliced(t *testing.T) {
	f, _, create := newNotesTools(t)
	f.out = `{"title":"T","created":true}`

	_, err := create.Handler(context.Background(), jsonArgs(t, `{"title":"T","body":"line one\nline two"}`))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	script := f.gotScripts[0]
	if !strings.Contains(script, `set b to ("line one" & linefeed & "line two")`) {
		t.Errorf("multiline body should be spliced with linefeed, got:\n%s", script)
	}
}

func TestNotesCreateValidationAndParse(t *testing.T) {
	f, _, create := newNotesTools(t)

	if _, err := create.Handler(context.Background(), map[string]any{}); err == nil {
		t.Error("missing title: expected error")
	}

	f.out = `garbage`
	_, err := create.Handler(context.Background(), jsonArgs(t, `{"title":"T"}`))
	if err == nil || !strings.Contains(err.Error(), "parse AppleScript output") {
		t.Errorf("unparsable output error = %v", err)
	}
}

func TestNotesSearchEmptyResultIsEmptySlice(t *testing.T) {
	f, search, _ := newNotesTools(t)
	f.out = `[]`
	result, err := search.Handler(context.Background(), jsonArgs(t, `{"query":"nothing"}`))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	notes := result["notes"].([]map[string]any)
	if notes == nil || len(notes) != 0 {
		t.Errorf("notes = %#v, want empty non-nil slice", notes)
	}
}
