## 1. Phase 1 — Linux v1 daemon (token-only)

- [ ] 1.1 Implement the `daemon` subcommand (foreground; clean exit 0 on SIGTERM/SIGINT; non-zero on invalid config; automatic reconnect with backoff after transient loss); verify unit tests plus a signal-handling integration test and a reconnect test against a restartable stub hub
- [ ] 1.2 Implement the single-instance guard (lock per configuration) so a second daemon start fails fast; verify unit tests cover refusal while held and successful re-acquire after release
- [ ] 1.3 Implement the systemd user unit template and idempotent `install` / `uninstall`; verify a golden unit-render unit test, a re-run/idempotency test, and a manual `systemctl --user` smoke check (e2e, stretch)
- [ ] 1.4 Route Linux config/state/log paths through XDG and send daemon logs to the journal; verify unit tests for path resolution and log selection
- [ ] 1.5 Add the Linux tool provider returning an empty set plus a platform note; verify unit tests assert the empty set and that `status` explains the absence
- [ ] 1.6 Add Linux build/release wiring (amd64, arm64); verify both binaries build and print the version when run with no arguments
- [ ] 1.7 Confirm the token-only `init` / `relay` / `status` text behavior is unchanged on Linux; verify the full `connector` test suite passes (`cd connector && go test ./...`)
- [ ] 1.8 Publish Linux v1 artifacts (linux/amd64, linux/arm64) and confirm the v1 exit criteria in design "Release Boundaries" (token-only `init` → `install` → node connected, healthy `systemctl --user status`); record the released version

## 2. Phase 2a — Adopt Memory core SDK components

- [ ] 2.1 Add the Memory core Go SDK (`github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`) and use its public `auth`, `apitokens`, and `projects` packages; implement a thin `internal/memoryapi` wrapper that unit-tests against a stub HTTP server
- [ ] 2.2 Close the device-flow `offline_access` gap: land the small upstream SDK scope change (see design D11), or, as a fallback, issue the device-authorization request locally with the extra scope and reuse the SDK `PollForToken`; verify a unit test asserts a refresh token is obtained after device authorization
- [ ] 2.3 Implement headless sign-in using the SDK `auth` device flow (`InitiateDeviceFlow` / `PollForToken`) and the platform's existing OAuth client; verify unit tests cover device-code presentation, successful poll, expiry, and denial
- [ ] 2.4 Implement optional import of an existing Memory CLI session (`~/.memory/credentials.json`) via the SDK credential store without writing to it; verify unit tests cover import-when-unsigned and that the CLI file is not mutated

## 3. Phase 2b — Shared brain behaviors

- [ ] 3.1 Implement the per-account session store with serialized rotating refresh (persist rotated token before use, clear on failure); verify unit tests cover silent refresh, serialization, and refresh-failure sign-out
- [ ] 3.2 Implement account/project management and `projects list` / `projects use` with persisted selection; verify unit tests cover listing and that the selection survives a store reload
- [ ] 3.3 Implement project-scoped token mint / reuse / revoke via the SDK for the active project; verify unit tests cover mint-when-absent (with least-privilege scope requested), reuse-when-present, and clear-on-sign-out
- [ ] 3.4 Implement the file-backed secret store (0600 files, 0700 dir, atomic writes, tolerant reads); verify unit tests cover permissions, atomicity, missing/partial files
- [ ] 3.5 Implement engine-config materialization from the active profile and token, plus configuration/path compatibility (existing macOS path, XDG on Linux, explicit `--config` override, no rewrite on upgrade); verify unit tests for each
- [ ] 3.6 Implement `auth login` / `auth logout`; verify CLI tests plus a unit test that sign-out clears the active project's connector token
- [ ] 3.7 Introduce the structured status model and the versioned machine contract (`status --json` with schema version, version command, stable exit codes); verify unit tests incl. a golden JSON fixture and each exit-code class
- [ ] 3.8 Extract the bounded restart / give-up / user-stop-suppressed supervision policy as a pure component; verify unit tests cover restart-within-bound, breaker trip, and deliberate-stop suppression
- [ ] 3.9 Implement the two-step PKCE login surface (`auth start` / `auth complete` / `auth cancel`) with one-shot, TTL-bounded pending-login records (0600; verifier never emitted) and the stable error codes from design; verify unit tests cover start, complete, state mismatch, unknown/expired login, and concurrent-login isolation

## 4. Phase 3 — macOS adoption of the shared core

- [ ] 4.1 Add a Swift decoder for `status --json` and switch `StatusMonitor` to it; verify Swift unit tests decode fixtures including an unknown `hub_state`
- [ ] 4.2 Replace the Swift brain calls (auth, projects, config materialization, token store) with the Go CLI surface, using the `auth start` / `auth complete` bridging for the PKCE custom-scheme callback (design "Port Plan"); verify the existing Swift test suite stays green
- [ ] 4.3 Delete the orphaned Swift brain code; verify the macOS app builds and tests pass on the Mac build machine (`tools/mac-build.sh`)
- [ ] 4.4 Handle migration of a legacy Keychain-backed session to the file store on first shared-core run; verify an existing session is migrated (or the user is cleanly prompted to sign in) with no Keychain prompts on subsequent rebuilds

## 5. Verification and docs

- [ ] 5.1 Run `go build ./...`, `go test ./...`, and `task lint` in `connector/`; verify all pass
- [ ] 5.2 End-to-end smoke: token-only `init`, start the daemon, confirm the node registers and `status` reports connected with no local tools; record result
- [ ] 5.3 Update `connector/README.md` (Linux daemon, systemd, XDG paths, headless sign-in, reused Memory CLI auth/SDK) and verify `openspec validate add-linux-connector --strict` passes
