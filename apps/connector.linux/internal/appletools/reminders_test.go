package appletools

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

func newRemindersTools(t *testing.T) (*fakeRunner, toolreg.Tool, toolreg.Tool) {
	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	return f, lookupTool(t, tools, "reminders_list"), lookupTool(t, tools, "reminders_add")
}

func TestRemindersListSuccessOpenOnly(t *testing.T) {
	f, list, _ := newRemindersTools(t)
	f.out = `[{"id":"r1","name":"Buy milk","list":"Errands","due_date":"2026-09-09T12:00:00Z","completed":false},{"id":"r2","name":"Call dentist","list":"Health","due_date":null,"completed":false}]`

	result, err := list.Handler(context.Background(), jsonArgs(t, `{"list_name":"Errands"}`))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	reminders, ok := result["reminders"].([]map[string]any)
	if !ok {
		t.Fatalf("result[\"reminders\"] type = %T", result["reminders"])
	}
	if len(reminders) != 2 {
		t.Fatalf("reminders length = %d, want 2", len(reminders))
	}
	first := reminders[0]
	if first["id"] != "r1" || first["name"] != "Buy milk" || first["list"] != "Errands" ||
		first["due_date"] != "2026-09-09T12:00:00Z" || first["completed"] != false {
		t.Errorf("reminders[0] = %v", first)
	}
	if _, hasNotes := first["notes"]; hasNotes {
		t.Errorf("reminders[0] should omit notes without include_notes, got %v", first)
	}
	second := reminders[1]
	if second["id"] != "r2" || second["name"] != "Call dentist" || second["list"] != "Health" {
		t.Errorf("reminders[1] = %v", second)
	}
	if due, hasDue := second["due_date"]; !hasDue || due != nil {
		t.Errorf("reminders[1] due_date should be present and null, got %v (present=%v)", due, hasDue)
	}
	if second["completed"] != false {
		t.Errorf("reminders[1] completed = %v, want false", second["completed"])
	}

	script := f.gotScripts[0]
	for _, want := range []string{`set listName to "Errands"`, "set includeCompleted to false", "set includeNotes to false", "NSISO8601DateFormatter"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
}

func TestRemindersListIncludeNotes(t *testing.T) {
	f, list, _ := newRemindersTools(t)
	f.out = `[{"id":"r1","name":"Buy milk","list":"Errands","due_date":null,"completed":true,"notes":"2% milk"}]`

	result, err := list.Handler(context.Background(), map[string]any{"include_notes": true})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	reminders := result["reminders"].([]map[string]any)
	if len(reminders) != 1 {
		t.Fatalf("reminders = %v", reminders)
	}
	if reminders[0]["notes"] != "2% milk" {
		t.Errorf("notes = %v, want %q", reminders[0]["notes"], "2% milk")
	}
	if reminders[0]["completed"] != true {
		t.Errorf("completed = %v, want true", reminders[0]["completed"])
	}
	script := f.gotScripts[0]
	if !strings.Contains(script, "set includeNotes to true") {
		t.Errorf("include_notes true not honored:\n%s", script)
	}
}

func TestRemindersListOmitsNullNotes(t *testing.T) {
	f, list, _ := newRemindersTools(t)
	f.out = `[{"id":"r1","name":"No note","list":"","due_date":null,"completed":false,"notes":null}]`

	result, err := list.Handler(context.Background(), map[string]any{"include_notes": true})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	reminders := result["reminders"].([]map[string]any)
	if _, hasNotes := reminders[0]["notes"]; hasNotes {
		t.Errorf("null notes must be omitted from the row map, got %v", reminders[0])
	}
}

func TestRemindersListAllListsAndCompleted(t *testing.T) {
	f, list, _ := newRemindersTools(t)
	f.out = `[]`

	if _, err := list.Handler(context.Background(), map[string]any{"include_completed": true}); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	script := f.gotScripts[0]
	if !strings.Contains(script, `set listName to ""`) {
		t.Errorf("empty list_name should list all lists:\n%s", script)
	}
	if !strings.Contains(script, "set includeCompleted to true") {
		t.Errorf("include_completed true not honored:\n%s", script)
	}
}

func TestRemindersListParseError(t *testing.T) {
	f, list, _ := newRemindersTools(t)
	f.out = `not json`
	_, err := list.Handler(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "parse AppleScript output") {
		t.Errorf("unparsable output error = %v", err)
	}
}

func TestRemindersAddSuccessWithDueAndNotes(t *testing.T) {
	f, _, add := newRemindersTools(t)
	f.out = `{"title":"Ship the release","added":true}`

	result, err := add.Handler(context.Background(), jsonArgs(t, `{"title":"Ship the release","due_date":"2026-10-01T09:00:00Z","notes":"with changelog"}`))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result["title"] != "Ship the release" || result["added"] != true {
		t.Errorf("result = %v", result)
	}

	script := f.gotScripts[0]
	for _, want := range []string{
		`set t to "Ship the release"`,
		`set nText to "with changelog"`,
		"set hasDue to true",
		"set dueExpr to (current date) +",
		"set due date of newRem to dueExpr",
		"set body of newRem to nText",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
	if !strings.Contains(script, "make new reminder at end of default list") {
		t.Errorf("script should create the reminder:\n%s", script)
	}
}

func TestRemindersAddTargetList(t *testing.T) {
	f, _, add := newRemindersTools(t)
	f.out = `{"title":"Errand","added":true}`

	if _, err := add.Handler(context.Background(), map[string]any{"title": "Errand", "list_name": "Errands"}); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	script := f.gotScripts[0]
	for _, want := range []string{
		`set targetList to "Errands"`,
		"make new reminder at end of list targetList",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
}

func TestRemindersAddNoDue(t *testing.T) {
	f, _, add := newRemindersTools(t)
	f.out = `{"title":"Plain","added":true}`

	if _, err := add.Handler(context.Background(), map[string]any{"title": "Plain"}); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	script := f.gotScripts[0]
	if !strings.Contains(script, "set hasDue to false") {
		t.Errorf("hasDue should be false:\n%s", script)
	}
	if !strings.Contains(script, "set dueExpr to missing value") {
		t.Errorf("dueExpr should be missing value:\n%s", script)
	}
	if strings.Contains(script, "(current date)") {
		t.Errorf("no due-date arithmetic should appear when due_date absent:\n%s", script)
	}
	if !strings.Contains(script, `set targetList to ""`) {
		t.Errorf("no list_name should target the default list:\n%s", script)
	}
}

func TestRemindersAddInvalidDueDate(t *testing.T) {
	f, _, add := newRemindersTools(t)
	f.out = `{"title":"X","added":true}`

	_, err := add.Handler(context.Background(), map[string]any{"title": "X", "due_date": "tomorrow"})
	if err == nil {
		t.Fatal("Handler: expected RFC3339 error, got nil")
	}
	if !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("Handler error %q should mention RFC3339", err)
	}
	if len(f.gotScripts) != 0 {
		t.Error("runner must not be invoked for invalid due_date")
	}
}

func TestRemindersAddValidationAndParse(t *testing.T) {
	f, _, add := newRemindersTools(t)

	if _, err := add.Handler(context.Background(), map[string]any{}); err == nil {
		t.Error("missing title: expected error")
	}

	f.out = `junk`
	_, err := add.Handler(context.Background(), map[string]any{"title": "T"})
	if err == nil || !strings.Contains(err.Error(), "parse AppleScript output") {
		t.Errorf("unparsable output error = %v", err)
	}
}

func TestBuildRemindersAddArgv(t *testing.T) {
	due := mustParseRFC3339(t, "2026-09-09T12:00:00Z")
	notes := "remember"

	cases := []struct {
		name string
		req  remindersAddRequest
		want []string
	}{
		{
			name: "title only",
			req:  remindersAddRequest{Title: "Milk"},
			want: []string{"add", "--title", "Milk"},
		},
		{
			name: "all optional fields",
			req:  remindersAddRequest{Title: "Milk", ListName: "Errands", Due: &due, Notes: &notes},
			want: []string{"add", "--title", "Milk", "--list", "Errands", "--due", "2026-09-09T12:00:00Z", "--notes", "remember"},
		},
		{
			name: "empty notes still requested",
			req:  remindersAddRequest{Title: "Milk", Notes: new(string)},
			want: []string{"add", "--title", "Milk", "--notes", ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildRemindersAddArgv(tc.req)
			assertArgv(t, got, tc.want)
		})
	}
}

func TestBuildRemindersDeleteArgv(t *testing.T) {
	assertArgv(t, buildRemindersDeleteArgv("ID123"), []string{"delete", "--id", "ID123"})
}

func TestRemindersListsFallbackAppleScript(t *testing.T) {
	t.Setenv("MEMORY_REMINDERS_HELPER", filepath.Join(t.TempDir(), "missing"))
	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	listsTool := lookupTool(t, tools, "reminders_lists")
	f.out = `[{"id":"","name":"Errands","count":0},{"id":"","name":"Work","count":0}]`

	result, err := listsTool.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	lists, ok := result["lists"].([]map[string]any)
	if !ok || len(lists) != 2 {
		t.Fatalf("result[\"lists\"] = %#v", result["lists"])
	}
	if lists[0]["name"] != "Errands" || lists[1]["name"] != "Work" {
		t.Errorf("lists = %v", lists)
	}
	if len(f.gotScripts) != 1 || !strings.Contains(f.gotScripts[0], "name of every list") {
		t.Errorf("fallback script should enumerate list names, got %v", f.gotScripts)
	}
}

func TestRemindersUpdateDeleteRequireHelper(t *testing.T) {
	t.Setenv("MEMORY_REMINDERS_HELPER", filepath.Join(t.TempDir(), "missing"))
	f := &fakeRunner{available: true}
	tools, _ := Provider(f)

	update := lookupTool(t, tools, "reminders_update")
	if _, err := update.Handler(context.Background(), map[string]any{"id": "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("reminders_update without helper error = %v, want ErrUnavailable", err)
	}
	deleteTool := lookupTool(t, tools, "reminders_delete")
	if _, err := deleteTool.Handler(context.Background(), map[string]any{"id": "x"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("reminders_delete without helper error = %v, want ErrUnavailable", err)
	}
	if len(f.gotScripts) != 0 {
		t.Errorf("write tools must not fall back to AppleScript, got %d runs", len(f.gotScripts))
	}
}

// mustParseRFC3339 parses raw or fails the test.
func mustParseRFC3339(t *testing.T, raw string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return tm
}

// assertArgv compares an argv slice element by element.
func assertArgv(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv = %v, want %v", got, want)
		}
	}
}
