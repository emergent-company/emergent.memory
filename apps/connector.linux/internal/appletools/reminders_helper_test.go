package appletools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeFakeHelper creates an executable fake `memory-reminders` that records
// its argv (newline-separated) to $ARGS_FILE and prints stdout. It never
// touches EventKit, so the helper path is testable on any host.
func writeFakeHelper(t *testing.T, dir, stdout string) string {
	t.Helper()
	path := filepath.Join(dir, "memory-reminders")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$ARGS_FILE\"\n" +
		"printf '%s' '" + stdout + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake helper: %v", err)
	}
	return path
}

// writeFailingHelper creates a fake helper that writes message to stderr and
// exits non-zero.
func writeFailingHelper(t *testing.T, dir, message string, code int) string {
	t.Helper()
	path := filepath.Join(dir, "memory-reminders")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '" + message + "' >&2\n" +
		"exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write failing helper: %v", err)
	}
	return path
}

// TestRemindersListPrefersHelperWhenPresent verifies that an embedded/present
// helper is selected over the AppleScript runner, that the scope flags are
// forwarded, and that its stdout JSON is parsed into the result.
func TestRemindersListPrefersHelperWhenPresent(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	helper := writeFakeHelper(t, dir, `[{"name":"From helper","due_date":"2026-09-09T12:00:00Z"},{"name":"No due","due_date":null}]`)

	t.Setenv("MEMORY_REMINDERS_HELPER", helper)
	t.Setenv("ARGS_FILE", argsFile)

	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	list := lookupTool(t, tools, "reminders_list")

	result, err := list.Handler(context.Background(), map[string]any{
		"list_name":         "Errands",
		"include_completed": true,
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	reminders, ok := result["reminders"].([]map[string]any)
	if !ok || len(reminders) != 2 {
		t.Fatalf("reminders = %#v", result["reminders"])
	}
	if reminders[0]["name"] != "From helper" || reminders[0]["due_date"] != "2026-09-09T12:00:00Z" {
		t.Errorf("reminders[0] = %v", reminders[0])
	}
	if reminders[1]["name"] != "No due" {
		t.Errorf("reminders[1] = %v", reminders[1])
	}
	if due, hasDue := reminders[1]["due_date"]; !hasDue || due != nil {
		t.Errorf("null due_date must be present and null, got %v (present=%v)", due, hasDue)
	}

	if len(f.gotScripts) != 0 {
		t.Errorf("AppleScript runner must not be used when the helper is present; got %d runs", len(f.gotScripts))
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	got := strings.Fields(string(raw))
	want := []string{"list", "--list", "Errands", "--include-completed"}
	if len(got) != len(want) {
		t.Fatalf("helper argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("helper argv = %v, want %v", got, want)
		}
	}
}

// TestRemindersListHelperDefaultsToOpenOnly verifies no flags are added for the
// default scope (all lists, open reminders only).
func TestRemindersListHelperDefaultsToOpenOnly(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	helper := writeFakeHelper(t, dir, `[]`)

	t.Setenv("MEMORY_REMINDERS_HELPER", helper)
	t.Setenv("ARGS_FILE", argsFile)

	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	list := lookupTool(t, tools, "reminders_list")

	if _, err := list.Handler(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	raw, _ := os.ReadFile(argsFile)
	if got := strings.TrimSpace(string(raw)); got != "list" {
		t.Errorf("helper argv = %q, want %q", got, "list")
	}
}

// TestRemindersListFallsBackWhenHelperAbsent verifies that a missing/override
// path falls back to the AppleScript runner (the Linux/test path).
func TestRemindersListFallsBackWhenHelperAbsent(t *testing.T) {
	t.Setenv("MEMORY_REMINDERS_HELPER", filepath.Join(t.TempDir(), "does-not-exist"))

	f, list, _ := newRemindersTools(t)
	f.out = `[{"name":"From AppleScript","due_date":null}]`

	result, err := list.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if len(f.gotScripts) != 1 {
		t.Fatalf("AppleScript runner runs = %d, want 1", len(f.gotScripts))
	}
	reminders := result["reminders"].([]map[string]any)
	if len(reminders) != 1 || reminders[0]["name"] != "From AppleScript" {
		t.Errorf("reminders = %v", reminders)
	}
}

// TestRemindersListHelperAuthError verifies a denied-access helper failure is
// surfaced as ErrNotAuthorized (so mapRunnerError adds System Settings advice).
func TestRemindersListHelperAuthError(t *testing.T) {
	dir := t.TempDir()
	helper := writeFailingHelper(t, dir, "Reminders access denied. Grant Memory access.", 1)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)

	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	list := lookupTool(t, tools, "reminders_list")

	_, err := list.Handler(context.Background(), map[string]any{})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Handler error = %v, want ErrNotAuthorized", err)
	}
	if !strings.Contains(err.Error(), "System Settings") {
		t.Errorf("mapped auth error %q should advise System Settings", err)
	}
	if len(f.gotScripts) != 0 {
		t.Error("AppleScript runner must not run when the helper is present")
	}
}

// TestRemindersListHelperBadJSON verifies a malformed helper payload is a
// parse error, not a silent empty list.
func TestRemindersListHelperBadJSON(t *testing.T) {
	dir := t.TempDir()
	helper := writeFakeHelper(t, dir, `not json`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)

	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	list := lookupTool(t, tools, "reminders_list")

	_, err := list.Handler(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "parse memory-reminders output") {
		t.Errorf("parse error = %v", err)
	}
}

// helperRecordedArgs reads the argv the fake helper recorded.
func helperRecordedArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	return strings.Fields(string(raw))
}

// TestRemindersListHelperIncludeNotesArgv verifies include_notes is forwarded
// and notes are surfaced from the helper payload.
func TestRemindersListHelperIncludeNotesArgv(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	helper := writeFakeHelper(t, dir, `[{"id":"r1","name":"From helper","list":"Errands","due_date":null,"completed":false,"notes":"buy 2%"}]`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)
	t.Setenv("ARGS_FILE", argsFile)

	tools, _ := Provider(&fakeRunner{available: true})
	list := lookupTool(t, tools, "reminders_list")

	result, err := list.Handler(context.Background(), map[string]any{"include_notes": true})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	reminders := result["reminders"].([]map[string]any)
	if reminders[0]["notes"] != "buy 2%" || reminders[0]["id"] != "r1" || reminders[0]["list"] != "Errands" {
		t.Errorf("reminders[0] = %v", reminders[0])
	}
	assertArgv(t, helperRecordedArgs(t, argsFile), []string{"list", "--include-notes"})
}

// TestRemindersListsViaHelper verifies the lists subcommand argv and parsing.
func TestRemindersListsViaHelper(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	helper := writeFakeHelper(t, dir, `[{"id":"L1","name":"Errands","count":3},{"id":"L2","name":"Work","count":0}]`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)
	t.Setenv("ARGS_FILE", argsFile)

	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	listsTool := lookupTool(t, tools, "reminders_lists")

	result, err := listsTool.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	lists := result["lists"].([]map[string]any)
	if len(lists) != 2 || lists[0]["id"] != "L1" || lists[0]["count"] != 3 {
		t.Fatalf("lists = %v", lists)
	}
	assertArgv(t, helperRecordedArgs(t, argsFile), []string{"lists"})
	if len(f.gotScripts) != 0 {
		t.Errorf("helper present: AppleScript must not run, got %d", len(f.gotScripts))
	}
}

// TestRemindersAddViaHelper verifies add prefers the helper, forwards every
// optional field, and reports the backward-compatible result.
func TestRemindersAddViaHelper(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	helper := writeFakeHelper(t, dir, `{"id":"r9","name":"Milk","list":"Errands","due_date":"2026-09-09T12:00:00Z","completed":false}`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)
	t.Setenv("ARGS_FILE", argsFile)

	f := &fakeRunner{available: true}
	tools, _ := Provider(f)
	add := lookupTool(t, tools, "reminders_add")

	result, err := add.Handler(context.Background(), jsonArgs(t, `{"title":"Milk","list_name":"Errands","due_date":"2026-09-09T12:00:00Z","notes":"2%"}`))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result["title"] != "Milk" || result["added"] != true {
		t.Errorf("result = %v", result)
	}
	assertArgv(t, helperRecordedArgs(t, argsFile), []string{"add", "--title", "Milk", "--list", "Errands", "--due", "2026-09-09T12:00:00Z", "--notes", "2%"})
	if len(f.gotScripts) != 0 {
		t.Errorf("helper present: AppleScript must not run, got %d", len(f.gotScripts))
	}
}

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }
func strPtr(s string) *string {
	return &s
}

// TestBuildRemindersUpdateArgv covers every optional field and clear flag.
func TestBuildRemindersUpdateArgv(t *testing.T) {
	due := mustParseRFC3339(t, "2026-09-09T12:00:00Z")

	cases := []struct {
		name string
		req  remindersUpdateRequest
		want []string
	}{
		{
			name: "id only",
			req:  remindersUpdateRequest{ID: "r1"},
			want: []string{"update", "--id", "r1"},
		},
		{
			name: "title",
			req:  remindersUpdateRequest{ID: "r1", Title: strPtr("New")},
			want: []string{"update", "--id", "r1", "--title", "New"},
		},
		{
			name: "due date",
			req:  remindersUpdateRequest{ID: "r1", DueDate: &due},
			want: []string{"update", "--id", "r1", "--due", "2026-09-09T12:00:00Z"},
		},
		{
			name: "clear due",
			req:  remindersUpdateRequest{ID: "r1", ClearDue: true},
			want: []string{"update", "--id", "r1", "--clear-due"},
		},
		{
			name: "notes",
			req:  remindersUpdateRequest{ID: "r1", Notes: strPtr("hello")},
			want: []string{"update", "--id", "r1", "--notes", "hello"},
		},
		{
			name: "clear notes",
			req:  remindersUpdateRequest{ID: "r1", ClearNotes: true},
			want: []string{"update", "--id", "r1", "--clear-notes"},
		},
		{
			name: "priority complete and move",
			req: remindersUpdateRequest{
				ID:        "r1",
				Priority:  intPtr(1),
				Completed: boolPtr(false),
				ListName:  strPtr("Work"),
			},
			want: []string{"update", "--id", "r1", "--priority", "1", "--completed", "false", "--list", "Work"},
		},
		{
			name: "all fields",
			req: remindersUpdateRequest{
				ID:        "r1",
				Title:     strPtr("New"),
				DueDate:   &due,
				Notes:     strPtr("n"),
				Priority:  intPtr(5),
				Completed: boolPtr(true),
				ListName:  strPtr("Work"),
			},
			want: []string{"update", "--id", "r1", "--title", "New", "--due", "2026-09-09T12:00:00Z", "--notes", "n", "--priority", "5", "--completed", "true", "--list", "Work"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertArgv(t, buildRemindersUpdateArgv(tc.req), tc.want)
		})
	}
}

// TestRemindersUpdateValidation verifies missing id and the clear-flag
// exclusions fail before any helper is invoked.
func TestRemindersUpdateValidation(t *testing.T) {
	dir := t.TempDir()
	helper := writeFakeHelper(t, dir, `{}`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)

	tools, _ := Provider(&fakeRunner{available: true})
	update := lookupTool(t, tools, "reminders_update")

	if _, err := update.Handler(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "id") {
		t.Errorf("missing id error = %v", err)
	}
	if _, err := update.Handler(context.Background(), jsonArgs(t, `{"id":"r1","due_date":"2026-09-09T12:00:00Z","clear_due":true}`)); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("due/clear_due conflict error = %v", err)
	}
	if _, err := update.Handler(context.Background(), jsonArgs(t, `{"id":"r1","notes":"x","clear_notes":true}`)); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("notes/clear_notes conflict error = %v", err)
	}
	if _, err := update.Handler(context.Background(), map[string]any{"id": "r1", "due_date": "tomorrow"}); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("invalid due_date error = %v", err)
	}
}

// TestRemindersDeleteValidation verifies the required id.
func TestRemindersDeleteValidation(t *testing.T) {
	dir := t.TempDir()
	helper := writeFakeHelper(t, dir, `{}`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)

	tools, _ := Provider(&fakeRunner{available: true})
	deleteTool := lookupTool(t, tools, "reminders_delete")
	if _, err := deleteTool.Handler(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "id") {
		t.Errorf("missing id error = %v", err)
	}
}

// TestRemindersUpdateViaHelper verifies dispatch and canonical result.
func TestRemindersUpdateViaHelper(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	helper := writeFakeHelper(t, dir, `{"id":"r1","name":"Milk","list":"Work","due_date":null,"completed":true,"notes":"n"}`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)
	t.Setenv("ARGS_FILE", argsFile)

	tools, _ := Provider(&fakeRunner{available: true})
	update := lookupTool(t, tools, "reminders_update")

	result, err := update.Handler(context.Background(), jsonArgs(t, `{"id":"r1","title":"Milk","notes":"n","completed":true,"list_name":"Work"}`))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	reminder, ok := result["reminder"].(map[string]any)
	if !ok || reminder["id"] != "r1" || reminder["completed"] != true || reminder["list"] != "Work" {
		t.Errorf("result[reminder] = %v", result["reminder"])
	}
	assertArgv(t, helperRecordedArgs(t, argsFile), []string{"update", "--id", "r1", "--title", "Milk", "--notes", "n", "--completed", "true", "--list", "Work"})
}

// TestRemindersDeleteViaHelper verifies dispatch and confirmation parsing.
func TestRemindersDeleteViaHelper(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	helper := writeFakeHelper(t, dir, `{"id":"r1","deleted":true}`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)
	t.Setenv("ARGS_FILE", argsFile)

	tools, _ := Provider(&fakeRunner{available: true})
	deleteTool := lookupTool(t, tools, "reminders_delete")

	result, err := deleteTool.Handler(context.Background(), map[string]any{"id": "r1"})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result["id"] != "r1" || result["deleted"] != true {
		t.Errorf("result = %v", result)
	}
	assertArgv(t, helperRecordedArgs(t, argsFile), []string{"delete", "--id", "r1"})
}

// TestRemindersHelperErrorMapping verifies unknown-id and invalid-target-list
// helper failures surface distinctly and name the addressed entity.
func TestRemindersHelperErrorMapping(t *testing.T) {
	cases := []struct {
		name      string
		tool      string
		args      string
		stderr    string
		wantErr   error
		wantInMsg string
	}{
		{
			name:      "unknown id",
			tool:      "reminders_delete",
			args:      `{"id":"ID-404"}`,
			stderr:    "Reminder not found: ID-404",
			wantErr:   ErrNotFound,
			wantInMsg: "ID-404",
		},
		{
			name:      "unknown update id",
			tool:      "reminders_update",
			args:      `{"id":"ID-500"}`,
			stderr:    "No reminder with id ID-500",
			wantErr:   ErrNotFound,
			wantInMsg: "ID-500",
		},
		{
			name:      "invalid target list",
			tool:      "reminders_update",
			args:      `{"id":"r1","list_name":"Nowhere"}`,
			stderr:    "List not found: Nowhere",
			wantErr:   ErrInvalidTarget,
			wantInMsg: "Nowhere",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			helper := writeFailingHelper(t, dir, tc.stderr, 1)
			t.Setenv("MEMORY_REMINDERS_HELPER", helper)

			tools, _ := Provider(&fakeRunner{available: true})
			tool := lookupTool(t, tools, tc.tool)

			_, err := tool.Handler(context.Background(), jsonArgs(t, tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Errorf("error %q should name %q", err, tc.wantInMsg)
			}
		})
	}
}

// TestRemindersHelperAuthErrorOnMutation verifies denied access maps to
// ErrNotAuthorized (with System Settings guidance) on write tools too.
func TestRemindersHelperAuthErrorOnMutation(t *testing.T) {
	dir := t.TempDir()
	helper := writeFailingHelper(t, dir, "Reminders access denied", 1)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)

	tools, _ := Provider(&fakeRunner{available: true})
	deleteTool := lookupTool(t, tools, "reminders_delete")

	_, err := deleteTool.Handler(context.Background(), map[string]any{"id": "r1"})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("error = %v, want ErrNotAuthorized", err)
	}
	if !strings.Contains(err.Error(), "System Settings") {
		t.Errorf("mapped auth error %q should advise System Settings", err)
	}
}

// TestRemindersDeleteHelperBadJSON verifies malformed delete output fails loudly.
func TestRemindersDeleteHelperBadJSON(t *testing.T) {
	dir := t.TempDir()
	helper := writeFakeHelper(t, dir, `not json`)
	t.Setenv("MEMORY_REMINDERS_HELPER", helper)

	tools, _ := Provider(&fakeRunner{available: true})
	deleteTool := lookupTool(t, tools, "reminders_delete")

	_, err := deleteTool.Handler(context.Background(), map[string]any{"id": "r1"})
	if err == nil || !strings.Contains(err.Error(), "parse memory-reminders output") {
		t.Errorf("parse error = %v", err)
	}
}
