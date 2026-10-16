## 1. Landing resolution

- [x] 1.1 Resolve `ListOrgs` once in `uiRoot` and validate the remembered org against the live list before redirecting to `/o/<remembered>`
- [x] 1.2 Clear the stale session context (`clearContext`) and fall through to the first live org, else `/orgs/new`, when the remembered org is gone
- [x] 1.3 Preserve the last-used project ordering, the `ListOrgs` unavailable page, the zero-org wizard, and the dev/API-key `/agents` redirect

## 2. Tests

- [x] 2.1 Add a case: remembered org no longer in `ListOrgs` → 302 `/o/<first-live-org>` (never the dead id) and the session org is cleared
- [x] 2.2 Add a case: remembered org gone with no live orgs → 302 `/orgs/new` and the session org cleared to empty
- [x] 2.3 Confirm existing cases (remembered org still live, `ListOrgs` failure, no session) still pass unchanged

## 3. Verification

- [x] 3.1 `go build ./...` and `go test ./...` pass in `apps/web-ui/gateway/`
- [x] 3.2 `golangci-lint run ./...` passes for this change (0 issues)
- [x] 3.3 `openspec validate fix-stale-landing-org --strict` passes
