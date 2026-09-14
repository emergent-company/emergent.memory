# Mac connector e2e / integration coverage

**Status:** proposed
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

## Depends on
- Connector Go module (`connector/`) and the macOS `MemoryConnectorTests`.
- A relay test fixture (WS client) or the connector CLI as the fixture.

## Notes
- See `connector/internal/relay` fake-hub tests for the protocol harness shape.
- Keep deterministic; the browser round-trip stays a manual smoke.
