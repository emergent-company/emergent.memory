# Implementation tasks

## 1. Narrow the chokepoint

- [ ] 1.1 Remove the `org_admin` OR-arm from `pkg/auth.CanGrantAdminAll` so only an active `superadmin_full` (`core.superadmins`, `revoked_at IS NULL AND role = 'superadmin_full'`) qualifies, and update its doc comment to describe the new rule and note it supersedes #812 §4.3
- [ ] 1.2 Fix the `admin:all` denial messages in `domain/apitoken/service.go` to state `superadmin_full`
- [ ] 1.3 Flip the `org_admin can mint admin:all` case in `domain/apitoken/repository_test.go` to assert denial; keep superadmin_full allowed, superadmin_readonly denied, project_admin denied, no-role denied
- [ ] 1.4 Correct `openspec/changes/unify-scope-authority/design.md` D4 and `tasks.md` §4.3 so they no longer pin `superadmin_full OR org_admin` as intended

## 2. Verification

- [ ] 2.1 `cd apps/server && PATH="/root/go/bin:$PATH" go build ./...`
- [ ] 2.2 `TEST_DATABASE_URL=<hermetic> REQUIRE_DB=1 go test ./pkg/auth/... ./domain/apitoken/...` (hermetic Postgres only)
- [ ] 2.3 `openspec validate --all --strict`
- [ ] 2.4 `./scripts/lint-ratchet.sh` — auth guards stay 13; apperror Style A <= 1201
- [ ] 2.5 `golangci-lint run` + `gofmt` + `go vet` clean on changed files
