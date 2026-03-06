## 1. Pre-flight

- [ ] 1.1 Verify `go build ./...` passes from repo root (baseline green build before any changes)
- [ ] 1.2 Confirm GitHub repo name `emergent.memory` is available in the `emergent-company` org

## 2. Rename GitHub Repository

- [ ] 2.1 In GitHub Settings → Rename repository from `emergent` to `emergent.memory`
- [ ] 2.2 Confirm GitHub redirect is active: `curl -I https://github.com/emergent-company/emergent` should return 301 → `emergent.memory`

## 3. Update Go Module Declarations (go.mod)

- [ ] 3.1 `apps/server-go/go.mod` — change `module github.com/emergent-company/emergent` → `module github.com/emergent-company/emergent.memory`; update `require` entry for the SDK
- [ ] 3.2 `apps/server-go/pkg/sdk/go.mod` — update `module` declaration
- [ ] 3.3 `tools/emergent-cli/go.mod` — update `module`, `require`, and `replace` entries
- [ ] 3.4 `tools/emergent-cli/tests/docker/go.mod` — update `module` declaration
- [ ] 3.5 `tools/e2e-suite/go.mod` — update `module`, `require`, and `replace` entries
- [ ] 3.6 `tools/huma-test-suite/go.mod` — update `module`, `require`, and `replace` entries
- [ ] 3.7 `tools/opencode-test-suite/go.mod` — update `module`, `require`, and `replace` entries

## 4. Bulk Replace Go Import Paths (393 source files)

- [ ] 4.1 Run bulk replacement: `find . -name '*.go' | xargs sed -i 's|github.com/emergent-company/emergent|github.com/emergent-company/emergent.memory|g'`
- [ ] 4.2 Verify clean: `rg 'github\.com/emergent-company/emergent[^.]' --glob '*.go'` must return empty
- [ ] 4.3 Run `go mod tidy` in each of the 7 workspace modules
- [ ] 4.4 Run `go build ./...` — must pass with zero errors

## 5. CLI Binary Name

- [ ] 5.1 `Taskfile.yml` — change `go build -o ~/.local/bin/emergent` to `~/.local/bin/memory`; update `traces:list` and `traces:get` invocations
- [ ] 5.2 `tools/emergent-cli/install.sh` — rename binary target to `memory`; update all help text
- [ ] 5.3 `deploy/minimal/install-online.sh` — rename binary install target to `memory`
- [ ] 5.4 `deploy/homebrew/emergent-cli.rb` — change all `bin.install ... => "emergent"` to `"memory"`; rename completion files to `memory`, `_memory`, `memory.fish`; update test assertion string

## 6. CLI Root Command + Help Text

- [ ] 6.1 `tools/emergent-cli/internal/cmd/root.go` — change `Use: "emergent"` to `Use: "memory"`; update Short/Long descriptions
- [ ] 6.2 `tools/emergent-cli/internal/cmd/version.go` — change printed `"Emergent CLI"` to `"Memory CLI"`; update Long description
- [ ] 6.3 `tools/emergent-cli/internal/cmd/auth.go` — update Short/Long strings; change printed `"Emergent Status"` to `"Memory Status"`
- [ ] 6.4 `tools/emergent-cli/internal/cmd/install.go` — update Short/Long strings; change `--dir /opt/emergent` example to `/opt/memory`
- [ ] 6.5 `tools/emergent-cli/internal/cmd/ctl.go` — update Short/Long strings
- [ ] 6.6 `tools/emergent-cli/internal/cmd/doctor.go` — update Long, printed `"Emergent CLI Diagnostics"` to `"Memory CLI Diagnostics"`, shell rc comment `# Emergent CLI` to `# Memory CLI`
- [ ] 6.7 `tools/emergent-cli/internal/cmd/completion.go` — update Long description
- [ ] 6.8 `tools/emergent-cli/internal/cmd/config.go` — update Short/Long strings
- [ ] 6.9 `projects.go`, `documents.go`, `graph.go`, `tokens.go`, `template_packs.go`, `mcp_servers.go`, `embeddings.go` — update Long description strings

## 7. CLI Config Directory and Install Paths

- [ ] 7.1 `tools/emergent-cli/internal/cmd/root.go` — change `$HOME/.emergent/config.yaml` to `$HOME/.memory/config.yaml` in flag description and `viper.AddConfigPath`
- [ ] 7.2 `tools/emergent-cli/internal/config/config.go` — change `.emergent/` paths to `.memory/`
- [ ] 7.3 `tools/emergent-cli/internal/cmd/install.go` — change `defaultDir` to `filepath.Join(homeDir, ".memory")`; update example path
- [ ] 7.4 `tools/emergent-cli/install.sh` — change `INSTALL_DIR` default to `$HOME/.memory/bin`; update all `.emergent` path references and PATH export
- [ ] 7.5 `deploy/minimal/install-online.sh` — change `INSTALL_DIR` default and PATH export to `.memory`
- [ ] 7.6 `tools/emergent-cli/internal/installer/installer.go` — update install binary path to `memory`; update all banner strings (`"Emergent Standalone Installer"` to `"Memory Standalone Installer"`, etc.)
- [ ] 7.7 `tools/emergent-cli/internal/installer/templates.go` — update volume mount `emergent_cli_config:/root/.emergent` to `memory_cli_config:/root/.memory`

## 8. Environment Variable Prefixes

- [ ] 8.1 `tools/emergent-cli/internal/cmd/root.go` — change `viper.SetEnvPrefix("EMERGENT")` to `viper.SetEnvPrefix("MEMORY")`
- [ ] 8.2 `tools/emergent-cli/internal/config/config.go` — change `v.SetEnvPrefix("EMERGENT")` to `"MEMORY"`
- [ ] 8.3 Rename all EMERGENT_* env vars in CLI source and flag descriptions: `EMERGENT_PROJECT` → `MEMORY_PROJECT`, `EMERGENT_PROJECT_ID` → `MEMORY_PROJECT_ID`, `EMERGENT_PROJECT_TOKEN` → `MEMORY_PROJECT_TOKEN`, `EMERGENT_PROJECT_NAME` → `MEMORY_PROJECT_NAME`, `EMERGENT_SERVER_URL` → `MEMORY_SERVER_URL`, `EMERGENT_API_KEY` → `MEMORY_API_KEY`, `EMERGENT_ORG_ID` → `MEMORY_ORG_ID`, `EMERGENT_CONFIG` → `MEMORY_CONFIG`, `EMERGENT_TEMPO_URL` → `MEMORY_TEMPO_URL`, `EMERGENT_GITHUB_TOKEN` → `MEMORY_GITHUB_TOKEN`, `EMERGENT_DATABASE_URL` → `MEMORY_DATABASE_URL`, `EMERGENT_TEST_SERVER` → `MEMORY_TEST_SERVER`
- [ ] 8.4 `apps/server-go/domain/workspace/gvisor_provider.go` — rename `EMERGENT_API_URL` injected into workspace containers to `MEMORY_API_URL`
- [ ] 8.5 Update any `.env.example` files that document `EMERGENT_*` variable names

## 9. Docker Image Names

- [ ] 9.1 `tools/emergent-cli/internal/installer/templates.go` — change `ServerImageRepo` to `"ghcr.io/emergent-company/memory-server-with-cli"`
- [ ] 9.2 `apps/server-go/domain/workspace/gvisor_provider.go` — change `defaultWorkspaceImage` to `"memory-workspace:latest"`
- [ ] 9.3 `deploy/minimal/docker-compose.yml` and `docker-compose.local.yml` — update image references
- [ ] 9.4 `deploy/minimal/install-online.sh` — change `SERVER_IMAGE_BASE` to `memory-server-with-cli`
- [ ] 9.5 `.github/workflows/publish-minimal-images.yml` — update image name to `memory-server-with-cli`
- [ ] 9.6 `.github/workflows/publish-workspace-image.yml` — update to `memory-workspace`
- [ ] 9.7 `.github/workflows/emergent-cli.yml` — update `emergent-cli` image name to `memory-cli`
- [ ] 9.8 `apps/server-go/Taskfile.yml` — change local Docker tag to `memory-server-go:latest`

## 10. Docker Container / Service Names and Volumes

- [ ] 10.1 `tools/emergent-cli/internal/installer/templates.go` — rename all container names to `memory-*`; rename volume to `memory_cli_config` and network to `memory`
- [ ] 10.2 `docker-compose.dev.yml` — rename `emergent-whisper`, `emergent-tempo`
- [ ] 10.3 `deploy/minimal/docker-compose.yml` and `docker-compose.local.yml` — rename all container/volume/network names
- [ ] 10.4 `apps/server-go/domain/workspace/gvisor_provider.go` — change `defaultRuntimeLabel` to `"memory.workspace"`; update volume format to `"memory-workspace-%d"`

## 11. MCP Server Identity

- [ ] 11.1 `apps/server-go/domain/mcp/jsonrpc.go` — change server name to `"memory-mcp-server-go"`
- [ ] 11.2 `apps/server-go/domain/mcp/service.go` — change `emergent://` URI scheme to `memory://` across all MCP resource URI strings
- [ ] 11.3 `tools/emergent-cli/internal/cmd/auth.go` — change MCP config key `"emergent"` to `"memory"` in both stdio and SSE config blocks
- [ ] 11.4 `tools/emergent-cli/internal/installer/installer.go` — change MCP JSON key `"emergent"` to `"memory"`

## 12. Server Brand Strings

- [ ] 12.1 `apps/server-go/cmd/server/main.go` — update swag annotations: `@title`, `@description`, `@contact.name` from Emergent to Memory
- [ ] 12.2 `apps/server-go/domain/devtools/handler.go` — update HTML title and inline spec title/description strings
- [ ] 12.3 `apps/server-go/domain/email/worker.go` — update email footer from `"This email was sent by Emergent."` to `"This email was sent by Memory."`
- [ ] 12.4 `apps/server-go/internal/config/otel.go` — change `OTEL_SERVICE_NAME` default from `"emergent-server"` to `"memory-server"`
- [ ] 12.5 `apps/server-go/internal/config/config.go` — change `EMAIL_FROM_NAME` default from `"Emergent"` to `"Memory"`

## 13. GitHub App Identity

- [ ] 13.1 `apps/server-go/domain/githubapp/service.go` — update GitHub App manifest `"name"` from `"Emergent"` to `"Memory"`
- [ ] 13.2 `apps/server-go/domain/githubapp/token.go` — update bot identity: `"emergent-app[bot]"` to `"memory-app[bot]"`; committer email; `"Emergent Agent"` to `"Memory Agent"`; `agent@emergent.local` to `agent@memory.local`
- [ ] 13.3 `apps/server-go/domain/workspace/checkout.go` — same git committer identity strings

## 14. Homebrew Formula

- [ ] 14.1 `deploy/homebrew/emergent-cli.rb` — rename Ruby class `EmergentCli` to `MemoryCli`; update `desc`, `homepage`, all 4 versioned download URLs, all `bin.install` targets, completion file names, and test assertion string

## 15. Functional GitHub URL References

- [ ] 15.1 `tools/emergent-cli/internal/cmd/upgrade.go` — update GitHub API URL to `api.github.com/repos/emergent-company/emergent.memory/releases/latest`
- [ ] 15.2 `install.sh` — update `GITHUB_REPO` to `"emergent-company/emergent.memory"`
- [ ] 15.3 `tools/emergent-cli/install.sh` — same `GITHUB_REPO` variable
- [ ] 15.4 `deploy/minimal/install.sh` — update `git clone` URL
- [ ] 15.5 `deploy/minimal/uninstall.sh` — update `raw.githubusercontent.com` URL
- [ ] 15.6 `deploy/minimal/emergent-ctl.sh` — update `raw.githubusercontent.com` URL
- [ ] 15.7 `scripts/install-minimal.sh` — update `raw.githubusercontent.com` URL
- [ ] 15.8 `tools/emergent-cli/tests/docker/entrypoint.sh` — update install URL
- [ ] 15.9 `apps/server-go/scripts/release-xcframework.sh` — update GitHub releases download URL

## 16. CI/CD Workflows

- [ ] 16.1 `.github/workflows/emergent-cli.yml` — update `-ldflags` module paths; update image names; optionally rename file to `memory-cli.yml`

## 17. Docs and Agent / Skill Files

- [ ] 17.1 `mkdocs.yml` — update `repo_url` and `repo_name`
- [ ] 17.2 `README.md` — update install URL and brand references
- [ ] 17.3 `apps/server-go/pkg/sdk/README.md` — update `go get` import paths
- [ ] 17.4 `docs/llms.md`, `docs/llms-go-sdk.md` — update SDK import paths and brand references
- [ ] 17.5 `AGENTS.md` — update module name, local path, and binary name references
- [ ] 17.6 `/root/emergent.memory.ui/AGENTS.md` — update `Backend lives at /root/emergent` to `/root/emergent.memory`
- [ ] 17.7 `.agents/skills/emergent-onboard/SKILL.md` — update path and binary references
- [ ] 17.8 `.opencode/skills/release/SKILL.md` — update repo, image, and path references
- [ ] 17.9 `tools/emergent-cli/internal/skillsfs/skills/emergent-onboard/SKILL.md` — update path and binary references
- [ ] 17.10 `tools/emergent-cli/internal/skillsfs/skills/pre-commit-check/SKILL.md` — update path references

## 18. Local Git Remote and Directory

- [ ] 18.1 `git remote set-url origin https://github.com/emergent-company/emergent.memory.git`
- [ ] 18.2 `mv /root/emergent /root/emergent.memory`

## 19. Verification

- [ ] 19.1 Run `go build ./...` — must pass
- [ ] 19.2 Run `POSTGRES_PORT=5436 POSTGRES_PASSWORD=local-test-password go test ./tests/e2e/... -count=1` — must pass
- [ ] 19.3 `rg 'github\.com/emergent-company/emergent[^.]' --glob '*.go' --glob '*.mod'` — must return empty
- [ ] 19.4 `memory version` — must print `Memory CLI`
- [ ] 19.5 `memory whoami` — must print `Memory Status`
- [ ] 19.6 Confirm config is read from `~/.memory/config.yaml`
- [ ] 19.7 Confirm `MEMORY_SERVER_URL` env var is respected
- [ ] 19.8 `memory auth mcp-config` — must use `"memory"` as the server key and `memory://` URI scheme
