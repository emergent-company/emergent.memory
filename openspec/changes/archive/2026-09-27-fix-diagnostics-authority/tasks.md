## 1. Route gating

- [x] 1.1 Gate `GET /api/diagnostics` and `GET /debug` behind `RequireAuth()` + `RequireSuperadminFull()` in `health/routes.go`.
- [x] 1.2 Enumerate every health-domain route and give each an explicit authority decision (probes public; diagnostics platform-tier; scope-authority + metrics authenticated).
- [x] 1.3 Redact the long-running-query report in `Diagnose` (raw `query` → `query_length`).

## 2. Tests (TDD)

- [x] 2.1 `diagnostics_authz_test.go` — caller-class matrix: unauthenticated 401, non-superadmin 403, org_admin `admin:all` token 403, `superadmin_full` 200, sibling `/debug` gated identically.
- [x] 2.2 `diagnostics_redaction_test.go` — assert the long-queries SQL no longer selects raw query text.
- [x] 2.3 Fail-first run against pre-fix code: unauthenticated and non-superadmin reach 200.

## 3. Spec

- [x] 3.1 Add the `platform-diagnostics` delta spec documenting the diagnostic-surface authorization posture.

## 4. Verify

- [x] 4.1 `go build ./...` clean.
- [x] 4.2 `bash scripts/lint-ratchet.sh` — auth guards <= 13, apperror Style A <= 1201.
- [x] 4.3 `golangci-lint run` + `gofmt` + `go vet` clean on changed files.
- [x] 4.4 `openspec validate --all --strict` green.
