# Connector `reminders_list` cannot scale on a large Reminders library

**Status:** implemented (EventKit option chosen)
**Created:** 2026-09-11
**Source:** live debugging of agent `test` calling `mcj-mini-connector_reminders_list` on mcj-mini

## What

Make the connector's `reminders_list` tool return open reminders for realistic
libraries. Today it fails on the build Mac's actual library (**~700 reminders
across 21 lists**).

## Evidence (all measured on `mcj-mini`, 2026-09-11)

- `every reminder whose completed is false` (whole library): hangs / returns
  `-1728` with a partially-materialized result. Unusable.
- `get {name, due date, completed} of every reminder` (one batched get):
  works, ~15 s, but returns **column-major** `{{all names}, {all due dates},
  {all completed}}`, not a list of rows.
- Indexing a returned column per item (`item i of names`) round-trips to the
  Reminders app: 700 rows × 2 > 50 s. Unusable.
- Per-list scoped `whose` is fast per call (`every reminder of list "test"
  whose completed is false` ~2 s incl. `osascript` startup; `get
  {name, due date} of (...)` ~5 s), but looping all 21 lists inside one script
  still exceeds 45 s (~2 s/app query × 21).
- JXA equivalent (`osascript -l JavaScript`, `Application("Reminders").reminders.whose(...)`)
  is equally slow / hangs.

Net: the scripting bridge (`osascript` AppleScript or JXA) costs ~2 s per app
query and per-item property access is a round trip, so neither a single big
query (can't iterate the result) nor per-list queries (21 × 2 s) fit the relay
tool-call deadline.

## Fix shipped alongside this task

`fix/connector-reminders-list-batch-deref` (PR #93) fixes the **compile** bug:
the batched rows were dereferenced with Reminders property names outside the
`tell` scope (`due date of rec`), which never compiled (`-2741`). That is a
strict prerequisite but does not solve the scaling above.

## Options

1. **EventKit (recommended).** The macOS app is already Swift. Add a small
   EventKit-backed fetch (or expose EventKit via the app to the embedded
   connector) and have `reminders_list` use it. EventKit is the supported,
   fast API; needs the user to grant Reminders privacy access to Memory.app.
2. **Read the Reminders SQLite store directly** (`~/Library/Group Containers/
   group.com.apple.reminders/Container_v1/Stores/Data-*.sqlite`). Fast, but
   private/undocumented schema and requires Full Disk Access for the connector.
   Not recommended.
3. **Bounded/streaming AppleScript.** Keep `osascript` but add a hard timeout
   and an explicit `limit`, returning partial results plus a `truncated` flag
   instead of erroring. Cheap, but the tool stays slow and can't return the
   full list.
4. **Text-column fast path.** Return `name of every reminder`, `completed of
   every reminder`, and `due date of every reminder` as delimiter-joined text
   (one app query each, no per-item round trips) and filter/zip in Go. Names +
   completed work; `due date as text` is locale-formatted and not ISO, so this
   does not fully meet the schema.

## Implementation (option 1 shipped)

EventKit is implemented as a small Swift CLI, `memory-reminders`, embedded in
`Memory.app/Contents/Resources/` next to the engine:

- `client/macos/MemoryConnector/RemindersHelper/main.swift` — `import EventKit`,
  `EKEventStore`, `requestFullAccessToReminders`, `predicateForReminders(in:)`;
  filters completed unless `--include-completed`, filters `calendar.title ==
  NAME` when `--list NAME`; emits `[{"name":...,"due_date":ISO8601|null},...]`
  on stdout, errors on stderr with non-zero exit.
- `client/macos/MemoryConnector/Scripts/build-reminders-helper.sh` — second
  preBuildScript in `project.yml`; `xcrun --sdk macosx swiftc -O -framework
  EventKit` into the bundle's Resources dir.
- `client/macos/MemoryConnector/Info.plist` — `NSRemindersUsageDescription`.
  TCC attribution is Memory.app because the helper is a direct child of the
  app's engine (never launchd/`open`).
- Go: `connector/internal/appletools/reminders_helper.go` resolves the helper
  (env override `MEMORY_REMINDERS_HELPER`, else next to `os.Executable()`) and
  `reminders_list` prefers it, falling back to `remindersListScript` when
  absent (Linux/tests). `reminders_add` is unchanged.

The AppleScript evidence/limitations above still describe the fallback path;
the EventKit helper is the production path on macOS.

## Notes

- The connector is the Go engine embedded in `Memory.app`; `appletools` runs
  `osascript`. A Swift helper would likely live in `client/macos/MemoryConnector`.
- Gating: needs the relay tool-call deadline headroom and a decision on the
  permission surface (Reminders vs Full Disk Access).
