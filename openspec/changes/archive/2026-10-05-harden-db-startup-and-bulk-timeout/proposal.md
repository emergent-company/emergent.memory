# Harden DB startup retry and bulk statement timeout

## Why

Issue #1481's recon found the Go server could contribute to restart-flaps and
pool starvation during a database incident:

- `NewPgxPool` hard-failed on the first failed `pool.Ping`, so a transient DB
  blip at startup crashed the process and amplified the flap.
- Long `bulk-update` / `bulk-upsert` requests issue many statements; a single
  runaway statement can hold a pool connection for the whole request budget and
  starve other requests.

## What Changes

- Startup connectivity verification retries `pool.Ping` with bounded
  exponential backoff before failing. Tunable via `DB_PING_MAX_ATTEMPTS`
  (default 10), `DB_PING_INITIAL_BACKOFF` (default 250ms) and
  `DB_PING_MAX_BACKOFF` (default 5s). The pool is never recreated on retry; the
  final error still names the last ping failure.
- Batch (`bulk-*`) endpoints attach a per-statement `statement_timeout` to each
  item transaction via `set_config('statement_timeout', …, true)`. Tunable via
  `DB_BULK_STATEMENT_TIMEOUT` (default 10m, `0` disables). Normal bulk ops are
  unaffected; the existing 10-minute per-request budget is preserved.
- Removes the dead, misleading `DB_AUTOINIT` environment variable (no Go
  reader) from compose files, env examples, installer templates and docs.

## Impact

- Config surface: three new `DB_PING_*` variables and one `DB_BULK_STATEMENT_TIMEOUT`.
- No API, schema, or migration change. `*bun.DB` construction and migration
  semantics are unchanged.
- Self-hosted `server` compose service gains `restart: unless-stopped`, matching
  the other services and dev.
