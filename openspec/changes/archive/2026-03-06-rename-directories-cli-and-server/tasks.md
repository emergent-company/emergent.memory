<!-- Baseline failures (pre-existing, not introduced by this change):
- apps/server/domain/provider/catalog_test.go: staticModels undefined
- apps/server/pkg/tracing/tracer_test.go: StartLinked / RecordErrorWithType undefined
-->

## 1. Git directory moves

- [x] 1.1 `git mv tools/emergent-cli tools/cli`
- [x] 1.2 `git mv apps/server-go apps/server`

## 2. Bulk replace Go import paths

- [x] 2.1 Replace `github.com/emergent-company/emergent.memory/tools/emergent-cli` → `.../tools/cli` in all `.go` files
- [x] 2.2 Replace `github.com/emergent-company/emergent.memory/apps/server-go` → `.../apps/server` in all `.go` files
- [x] 2.3 Verify clean: `rg 'emergent-company/emergent\.memory/tools/emergent-cli|emergent-company/emergent\.memory/apps/server-go' --glob '*.go'` must return empty

## 3. Update go.mod files

- [x] 3.1 `tools/cli/go.mod` — update `module` declaration to `.../tools/cli`
- [x] 3.2 `tools/cli/tests/docker/go.mod` — update `module` declaration
- [x] 3.3 `apps/server/pkg/sdk/go.mod` — update `module` declaration to `.../apps/server/pkg/sdk`
- [x] 3.4 `tools/cli/go.mod` — update `require` and `replace` entries for the SDK
- [x] 3.5 `tools/e2e-suite/go.mod` — update `require` and `replace` for SDK
- [x] 3.6 `tools/huma-test-suite/go.mod` — update `require` and `replace` for SDK
- [x] 3.7 `tools/opencode-test-suite/go.mod` — update `require` and `replace` for SDK
- [x] 3.8 `apps/server/go.mod` — update `require` entry for `apps/server/pkg/sdk`

## 4. Fix runtime path literals in Go source

- [x] 4.1 `apps/server/domain/devtools/handler.go` — change `"apps/server-go"` → `"apps/server"` in `os.Stat` call and `baseDir` assignment
- [x] 4.2 `apps/server/domain/email/module.go` — change `"apps/server-go/templates/email"` → `"apps/server/templates/email"`
- [x] 4.3 `apps/server/cmd/tasks/main.go` — change `"apps/server-go/go.mod"` → `"apps/server/go.mod"` and `return "apps/server-go"` → `return "apps/server"`

## 5. Fix `apps/server-go` references in `apps/server/` Go source comments and non-path strings

- [x] 5.1 `apps/server/AGENT.md` — update self-references (`apps/server-go/` → `apps/server/`)
- [x] 5.2 Run targeted grep for any remaining `apps/server-go` literal strings in `apps/server/**/*.go` and fix them

## 6. Update go.mod replace path targets (filesystem paths in replace directives)

- [x] 6.1 `tools/cli/go.mod` — `replace ... => ../../apps/server-go/pkg/sdk` → `../../apps/server/pkg/sdk`
- [x] 6.2 `tools/e2e-suite/go.mod` — same replace path update
- [x] 6.3 `tools/huma-test-suite/go.mod` — same
- [x] 6.4 `tools/opencode-test-suite/go.mod` — same

## 7. Run go mod tidy

- [x] 7.1 `go mod tidy` in `apps/server/`
- [x] 7.2 `go mod tidy` in `apps/server/pkg/sdk/`
- [x] 7.3 `go mod tidy` in `tools/cli/`
- [x] 7.4 `go mod tidy` in `tools/cli/tests/docker/`
- [x] 7.5 `go mod tidy` in `tools/e2e-suite/`
- [x] 7.6 `go mod tidy` in `tools/huma-test-suite/`
- [x] 7.7 `go mod tidy` in `tools/opencode-test-suite/`

## 8. Update GitHub Actions workflows

- [x] 8.1 `.github/workflows/emergent-cli.yml` — rename file to `cli.yml`; update all `tools/emergent-cli` path references, `-ldflags` module paths, artifact names, Docker image names
- [x] 8.2 `.github/workflows/server-go.yml` — rename file to `server.yml`; update all `apps/server-go` path references
- [x] 8.3 `.github/workflows/server-go-sdk.yml` — rename file to `server-sdk.yml`; update `apps/server-go/pkg/sdk` path references and SDK git tag naming convention
- [x] 8.4 `.github/workflows/publish-minimal-images.yml` — update any `tools/emergent-cli` or `apps/server-go` references

## 9. Update root Taskfile.yml

- [x] 9.1 `Taskfile.yml` — change `dir: tools/emergent-cli` → `dir: tools/cli`; update `apps/server-go` references in server tasks

## 10. Update shell scripts

- [x] 10.1 `scripts/release.sh` — update `apps/server-go/cmd/server/main.go` path, SDK tag `apps/server-go/pkg/sdk` → `apps/server/pkg/sdk`, workflow file name references
- [x] 10.2 `scripts/build-firecracker-rootfs.sh` — update `SERVER_DIR` path
- [x] 10.3 `scripts/preflight-check.sh` — update `cd apps/server-go` → `cd apps/server`
- [x] 10.4 `scripts/benchmark-compare.sh` — update `cd apps/server-go`
- [x] 10.5 `scripts/verify-remote-services.sh` — update `apps/server-go/.env.local` references
- [x] 10.6 `install.sh` — update binary artifact name from `emergent-cli` to `memory`
- [x] 10.7 `tools/cli/install.sh` — update binary artifact name and any `emergent-cli` refs
- [x] 10.8 `deploy/minimal/install-online.sh` — update binary artifact references
- [x] 10.9 `deploy/minimal/emergent-cli-wrapper.sh` — rename file to `memory-wrapper.sh`; update binary name
- [x] 10.10 `deploy/minimal/build-server-with-cli.sh` — update binary name reference
- [x] 10.11 `deploy/minimal/verify-install.sh` — update binary name references

## 11. Update Dockerfiles

- [x] 11.1 `tools/cli/Dockerfile` — update `-ldflags` module path, binary name `emergent-cli` → `memory`, ENTRYPOINT
- [x] 11.2 `tools/cli/Dockerfile.prebuilt` — update `COPY emergent-cli-*` → `memory-*`, ENTRYPOINT
- [x] 11.3 `deploy/minimal/Dockerfile.server-with-cli` — update `COPY tools/emergent-cli/` → `tools/cli/`, `COPY apps/server-go/` → `apps/server/`, binary name
- [x] 11.4 `apps/server/Dockerfile` — update `COPY apps/server-go/` → `apps/server/`
- [x] 11.5 `deploy/firecracker/Dockerfile` — update `COPY apps/server-go/` → `apps/server/`
- [x] 11.6 `deploy/firecracker/Dockerfile.rootfs` — update `COPY apps/server-go/` → `apps/server/`

## 12. Update AGENTS.md, skill files, and openspec config

- [x] 12.1 `AGENTS.md` (root) — update `tools/emergent-cli/` → `tools/cli/` and `apps/server-go/` → `apps/server/`
- [x] 12.2 `apps/server/AGENT.md` — update self-references
- [x] 12.3 `.opencode/skills/release/SKILL.md` — update path references
- [x] 12.4 `.opencode/skills/dev-services/scripts/restart.sh` — update `apps/server-go/dist/server` → `apps/server/dist/server`
- [x] 12.5 `openspec/config.yaml` — update directory references

## 13. Verify build and install

- [x] 13.1 `go build ./...` from `apps/server/` — must pass
- [x] 13.2 `go build ./...` from `tools/cli/` — must pass
- [x] 13.3 `task cli:install` — must pass
- [x] 13.4 `memory version` — must print `Memory CLI`
- [x] 13.5 `rg 'tools/emergent-cli|apps/server-go' --glob '*.go' --glob '*.mod'` — must return empty (excluding archived openspec docs)
