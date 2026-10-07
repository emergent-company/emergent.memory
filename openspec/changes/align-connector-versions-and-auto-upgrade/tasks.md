## 1. Connector self-update tracks core releases

- [x] 1.1 `internal/upgrade/client.go`: `TagPrefix` `connector-v` -> `v`; docs updated; resolver ignores non-`v` tags (SDK module tags). Verify: `go test ./internal/upgrade/...`.
- [x] 1.2 `internal/upgrade/client_test.go`: fixtures inverted — plain `vX.Y.Z` selected as highest, `apps/server/pkg/sdk/v9.9.9` ignored; download URLs use `.../download/v<X.Y.Z>/...`. Verify: `go test ./internal/upgrade/...`.
- [x] 1.3 `cmd/memory-connector/upgrade.go` help text points at the core release; `cmd/memory-connector/upgrade_test.go` fixtures use `v*`. Verify: `go test ./cmd/memory-connector/...`.
- [x] 1.4 `README.md`: tag scheme `vX.Y.Z`, repo `emergent.memory`, self-update section rewritten.

## 2. Linux connector automatic update check

- [x] 2.1 `internal/upgrade/cache.go`: version cache (JSON, atomic write, `DefaultCachePath`). Unit tests: round-trip + freshness.
- [x] 2.2 `internal/upgrade/check.go`: `CheckForUpdate` with fresh-cache short-circuit, `dev`/empty skip, network-error tolerance. Unit tests: fresh-no-network, stale-fetch, dev-skip, error.
- [x] 2.3 `internal/config/config.go`: `auto_update` (`enabled`/`mode`/`check_interval`) with defaults, validation, and `MEMORY_CONNECTOR_NO_AUTO_UPDATE` opt-out. Unit tests: defaults, invalid mode/interval, env opt-out.
- [x] 2.4 `cmd/memory-connector/relay.go`: background checker at startup + interval; `notify` logs, `auto` installs then stops the relay (exit 1 so systemd `Restart=on-failure` relaunches); refuses inside a macOS `.app`. Unit tests for mode selection.
- [x] 2.5 `internal/mcphost/manager.go` + `cmd/memory-connector/main.go`: MCP handshake reports the build-injected version, not hardcoded `0.1.0`.

## 3. Canonical core version + drift guard

- [x] 3.1 Root `VERSION` bumped `0.83.0` -> `0.86.0` to match `apps/server/cmd/server/main.go` `// @version`.
- [x] 3.2 `scripts/preflight/version-drift.sh`: assert `VERSION` == server `@version` == every iOS `MARKETING_VERSION`. Negative-tested (exits 1 on mismatch).
- [x] 3.3 Wire the guard into `.github/workflows/ci.yml` (always-on `ci` job) and `lefthook.yml` pre-commit glob (VERSION, server `main.go`, iOS pbxproj).
- [x] 3.4 `Taskfile.yml` `release` task bumps `VERSION` + server `@version` + iOS `MARKETING_VERSION` in one commit.
- [x] 3.5 `apps/ios/VoiceAgent.xcodeproj/project.pbxproj` `MARKETING_VERSION` -> `0.86.0` (6 occurrences: app + broadcast extension, Debug/Release).
- [x] 3.6 Delete dead `apps/cli/VERSION` (unreferenced; `cli.yml` derives from the git tag).
- [x] 3.7 Docs: `apps/server/VERSIONING.md` + `.opencode/skills/release/SKILL.md` state root `VERSION` is canonical.

## 4. Verification

- [ ] 4.1 `apps/connector.linux`: `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./...` (one pre-existing unrelated failure in `internal/appletools` due to a past `due_date` fixture).
- [x] 4.2 `bash scripts/preflight/version-drift.sh` passes at `0.86.0`; negative test fails.
- [ ] 4.3 Root `task lint` on the worktree.
- [ ] 4.4 Open PR against `main`; archive after merge (`openspec archive align-connector-versions-and-auto-upgrade --yes`).
