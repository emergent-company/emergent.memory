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

<!-- openspec:archive-hold: blocked — this change's `web-routing` delta MODIFIES the "Scoped landing redirect" requirement, but the `web-routing` capability does not exist in `openspec/specs/`: it is introduced by the still-unarchived `scoped-web-urls` change (#1564), which is merged to `main` but not archive-ready (task 7.4 manual browser check unticked, no `openspec:archive-ready` marker). `openspec archive fix-stale-landing-org --yes` is therefore structurally refused ("web-routing: target spec does not exist; only ADDED requirements are allowed for new specs"). Same shape as the `fix-share-parity-and-retention` hold in issue #1914. Unblock: archive `scoped-web-urls` first (tick 7.4 or add its own archive-ready marker), then delete this hold and run `openspec archive fix-stale-landing-org --yes`. -->
