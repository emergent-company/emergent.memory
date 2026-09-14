## Context

See proposal.md for motivation. Current state that constrains the approach:

- `connector/internal/appletools/reminders.go` defines `reminderRow{name, due_date}` and the two Reminders tools; `reminders_add` always runs AppleScript and only ever writes to the default list.
- `connector/internal/appletools/reminders_helper.go` shells out to the embedded `memory-reminders` EventKit CLI, which today only implements `list`. `reminders_list` prefers the helper when present and falls back to AppleScript (Linux/unit tests).
- `client/macos/MemoryConnector/RemindersHelper/main.swift` already fetches `EKReminder` objects, so `calendarItemIdentifier`, `calendar` (list), `isCompleted`, `notes`, and `priority` are in hand at list time and are currently discarded.
- `Info.plist` already carries `NSRemindersFullAccessUsageDescription`; `.fullAccess` covers read, write, move, and delete. No new entitlement or permission prompt is required.
- The mac app's `ToolCatalog.swift` is a static list that must match the engine's registered tool ids.

## Goals / Non-Goals

**Goals:**

- Give every listed reminder a stable, addressable identity and its list name.
- Provide list enumeration, add-to-list, update (including complete/move), and delete.
- Keep the existing result/argument keys working (additive evolution).
- Keep the helper as the preferred backend and AppleScript as a fallback consistent with today's model.

**Non-Goals:**

- Subtasks/checklists, alarms, recurrence, tags/flagged, attachments, or location reminders.
- Creating, renaming, or deleting reminder lists.
- Sharing, account/scoping management, or cross-account moves beyond a best-effort, validated move.
- New permissions or TCC flows.

## Decisions

### D1: EventKit helper is the mutation backend; AppleScript is fallback only

All read and write operations prefer `memory-reminders` when embedded, matching today's `reminders_list` selection. Rationale: EventKit is the supported API, is fast on large libraries, and provides stable ids and atomic saves. Alternative considered — implement writes purely in AppleScript — rejected because the connector already treats EventKit as the primary path and AppleScript per-item property access does not scale.

### D2: Reminder identity = `EKReminder.calendarItemIdentifier`

The helper emits `calendarItemIdentifier` and resolves it back with `EKEventStore.calendarItem(withIdentifier:) as? EKReminder`. Rationale: locally consistent, single-item lookup, not deprecated. `calendarItemExternalIdentifier` was rejected: it is not stable per device for reminders and requires a plural fetch with possible duplicates. `uuid` is deprecated.

Constraint: EventKit ids and AppleScript Reminders ids are different namespaces. A list produced by one backend cannot be mutated by the other. Mitigation: the helper-vs-AppleScript choice is a deterministic runtime check (`remindersHelperPath()`), and both list and mutate use the same selection, so ids always round-trip within a backend.

Caveat: Apple documents that `calendarItemIdentifier` can be lost after a full calendar sync. A lookup miss returns a clear error naming the id; a later enhancement may add a `list+title` disambiguation fallback (ambiguity → error), but this change does not depend on it.

### D3: Enriched row shape (additive)

```json
{"id":"...","name":"...","list":"Errands","due_date":"2026-09-09T12:00:00Z"|null,"completed":false}
```

`name` and `due_date` keep their existing spelling and semantics. `id`, `list`, and `completed` are added. `notes` is added only when the caller requests it (`include_notes`), keeping large-library payloads compact. The Go `reminderRow` struct gains `ID`, `List`, `Completed`, and an optional `Notes`.

### D4: One `reminders_update`, not separate move/complete tools

`reminders_update` takes required `id` plus optional `title`, `due_date`, `clear_due`, `notes`, `clear_notes`, `priority`, `completed`, and `list_name`. Presence means "change"; explicit clear flags distinguish "leave alone" from "remove a value". Rationale: fewer tools improve agent tool selection, and move is just a field change. Alternatives considered: separate `reminders_move`/`reminders_complete` tools — rejected as surface bloat; deferred if discoverability proves poor in practice.

`reminders_delete` is a separate tool because deletion is destructive and deserves its own name and approval semantics.

### D5: Helper CLI contract

```
memory-reminders list   [--list NAME] [--include-completed] [--include-notes]
memory-reminders lists
memory-reminders add    --title T [--list NAME] [--due RFC3339] [--notes N]
memory-reminders update --id ID [--title T]
                        [--due RFC3339 | --clear-due]
                        [--notes N | --clear-notes]
                        [--priority N]
                        [--completed true|false]
                        [--list NAME]
memory-reminders delete --id ID
```

`list` stays backward compatible. `lists` emits `[{"id":"...","name":"...","count":N}]`. Write subcommands emit a canonical representation of the affected reminder (for `update`) or `{"id":"...","deleted":true}` (for `delete`), on stdout as JSON, with human-readable errors on stderr and non-zero exit — consistent with today.

### D6: List targeting

`reminders_update` / `reminders_add` accept `list_name`. `reminders_lists` returns ids so a caller can disambiguate if names collide. The helper resolves a target list from `store.calendars(for: .reminder)` by name and validates `allowedEntityTypes.contains(.reminder)` and `allowsContentModifications`; ambiguous or invalid targets fail with an error naming the list.

### D7: Due-date handling

Go parses `due_date` as RFC3339 and passes the timestamp to the helper; the helper builds `dueDateComponents` from the calendar and sets `reminder.timeZone` separately rather than embedding a time zone inside the components. Rationale: community reports that embedding `.timeZone` in components can be silently dropped by iCloud. `clear_due` sets `dueDateComponents = nil`.

### D8: Error mapping

New helper failures map onto the existing connector error taxonomy: auth/denied/restricted → `ErrNotAuthorized` (System Settings guidance); unknown id → a not-found error naming the id; invalid target list → an error naming the list; AppleScript fallback unavailable on non-macOS for write tools → the platform-unavailable error.

## Risks / Trade-offs

- [Id instability after a full calendar sync] → return a clear not-found error; keep the lookup centralized so a resolution fallback can be added later.
- [Backend id mismatch (EventKit vs AppleScript)] → single runtime backend selection drives both list and mutation; document the invariant.
- [Wrong-time-zone due dates] → set `reminder.timeZone` separately; verify on both iCloud and local lists on the Mac.
- [Cross-account / shared-list moves fail or lose alarm/recurrence fidelity] → validate the target list and re-read the reminder after save; report failure rather than silently degrading.
- [List name collisions across accounts] → expose list ids from `reminders_lists`; error on ambiguous resolution.
- [Swift 6 concurrency: `EKEventStore`/`EKReminder` are not `Sendable`] → keep the existing single-threaded semaphore pattern; do not introduce concurrent EventKit access.
- [Helper and connector schemas drift] → the JSON contract is asserted by Go-side argv/parse tests and exercised by a manual Mac round trip.
- [Additive fields break strict consumers] → existing keys are preserved; new keys only added. Acceptable given no external consumer pins the exact row shape.

## Migration Plan

No data migration. Deploy order is immaterial because the change is additive: the Go connector, the embedded Swift helper, and the app catalog can ship together in one app build. Rollback is reverting the branch; older app builds remain compatible with the current hub.

## Open Questions

- Whether to add a `list_id` alternative to `list_name` on write tools (for collision-free targeting) can be decided during implementation without changing the specs.
- Whether notes should ever be returned by default can be revisited if agents frequently need a second listing call; default-off is safe and reversible.
