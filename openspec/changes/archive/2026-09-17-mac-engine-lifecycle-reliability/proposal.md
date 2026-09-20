## Why

The Mac connector app's engine supervision can oscillate: the engine is started
and SIGTERM'd repeatedly, and the status footer can report "Disconnected" while
the Project & Account page reports the same project as "Connected". Observed on
a real Mac (`~/Library/Logs/memory-connector-app.log`): repeated
`engine start` → `engine stop (SIGTERM)` cycles over minutes, a stop-storm of
`=== memory-connector engine stop (SIGTERM) ===` lines, `management API: listen
tcp 127.0.0.1:8931: bind: address already in use`, and the engine connecting to
`projectId=9cc039e3…` (the prod project) while a different project was
"connected".

Root causes found in the app's lifecycle code (all pre-existing — unrelated to
the sign-in/UI work):

1. `EngineLifecyclePolicy.decision` only checks that a connected project exists
   and *a* parsable config exists. It never checks the config binds **that**
   project, so the engine happily starts (or keeps running) against another
   account's/ecosystem's config.
2. `ProjectStore.applyScope` sets `connectedProjectID = nil` and then to a
   value, and `AppEnvironment`'s `$connectedProjectID` sink reacts to **every**
   transition while a parallel async `reassertConnection()` rewrites the config
   — so one scope swap produces ~4 reconciles, including a start against the
   **stale** config.
3. `EngineManager.stop()` writes its `=== … stop (SIGTERM) ===` log line
   *before* the guard, so repeated no-op stops look like a kill storm and hide
   the real behaviour.
4. `EngineManager.start()` resets the restart circuit breaker on **every** call,
   so an app-driven stop/start oscillation can never trip it (only unexpected
   exits count).
5. The app never checks whether the loopback management port (`8931`) is already
   held before spawning, and never surfaces the engine's own bind failure — so a
   second engine can run with a dead management API, invisibly.

## What Changes

- **Match the config to the connected project**: `EngineLifecyclePolicy` gains a
  `.wrongProject` decision; the engine is never started when the on-disk config
  binds a different project, and (when the active server is known) a different
  server. A mismatch stops the engine and reports a configuration error.
- **One reconcile per scope swap**: a scope swap suppresses the
  `$connectedProjectID` sink and reconciles exactly once, after
  `reassertConnection()` has rewritten the config.
- **Truthful stop logging**: `stop()` logs only when it actually terminates a
  live process; `stoppedByUser` semantics unchanged.
- **Circuit breaker that can trip**: the breaker window resets only for a genuine
  configuration-change restart (`restart()`), not for every `start()`.
- **Refuse to spawn into a held port**: a pre-spawn probe of the management port
  fails fast with a clear error, and the engine's stderr bind failure is
  surfaced instead of being only lines in a log file.
- **Lifecycle observability**: log the account-change source and each scope
  apply, so an autonomous reconcile loop is distinguishable from user-driven
  sign-in. This settles an open question from the investigation: the amplifiers
  are provable from code, but whether the loop is self-driving or driven by
  repeated user sign-in/switch could not be determined statically.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities

- `mac-connector-app`: engine supervision gains a config↔project match gate,
  single-reconcile-per-scope-swap, a circuit breaker that actually bounds
  app-driven oscillation, fail-fast on a held management port, and lifecycle
  logging that distinguishes autonomous churn from user actions.

## Impact

- `MemoryConnector/Sources/EngineLifecyclePolicy.swift`: `.wrongProject`
  decision + `expectedServerURL`.
- `MemoryConnector/Sources/AppEnvironment.swift`: `syncEngineWithConnection`
  passes the expected server and handles `.wrongProject`; scope-swap reconcile
  gate (one reconcile, after the config rewrite); lifecycle log lines.
- `MemoryConnector/Sources/EngineManager.swift`: `start(resetBreaker:)`;
  `stop()` logs after the guard; management-port pre-flight probe; engine stderr
  bind-failure surfacing.
- `MemoryConnector/Sources/ProjectStore.swift`: no behaviour change expected;
  only if the single-reconcile gate needs a hook (prefer none).
- `MemoryConnector/Tests/`: new policy tests (`.wrongProject`), a
  scope-swap-gate test, a port-conflict decision test.
- Explicit non-goals: no Go/engine changes (`relay.go` single-instance lock,
  bind-fatal behaviour, truthful "listening" line), no reaping of unknown PIDs,
  no rendering of `lastError` in the footer, and no change to the footer/page
  status split.
