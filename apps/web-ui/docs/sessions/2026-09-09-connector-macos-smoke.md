# macOS smoke — memory-connector relay (add-mcp-connector, 2026-09-09)

Goal: task 6.3 — real-Mac validation of the new `connector/` Go CLI against a
live Memory relay hub.

## Setup
- Cross-built `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0` from the lane
  (`connector/`), scp'd to `mcj-mini:/tmp/memory-connector`.
- Config derived on-mac from the existing Diane config
  (`~/.config/diane.yml`): server `https://memory.emergent-company.ai`,
  project `e59a7c1c-6ec9-41aa-9fb4-79071a9569c7`, its `emt_*` token
  (token never printed/leaked); instance `mcj-mini-smoke-conn`.
- Mac: macOS 26.6.2 arm64; local Diane stack running (serve + mcp serve).

## Results
- `status` — local tools listed (4), hub presence correct (not connected → connected).
- Connect + register — stable. Sessions API showed the node with `tool_count: 4`.
- **Control experiment:** reference `diane mcp relay` flapped on this hub
  (its own `mcp serve` subprocess died → drop/reconnect cycles). Ours stayed
  connected continuously (44s+ watch window). One earlier transient drop
  (close 1006 @ ~9s) exercised reconnect + re-register correctly (log: `reconnecting in=30s`).
- Tool-call round trips through the full chain (hub REST → relay WS → dispatch
  → osascript runner → error back to caller):
  - `notes_search`, `reminders_list`, `reminders_add` all routed + dispatched;
    no app data returned under headless ssh.
- **Found + fixed two real AppleScript generation bugs via osascript:**
  1. shared JSON-escape prelude wrote an unterminated string literal
     (`replaceAll(s, "\", "\\")` — AppleScript backslash escapes inside
     literals) → parser desync, misleading `50:51 ... (-2741)`; fixed to
     `"\\"`/`"\\\\"`, handlers hoisted out of concatenations.
  2. `reminders_list` glued `use framework "Foundation"` onto the handler
     prelude without a trailing newline → `25:28: A "on" can't go after this
     """ (-2740)`; explicit newline added.
  Both covered by deterministic no-osascript regression tests
  (escape-aware script lexer + handler-placement + line-layout assertions).
- **Headless TCC boundary (expected, not a bug):** every AppleScript data call
  now executes but blocks past the 15s runner deadline — the macOS Automation
  permission prompt cannot render in an ssh session. Caller receives
  `osascript: context deadline exceeded` via the hub. Real Notes/Reminders
  data access requires a GUI login with Automation permission granted to the
  host process (README documents this).

## Follow-ups (outside this change)
- GUI-session test: grant Automation → real `notes_search`/`reminders_list`
  data round trip, `reminders_add` real write (then delete test reminder).
- Shell phase (2b) owns permission-prompt UX + launchd supervision.

## Artifacts
- Binary: cross-build reproducible via `GOOS=darwin GOARCH=arm64 go build ./cmd/memory-connector`.
- Relay/logs: ephemeral on mcj-mini `/tmp` (cleaned by reboot).
