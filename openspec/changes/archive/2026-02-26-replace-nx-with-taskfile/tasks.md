## 1. Expand apps/server-go/Taskfile.yml

- [x] 1.1 Add `swagger` task: run `swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal`
- [x] 1.2 Add `swagger:check` task: run `./scripts/check-swagger-annotations.sh`
- [x] 1.3 Add `dev` task: run `air` with PATH set (hot reload)
- [x] 1.4 Add `start` task: build then run `./dist/server` in background, write PID to `../../pids/server.pid`
- [x] 1.5 Add `stop` task: read PID from `../../pids/server.pid` and kill process
- [x] 1.6 Add `status` task: check if server PID is alive and hit `/health`
- [x] 1.7 Add `test:integration` task: run `go test ./tests/integration/... -v -count=1`
- [x] 1.8 Add `test:coverage` task: run go test with `-coverprofile=coverage.out` and `go tool cover`
- [x] 1.9 Add `docker:build` task: run `docker build -f Dockerfile -t emergent-server-go:latest ../..`

## 2. Create root Taskfile.yml

- [x] 2.1 Create `/root/emergent/Taskfile.yml` with `includes: {server: apps/server-go/Taskfile.yml}`
- [x] 2.2 Add top-level alias tasks: `build`, `test`, `test:e2e`, `test:integration`, `lint`, `migrate:up`, `migrate:down`, `migrate:status` — all delegating to `server:*`
- [x] 2.3 Add `dev` task: cd to `apps/server-go` and run `air` (hot reload, foreground)
- [x] 2.4 Add `start`, `stop`, `status` workspace tasks delegating to `server:start/stop/status`
- [x] 2.5 Add `swagger` task delegating to `server:swagger`

## 3. Remove JS infrastructure

- [x] 3.1 Delete `tools/workspace-cli/` directory
- [x] 3.2 Delete `nx.json`
- [x] 3.3 Delete `apps/server-go/project.json`
- [x] 3.4 Slim `package.json`: remove all deps/devDeps except husky; remove workspaces entry for tools/workspace-cli; keep only `prepare: husky` script
- [x] 3.5 Delete `tsconfig.json` at root (used only by Nx/workspace-cli)
- [x] 3.6 Run `npm install` to regenerate minimal `node_modules` with only husky
- [x] 3.7 Delete `pnpm-lock.yaml` (was no package-lock.json; removed stale pnpm lockfile instead)

## 4. Remove CI workflow for workspace-cli

- [x] 4.1 Delete `.github/workflows/workspace-cli-verify.yml`
- [x] 4.2 Delete `.github/workflows/admin-e2e.yml` (admin is now in separate repo)

## 5. Update documentation

- [x] 5.1 Update `AGENTS.md`: replace `nx run server-go:*` commands with `task *` equivalents
- [x] 5.2 Update `openspec/project.md`: replace test commands block with task equivalents
- [x] 5.3 Update `.opencode/instructions.md`: replace nx/pnpm workspace commands with task commands
- [x] 5.4 Update `apps/server-go/AGENT.md`: nx references replaced with task commands
- [x] 5.5 Update `.github/instructions/testing.instructions.md`: replaced all nx/workspace-cli commands
- [x] 5.6 Update `.github/instructions/workspace.instructions.md`: replaced all nx/workspace-cli commands
- [x] 5.7 Update `.github/copilot-instructions.md`: replaced all nx/workspace-cli commands
- [x] 5.8 Update `.github/instructions/self-learning.instructions.md`: replaced stale references

## 6. Validate

- [x] 6.1 Run `task build` from repo root — compiled Go server successfully
- [x] 6.2 Run `task server:test` from repo root — ran unit tests (pre-existing failures unrelated to migration)
- [x] 6.3 Verify `node_modules` is minimal (only husky) — 44K, 1 package
- [x] 6.4 Verify `nx.json` and `apps/server-go/project.json` are gone — confirmed
