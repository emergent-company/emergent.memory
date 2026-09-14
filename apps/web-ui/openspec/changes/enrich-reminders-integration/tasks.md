## 1. EventKit helper — enriched listing

- [x] 1.1 Extend `ReminderRow` in `client/macos/MemoryConnector/RemindersHelper/main.swift` with `id` (`calendarItemIdentifier`), `list` (`calendar.title`), and `completed` (`isCompleted`), keeping `name`/`due_date` unchanged; verify by building the helper and running `memory-reminders list` against a real library.
- [x] 1.2 Add `--include-notes` to the `list` subcommand, emitting `notes` only when requested; verify `memory-reminders list` still omits notes and `--include-notes` includes them.
- [x] 1.3 Add a `lists` subcommand emitting `[{"id","name","count"}]` from `store.calendars(for: .reminder)`; verify it lists real lists with non-empty ids.
- [x] 1.4 Add Swift unit tests for the helper's argument parsing/row encoding (extract pure helpers so they are testable) and verify they pass via the Mac build.

## 2. EventKit helper — mutations

- [x] 2.1 Implement `add --title T [--list NAME] [--due RFC3339] [--notes N]`, resolving the target list (default when omitted) and validating `allowedEntityTypes.contains(.reminder)`; verify each failure mode returns a distinct non-zero error.
- [x] 2.2 Implement `update --id ID` with optional `--title`, `--due`/`--clear-due`, `--notes`/`--clear-notes`, `--priority`, `--completed`, `--list` (move); set `reminder.timeZone` separately from `dueDateComponents`; verify field changes and clears round-trip on a real list.
- [x] 2.3 Implement `delete --id ID` using `store.remove(_:commit:)`; verify the reminder disappears from `list`.
- [x] 2.4 Implement identifier resolution via `calendarItem(withIdentifier:) as? EKReminder` and verify an unknown id fails with an error naming the id.

## 3. Go connector — enriched read tools

- [x] 3.1 Extend `reminderRow` in `connector/internal/appletools/reminders.go` with `ID`, `List`, `Completed`, optional `Notes` and update `newRemindersListTool` to pass `include_notes` and surface the new fields; verify with unit tests asserting the emitted map shape.
- [x] 3.2 Update `remindersListScript` to also select/emit the reminder identifier, list name, and completion state, with unit tests in `scripts_test.go` asserting the emitted JSON keys.
- [x] 3.3 Add a `reminders_lists` tool and helper invocation, returning `{lists:[...]}`; verify with unit tests.

## 4. Go connector — write tools

- [x] 4.1 Extend `remindersAddScript`/`reminders_add` with an optional `list_name` targeting a named list while preserving default-list behavior when omitted; verify with script unit tests.
- [x] 4.2 Add `reminders_update` (id + optional title/due_date/clear_due/notes/clear_notes/priority/completed/list_name) dispatching to the helper; verify with argument-building unit tests covering each optional field and the clear flags.
- [x] 4.3 Add `reminders_delete` (id) dispatching to the helper; verify with argument-building unit tests.
- [x] 4.4 Register the new tools in `provider.go` and add a test asserting the registered tool set includes all five Reminders tools in order.
- [x] 4.5 Extend error mapping so helper not-found and invalid-target-list errors surface distinctly, with unit tests for each branch.

## 5. Mac app, docs, spec

- [x] 5.1 Add `reminders_lists`, `reminders_update`, and `reminders_delete` to `client/macos/MemoryConnector/Sources/ToolCatalog.swift`; verify `ToolCatalogTests` updated and green.
- [x] 5.2 Update the tool table in `connector/README.md` to describe the new arguments and results; verify against the implemented schemas.
- [x] 5.3 Update `openspec/specs/mcp-connector/spec.md` for the expanded Reminders tool set (sync the delta on archive), and confirm no other spec enumerates the old tool ids.
- [x] 5.4 Update the `NSRemindersFullAccessUsageDescription` wording in `Info.plist` to mention managing reminders, not only listing.

## 6. Verification

- [x] 6.1 Run `go build ./...`, `go test ./...`, and the linter in the connector module; confirm all pass.
- [x] 6.2 Build the mac app (including the embedded helper) with the Mac build script; confirm the helper and engine embed and the app launches.
- [ ] 6.3 Manual Mac round trip on `mcj-mini`: `lists → list → add(list) → update(title, due) → move → complete → delete`, confirming ids round-trip and each observable effect.
- [ ] 6.4 Confirm end-to-end through the hub that an agent can list with list context and perform update/move/delete, and that disabled-tool toggles still gate the new tools.
