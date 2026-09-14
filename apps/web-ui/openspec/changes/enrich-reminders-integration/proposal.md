## Why

The connector's Reminders tools are read-poor and write-poor: `reminders_list` returns only `{name, due_date}` (no id, no list, no completion state), and `reminders_add` can only write to the default list. An agent cannot tell which list a reminder is in, cannot disambiguate two reminders with the same name, and cannot edit, complete, move, or delete anything. The mac app surfaces this directly — the assistant sees names and dates but no list context, so it cannot act on the user's requests.

## What Changes

- Enrich `reminders_list` rows with a stable `id`, `list` name, `completed` flag, and optional `notes` (behind `include_notes`), preserving the existing `name`/`due_date` keys.
- Add `reminders_lists` to enumerate reminder lists (name, id, count).
- Let `reminders_add` target a named list instead of only the default list.
- Add `reminders_update`: edit title, due date (set or clear), notes (set or clear), priority, completion, and move between lists — all by `id`. Change-by-presence; no separate move/complete tools.
- Add `reminders_delete`: delete a reminder by `id`.
- Extend the embedded EventKit helper (`memory-reminders`) with `lists`, `add`, `update`, and `delete` subcommands alongside the existing `list`.
- Add the new tools to the mac app tool catalog so they appear in per-tool enable/disable toggles.
- Update the connector README and the `mcp-connector` spec.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `mcp-connector`: the "Serve Apple Notes and Reminders tools on macOS" requirement expands from "at least listing and adding in Reminders" to a full reminders tool set — listing with identities and list context, enumerating lists, adding to a target list, updating/completing/moving, and deleting.

## Impact

- Connector (Go): `connector/internal/appletools/reminders.go`, `reminders_helper.go`, `provider.go`, `connector/README.md`.
- macOS helper (Swift): `client/macos/MemoryConnector/RemindersHelper/main.swift`.
- macOS app (Swift): `client/macos/MemoryConnector/Sources/ToolCatalog.swift` and its tests.
- Tests: `connector/internal/appletools/*_test.go`, `client/macos/MemoryConnector/Tests/ToolCatalogTests.swift`.
- Permissions: no change — `NSRemindersFullAccessUsageDescription` is already present and `.fullAccess` covers write/move/delete.
- Compatibility: additive. Existing `reminders_list`/`reminders_add` argument and result keys remain valid; only new fields/args/tools are introduced.
