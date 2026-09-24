## Purpose

Defines the audit semantics of `core.api_tokens.last_used_at` for `emt_*` API tokens: what "last used" means, the resolution of the recorded timestamp, the write-frequency bound, and the guarantee that recording it is best-effort and non-blocking. It generalizes the device-credential touch introduced for `web-device-credential` to every successfully validated `emt_*` token.

## ADDED Requirements

### Requirement: Record last use on successful validation

The server SHALL record `core.api_tokens.last_used_at` for an `emt_*` token whenever that token is successfully validated (`validateAPIToken` finds a live, non-revoked, non-expired row within any scope ceiling). "Last used" SHALL mean "last seen on a successful validation", not "last authorized action". The touch SHALL cover every `emt_*` token class validated through `validateAPIToken` — project and account tokens, device credentials, agent-share keys, and ephemeral sandbox tokens — and therefore SHALL apply identically to direct Memory use and to gateway-proxied use, because the gateway forwards the credential verbatim and the same validation path runs server-side in both cases.

#### Scenario: Direct token use is recorded

- **WHEN** a client presents a valid `emt_*` token directly to the server and validation succeeds
- **THEN** the token's `last_used_at` is set (or refreshed) on the `core.api_tokens` row

#### Scenario: Gateway-proxied token use is recorded

- **WHEN** the gateway forwards a client's `emt_*` credential verbatim and the server validates it
- **THEN** the token's `last_used_at` is set through the same validation path

#### Scenario: Invalid tokens are not recorded

- **WHEN** a token is missing, revoked, expired, or outside its scope ceiling
- **THEN** validation fails and `last_used_at` is not touched

### Requirement: Best-effort, non-blocking write

Recording `last_used_at` SHALL be best-effort and non-blocking. It SHALL NOT perform a synchronous write on the hot auth path, and a failure in the write SHALL NOT fail or delay the request: the write is dispatched on a detached goroutine with a bounded lifetime, and any error is logged and otherwise discarded.

#### Scenario: A failed touch never fails the request

- **WHEN** the `last_used_at` write fails (store error, timeout)
- **THEN** the authenticated request still succeeds and the caller receives no indication of the touch failure

### Requirement: Bounded write frequency

The server SHALL coalesce `last_used_at` writes per token so that at most one write per token occurs per throttle interval (default one minute). The stored timestamp SHALL therefore be coarse — it is the time of a flush, which is within one throttle interval of the true last use — and readers MUST treat `last_used_at` as minute-grained, not an exact per-request timestamp.

#### Scenario: Rapid repeated use produces a bounded number of writes

- **WHEN** a token is used many times in rapid succession within the throttle interval
- **THEN** at most one `last_used_at` write occurs for that token in that interval

#### Scenario: Use after the interval is recorded again

- **WHEN** a token is used after the throttle interval has elapsed since its last flush
- **THEN** a new `last_used_at` write occurs
