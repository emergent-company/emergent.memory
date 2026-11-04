package appletools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// remindersHelperName is the embedded EventKit CLI shipped in
// Memory.app/Contents/Resources next to the memory-connector engine.
const remindersHelperName = "memory-reminders"

// remindersHelperTimeout bounds a single helper invocation. EventKit fetches
// the whole library locally in one call, so this is generous headroom, not the
// expected duration.
const remindersHelperTimeout = 45 * time.Second

// remindersHelperPath resolves the EventKit helper executable. An explicit
// MEMORY_REMINDERS_HELPER override wins (used by tests); otherwise the helper
// is looked up next to the running engine executable, where the app bundle
// build places it. Returns "" when no usable file is found, which selects the
// AppleScript fallback (read tools) or the platform-unavailable error (write
// tools, which cannot use AppleScript ids).
func remindersHelperPath() string {
	if override := os.Getenv("MEMORY_REMINDERS_HELPER"); override != "" {
		if isExecutableFile(override) {
			return override
		}
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(exe), remindersHelperName)
	if isExecutableFile(candidate) {
		return candidate
	}
	return ""
}

// isExecutableFile reports whether path is an existing regular file. The
// executable bit is not checked: on non-POSIX filesystems and in tests the bit
// may be absent while the file is still runnable by its interpreter.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// runRemindersHelper invokes the embedded EventKit helper with argv, bounded by
// remindersHelperTimeout, and returns stdout. Denied/restricted Reminders
// access is surfaced as ErrNotAuthorized so mapRunnerError adds the System
// Settings guidance; other failures carry the helper's first stderr line.
func runRemindersHelper(ctx context.Context, helper string, argv []string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, remindersHelperTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, helper, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return nil, fmt.Errorf("memory-reminders: %w", runCtx.Err())
		}
		msg := firstLine(stderr.Bytes())
		lower := strings.ToLower(msg)
		if isAuthError(stderr.Bytes()) || strings.Contains(lower, "denied") ||
			strings.Contains(lower, "not authorized") || strings.Contains(lower, "restricted") {
			return nil, fmt.Errorf("%w: %s", ErrNotAuthorized, msg)
		}
		return nil, fmt.Errorf("memory-reminders: %w: %s", err, msg)
	}
	return stdout.Bytes(), nil
}

// remindersViaHelper runs `memory-reminders list [...]` and parses its stdout
// JSON array into rows.
func remindersViaHelper(ctx context.Context, helper, listName string, includeCompleted, includeNotes bool) ([]reminderRow, error) {
	argv := []string{"list"}
	if listName != "" {
		argv = append(argv, "--list", listName)
	}
	if includeCompleted {
		argv = append(argv, "--include-completed")
	}
	if includeNotes {
		argv = append(argv, "--include-notes")
	}

	out, err := runRemindersHelper(ctx, helper, argv)
	if err != nil {
		return nil, err
	}

	var rows []reminderRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("reminders_list: parse memory-reminders output: %w (output: %s)", err, preview(string(out)))
	}
	return rows, nil
}

// remindersListsViaHelper runs `memory-reminders lists` and parses its stdout
// JSON array into list rows.
func remindersListsViaHelper(ctx context.Context, helper string) ([]reminderListRow, error) {
	out, err := runRemindersHelper(ctx, helper, []string{"lists"})
	if err != nil {
		return nil, err
	}
	var rows []reminderListRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("reminders_lists: parse memory-reminders output: %w (output: %s)", err, preview(string(out)))
	}
	return rows, nil
}

// remindersAddViaHelper runs `memory-reminders add ...`. Success is signalled by
// a zero exit; the created reminder is not needed here, so the payload is not
// parsed (whether the helper emits the canonical row or a compact confirmation).
func remindersAddViaHelper(ctx context.Context, helper string, req remindersAddRequest) error {
	_, err := runRemindersHelper(ctx, helper, buildRemindersAddArgv(req))
	return err
}

// remindersUpdateViaHelper runs `memory-reminders update ...` and parses the
// canonical updated-reminder object.
func remindersUpdateViaHelper(ctx context.Context, helper string, req remindersUpdateRequest) (reminderRow, error) {
	out, err := runRemindersHelper(ctx, helper, buildRemindersUpdateArgv(req))
	if err != nil {
		return reminderRow{}, err
	}
	var row reminderRow
	if err := json.Unmarshal(out, &row); err != nil {
		return reminderRow{}, fmt.Errorf("reminders_update: parse memory-reminders output: %w (output: %s)", err, preview(string(out)))
	}
	return row, nil
}

// remindersDeleteViaHelper runs `memory-reminders delete --id ID` and parses the
// {"id":..., "deleted":true} confirmation.
func remindersDeleteViaHelper(ctx context.Context, helper, id string) (struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}, error) {
	var result struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
	out, err := runRemindersHelper(ctx, helper, buildRemindersDeleteArgv(id))
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return result, fmt.Errorf("reminders_delete: parse memory-reminders output: %w (output: %s)", err, preview(string(out)))
	}
	return result, nil
}
