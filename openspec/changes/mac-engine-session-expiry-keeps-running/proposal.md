## Why

On every app launch the macOS connector starts its embedded engine, then within ~2s SIGTERMs it, leaving `http://127.0.0.1:8931` dead and the UI showing "Can't reach the connector's local management API" and "Can't load the registry".

The engine is healthy (connected, 7 tools) and runs on the config's long-lived project token, but the launch scope-settle calls `ProjectStore.reassertConnection()`, which runs the CLI `projects use`. That call needs a valid OIDC access token; when it has expired/cleared, `reassertConnection()`'s `catch` unconditionally does `connectedProjectID = nil; stop()` *before* classifying the failure as auth. The settle-chain's subsequent `syncEngineWithConnection()` then sees `connectedProjectID == nil` → `.noConnectedProject` and keeps the engine stopped.

## What Changes

- `ProjectStore.reassertConnection()` reorders its `catch`: an auth/session failure (CLI `auth status` signed-out, or the `not signed in` / `invalid_token` / `401` text fallback) sets `state = .signedOut` and returns `false` **without** clearing `connectedProjectID` or stopping the engine. Only a non-auth failure clears the connection and stops the engine, as before.
- The `reassertConnection` doc comment is updated to describe the new split: auth failure keeps the connected intent and running engine; a genuine config failure tears down.
- Unit tests cover the auth-failure path (connection kept, engine not stopped, `.signedOut`), the non-auth path (connection cleared, engine stopped, `.error`), and that a matching on-disk config still passes `EngineLifecyclePolicy.shouldRun` after an auth failure.

## Capabilities

### Modified Capabilities

- `mac-connector-app`: the "Signed-out state prompts re-authentication" requirement now states that an auth/session failure while a project is connected keeps the connected intent and does not stop a running engine whose config still binds that project; only non-auth failures tear down.

## Impact

- `apps/connector.mac/MemoryConnector/Sources/ProjectStore.swift`: reorder the `reassertConnection()` catch.
- `apps/connector.mac/MemoryConnector/Tests/ProjectStoreTests.swift`: new focused tests.
- No engine, server, or CLI changes; the connector CLI's `projects use`/`auth status` behavior is unchanged.
