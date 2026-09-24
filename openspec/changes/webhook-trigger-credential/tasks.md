## 1. Server — reserved marker + mint path

- [ ] 1.1 Add `webhook:trigger` to `ValidApiTokenScopes` and `memoryScopeVocabulary`
- [ ] 1.2 Add `webhookTriggerScope`/`webhookTriggerScopes` (hardcoded ceiling) in `domain/apitoken`
- [ ] 1.3 Add `Service.CreateWebhookTriggerToken` (no scopes parameter; NULL user_id; name + expiry)
- [ ] 1.4 Extend `rejectReservedScopes` to reject `webhook:trigger` on every user-facing path
- [ ] 1.5 Unit tests: reserved-scope rejection, exact ceiling (TDD)

## 2. Server — validate-time ceiling + surface guard

- [ ] 2.1 Add `webhookScopesMatchCeiling` + `rejectWebhookTokenOutsideSurface` in `pkg/auth`
- [ ] 2.2 Wire the ceiling into `validateAPIToken` and the surface guard into `RequireAuth`
- [ ] 2.3 Unit tests: ceiling match, surface allow/deny, effective-scope expansion (TDD)

## 3. Server — DB-backed lifecycle coverage

- [ ] 3.1 Fail-first: tampered (out-of-ceiling) row rejected; admin scope rejected
- [ ] 3.2 Mint → validate (use) → revoke → deny; expired → deny; absent → deny
- [ ] 3.3 Per-integration isolation: revoking X leaves Y valid (service + validation)
- [ ] 3.4 Store-unavailable denies, never passes through
- [ ] 3.5 Rotation reuses `Regenerate` and preserves the ceiling

## 4. Gateway

- [ ] 4.1 Update `config.go` / `webhook_github.go` / `supervisor.go` comments to the scoped credential
- [ ] 4.2 Preserve the HMAC ingress gate and the fail-closed 503 + startup validation

## 5. Docs

- [ ] 5.1 `09-security.md` — replace the residual-risk wording with the scoped-credential posture
- [ ] 5.2 `10-api-contracts.md`, `02-memory-backend.md`, `08-deployment.md`, `04-go-application.md`, `MEMORY_GUIDE.md`

## 6. Verification

- [ ] 6.1 `go build ./...` clean (server + gateway)
- [ ] 6.2 `pkg/auth` + `domain/apitoken` tests green (unit + DB-backed)
- [ ] 6.3 `apps/server/scripts/lint-ratchet.sh` all "ratchet ok" (auth guards = 13, apperror Style A ≤ 1201)
- [ ] 6.4 `openspec validate --all --strict` green
