## Why

Connector apps and the core have drifted apart:

- The Linux connector's self-update resolves `connector-v*` release tags that
  never exist — releases publish `memory-connector_<X.Y.Z>_<os>_<arch>.tar.gz`
  on the **core** `vX.Y.Z` release — so `memory-connector upgrade` cannot find
  any release (both `Latest` and `ReleaseFor` fail). `install.sh` already uses
  core `v*`; only the self-update path was left behind.
- The mac app and iOS app are core-versioned at release time (the workflows
  derive the marketing version from the core tag) but their checked-in projects
  hardcode unrelated defaults (`0.1.0`, `1.0`), so the repository does not
  reflect the shipped version and nothing prevents divergence.
- The canonical root `VERSION` file stopped being bumped: it sat at `0.83.0`
  while `v0.84.0`/`v0.85.0` shipped and the server annotation reached `0.86.0`.
  `.github/workflows/auto-tag.yml` only fires on `VERSION` changes, so automatic
  tagging is effectively dead.
- There is no automatic update path for the Linux connector (only a manual
  `upgrade` command), unlike the CLI and the Sparkle-enabled mac app.

## What Changes

- **Self-update tracks core releases.** `memory-connector upgrade` resolves the
  newest core `vX.Y.Z` release and ignores non-`v` tags (notably Go module tags
  under `apps/server/pkg/sdk/`).
- **Automatic update check for the Linux connector.** A background checker runs
  at relay/daemon startup and on the configured interval. Default mode is
  `notify`; opt-in `auto` installs and lets systemd relaunch. It is enabled by
  default and fully disabled by `auto_update.enabled=false` or
  `MEMORY_CONNECTOR_NO_AUTO_UPDATE=1`; it never blocks startup, treats network
  errors as non-fatal, and refuses in-place installs inside a macOS `.app`.
- **MCP handshake version** reports the build-injected connector version instead
  of a hardcoded `0.1.0`.
- **Canonical core version.** Root `VERSION` is the single source; the server
  `// @version` annotation and the iOS `MARKETING_VERSION` (app + broadcast
  extension) must equal it, enforced by a drift guard in CI and pre-commit. The
  `release` task bumps `VERSION`, the server annotation, and the iOS project
  atomically.
- **Current drift reconciled** (`VERSION` `0.83.0` -> `0.86.0`).

## Capabilities

### New Capabilities

- `connector-update`: the Linux `memory-connector` self-update and automatic
  update checking, aligned with the core monorepo releases.

### Modified Capabilities

- `release-automation`: root `VERSION` becomes the canonical release version and
  every shipped app marketing version is derived from / guarded against it.

## Impact

- `apps/connector.linux/internal/upgrade/{client,check,cache}.go` + tests —
  core `v*` resolution and background update check.
- `apps/connector.linux/cmd/memory-connector/{relay,upgrade,main}.go` + tests —
  checker wiring, help text, handshake version.
- `apps/connector.linux/internal/{config,mcphost}` + tests — `auto_update`
  config, opt-out env, non-hardcoded handshake version.
- `apps/connector.linux/README.md` — tag scheme and self-update docs.
- `VERSION`, `apps/ios/VoiceAgent.xcodeproj/project.pbxproj`,
  `scripts/preflight/version-drift.sh`, `.github/workflows/ci.yml`,
  `lefthook.yml`, `Taskfile.yml` — canonical version + guard.
- `apps/server/VERSIONING.md`, `.opencode/skills/release/SKILL.md` — release
  process docs.
- `apps/cli/VERSION` — deleted (dead; only root `VERSION` feeds auto-tag).
