# Mac connector e2e / integration coverage

**Status:** shipped (OpenSpec `add-mac-connector-e2e`, merged in
[#1751](https://github.com/emergent-company/emergent.memory/pull/1751))
**Created:** 2026-09-13
**Source:** [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md)

## What
Add automated end-to-end coverage for the macOS connector: engine register →
tools/list → call round-trip against a fake hub, account add/switch isolation
(per-account sessions/tokens/profiles), engine lifecycle gating (runs only when
connected), and the tool mint 409-recovery path. Optionally a Playwright/CLI e2e
that runs the connector against a dev Memory and asserts the node appears on the
MCP nodes page with the expected tools.

## Why
The app has strong unit/render coverage (271 hosted tests) but the deferred
Playwright e2e and a real connector fixture were never added; regressions in
relay protocol or account isolation would be caught only manually.

## Status / What landed
`add-mac-connector-e2e` covers the first two gaps:

- **App-level UI smoke** — new `MemoryConnectorUITests` (`bundle.ui-testing`)
  XCUITest suite driving the main window (opens; every sidebar page renders;
  toolbar + footer present), with a `-uiTestMode` launch hook for an isolated
  profile. Runs in a GUI session on the mac runner with stable signing — NOT via
  the ssh `mac-build.sh --test` path.
- **Darwin engine e2e** — a darwin runtime-gated sibling of the existing Linux
  hermetic daemon e2e (`apps/connector.linux/e2e/`): real `memory-connector`
  binary → in-process fake hub → `register` + tool list + `status --json`
  connected. Skips off-darwin and under `-short`.

## Verified (mac runner `mcj@tool`, macOS 27 / Xcode 27)

- `Scripts/mac-build.sh --test`: **424 hosted tests, 0 failures**.
- Darwin engine e2e (`go test ./e2e/...` on macOS):
  `TestDarwinDaemonRegisterAndStatus` **PASS**; the Linux tests skip off-linux.
- Connector lint clean (SwiftLint 0.65.1 + SwiftFormat 0.63.0, CI's binaries).

## Not yet verified

- The UI suite **compiles and lints** but has never been **executed**. XCUITest
  needs a GUI/WindowServer session with a stable signing identity; the ssh route
  is refused, so running it stays a manual step on the Mac
  (`Scripts/mac-build.sh --uitest`).

## Still deferred (manual smoke)
- Menu-bar `NSStatusItem`/popover (not reliably XCUITest-reachable).
- OAuth/sign-in web round-trip.
- Playwright/CLI e2e against a dev Memory asserting the node on the MCP nodes page.
- The tool mint 409-recovery path (track separately if it needs a server fixture).

## Depends on
- Connector Go module (`apps/connector.linux/`, whose `cmd/memory-connector` the
  mac app embeds) and the macOS `MemoryConnectorTests`.
- A relay test fixture: the in-process `fakeHub` in `apps/connector.linux/e2e/`.

## Notes
- See `apps/connector.linux/internal/relay` fake-hub tests for the protocol
  harness shape.
- Keep deterministic; the browser round-trip stays a manual smoke.
