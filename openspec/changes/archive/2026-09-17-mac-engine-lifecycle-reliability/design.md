## Context

`AppEnvironment` (`AppEnvironment.swift`) owns the app's single instance graph and
decides "may the engine run?" via `EngineLifecyclePolicy.decision`
(`EngineLifecyclePolicy.swift:27-39`), which today only requires a connected
project id **and** a config file that exists and parses (has a `server_url`).
`ProjectStore.applyScope` (`ProjectStore.swift:167-181`) sets
`connectedProjectID = nil` then to the scope's value; `AppEnvironment`'s
`$connectedProjectID` sink (`:89-93`) reconciles on every transition, in
parallel with `applyScope(for:)`'s async `Task { reassertConnection();
syncEngineWithConnection() }` (`:154-165`). `EngineManager` (`EngineManager.swift`)
spawns `relay --config … --api-port 8931` as a direct child; `stop()` logs before
its guard (`:141-151`); `start()` resets `RestartPolicy` unconditionally (`:92`).

`EngineConfigWriter.read` (`EngineConfigWriter.swift:75-77`) already returns
`Values` with `serverURL` and `projectID`, so the match gate needs no new parsing.

Evidence from the real Mac: repeated `engine start` → `engine stop (SIGTERM)`
cycles, a stop-storm of `=== engine stop (SIGTERM) ===` lines, engine the
connecting to prod `projectId=9cc039e3…` while a dev project was connected, and
`management API: listen tcp 127.0.0.1:8931: bind: address already in use`.

## Goals / Non-Goals

**Goals**
- The engine never starts against a config that binds a different project (or,
  when known, a different server) than the connected one.
- One account-scope swap produces exactly one engine reconcile, after the config
  is rewritten.
- The restart circuit breaker can actually bound an app-driven stop/start
  oscillation.
- A spawn into an already-held management port fails fast and visibly.
- The engine log distinguishes autonomous reconcile churn from user-driven
  sign-in/switch.

**Non-Goals**
- No Go/engine changes: `relay.go`'s unconditional `management API listening…`
  line, its non-fatal bind failure, and `relay` having no single-instance lock
  (`runRelayCommand(..., withLock: false)`, `relay.go:25-27`) are a separate
  change against `apps/connector.linux`.
- No reaping of an unknown PID holding the port — that could kill an unrelated
  process. Fail-fast report only.
- No change to the footer/page status split (`AppStatus.derive` vs
  `ProjectStore.hasConnectedProject`) and no rendering of `EngineManager.lastError`
  in the footer. Separate follow-ups.
- No UI changes: no page, control, or copy changes.

## Decisions

### D1 — The config must bind the connected project, or it is a configuration error

`EngineLifecyclePolicy.Decision` gains
`.wrongProject(projectID: String, configuredProjectID: String?)`, and
`decision(connectedProjectID:configURL:expectedServerURL:fileManager:)` compares
the parsed config's `project_id` to the connected id, plus (when
`expectedServerURL` is non-blank) its `server_url`. A mismatch stops the engine
and reports a configuration error; it never starts.

- **Why:** the policy is the app's single "may the engine run?" gate; an
  existence-only check is what let the engine serve another account's config.
- **Alternative rejected:** validating in `syncEngineWithConnection` only —
  bypasses the single gate and leaves `shouldRun` lying to every other caller
  and its tests.
- **Compatibility:** `expectedServerURL` defaults to `nil` and a blank value
  skips the server check, so existing callers and tests compile and pass
  unchanged.

### D2 — One reconcile per scope swap, via a suppression gate

While a scope swap is in flight, the `$connectedProjectID` sink must not
reconcile. A depth counter (not a boolean) is incremented synchronously on the
`@MainActor` thread *before* `applyScope` mutates the id, decremented only after
`reassertConnection()` completes and the single `syncEngineWithConnection()` has
run.

- **Why:** the sink fires on both the `nil` and the restored value, and the
  swap's own `Task` reconciles too — ~4 calls per swap, one of which starts the
  engine on the **stale** config. Deferring to one call after the config rewrite
  makes the sequence deterministic. A counter handles overlapping swaps.
- **Alternative rejected:** deleting the sink or dropping
  `.receive(on: RunLoop.main)`. Both are load-bearing for
  `connect`/`disconnect`/`stopAndClear`; removing them turns `connect` into a
  synchronous start→stop→start. Guard, don't delete.
- **Deadlock safety:** `reassertConnection` never throws (it catches
  internally), so the decrement always runs; `AppEnvironment` is a singleton so
  the decrement can never be skipped by deallocation.

### D3 — `stop()` logs only a real SIGTERM

The `=== memory-connector engine stop (SIGTERM) ===` line moves below the
`guard let proc = process, proc.isRunning` early return. `stoppedByUser = true`
stays before the guard and is set on every call.

- **Why:** `processDidExit` reads `stoppedByUser` to suppress restart; the log
  line is only truthful when a process was actually terminated. This removes the
  stop-storm that obscured the real behaviour during the investigation.
- **Alternative rejected:** logging at a different level — the file is a plain
  appended log with no level filtering.

### D4 — The circuit breaker resets only on a genuine configuration change

`start()` gains `resetBreaker: Bool = false`; only `restart()` (the
config-change entry point: connect, project switch, tool change) passes `true`.
The delayed exit-retry (`processDidExit`) and the reconcile path
(`syncEngineWithConnection` `.run`) use the default.

- **Why:** resetting on every `start()` means an app-driven oscillation never
  accumulates and can never trip `giveUp`.
- **Honest limitation:** the *observed* flap is `stoppedByUser`-driven, which
  never consults the breaker. D4 bounds **crash-driven** oscillation; the
  observed flap is addressed by D1, D2 and D5. D4 is hardening, and can be
  dropped independently if review disagrees.
- **Alternative rejected:** resetting on any `start()` whose config mtime
  changed — more machinery for the same outcome, and the CLI owns the config
  file.

### D5 — Fail fast on a held management port; surface the engine's bind failure

`start()` probes `127.0.0.1:8931` before spawning, but only when the app has no
*live* child (`process?.isRunning != true`) so `restart()`'s just-SIGTERM'd child
cannot false-positive, while a child that has already exited is still probed. A
held port sets `.failed` with a clear `lastError` and does not spawn.
Independently, the engine's stderr is scanned for
`management API` + `address already in use`/`bind:` and surfaced, line-framed
across chunk boundaries so a diagnostic split across reads is not missed.
*(Both refinements landed in #577, after this design was written: the predicate
above and the line framing.)*

- **Why:** the engine prints "listening" unconditionally and keeps running with
  a dead management API when the bind fails, so a duplicate engine is invisible
  today. The stderr scan is needed as well as the probe because `restart()` can
  race the old child's shutdown.
- **Alternative rejected:** killing whatever holds the port, or a pidfile lock —
  killing an unknown PID is unsafe, and a pidfile belongs in the Go engine
  (out of scope).
- **Testability:** the socket probe is a settable closure and the decision is
  extracted as a pure `shouldAbortStartForPortConflict(hasLiveProcess:portInUse:)`.

### D6 — Bundle the two lifecycle log lines

Log the account-change source (`signIn`/`switchTo`/`signOut`) and each
`applyScope` (account id, environment, previous connected id).

- **Why:** the amplifiers are provable from code, but whether the loop is
  self-driving or driven by repeated user sign-in/switch could not be determined
  statically. These two lines settle it against the next reproduction, cost
  ~10 lines, and are reversible.
- **Alternative rejected:** shipping the fixes without them — then the next
  reproduction still cannot distinguish "we fixed it" from "the user stopped
  clicking".

### D7 — Prefer pure extracted gates over injecting the whole instance graph

The reviewed design proposed protocol-typing `AppEnvironment.engine` /
`.statusMonitor` and injecting them so the reconcile count is unit-testable. This
change instead extracts the two risky decisions into small pure units
(`ScopeSwapGate`, `shouldAbortStartForPortConflict`) that `AppEnvironment` /
`EngineManager` delegate to, and unit-tests those.

- **Why:** protocol-typing `engine` is not local — `MainWindowView`,
  `StatusItemController`, `DashboardPage` and others read `engine.state`, and
  `EngineManager.State` is nested in the concrete type. That is a wide, risky
  refactor for a bug fix, and it cannot be compiled on the development host
  (Swift builds happen on the Mac). The extracted gates keep the tested logic
  identical in behaviour while leaving the instance graph alone.
- **Accepted gap:** the ~6 lines of wiring in `AppEnvironment` (calling the gate
  and reconciling once) are covered by review and the build/manual repro, not by
  a unit test. If a future change makes the graph injectable, the
  reconcile-count test from the reviewed design becomes cheap and should be
  added.
- **Alternative rejected:** doing both now — doubles the blast radius for no
  additional coverage of the actual defect.

## Risks / Trade-offs

- **[Wrong-project gate stops a working setup]** if `connectedProjectID` and the
  CLI's written `project_id` can legitimately diverge (e.g. mid-switch). →
  Mitigation: the gate only blocks *starts*; D2's single reconcile runs after
  the config rewrite, and the previous behaviour (serving the wrong project) is
  strictly worse. Tests cover equal/unequal/missing `project_id`.
- **[Suppression gate hides a legitimate transition]** if an id change lands
  while a swap is in flight and is never reconciled. → Mitigation: the swap's
  final reconcile reads the *current* id, and the counter re-opens the sink
  after the swap; a counter (not a boolean) keeps overlapping swaps correct.
- **[Port probe false positive]** on a machine where another app legitimately
  holds 8931 (the doc comment notes 8890 is reserved for the Diane companion,
  not 8931). → Mitigation: probe only when the app has no live child, and the
  error names the port so the cause is obvious.
- **[Regression risk without local compilation]** Swift only builds on the Mac
  build machine. → Mitigation: the full unit-test suite runs on the Mac before
  the change is considered done, and each fix is independently reversible.
