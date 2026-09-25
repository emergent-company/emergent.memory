## 1. Server — reserved marker + mint path

- [x] 1.1 Add `webhook:trigger` to `ValidApiTokenScopes` and `memoryScopeVocabulary`
- [x] 1.2 Add `webhookTriggerScope`/`webhookTriggerScopes` (hardcoded ceiling) in `domain/apitoken`
- [x] 1.3 Add `Service.CreateWebhookTriggerToken` (no scopes parameter; NULL user_id; name + expiry)
- [x] 1.4 Extend `rejectReservedScopes` to reject `webhook:trigger` on every user-facing path
- [x] 1.5 Unit tests: reserved-scope rejection, exact ceiling (TDD)

## 1b. Server — mint surface (operator path)

- [x] 1b.1 Add `POST /api/projects/:projectId/webhook-trigger-tokens` route (mirrors device-token `RequireAuth()`)
- [x] 1b.2 Add `Handler.CreateWebhookTriggerToken` (name + default 90-day expiry; constructors only)
- [x] 1b.3 Tests: handler shape (name/expiry/ceiling/NULL user), route auth gate (401 unauth), end-to-end mint→validate→revoke→deny, expiry

## 2. Server — validate-time ceiling + surface guard

- [x] 2.1 Add `webhookScopesMatchCeiling` + `rejectWebhookTokenOutsideSurface` in `pkg/auth`
- [x] 2.2 Wire the ceiling into `validateAPIToken` and the surface guard into `RequireAuth`
- [x] 2.3 Unit tests: ceiling match, surface allow/deny, effective-scope expansion (TDD)

## 3. Server — DB-backed lifecycle coverage

- [x] 3.1 Fail-first: tampered (out-of-ceiling) row rejected; admin scope rejected
- [x] 3.2 Mint → validate (use) → revoke → deny; expired → deny; absent → deny
- [x] 3.3 Per-integration isolation: revoking X leaves Y valid (service + validation)
- [x] 3.4 Store-unavailable denies, never passes through
- [x] 3.5 Rotation reuses `Regenerate` and preserves the ceiling

## 4. Gateway

- [x] 4.1 Update `config.go` / `webhook_github.go` / `supervisor.go` comments to the scoped credential
- [x] 4.2 Preserve the HMAC ingress gate and the fail-closed 503 + startup validation

## 5. Docs

- [x] 5.1 `09-security.md` — replace the residual-risk wording with the scoped-credential posture + operational model
- [x] 5.2 `10-api-contracts.md`, `02-memory-backend.md`, `08-deployment.md`, `04-go-application.md`, `MEMORY_GUIDE.md`

## 6. Verification

- [x] 6.1 `go build ./...` clean (server + gateway)
- [x] 6.2 `pkg/auth` + `domain/apitoken` tests green (unit + DB-backed)
- [x] 6.3 `apps/server/scripts/lint-ratchet.sh` all "ratchet ok" (auth guards = 13, apperror Style A ≤ 1201)
- [x] 6.4 `openspec validate --all --strict` green
