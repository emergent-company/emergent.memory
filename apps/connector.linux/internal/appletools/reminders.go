package appletools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

// ErrNotFound reports a reminder identifier that resolved to no reminder.
var ErrNotFound = errors.New("reminder not found")

// ErrInvalidTarget reports a named reminder list that is absent or does not
// accept reminders.
var ErrInvalidTarget = errors.New("reminder list not found")

// remindersListScript emits JSON:
// [{"id":..., "name":..., "list":..., "due_date":<RFC3339|null>,
// "completed":<bool>, "notes":<string|null>?},...].
// listName "" lists every reminder; includeCompleted selects completed ones
// too; includeNotes adds the reminder body as `notes` (and a `notes:null` key
// when it has none).
//
// This is a best-effort FALLBACK for hosts without the embedded EventKit helper
// (see reminders_helper.go); macOS uses the helper, which is fast and reliable.
//
// AppleScript shape: `get {name, due date, completed, id, body} of every
// reminder` (one Apple event) returns PARALLEL COLUMN LISTS, not per-reminder
// rows: {{all names}, {all due dates}, {all completed}, {all ids}, {all bodies}}.
// The script destructures the columns and zips them by index in a loop that
// runs strictly AFTER the `tell` block closes, so it never issues an Apple event
// per reminder. Reading a bare Reminders property (e.g. `due date of rec`)
// outside the tell scope does not compile ("found class name", -2741), which is
// why the loop reads the materialized columns positionally instead.
//
// The `list` name in the fallback is the requested listName (empty when listing
// all lists, because the batched `every reminder` form has no per-item list
// column). EventKit ids and AppleScript ids are different namespaces; list and
// mutation must use the same backend.
func remindersListScript(listName string, includeCompleted, includeNotes bool) string {
	completedFlag := "false"
	if includeCompleted {
		completedFlag = "true"
	}
	notesFlag := "false"
	if includeNotes {
		notesFlag = "true"
	}
	// `use framework` must be the first line of the script and followed by a
	// newline — escapeHelpers starts with an `on` handler declaration, so a
	// missing break would glue `on escapeForJSON` onto the use statement.
	script := "use framework \"Foundation\"\n"
	script += escapeHelpers
	script += fmt.Sprintf(`
set isoFmt to current application's NSISO8601DateFormatter's new()
set listName to %s
set includeCompleted to %s
set includeNotes to %s
set dq to "\""
set rows to ""
tell application "Reminders"
	if listName is "" then
		set cols to (get {name, due date, completed, id, body} of every reminder)
	else
		set cols to (get {name, due date, completed, id, body} of every reminder of list listName)
	end if
end tell
if (count of cols) is 5 then
	set theNames to item 1 of cols
	set theDates to item 2 of cols
	set theDone to item 3 of cols
	set theIDs to item 4 of cols
	set theBodies to item 5 of cols
	set listJSON to escapeForJSON(listName)
	repeat with i from 1 to (count of theNames)
		set nm to item i of theNames
		if nm is missing value then
			set nm to ""
		end if
		set idVal to item i of theIDs
		if idVal is missing value then
			set idVal to ""
		end if
		set dueVal to item i of theDates
		set doneVal to item i of theDone
		if includeCompleted is true or doneVal is false then
			if rows is not "" then
				set rows to rows & ","
			end if
			if dueVal is missing value then
				set dueISO to "null"
			else
				set dueISO to isoFmt's stringFromDate:dueVal
				set dueISO to dq & dueISO & dq
			end if
			if doneVal is true then
				set doneJSON to "true"
			else
				set doneJSON to "false"
			end if
			set nmJSON to escapeForJSON(nm)
			set idJSON to escapeForJSON(idVal)
			set rows to rows & "{\"id\":" & idJSON & ",\"name\":" & nmJSON & ",\"list\":" & listJSON & ",\"due_date\":" & dueISO & ",\"completed\":" & doneJSON
			if includeNotes is true then
				set bodyVal to item i of theBodies
				if bodyVal is missing value then
					set rows to rows & ",\"notes\":null"
				else
					set rows to rows & ",\"notes\":" & escapeForJSON(bodyVal)
				end if
			end if
			set rows to rows & "}"
		end if
	end repeat
end if
return "[" & rows & "]"
`, appleExpr(listName), completedFlag, notesFlag)
	return script
}

// remindersListsScript emits JSON: [{"id":"", "name":..., "count":0},...].
// AppleScript has no cheap per-list reminder count here, so count is 0 and the
// (AppleScript-namespace) id is empty; the embedded EventKit helper is preferred
// and supplies real ids and counts.
func remindersListsScript() string {
	return escapeHelpers + `set rows to ""
set nms to {}
tell application "Reminders"
	set nms to name of every list
end tell
set listCount to count of nms
repeat with i from 1 to listCount
	set nm to item i of nms
	if nm is missing value then
		set nm to ""
	end if
	set nmJSON to escapeForJSON(nm)
	if rows is not "" then
		set rows to rows & ","
	end if
	set rows to rows & "{\"id\":\"\",\"name\":" & nmJSON & ",\"count\":0}"
end repeat
return "[" & rows & "]"
`
}

// remindersAddScript emits JSON: {"title":..., "added":true}. When hasDue is
// true, dueExpr is an AppleScript date expression like "(current date) + 3600"
// (offset seconds, avoiding locale-dependent date parsing). listName "" targets
// the default list; otherwise the reminder is created in the named list.
func remindersAddScript(title, notesText string, hasDue bool, dueExpr, listName string) string {
	dueFlag := "false"
	if hasDue {
		dueFlag = "true"
	}
	return escapeHelpers + fmt.Sprintf(`set t to %s
set nText to %s
set hasDue to %s
set dueExpr to %s
set targetList to %s
set titleJSON to escapeForJSON(t)
tell application "Reminders"
	if targetList is "" then
		set newRem to make new reminder at end of default list with properties {name:t}
	else
		set newRem to make new reminder at end of list targetList with properties {name:t}
	end if
	if hasDue is true then set due date of newRem to dueExpr
	if nText is not "" then set body of newRem to nText
end tell
return "{\"title\":" & titleJSON & ",\"added\":true}"
`, appleExpr(title), appleExpr(notesText), dueFlag, dueExpr, appleExpr(listName))
}

// offsetExpr renders a signed second offset as an AppleScript date expression
// relative to the current time.
func offsetExpr(secs int64) string {
	if secs >= 0 {
		return fmt.Sprintf("(current date) + %d", secs)
	}
	return fmt.Sprintf("(current date) - %d", -secs)
}

// dueDateExpr returns the AppleScript expression that yields the date secsFromNow
// seconds from now.
func dueDateExpr(secsFromNow time.Duration) string {
	return offsetExpr(int64(secsFromNow.Round(time.Second) / time.Second))
}

// reminderRow is the JSON shape shared by the AppleScript and EventKit helper
// paths. `name`/`due_date` keep their original spelling; `id`, `list`, and
// `completed` are added by this change. `notes` is populated only when the
// caller requests notes (include_notes) or when the helper returns one.
type reminderRow struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	List      string  `json:"list"`
	DueDate   *string `json:"due_date"`
	Completed bool    `json:"completed"`
	Notes     *string `json:"notes"`
}

// reminderListRow is the JSON shape emitted by the helper `lists` subcommand:
// [{"id":..., "name":..., "count":N},...].
type reminderListRow struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// reminderRowMap renders a row as the tool result map. id, name, list,
// due_date, and completed are always present; due_date is null when the
// reminder has none, and notes only appears when the row carries notes.
func reminderRowMap(row reminderRow) map[string]any {
	item := map[string]any{
		"id":        row.ID,
		"name":      row.Name,
		"list":      row.List,
		"due_date":  nil,
		"completed": row.Completed,
	}
	if row.DueDate != nil {
		item["due_date"] = *row.DueDate
	}
	if row.Notes != nil {
		item["notes"] = *row.Notes
	}
	return item
}

// newRemindersListTool builds the reminders_list tool backed by r.
func newRemindersListTool(r Runner) toolreg.Tool {
	return toolreg.Tool{
		Name:        "reminders_list",
		Description: "List reminders from Apple Reminders, optionally restricted to a named list. Open (incomplete) reminders by default; completed reminders included when include_completed is true. Each reminder carries an id usable with reminders_update/reminders_delete, its list name, and completion state; notes are added only when include_notes is true.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"list_name":         map[string]any{"type": "string", "description": "Reminders list name; empty lists all lists"},
				"include_completed": map[string]any{"type": "boolean", "description": "include completed reminders (default false)"},
				"include_notes":     map[string]any{"type": "boolean", "description": "include each reminder's note text (default false)"},
			},
		},
		Handler: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			listName := optionalString(args, "list_name")
			includeCompleted := optionalBool(args, "include_completed", false)
			includeNotes := optionalBool(args, "include_notes", false)

			rows, err := listReminders(ctx, r, listName, includeCompleted, includeNotes)
			if err != nil {
				return nil, mapRunnerError(err)
			}
			reminders := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				reminders = append(reminders, reminderRowMap(row))
			}
			return map[string]any{"reminders": reminders}, nil
		},
	}
}

// listReminders returns the reminder rows for the requested scope, preferring
// the fast EventKit helper when it is embedded next to the engine and falling
// back to the AppleScript runner otherwise (Linux, unit tests). A helper that
// is present but fails is reported as-is — falling back would silently hide a
// permission problem and reintroduce the slow path.
func listReminders(ctx context.Context, r Runner, listName string, includeCompleted, includeNotes bool) ([]reminderRow, error) {
	if helper := remindersHelperPath(); helper != "" {
		return remindersViaHelper(ctx, helper, listName, includeCompleted, includeNotes)
	}
	out, err := r.Run(ctx, remindersListScript(listName, includeCompleted, includeNotes), 0)
	if err != nil {
		return nil, err
	}
	var rows []reminderRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return nil, fmt.Errorf("reminders_list: parse AppleScript output: %w (output: %s)", err, preview(out))
	}
	return rows, nil
}

// newRemindersListsTool builds the reminders_lists tool backed by r.
func newRemindersListsTool(r Runner) toolreg.Tool {
	return toolreg.Tool{
		Name:        "reminders_lists",
		Description: "List the Apple Reminders lists with each list's name, id, and reminder count, so a list can be targeted unambiguously by reminders_add/reminders_update.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			lists, err := listReminderLists(ctx, r)
			if err != nil {
				return nil, mapRunnerError(err)
			}
			out := make([]map[string]any, 0, len(lists))
			for _, l := range lists {
				out = append(out, map[string]any{"id": l.ID, "name": l.Name, "count": l.Count})
			}
			return map[string]any{"lists": out}, nil
		},
	}
}

// listReminderLists enumerates reminder lists, preferring the EventKit helper
// (real ids and counts) and falling back to AppleScript names on hosts without
// the helper.
func listReminderLists(ctx context.Context, r Runner) ([]reminderListRow, error) {
	if helper := remindersHelperPath(); helper != "" {
		return remindersListsViaHelper(ctx, helper)
	}
	out, err := r.Run(ctx, remindersListsScript(), 0)
	if err != nil {
		return nil, err
	}
	var rows []reminderListRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return nil, fmt.Errorf("reminders_lists: parse AppleScript output: %w (output: %s)", err, preview(out))
	}
	return rows, nil
}

// remindersAddRequest is the validated input to reminders_add. ListName ""
// targets the default list.
type remindersAddRequest struct {
	Title    string
	ListName string
	Due      *time.Time
	Notes    *string
}

// buildRemindersAddArgv renders the helper `add` argv for req.
func buildRemindersAddArgv(req remindersAddRequest) []string {
	argv := []string{"add", "--title", req.Title}
	if req.ListName != "" {
		argv = append(argv, "--list", req.ListName)
	}
	if req.Due != nil {
		argv = append(argv, "--due", req.Due.Format(time.RFC3339))
	}
	if req.Notes != nil {
		argv = append(argv, "--notes", *req.Notes)
	}
	return argv
}

// buildRemindersDeleteArgv renders the helper `delete` argv.
func buildRemindersDeleteArgv(id string) []string {
	return []string{"delete", "--id", id}
}

// parseRemindersAddArgs validates the reminders_add argument map.
func parseRemindersAddArgs(args map[string]any) (remindersAddRequest, error) {
	var req remindersAddRequest
	title, err := requireString(args, "title")
	if err != nil {
		return req, err
	}
	req.Title = title
	req.ListName = optionalString(args, "list_name")
	if argPresent(args, "notes") {
		notes, ok := args["notes"].(string)
		if !ok {
			return req, fmt.Errorf("argument %q must be a string", "notes")
		}
		req.Notes = &notes
	}
	if argPresent(args, "due_date") {
		raw := optionalString(args, "due_date")
		due, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return req, fmt.Errorf("argument %q must be RFC3339 (e.g. 2026-09-09T17:00:00Z): %w", "due_date", err)
		}
		req.Due = &due
	}
	return req, nil
}

// newRemindersAddTool builds the reminders_add tool backed by r.
func newRemindersAddTool(r Runner) toolreg.Tool {
	return toolreg.Tool{
		Name:        "reminders_add",
		Description: "Add a reminder to a Reminders list (the default list unless list_name is given), with an optional RFC3339 due date and notes.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":     map[string]any{"type": "string", "description": "reminder name"},
				"list_name": map[string]any{"type": "string", "description": "target Reminders list name; empty uses the default list"},
				"due_date":  map[string]any{"type": "string", "description": "due date as RFC3339 (e.g. 2026-09-09T17:00:00Z)"},
				"notes":     map[string]any{"type": "string", "description": "optional note text on the reminder"},
			},
			"required": []string{"title"},
		},
		Handler: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			req, err := parseRemindersAddArgs(args)
			if err != nil {
				return nil, err
			}
			notesText := ""
			if req.Notes != nil {
				notesText = *req.Notes
			}

			// Prefer the EventKit helper when embedded: it validates the target
			// list and writes atomically. Its stdout is not parsed — the frozen
			// contract does not pin the add payload — so the result stays the
			// backward-compatible {title, added}.
			if helper := remindersHelperPath(); helper != "" {
				if err := remindersAddViaHelper(ctx, helper, req); err != nil {
					return nil, mapRunnerError(wrapReminderMutationError(err, "", req.ListName))
				}
				return map[string]any{"title": req.Title, "added": true}, nil
			}

			hasDue := req.Due != nil
			dueExpr := "missing value"
			if hasDue {
				dueExpr = dueDateExpr(time.Until(*req.Due))
			}
			out, err := r.Run(ctx, remindersAddScript(req.Title, notesText, hasDue, dueExpr, req.ListName), 0)
			if err != nil {
				return nil, mapRunnerError(err)
			}
			var result struct {
				Title string `json:"title"`
				Added bool   `json:"added"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				return nil, fmt.Errorf("reminders_add: parse AppleScript output: %w (output: %s)", err, preview(out))
			}
			return map[string]any{"title": result.Title, "added": result.Added}, nil
		},
	}
}

// remindersUpdateRequest is the validated input to reminders_update. Pointer
// fields mean "present, therefore change it"; explicit clear flags remove a
// value. A nil pointer plus a false clear flag leaves the field untouched.
type remindersUpdateRequest struct {
	ID         string
	Title      *string
	DueDate    *time.Time
	ClearDue   bool
	Notes      *string
	ClearNotes bool
	Priority   *int
	Completed  *bool
	ListName   *string
}

// buildRemindersUpdateArgv renders the helper `update` argv for req. Callers
// must have validated that DueDate/ClearDue and Notes/ClearNotes are not both
// set.
func buildRemindersUpdateArgv(req remindersUpdateRequest) []string {
	argv := []string{"update", "--id", req.ID}
	if req.Title != nil {
		argv = append(argv, "--title", *req.Title)
	}
	if req.ClearDue {
		argv = append(argv, "--clear-due")
	} else if req.DueDate != nil {
		argv = append(argv, "--due", req.DueDate.Format(time.RFC3339))
	}
	if req.ClearNotes {
		argv = append(argv, "--clear-notes")
	} else if req.Notes != nil {
		argv = append(argv, "--notes", *req.Notes)
	}
	if req.Priority != nil {
		argv = append(argv, "--priority", strconv.Itoa(*req.Priority))
	}
	if req.Completed != nil {
		argv = append(argv, "--completed", strconv.FormatBool(*req.Completed))
	}
	if req.ListName != nil {
		argv = append(argv, "--list", *req.ListName)
	}
	return argv
}

// newRemindersUpdateTool builds the reminders_update tool. Mutation requires
// the embedded EventKit helper: AppleScript reminder ids are a different
// namespace, so there is no safe AppleScript id-based fallback.
func newRemindersUpdateTool() toolreg.Tool {
	return toolreg.Tool{
		Name:        "reminders_update",
		Description: "Update an existing reminder addressed by its id: change title, set or clear the due date, set or clear notes, change priority, mark complete/incomplete, and/or move it to another list. Only the provided fields change.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":          map[string]any{"type": "string", "description": "reminder id from reminders_list"},
				"title":       map[string]any{"type": "string", "description": "new reminder name"},
				"due_date":    map[string]any{"type": "string", "description": "new due date as RFC3339; mutually exclusive with clear_due"},
				"clear_due":   map[string]any{"type": "boolean", "description": "remove the due date; mutually exclusive with due_date"},
				"notes":       map[string]any{"type": "string", "description": "new note text; mutually exclusive with clear_notes"},
				"clear_notes": map[string]any{"type": "boolean", "description": "remove the notes; mutually exclusive with notes"},
				"priority":    map[string]any{"type": "integer", "description": "new priority (0 = none, 1 = high, 5 = medium, 9 = low)"},
				"completed":   map[string]any{"type": "boolean", "description": "mark the reminder complete (true) or incomplete (false)"},
				"list_name":   map[string]any{"type": "string", "description": "move the reminder to this list"},
			},
			"required": []string{"id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			req, err := parseRemindersUpdateArgs(args)
			if err != nil {
				return nil, err
			}

			helper := remindersHelperPath()
			if helper == "" {
				return nil, mapRunnerError(fmt.Errorf("%w: reminders_update requires the EventKit helper", ErrUnavailable))
			}
			row, err := remindersUpdateViaHelper(ctx, helper, req)
			if err != nil {
				return nil, mapRunnerError(wrapReminderMutationError(err, req.ID, derefString(req.ListName)))
			}
			return map[string]any{"reminder": reminderRowMap(row)}, nil
		},
	}
}

// parseRemindersUpdateArgs validates the reminders_update argument map,
// enforcing the due_date/clear_due and notes/clear_notes exclusions.
func parseRemindersUpdateArgs(args map[string]any) (remindersUpdateRequest, error) {
	var req remindersUpdateRequest
	id, err := requireString(args, "id")
	if err != nil {
		return req, err
	}
	req.ID = id

	if argPresent(args, "title") {
		title, ok := args["title"].(string)
		if !ok {
			return req, fmt.Errorf("argument %q must be a string", "title")
		}
		req.Title = &title
	}

	duePresent := argPresent(args, "due_date")
	req.ClearDue = optionalBool(args, "clear_due", false)
	if duePresent && req.ClearDue {
		return req, fmt.Errorf("arguments %q and %q are mutually exclusive", "due_date", "clear_due")
	}
	if duePresent {
		raw := optionalString(args, "due_date")
		due, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return req, fmt.Errorf("argument %q must be RFC3339 (e.g. 2026-09-09T17:00:00Z): %w", "due_date", err)
		}
		req.DueDate = &due
	}

	notesPresent := argPresent(args, "notes")
	req.ClearNotes = optionalBool(args, "clear_notes", false)
	if notesPresent && req.ClearNotes {
		return req, fmt.Errorf("arguments %q and %q are mutually exclusive", "notes", "clear_notes")
	}
	if notesPresent {
		notes, ok := args["notes"].(string)
		if !ok {
			return req, fmt.Errorf("argument %q must be a string", "notes")
		}
		req.Notes = &notes
	}

	if argPresent(args, "priority") {
		priority, err := optionalInt(args, "priority", 0)
		if err != nil {
			return req, err
		}
		req.Priority = &priority
	}
	if argPresent(args, "completed") {
		completed, ok := args["completed"].(bool)
		if !ok {
			return req, fmt.Errorf("argument %q must be a boolean", "completed")
		}
		req.Completed = &completed
	}
	if argPresent(args, "list_name") {
		listName, ok := args["list_name"].(string)
		if !ok {
			return req, fmt.Errorf("argument %q must be a string", "list_name")
		}
		req.ListName = &listName
	}
	return req, nil
}

// newRemindersDeleteTool builds the reminders_delete tool. Like update it
// requires the embedded EventKit helper, because AppleScript cannot resolve an
// EventKit id.
func newRemindersDeleteTool() toolreg.Tool {
	return toolreg.Tool{
		Name:        "reminders_delete",
		Description: "Delete an existing reminder addressed by its id from reminders_list.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "reminder id from reminders_list"},
			},
			"required": []string{"id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			id, err := requireString(args, "id")
			if err != nil {
				return nil, err
			}
			helper := remindersHelperPath()
			if helper == "" {
				return nil, mapRunnerError(fmt.Errorf("%w: reminders_delete requires the EventKit helper", ErrUnavailable))
			}
			deleted, err := remindersDeleteViaHelper(ctx, helper, id)
			if err != nil {
				return nil, mapRunnerError(wrapReminderMutationError(err, id, ""))
			}
			return map[string]any{"id": deleted.ID, "deleted": deleted.Deleted}, nil
		},
	}
}

// argPresent reports whether key was supplied with a non-nil value.
func argPresent(args map[string]any, key string) bool {
	v, ok := args[key]
	return ok && v != nil
}

// derefString returns *s, or "" when s is nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// wrapReminderMutationError attaches the connector's error taxonomy to a helper
// failure and guarantees the addressed reminder id or list name appears. Auth
// errors pass through untouched so mapRunnerError adds its guidance.
func wrapReminderMutationError(err error, id, listName string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotAuthorized) || errors.Is(err, ErrUnavailable) {
		return err
	}
	lower := strings.ToLower(err.Error())

	// A message that talks about a list, when a list was addressed, is the
	// most specific invalid-target signal.
	if listName != "" && strings.Contains(lower, "list") &&
		(strings.Contains(lower, "not found") || strings.Contains(lower, "unknown") ||
			strings.Contains(lower, "invalid") || strings.Contains(lower, "does not")) {
		return fmt.Errorf("%w: %s: %w", ErrInvalidTarget, listName, err)
	}
	if id != "" &&
		(strings.Contains(lower, "not found") || strings.Contains(lower, "no reminder") ||
			strings.Contains(lower, "unknown reminder") || strings.Contains(lower, "could not find")) {
		return fmt.Errorf("%w: %s: %w", ErrNotFound, id, err)
	}
	// Fall back to naming the addressed entity so the caller can act.
	switch {
	case listName != "":
		return fmt.Errorf("list %q: %w", listName, err)
	case id != "":
		return fmt.Errorf("reminder %q: %w", id, err)
	default:
		return err
	}
}
