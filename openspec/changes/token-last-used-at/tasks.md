## 1. Server — general last_used_at touch

- [ ] 1.1 Add `tokenUsageTracker` (per-token throttle + fire-and-forget flush) in `pkg/auth`
- [ ] 1.2 Wire the tracker into `Middleware` and call it from `validateAPIToken` for every `emt_*` token
- [ ] 1.3 Remove the device-only `touchDeviceTokenLastUsed` (superseded by the generalized touch)
- [ ] 1.4 Unit tests: throttle bound, non-fatal touch (TDD)

## 2. Server — DB-backed coverage

- [ ] 2.1 Add `core.api_tokens` to the auth DB test DDL
- [ ] 2.2 Fail-first test: `last_used_at` untouched before the change, set after successful validation (non-device token)
- [ ] 2.3 Device-path test: a device credential still gets `last_used_at` (no regression)
- [ ] 2.4 Non-fatal test: an injected store error does not fail validation

## 3. Docs + OpenSpec

- [ ] 3.1 OpenSpec change `token-last-used-at` (proposal + `api-token-audit` delta spec in SHALL form)

## 4. Verification

- [ ] 4.1 `go build ./...` clean
- [ ] 4.2 `pkg/auth` + `domain/apitoken` tests green (incl. new tests)
- [ ] 4.3 `apps/server/scripts/lint-ratchet.sh` all "ratchet ok" (`auth guards = 13`)
- [ ] 4.4 `golangci-lint run` on touched packages
- [ ] 4.5 `openspec validate --all --strict` green
