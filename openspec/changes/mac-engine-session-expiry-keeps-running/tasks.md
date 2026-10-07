# Tasks — mac-engine-session-expiry-keeps-running

## 1. Fix `reassertConnection` auth-failure teardown

- [x] 1.1 Reorder the `catch` so an auth/session failure sets `.signedOut` and
  returns `false` without clearing `connectedProjectID` or calling `stop`;
  a non-auth failure still clears the connection, stops the engine, and sets
  `.error`.
- [x] 1.2 Update the `reassertConnection` doc comment to state that an auth
  failure keeps the connected intent and the running engine, while a genuine
  config failure tears down.

## 2. Unit tests

- [x] 2.1 Auth failure via CLI `auth status` signed-out: `reassertConnection`
  returns `false`, `state == .signedOut`, keeps `connectedProjectID`, never
  invokes `stop`.
- [x] 2.2 Auth failure via text fallback (`invalid_token`, no `auth status`
  stub): same assertions.
- [x] 2.3 Non-auth failure: clears `connectedProjectID`, calls `stop`, sets
  `state == .error`.
- [x] 2.4 After an auth failure with a matching on-disk config,
  `EngineLifecyclePolicy.shouldRun(connectedProjectID:configURL:)` is still
  `true`.

## 3. Spec + verify

- [x] 3.1 Delta spec modifies "Signed-out state prompts re-authentication" with
  the keep-connected/keep-running requirement and a new scenario.
- [x] 3.2 `openspec validate mac-engine-session-expiry-keeps-running` passes.

<!-- openspec:archive-hold: tests added but Mac build/test not runnable on the Linux host; verify on mcj-mini before archive -->
