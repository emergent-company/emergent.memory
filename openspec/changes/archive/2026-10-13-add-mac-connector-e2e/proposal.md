## Why

The macOS connector has strong unit/render coverage (`MemoryConnectorTests`, 271+ hosted tests) but **no end-to-end coverage**. The deferred Playwright e2e and a real connector fixture were never added (see `apps/web-ui/docs/tasks/mac-connector-e2e-coverage.md`), so regressions in the app's window/navigation surface, and in the darwin engine's hub handshake, are caught only by manual smoke.

Two distinct gaps:

1. **App-level (UI)** — nothing drives the running SwiftUI app. The app is a menu-bar `NSStatusItem` + a `Window(id: "main")`; there is no `bundle.ui-testing` target, and there is no accessibility-identifier convention to target views with.
2. **Engine-level (process)** — the hermetic engine↔hub e2e already exists at `apps/connector.linux/e2e/connector_daemon_e2e_test.go`, but it is **Linux-only** (`requireRunnable` skips non-Linux) and asserts the Linux tool set. The Mac app spawns that same engine built `GOOS=darwin` (Apple Notes/Reminders tools via `internal/appletools`), so the darwin handshake has no automated coverage.

## What Changes

- Add a `MemoryConnectorUITests` (`bundle.ui-testing`) target and a small, deterministic UI smoke suite that drives the main window: it opens, every sidebar page renders, and the toolbar/footer controls are present.
- Add a `-uiTestMode` launch hook so UI tests run against a **temp profile** — no real config, Keychain, sign-in, or engine start.
- Add stable `accessibilityIdentifier`s to the sidebar rows, page roots, toolbar controls, and the status footer.
- Add a **darwin** end-to-end test for the engine: build the real `memory-connector` binary and run it as a subprocess against the in-process fake relay hub, asserting `register` → tool list → `status --json` connected. Runs on macOS only; skips under `-short` and on non-darwin hosts.
- Document the runner requirement: the UI suite needs a GUI/WindowServer session and a stable signing identity (ad-hoc signing resets TCC grants), so it cannot run over the ssh `tools/mac-build.sh` path — it needs a GUI-session invocation on the self-hosted mac runner.

## Capabilities

### New Capabilities

- `mac-connector-e2e`: end-to-end coverage for the macOS connector — the XCUITest app-level UI smoke suite and the darwin engine↔hub hermetic test, plus the launch-isolation and testability-identifier contracts they depend on.

### Modified Capabilities

<!-- none -->

## Impact

- `apps/connector.mac/MemoryConnector/project.yml`: new `MemoryConnectorUITests` target + scheme entry.
- `apps/connector.mac/MemoryConnector/UITests/`: new XCUITest suite.
- `apps/connector.mac/MemoryConnector/Sources/`: `accessibilityIdentifier`s on `MainWindowView` / sidebar / toolbar / footer; a `UITestMode` launch hook wired into `AppEnvironment` / `AppDelegate` / `EngineManager`.
- `apps/connector.linux/e2e/`: new darwin engine↔hub e2e (+ README note).
- `apps/connector.mac/Scripts/mac-build.sh`: `--uitest` mode (build the UI-test bundle; print the GUI-session run command).
- `apps/connector.mac/Scripts/README.md`: Tests section documenting how to run each suite manually.
- Not covered (explicitly out of scope): the menu-bar `NSStatusItem`/popover (not reliably XCUITest-reachable), OAuth web flows (manual), and **CI wiring for the macOS-only suites** — deferred by decision; the manual runs are documented instead.
