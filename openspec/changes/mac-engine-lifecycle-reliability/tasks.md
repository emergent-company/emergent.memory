## 1. Policy: the config must bind the connected project

- [x] 1.1 Add `.wrongProject(projectID:configuredProjectID:)` to `EngineLifecyclePolicy.Decision` and extend `decision(connectedProjectID:configURL:expectedServerURL:fileManager:)` so a parsed config whose `project_id` differs from the connected id (or whose `project_id` is empty) returns `.wrongProject`; add the same optional `expectedServerURL` parameter to `shouldRun`, and skip the server comparison when it is `nil`/blank
- [x] 1.2 Add `Tests/EngineLifecyclePolicyTests.swift` cases: config for a different project → `.wrongProject`; config with no `project_id` → `.wrongProject(configuredProjectID: nil)`; `server_url` mismatch with `expectedServerURL` set → `.wrongProject`; matching server + project → `.run`; blank `expectedServerURL` skips the server check → `.run`
- [x] 1.3 Verify the five existing `EngineLifecyclePolicyTests` still pass unchanged (`project_id: p1` with connected `p1`, and the `connectedProjectID: nil` cases, are unaffected by the additive parameter)
- [x] 1.4 Wire `.wrongProject` into `AppEnvironment.syncEngineWithConnection()`: pass `accountStore.activeEnvironment?.serverURLString ?? settings.serverURL` as `expectedServerURL`, and on `.wrongProject` stop the engine, stop the status monitor, `markStopped()`, and `reportConfigurationError` with a message naming the connected and configured project ids
- [x] 1.5 Delete the dead `AppEnvironment.shouldStartEngine()` (no callers) rather than leaving an unwired gate; verify `grep -rn "shouldStartEngine"` returns nothing
- [x] 1.6 Verified `EngineConfigSync.configURL` resolves to `EngineManager.defaultConfigPath` (`EngineConfigSync.swift:10`), so the policy validates the same file the engine is launched with — a stricter gate cannot block starts on a path mismatch. The `.wrongProject` message distinguishes a project mismatch, an empty `project_id`, and a server-only mismatch (the enum carries no mismatch kind, so the case is inferred by shape)

## 2. One reconcile per account-scope swap

- [x] 2.1 Add a small pure `ScopeSwapGate` (or equivalent) exposing "begin a swap" / "may the sink reconcile?" / "end a swap" with a depth counter, so overlapping swaps keep the sink suppressed until all settle
- [x] 2.2 Add `Tests/ScopeSwapGateTests.swift`: the gate reports *suppressed* between begin and end; it reports *allowed* before any swap and after the last end; two overlapping swaps stay suppressed until both end; begin/end are balanced
- [x] 2.3 Use the gate in `AppEnvironment`: increment synchronously on the `@MainActor` thread **before** `projectStore.applyScope(scope)`, guard the `$connectedProjectID` sink with it, and in the existing `Task` run `reassertConnection()` → exactly one `syncEngineWithConnection()` → decrement
- [x] 2.4 Verify the sink is guarded, not removed: `.receive(on: RunLoop.main)` is still present, and `connect` / `disconnect` / `stopAndClear` still reconcile through the sink when no swap is in flight
- [x] 2.5 Verify the decrement cannot be skipped: `reassertConnection` catches internally (never throws) and `AppEnvironment` is a singleton, so the trailing reconcile and decrement always run

## 3. Truthful stop logging and a breaker that can trip

- [x] 3.1 `EngineManager.stop()`: move the `=== memory-connector engine stop (SIGTERM) ===` append below the `guard let proc = process, proc.isRunning` early return, keeping `stoppedByUser = true` set unconditionally before the guard; verify a second `stop()` with no process writes no line and returns silently
- [x] 3.2 `EngineManager.start()`: add `resetBreaker: Bool = false`; reset `policy`/`restartCount` only when it is `true`
- [x] 3.3 `EngineManager.restart()`: call `start(resetBreaker: true)` in both branches, and verify every other `start()` call site (`processDidExit` retry, `syncEngineWithConnection` `.run`) uses the default so a crash oscillation now accumulates toward `giveUp`
- [x] 3.4 Verify `restart()` remains the only breaker-resetting caller: `grep -rn "resetBreaker" Sources/` shows the declaration, the two `restart()` calls, and the parameter at the two non-resetting sites only

## 4. Fail fast on a held management port

- [x] 4.1 Add a settable `EngineManager.managementPortInUse` probe (default: TCP connect to `127.0.0.1:8931`) and a pure `shouldAbortStartForPortConflict(hasLiveProcess:portInUse:)` decision
- [x] 4.2 `EngineManager.start()`: when `process == nil` and the probe reports the port held, set `.failed`, set a `lastError` naming the port, append a clear log line, and **do not spawn**; verify the probe is skipped when a child exists so `restart()`'s just-SIGTERM'd child cannot false-positive
- [x] 4.3 Scan engine stderr for `management API` together with `address already in use`/`bind:` and surface it (log line + `lastError`), since the engine keeps running with a dead management API after a failed bind
- [x] 4.4 Add `Tests/EngineManagerPortProbeTests.swift` asserting the extracted decision: abort only when there is no live process **and** the port is in use; never abort when a child exists; never abort when the port is free. Restore the probe closure in `tearDown`

## 5. Lifecycle observability

- [x] 5.1 Log the account-change source (`signIn` / `switchTo` / `signOut`) at the `AccountStore.activeAccountDidChange` invocation sites, including the resulting account id or `nil`
- [x] 5.2 Log each `AppEnvironment.applyScope(for:)` with the account id, its environment, and the previous `connectedProjectID`
- [x] 5.3 Verify the two lines share an unmistakable marker (e.g. `[lifecycle]`) that does not appear in the engine's own stdout/stderr, so the driver question can be answered by correlating them with the existing `engine start`/`engine stop` lines

## 6. Verification

- [x] 6.1 `xcodegen generate` + `xcodebuild -scheme MemoryConnector -destination 'platform=macOS' build` succeeds on the Mac build machine
- [x] 6.2 `xcodebuild … test` runs the unit-test bundle green, including the new policy, gate, and port-conflict tests — 358 tests, 0 failures (up from 343; +15 new: 5 policy, 6 gate, 4 port-conflict)
- [ ] 6.3 Manual repro on the built app: sign in, switch between the dev and prod accounts, and connect a project — confirm the log shows one `engine start` per genuine change with no stop-storm, no `wrongProject` start, and no engine spawned into a held port. NOT RUN: this branch is based on `main` and does not contain the sign-in/padding work, so installing it would replace the app build currently under review on the build machine. Deferred until the lanes are combined, or run on request against a merged build.
- [x] 6.4 Confirm the change is Swift-app-only: `git diff --stat` touches nothing under `apps/connector.linux/` or `apps/server/`
- [x] 6.5 Spec stays in sync: the `mac-connector-app` delta spec in this change matches the shipped behaviour (`openspec validate … --strict` passes)
