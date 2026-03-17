<!-- Baseline failures (pre-existing, not introduced by this change):
- None. `go build ./tools/cli/...` passes cleanly.
-->

## 1. Org Commands — `memory orgs` CRUD

- [x] 1.1 Create `tools/cli/internal/cmd/orgs.go` with the `orgs` parent command (GroupID: "account") and register it on `rootCmd`
- [x] 1.2 Implement `memory orgs list` — call `c.SDK.Orgs.List()`, display numbered list with name and ID, support `--json` output
- [x] 1.3 Implement `memory orgs get <id>` — call `c.SDK.Orgs.Get()`, display name and ID, support `--json` output
- [x] 1.4 Implement `memory orgs create --name <name>` — call `c.SDK.Orgs.Create()`, display created org name and ID, `--name` required
- [x] 1.5 Implement `memory orgs delete <id>` — call `c.SDK.Orgs.Delete()`, display confirmation message

## 2. Init Org Check — enhance `memory init`

- [x] 2.1 Add `initEnsureOrg` function in `init_project.go` that calls `c.SDK.Orgs.List()` and returns early if orgs exist
- [x] 2.2 When no orgs found, prompt user to create one with interactive name input (default: current directory name), call `c.SDK.Orgs.Create()`
- [x] 2.3 When user declines org creation, exit with message pointing to `memory orgs create --name <name>`
- [x] 2.4 Insert `initEnsureOrg` call in `runInitProject` fresh-run path (after client creation, before `initSelectOrCreateProject`)
- [x] 2.5 Verify re-run path (existing `.env.local` with `MEMORY_PROJECT_ID`) skips the org check

## 3. Verification

- [x] 3.1 Run `task build` to confirm no compile errors
- [x] 3.2 Run `task lint` to confirm no linting issues
