## 1. Database & Schema Setup

- [x] 1.1 Create Goose migration for `kb.agent_workspaces` table with all columns: `id`, `agent_session_id`, `container_type`, `provider`, `provider_workspace_id`, `repository_url`, `branch`, `deployment_mode`, `lifecycle`, `status`, `created_at`, `last_used_at`, `expires_at`, `resource_limits`, `snapshot_id`, `mcp_config`, `metadata`
- [x] 1.2 Add partial index `idx_workspaces_persistent` for MCP server queries (`container_type = 'mcp_server' AND lifecycle = 'persistent'`)
- [x] 1.3 Create Bun ORM model `AgentWorkspace` in `apps/server-go/internal/agent/workspace/model.go`
- [x] 1.4 Create workspace store with CRUD operations (`Create`, `GetByID`, `List`, `Update`, `Delete`, `ListPersistentMCPServers`, `ListExpired`)
- [x] 1.5 Write unit tests for workspace store operations

## 2. Provider Interface & Abstractions

- [x] 2.1 Define `WorkspaceProvider` interface in `apps/server-go/internal/agent/workspace/provider.go` with methods: `Create`, `Destroy`, `Stop`, `Resume`, `Exec`, `ReadFile`, `WriteFile`, `ListFiles`, `Health`, `Capabilities`
- [x] 2.2 Define provider capability types: `ProviderCapabilities` struct with `SupportsPersistence`, `SupportsSnapshots`, `SupportsWarmPool`, `RequiresKVM`, `EstimatedStartupMs`
- [x] 2.3 Define common request/response types: `CreateRequest`, `ExecRequest`, `ExecResult`, `FileReadRequest`, `FileReadResult`, `FileWriteRequest`, `HealthStatus`
- [x] 2.4 Define resource limit types: `ResourceLimits` struct with `CPU`, `Memory`, `Disk` fields and validation

## 3. gVisor Provider Implementation

- [x] 3.1 Create `gvisor_provider.go` implementing `WorkspaceProvider` using Docker SDK with `--runtime=runsc`
- [x] 3.2 Implement `Create`: Docker container creation with gVisor runtime, named volumes (`agent-workspace-{id}`), resource limits (`--cpus`, `--memory`)
- [x] 3.3 Implement `Destroy`: Container removal and named volume cleanup
- [x] 3.4 Implement `Stop` / `Resume`: Docker container pause/unpause with status tracking
- [x] 3.5 Implement `Exec`: Command execution via `docker exec` with timeout, stdout/stderr capture, and exit code
- [x] 3.6 Implement `ReadFile`: Read file content via `docker exec cat` with offset/limit support and line numbering
- [x] 3.7 Implement `WriteFile`: Write file content via `docker exec tee` with auto-create parent directories
- [x] 3.8 Implement `ListFiles`: File glob via `docker exec find` with pattern matching
- [x] 3.9 Implement `Health`: Check Docker daemon connectivity and gVisor runtime availability
- [x] 3.10 Implement fallback to standard Docker runtime when gVisor is not installed (with security warning log)
- [x] 3.11 Write integration tests for gVisor provider (container lifecycle, exec, file operations)

## 4. Firecracker Provider Implementation

- [x] 4.1 Add `firecracker-go-sdk` dependency to `go.mod`
- [x] 4.2 Create `firecracker_provider.go` implementing `WorkspaceProvider`
- [x] 4.3 Implement KVM availability detection (`/dev/kvm` access check)
- [x] 4.4 Implement `Create`: MicroVM creation with vCPUs, memory, root block device (sparse ext4 file), TAP network device
- [x] 4.5 Implement block device management: sparse file creation, ext4 formatting, copy-on-write cloning for warm pool
- [x] 4.6 Implement TAP device networking: create TAP device, assign IP from private subnet, configure iptables NAT for outbound access
- [x] 4.7 Create lightweight in-VM agent binary for command execution (receives commands via vsock or HTTP on private network, executes, returns results)
- [x] 4.8 Build base VM image (rootfs) with: Linux kernel, in-VM agent, git, common build tools, standard shell utilities
- [x] 4.9 Implement `Exec`: Send command to in-VM agent, receive structured stdout/stderr/exit_code response
- [x] 4.10 Implement `ReadFile` / `WriteFile`: Route through in-VM agent for filesystem operations
- [x] 4.11 Implement `Destroy`: Shutdown VM via Firecracker API, cleanup block device file, remove TAP device and iptables rules
- [x] 4.12 Implement `Stop` / `Resume`: Pause/resume VM, preserve block device
- [x] 4.13 Implement `Health`: Report KVM status, active VM count, available resources
- [x] 4.14 Create Dockerfile for `emergent/firecracker-manager` container with `/dev/kvm` mount and privileged mode
- [x] 4.15 Add `firecracker-manager` service to `docker-compose.yml` with KVM access, data volume, and environment config
- [x] 4.16 Write integration tests for Firecracker provider (requires KVM; skip on CI if unavailable)

## 5. E2B Provider Implementation

- [x] 5.1 Add `e2b-go` SDK dependency to `go.mod`
- [x] 5.2 Create `e2b_provider.go` implementing `WorkspaceProvider`
- [x] 5.3 Implement `Create`: E2B sandbox creation via SDK with template selection and resource mapping
- [x] 5.4 Implement `Exec`: Command execution via `sandbox.Process.Start()` with stdout/stderr streaming
- [x] 5.5 Implement `ReadFile` / `WriteFile`: Use `sandbox.Filesystem.Read()` / `sandbox.Filesystem.Write()`
- [x] 5.6 Implement `ListFiles`: Execute `find` command inside sandbox, parse output
- [x] 5.7 Implement `Destroy`: Call `sandbox.Close()` to release E2B resources
- [x] 5.8 Implement `Health`: Validate E2B API key, check connectivity to E2B service
- [x] 5.9 Implement E2B quota tracking: count sandbox creates, track compute minutes, warn on plan limits
- [x] 5.10 Document E2B persistence limitation (ephemeral by design) in provider capabilities
- [x] 5.11 Write integration tests for E2B provider (requires E2B API key; skip if not configured)

## 6. Workspace Orchestrator

- [x] 6.1 Create `orchestrator.go` with provider registry: register/deregister providers, list available providers
- [x] 6.2 Implement automatic provider selection logic: managed → E2B; self-hosted+KVM → Firecracker; fallback → gVisor
- [x] 6.3 Implement container-type-aware routing: agent workspaces prefer Firecracker; MCP servers prefer gVisor
- [x] 6.4 Implement fallback chain: on provider failure, try next provider in priority order; skip fallback for explicit provider requests
- [x] 6.5 Implement provider health monitoring: 30-second health check loop, cache results, remove unhealthy providers from selection pool
- [x] 6.6 Implement provider health status API endpoint: `GET /api/v1/agent/workspaces/providers`
- [x] 6.7 Write unit tests for orchestrator selection logic (mock providers)
- [x] 6.8 Write unit tests for fallback chain behavior

## 7. Workspace Tool Interface (OpenCode-Inspired API)

- [x] 7.1 Create tool handler structs in `apps/server-go/domain/workspace/tool_handler.go` (adapted from original path to match codebase conventions)
- [x] 7.2 Implement `POST /api/v1/agent/workspaces/:id/bash` handler: validate workspace exists and is ready, delegate to provider `Exec`, enforce timeout (default 120s, max configurable), truncate output at 50KB with truncation indicator
- [x] 7.3 Implement `POST /api/v1/agent/workspaces/:id/read` handler: validate file path, delegate to provider `ReadFile`, support offset/limit params, return line-numbered content, handle directory listing
- [x] 7.4 Implement `POST /api/v1/agent/workspaces/:id/write` handler: validate file path, delegate to provider `WriteFile`, auto-create parent directories
- [x] 7.5 Implement `POST /api/v1/agent/workspaces/:id/edit` handler: read file, find `old_string`, validate uniqueness (error on multiple matches unless `replace_all`), replace, write back
- [x] 7.6 Implement `POST /api/v1/agent/workspaces/:id/glob` handler: delegate to provider `ListFiles` with glob pattern, return sorted by modification time
- [x] 7.7 Implement `POST /api/v1/agent/workspaces/:id/grep` handler: execute `grep -rnE` via provider `Exec`, parse output into structured matches with file paths and line numbers, support `include` file filter
- [x] 7.8 Implement `POST /api/v1/agent/workspaces/:id/git` handler: support actions `status`, `diff`, `commit`, `push`, `pull`, `checkout`; credential sanitization in responses; never expose credentials in response
- [x] 7.9 Add tool operation audit logging middleware: log operation type, workspace ID, user ID, timestamp, duration, request summary (no file contents or command output)
- [x] 7.10 Write integration tests for each tool endpoint with a real gVisor workspace

## 8. Workspace Lifecycle API

- [x] 8.1 Create Echo route group for `/api/v1/agent/workspaces` in `apps/server-go/internal/agent/workspace/handler.go`
- [x] 8.2 Implement `POST /api/v1/agent/workspaces` handler: parse creation request (container_type, provider, repository_url, branch, resource_limits, warm_start), delegate to orchestrator, persist to database, return workspace ID and status
- [x] 8.3 Implement `GET /api/v1/agent/workspaces/:id` handler: return workspace status, provider, resource usage, creation time, last used time
- [x] 8.4 Implement `GET /api/v1/agent/workspaces` handler: list workspaces with filters (container_type, status, provider), pagination support
- [x] 8.5 Implement `DELETE /api/v1/agent/workspaces/:id` handler: send SIGTERM to running processes, wait 10s for graceful shutdown, destroy via provider, remove database record
- [x] 8.6 Implement `POST /api/v1/agent/workspaces/:id/stop` handler: pause workspace via provider, update status to `stopped`
- [x] 8.7 Implement `POST /api/v1/agent/workspaces/:id/resume` handler: resume paused workspace, update status to `ready`
- [x] 8.8 Implement concurrent workspace limit check: reject creation if max limit reached (configurable, default 10)
- [x] 8.9 Implement TTL extension on tool operations: update `last_used_at` and extend `expires_at` on every tool call
- [x] 8.10 Register workspace routes in the Echo router and add to the fx module
- [x] 8.11 Write API integration tests for workspace CRUD lifecycle

## 9. Code Checkout Automation

- [x] 9.1 Create `checkout.go` service in workspace package for git clone operations with `GitCredentialProvider` interface
- [x] 9.2 Implement auto-clone on workspace creation: `CloneRepository()` executes `git clone --depth 1` inside workspace after container is ready
- [x] 9.3 Implement private repo credential injection: `buildCloneURL()` generates installation access token from GitHub App, format clone URL as `https://x-access-token:${TOKEN}@github.com/...`, token never written to workspace filesystem
- [x] 9.4 Implement branch/tag/commit SHA checkout: `isSHA()` regex detection, detached HEAD for SHA, `--branch` flag for branches
- [x] 9.5 Implement clone retry logic: 3 retries with exponential backoff (2s, 4s, 8s), return error on total failure
- [x] 9.6 Implement transparent credential injection for git push/pull tool operations: `InjectCredentialsForPush()` temporarily injects token into remote URL, restores original after operation
- [x] 9.7 Write integration tests for clone operations (public repo, branch checkout, error handling)

## 10. MCP Server Hosting

- [x] 10.1 Create Echo route group for `/api/v1/mcp/servers` in `apps/server-go/internal/agent/workspace/mcp_handler.go`
- [x] 10.2 Implement `POST /api/v1/mcp/servers` handler: register MCP server config (name, image, stdio_bridge, restart_policy, environment, volumes, resource_limits), create persistent container, establish stdio bridge
- [x] 10.3 Implement stdio-to-HTTP bridge: attach to container stdin/stdout, serialize JSON-RPC requests to stdin, read JSON-RPC responses from stdout, handle concurrent calls via serialization queue
- [x] 10.4 Implement `POST /api/v1/mcp/servers/:id/call` handler: accept MCP method and params, route through stdio bridge, return parsed response, enforce 30s timeout
- [x] 10.5 Implement `GET /api/v1/mcp/servers/:id` handler: return server status (running/stopped/restarting), uptime, restart count, last crash timestamp, resource usage
- [x] 10.6 Implement `GET /api/v1/mcp/servers` handler: list all registered MCP servers with status summary
- [x] 10.7 Implement `DELETE /api/v1/mcp/servers/:id` handler: stop container, remove stdio bridge, delete database record
- [x] 10.8 Implement `POST /api/v1/mcp/servers/:id/restart` handler: SIGTERM → wait 10s → new container from same config, reset crash counters
- [x] 10.9 Implement auto-restart on crash: detect container exit, restart within 5s, re-establish stdio bridge, log crash event with exit code and stderr
- [x] 10.10 Implement crash loop backoff: track crash count within 60s window, apply exponential backoff (5s, 15s, 45s, 2m, 5m), emit health warning
- [x] 10.11 Implement auto-start on Emergent boot: query `kb.agent_workspaces` for persistent MCP servers, start all in parallel, establish stdio bridges
- [x] 10.12 Implement graceful shutdown: on SIGTERM, send SIGTERM to all MCP containers, wait 30s, SIGKILL remaining
- [x] 10.13 Implement persistent volume mounts for MCP servers: create named volumes for specified paths, mount into container
- [x] 10.14 Register MCP server routes and add to fx module
- [x] 10.15 Write integration tests for MCP server lifecycle (register, call, restart, crash recovery)

## 11. Warm Pool Management

- [x] 11.1 Create `warm_pool.go` service for pre-booted container pool management
- [x] 11.2 Implement pool initialization on server start: create N containers (configurable, default 2) using default provider
- [x] 11.3 Implement pool assignment: match warm container provider to orchestrator selection, assign to workspace, update database
- [x] 11.4 Implement async pool replenishment: after assignment, create replacement container in background goroutine
- [x] 11.5 Implement pool size adjustment: support runtime configuration changes, create/destroy to match new size within 60s
- [x] 11.6 Add warm pool metrics: hit rate, miss rate, pool size, replenishment latency
- [x] 11.7 Write unit tests for warm pool logic (assignment, replenishment, sizing)

## 12. Workspace Persistence & Snapshots

- [x] 12.1 Implement sequential workspace attachment: validate no active session before allowing new attachment, reject concurrent access with clear error
- [x] 12.2 Implement agent session tracking: update `agent_session_id` on attachment, log session history for audit
- [x] 12.3 Implement `POST /api/v1/agent/workspaces/:id/snapshot` handler: create point-in-time snapshot via provider (block device clone for Firecracker, volume snapshot for gVisor), store snapshot ID in database
- [x] 12.4 Implement `POST /api/v1/agent/workspaces/from-snapshot` handler: create new workspace from snapshot ID, restore filesystem state
- [x] 12.5 Write integration tests for persistence (stop/resume preserves files, snapshot/restore)

## 13. TTL Cleanup & Resource Management

- [x] 13.1 Create background cleanup job: periodic scan (every 1 hour) for expired workspaces (`expires_at < NOW()`)
- [x] 13.2 Implement cleanup logic: skip persistent MCP servers (NULL `expires_at`), destroy expired workspaces, log cleanup actions
- [x] 13.3 Implement resource monitoring: track CPU/memory/disk per workspace via provider APIs
- [x] 13.4 Implement resource exhaustion alerts: warn when aggregate resource usage exceeds 80%
- [x] 13.5 Add feature flag `ENABLE_AGENT_WORKSPACES` (default: false) to gate all new endpoints
- [x] 13.6 Write tests for cleanup job (TTL expiry, MCP exemption)

## 14. Configuration & Docker Compose

- [x] 14.1 Add workspace configuration environment variables: `ENABLE_AGENT_WORKSPACES`, `WORKSPACE_MAX_CONCURRENT`, `WORKSPACE_WARM_POOL_SIZE`, `WORKSPACE_DEFAULT_TTL_DAYS`, `WORKSPACE_DEFAULT_PROVIDER`, `WORKSPACE_DEFAULT_CPU`, `WORKSPACE_DEFAULT_MEMORY`, `WORKSPACE_DEFAULT_DISK`, `E2B_API_KEY`, `GITHUB_APP_ENCRYPTION_KEY`
- [x] 14.2 Add `firecracker-manager` service to `docker-compose.yml` with KVM mount, data volume, and configuration
- [x] 14.3 Add gVisor runtime configuration documentation for Docker daemon (`/etc/docker/daemon.json`)
- [x] 14.4 Configure Docker networking for workspace container isolation
- [x] 14.5 Add persistent volumes to `docker-compose.yml`: `firecracker-data`, `workspace-volumes`

## 15. Licensing & Documentation

- [x] 15.1 Create `docs/licenses/AGENT_WORKSPACE.md` with Apache 2.0 attributions for Firecracker (AWS), E2B SDK (E2B Inc), gVisor (Google LLC)
- [x] 15.2 Add NOTICE file entries for all Apache 2.0 dependencies
- [x] 15.3 Create deployment runbook: `docs/agent-workspace/DEPLOYMENT.md` covering Firecracker setup (KVM requirements), gVisor installation, E2B configuration
- [x] 15.4 Document provider selection guide: when to use which provider, platform requirements, trade-offs
- [x] 15.5 Document MCP server hosting guide: how to register stdio-based MCP servers, stdio bridge configuration, troubleshooting

## 16. End-to-End Testing & Hardening

- [x] 16.1 Write E2E test: create workspace → clone repo → read file → edit file → run tests → commit → destroy
- [x] 16.2 Write E2E test: register MCP server → call method → crash → auto-restart → call again → verify response
- [x] 16.3 Write E2E test: provider fallback (mock Firecracker failure → verify gVisor fallback)
- [x] 16.4 Write E2E test: warm pool hit (create pool → request workspace → verify sub-150ms assignment)
- [x] 16.5 Write E2E test: TTL cleanup (create workspace with short TTL → wait → verify cleanup)
- [x] 16.6 Write E2E test: concurrent workspace limit (create max workspaces → verify rejection of next request)
- [x] 16.7 Load test: 50 concurrent workspace creation requests, verify stability and resource cleanup
- [x] 16.8 Security review: verify credentials never exposed in workspace, no container escape via tool API, resource limits enforced

## 17. GitHub App Integration

- [x] 17.1 Create Goose migration `00024_create_github_app_config.sql` for `core.github_app_config` table with columns: `id`, `app_id`, `app_slug`, `private_key_encrypted`, `webhook_secret_encrypted`, `client_id`, `client_secret_encrypted`, `installation_id`, `installation_org`, `owner_id`, `created_at`, `updated_at`
- [x] 17.2 Create Bun ORM model `GitHubAppConfig` in `apps/server-go/domain/githubapp/entity.go` (adapted from original path to match codebase conventions)
- [x] 17.3 Create GitHub App store with CRUD operations (`Create`, `Get`, `GetByAppID`, `Update`, `Delete`, `UpdateInstallation`)
- [x] 17.4 Implement AES-256-GCM encryption/decryption service for PEM and webhook secret storage (key from `GITHUB_APP_ENCRYPTION_KEY` env var)
- [x] 17.5 Implement GitHub App manifest generation: build manifest JSON with `name`, `url`, `hook_attributes`, `redirect_url`, `default_permissions` (`contents: write`), `default_events` (`installation`)
- [x] 17.6 Implement `POST /api/v1/settings/github/connect` handler: generate manifest, return redirect URL to `https://github.com/settings/apps/new?manifest=<encoded>`
- [x] 17.7 Implement `GET /api/v1/settings/github/callback` handler: exchange temporary code via `POST https://api.github.com/app-manifests/{code}/conversions`, encrypt and store credentials, return success
- [x] 17.8 Implement webhook handler for `installation.created` event: extract `installation_id` and `account.login` (org name), update `core.github_app_config`
- [x] 17.9 Implement installation access token generation service: JWT signing from app_id + PEM (RSA PKCS1/PKCS8), token exchange via `POST /app/installations/{id}/access_tokens`, 55-minute in-memory caching with auto-refresh
- [x] 17.10 Implement `GET /api/v1/settings/github` handler: return connection status (connected/disconnected), app name, org, connected by, connected at
- [x] 17.11 Implement `DELETE /api/v1/settings/github` handler: delete credentials from database
- [x] 17.12 Implement `POST /api/v1/settings/github/cli` handler: accept app_id, PEM, installation_id; validate by generating test token; encrypt and store
- [x] 17.13 Implement bot commit identity: `BotCommitIdentity()` returns `emergent-app[bot] <{app_id}+emergent-app[bot]@users.noreply.github.com>`, `DefaultCommitIdentity()` returns fallback
- [ ] 17.14 Create admin UI page: Settings > Integrations > GitHub with Connect/Disconnect buttons, connection status display, CLI instructions (SKIPPED — user opted to skip frontend task)
- [x] 17.15 Register GitHub App routes in Echo router and add to fx module
- [x] 17.16 Write unit tests: AES-256-GCM encryption/decryption roundtrip, wrong key detection, entity validation, bot identity, manifest URL generation (17 tests)
- [x] 17.17 Write E2E test: connect GitHub App → clone private repo → push commit → verify bot authorship

## 18. Agent Workspace Configuration

- [x] 18.1 Create Goose migration to add `workspace_config JSONB` column to agent types table (or create `kb.agent_type_workspace_configs` table if agent types don't exist yet)
- [x] 18.2 Define `AgentWorkspaceConfig` Go struct: `Enabled`, `RepoSource` (Type, URL, Branch), `Tools` ([]string), `ResourceLimits`, `CheckoutOnStart`, `BaseImage`, `SetupCommands` ([]string)
- [x] 18.3 Implement workspace config validation: valid tool names (bash, read, write, edit, glob, grep, git), valid repo_source types (task_context, fixed, none), valid resource limits
- [x] 18.4 Implement `GET /api/v1/agent-types/:id/workspace-config` handler: return workspace configuration for agent type
- [x] 18.5 Implement `PUT /api/v1/agent-types/:id/workspace-config` handler: validate and persist workspace configuration
- [x] 18.6 Implement auto-provisioning on session start: hook into agent session creation, read agent type's workspace config, create workspace via orchestrator if enabled
- [x] 18.7 Implement task context extraction: parse `repository_url`, `branch`, `pull_request_number`, `base_branch` from task metadata
- [x] 18.8 Implement repo source resolution: task_context → use task.context.repository_url; fixed → use config URL; none → empty workspace
- [x] 18.9 Implement tool restriction middleware: check agent type's allowed tools list before executing any workspace tool, return 403 for disallowed tools
- [x] 18.10 Implement setup command execution: run `setup_commands` sequentially after checkout, 5-minute per-command timeout, skip remaining on failure with warning
- [x] 18.11 Implement session status tracking: `provisioning` → `active` lifecycle, expose via `GET /api/v1/agent/sessions/:id` status field
- [x] 18.12 Implement default workspace config for new agent types: `enabled = false`, applied automatically when agent type is created without workspace config
- [x] 18.13 Write unit tests for config validation, repo source resolution, tool restriction
- [x] 18.14 Write integration tests: auto-provisioning on session start, task context binding, setup command execution
- [x] 18.15 Write E2E test: create agent type with workspace config → start session → verify workspace auto-provisioned with correct repo/branch/tools
