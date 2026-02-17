## Context

Emergent currently runs as a monolithic Go server in Docker, with AI agents operating within the same process context. This creates two critical limitations:

### Problem 1: Agent Workspace Isolation

- Agents execute within the main server process (unsafe for arbitrary code)
- No isolation between agent operations
- Code checkout requires manual git operations
- No persistent workspace state across agent sessions
- Agents lack rich tooling (file operations, shell access, git, etc.)

**Inspiration:** OpenCode agents have comprehensive tools (bash, read, write, edit, glob, grep) that enable autonomous work. Emergent agents need similar capabilities within isolated sandboxes.

### Problem 2: MCP Server Hosting

- **Current:** MCP servers accessed via HTTP (stateless, external hosting)
- **Gap:** Some MCPs are stdio-based servers requiring persistent processes
- **Need:** Isolated, long-running containers for MCP servers that start with Emergent server

**Two Distinct Use Cases:**

| Use Case               | Lifecycle         | Interface        | Examples                      |
| ---------------------- | ----------------- | ---------------- | ----------------------------- |
| **Agent Workspace**    | Ephemeral/session | Tool-based API   | Code execution, git, file ops |
| **MCP Server Hosting** | Persistent/daemon | stdio/HTTP proxy | Langfuse MCP, database MCP    |

**Constraints:**

- Must be self-hosted (no reliance on external managed services)
- Must integrate with existing Docker-based infrastructure
- Must maintain backward compatibility with existing agent APIs
- Resource limits on self-hosted hardware (CPU, memory, disk)
- Must support both ephemeral (agent) and persistent (MCP) containers

**Stakeholders:**

- AI agents requiring isolated execution environments with rich tooling
- MCP server developers needing persistent hosting infrastructure
- Developers needing faster agent development cycles
- Security team requiring strong isolation boundaries

## Goals / Non-Goals

**Goals:**

- **Agent Workspaces:** Provide sub-150ms workspace cold starts with OpenCode-inspired tool interface
- **MCP Hosting:** Enable persistent, isolated MCP server containers with stdio/HTTP bridging
- Enable persistent workspace state across agent sessions
- Support tri-provider architecture (Firecracker/E2B/gVisor, all Apache 2.0)
- Automatic code checkout from private repositories
- Self-hosted deployment within existing Docker infrastructure
- Tool-based agent interface (bash, file operations, search, git) - no raw shell exposure
- Lifecycle management for both ephemeral and persistent containers
- Resource monitoring and limits per workspace/MCP server
- Graceful degradation when providers are unavailable

**Non-Goals:**

- Multi-tenant workspace sharing (each agent gets isolated workspace)
- Real-time workspace collaboration between agents
- Workspace migration across physical hosts
- Support for non-Docker deployment models in v1
- GUI workspace management interface (API-only initially)
- Direct shell/SSH access (security risk - use tool-based API instead)

## Decisions

### 1. CRITICAL: Licensing Analysis - Drop Daytona, Use E2B + Firecracker

**Finding:** Daytona is licensed under **AGPL-3.0**, which is incompatible with future closed-source/enterprise plans.

**AGPL-3.0 Implications:**

- **Network Use Clause (Section 13):** If users interact with Emergent over a network (SaaS model), we MUST provide source code for entire application including Emergent server
- **Viral Effect:** AGPL "infects" the entire codebase if Daytona is integrated as a service
- **Enterprise Blocker:** Google and many enterprises ban AGPL internally due to compliance risks
- **Dual Licensing:** Daytona may offer commercial licenses, but adds vendor dependency and cost

**E2B Licensing:** E2B is **Apache 2.0** - fully permissive, allows commercial/closed-source use with no source code disclosure requirements.

**Decision:** **Remove Daytona entirely. Use E2B + direct Firecracker integration.**

**New Architecture:**

- **Primary:** E2B (Apache 2.0) for managed Firecracker sandboxes (~150ms startup)
- **Secondary:** Direct Firecracker integration (Apache 2.0) for self-hosted deployments with full control
- **Tertiary:** gVisor (Apache 2.0) for lightweight Docker-based isolation (fallback)

**Rationale:**

- **All Apache 2.0 stack** - safe for enterprise/closed-source
- **Distributable with Emergent** - no licensing conflicts
- **Better security** - Firecracker microVMs > Docker isolation
- **E2B proven at scale** - used by Fortune 100, Perplexity, Hugging Face
- **Self-hosting flexibility** - can bundle Firecracker binary directly

**Alternatives Considered:**

- **Daytona + commercial license:** Vendor lock-in, recurring costs, still requires legal review
- **AGPL compliance mode:** Requires open-sourcing entire Emergent codebase (kills enterprise strategy)
- **Pure Docker:** Weaker security, no AGPL issues but doesn't meet isolation requirements

### 2. Tri-Provider Architecture (E2B + Firecracker + gVisor)

**Decision:** Implement three Apache 2.0-licensed providers with runtime selection.

**Provider Matrix:**

| Provider    | License    | Isolation      | Startup | Persistence  | Best For                         |
| ----------- | ---------- | -------------- | ------- | ------------ | -------------------------------- |
| E2B         | Apache 2.0 | Firecracker VM | ~150ms  | Ephemeral\*  | Managed, enterprise-grade        |
| Firecracker | Apache 2.0 | MicroVM (KVM)  | <125ms  | Configurable | Self-hosted, full control        |
| gVisor      | Apache 2.0 | App kernel     | ~50ms   | Stateful     | Speed-optimized, lower isolation |

\*E2B persistence possible via filesystem snapshots (future enhancement)

**Selection Algorithm:**

```
if deployment_mode == "managed" and e2b_available:
    use E2B
elif security_level == "high" and firecracker_available:
    use Firecracker
elif gvisor_available:
    use gVisor
else:
    error "No providers available"
```

**Rationale:**

- All providers are Apache 2.0 (enterprise-safe)
- Flexibility for different deployment scenarios
- No vendor lock-in (can switch providers)
- Progressive fallback strategy

### 3. Firecracker Deployment Model

**Decision:** Deploy Firecracker directly on host (requires KVM), with Docker as orchestrator.

**Architecture:**

```yaml
# docker-compose.yml
firecracker-manager:
  image: emergent/firecracker-manager:latest
  privileged: true # Required for KVM access
  volumes:
    - /dev/kvm:/dev/kvm # Hardware virtualization
    - firecracker-data:/data # Workspace storage
  environment:
    - FIRECRACKER_MAX_VMS=10
    - FIRECRACKER_WARM_POOL_SIZE=2
```

**Firecracker Manager (Custom Go Service):**

- Manages Firecracker binary (Apache 2.0)
- Creates/destroys microVMs via Firecracker API
- Handles networking (TAP devices, iptables)
- Integrates with Emergent's workspace API

**Rationale:**

- Firecracker is Apache 2.0 - can bundle binary with Emergent
- KVM provides hardware-backed isolation (stronger than Docker)
- Single binary deployment (no external server dependencies)
- Full control over VM lifecycle and configuration

**Alternatives Considered:**

- **Kata Containers:** Heavier wrapper around Firecracker, less direct control
- **Cloud Hypervisor:** Similar to Firecracker but less mature
- **QEMU directly:** More complex, slower startup times

**Trade-offs:**

- Requires KVM support (Linux with virtualization enabled)
- Won't work on macOS/Windows dev machines (fallback to gVisor)
- More complex than Docker-only (but better isolation)

### 4. OpenCode-Inspired Tool Interface (NOT raw shell)

**Decision:** Agents interact with workspaces via structured tool API, NOT direct shell/SSH access.

**Tool Interface (inspired by OpenCode):**

```go
// Workspace tools exposed via REST/WebSocket API
type WorkspaceTools struct {
    Bash       func(command, workdir string) (stdout, stderr string, exitCode int)
    Read       func(filePath string, offset, limit int) (content string)
    Write      func(filePath, content string) error
    Edit       func(filePath, oldString, newString string) error
    Glob       func(pattern, path string) (files []string)
    Grep       func(pattern, path, include string) (matches []Match)
    GitClone   func(repoURL, branch, destPath string) error
    GitCommit  func(message string, files []string) error
    // ... other tools
}
```

**API Endpoints:**

```
POST /api/v1/agent/workspaces/:id/exec     # Execute bash command
POST /api/v1/agent/workspaces/:id/read     # Read file content
POST /api/v1/agent/workspaces/:id/write    # Write file
POST /api/v1/agent/workspaces/:id/edit     # Edit file (string replacement)
POST /api/v1/agent/workspaces/:id/glob     # Find files by pattern
POST /api/v1/agent/workspaces/:id/grep     # Search file contents
POST /api/v1/agent/workspaces/:id/git      # Git operations
```

**Example Agent Workflow:**

```typescript
// Agent wants to modify code in workspace
const workspaceId = await createWorkspace({
  repository_url: 'https://github.com/org/repo',
  branch: 'main',
});

// Read file
const content = await workspace.read(workspaceId, 'src/server.ts');

// Edit file (structured, safe)
await workspace.edit(workspaceId, 'src/server.ts', {
  oldString: 'const port = 3000',
  newString: 'const port = 8080',
});

// Run tests
const result = await workspace.bash(workspaceId, 'npm test', '/workspace');

// Commit changes
await workspace.git(workspaceId, {
  action: 'commit',
  message: 'Change port to 8080',
  files: ['src/server.ts'],
});
```

**Rationale:**

- **Security:** No raw shell prevents arbitrary code execution exploits
- **Auditability:** Every operation logged with structured data
- **Familiarity:** Matches OpenCode's proven tool interface
- **Type Safety:** Structured requests/responses (no parsing stdout)
- **Provider Agnostic:** Works identically across Firecracker/E2B/gVisor

**Alternatives Considered:**

- **SSH access:** Flexible but insecure, hard to audit, credential management burden
- **Web terminal (TTY):** Nice UX but complex (WebSocket, pty), security concerns
- **VSCode server:** Heavy, requires persistent container, resource-intensive

**Trade-offs:**

- Less flexible than raw shell (can't pipe commands, complex bash scripts)
- Requires implementing each tool endpoint (more API surface)
- Acceptable because agents primarily need file ops, git, and simple commands

### 5. MCP Server Hosting (Persistent Containers)

**Decision:** Separate container type for long-running MCP servers with stdio bridging.

**Architecture:**

```yaml
# MCP server as persistent container
mcp-langfuse:
  type: mcp_server # Different from agent_workspace
  lifecycle: persistent # Starts with Emergent, restarts on crash
  image: emergent/mcp-langfuse:latest
  provider: gvisor # Prefer gVisor for persistent (less resource overhead)
  stdio_bridge: true # Bridge stdio to HTTP/WebSocket
  restart_policy: always
  resource_limits:
    memory: '512M'
    cpu: '0.5'
```

**Stdio Bridge (for stdio-based MCPs):**

```
┌─────────────┐      HTTP/WS       ┌──────────────┐      stdio      ┌─────────────┐
│   Agent     │ ─────────────────> │ Emergent     │ ────────────────> │ MCP Server  │
│             │                     │ (Bridge)     │                   │ (Container) │
└─────────────┘                     └──────────────┘                   └─────────────┘
                                           │
                                           ├─ Attach to container
                                           ├─ Read stdout (MCP responses)
                                           └─ Write stdin (MCP requests)
```

**MCP Lifecycle API:**

```
POST   /api/v1/mcp/servers              # Register MCP server config
GET    /api/v1/mcp/servers/:id          # Get server status
DELETE /api/v1/mcp/servers/:id          # Stop server
POST   /api/v1/mcp/servers/:id/restart  # Restart server
POST   /api/v1/mcp/servers/:id/call     # Call MCP method (bridged to stdio)
```

**MCP vs. Workspace Comparison:**

| Feature          | Agent Workspace        | MCP Server Container |
| ---------------- | ---------------------- | -------------------- |
| Lifecycle        | Ephemeral (session)    | Persistent (daemon)  |
| Startup          | On-demand              | Server boot          |
| Interface        | Tool API (REST)        | stdio + HTTP bridge  |
| Provider         | Firecracker/E2B/gVisor | Prefer gVisor        |
| Resource Profile | 2 CPU, 4GB RAM         | 0.5 CPU, 512MB RAM   |
| Warm Pool        | Yes (2-3 pre-warmed)   | No (always running)  |
| Code Checkout    | Yes (git clone)        | No (image-based)     |

**Rationale:**

- **Separation of Concerns:** Workspaces = ephemeral compute, MCPs = persistent services
- **Resource Efficiency:** gVisor for MCPs (lighter than microVMs for long-running)
- **Stdio Compatibility:** Many MCPs use stdio (Langfuse, filesystem, etc.)
- **Lifecycle Management:** MCPs restart on crash, workspaces don't
- **Simplified Networking:** stdio bridge avoids exposing MCP ports

**Alternatives Considered:**

- **Same container type:** Complex lifecycle management (ephemeral vs persistent)
- **External MCP hosting:** Requires network setup, credential management
- **HTTP-only MCPs:** Doesn't support stdio-based MCPs (common pattern)

### 6. E2B Integration Strategy

**Decision:** Use E2B SDK (Apache 2.0) for managed sandbox option.

**Integration:**

```go
import "github.com/e2b-dev/e2b-go"

// Create E2B sandbox (agent workspace)
sandbox := e2b.NewSandbox(e2b.SandboxConfig{
    Template: "base",  // Or custom Emergent template
})

// Tool-based interface (NOT raw exec)
result := sandbox.Filesystem.Read("/workspace/README.md")
sandbox.Filesystem.Write("/workspace/output.txt", "data")
sandbox.Process.Start("npm test", "/workspace")
```

// Persist via snapshot (future)
snapshot := sandbox.Snapshot() // Save filesystem state
sandbox2 := e2b.FromSnapshot(snapshot) // Restore later

```

**Deployment Modes:**

1. **Managed (default for enterprise):** Use E2B cloud infrastructure
2. **Self-hosted (future):** Deploy E2B's Firecracker orchestration locally

**Rationale:**

- Apache 2.0 SDK - safe to bundle
- Battle-tested (Fortune 100 companies)
- Option for managed service (reduces ops burden)
- Can self-host if needed (same Firecracker tech)

**Trade-offs:**

- Managed mode requires external dependency (E2B cloud)
- Self-hosted E2B setup is complex (prefer direct Firecracker)

### 7. Workspace Persistence Model

**Decision:** Provider-specific persistence strategies.

**Firecracker:**

- Persistent block device per VM: `/dev/vda` backed by host file
- Copy-on-write for warm pool (fast cloning)
- 30-day TTL with explicit cleanup

**E2B:**

- Ephemeral by default (VM destroyed after use)
- Optional snapshots for persistence (save/restore filesystem)
- Snapshot storage in S3-compatible backend (v2 feature)

**gVisor:**

- Named Docker volumes (same as current Docker model)
- Stateful by design

**Rationale:**

- Matches each provider's capabilities
- Firecracker provides best persistence + security balance
- E2B snapshots enable stateful workflows when needed

### 8. Code Checkout Strategy (GitHub App, not PAT)

**Decision:** Emergent server pre-clones repositories using **GitHub App installation tokens**, not Personal Access Tokens.

**GitHub App Manifest Flow (Zero-Config Setup):**

```

1. Admin clicks "Connect GitHub" in Emergent admin UI
2. Emergent redirects to GitHub with a manifest (app name, permissions, webhook URL)
3. GitHub creates the App and redirects back with a temporary code
4. Emergent exchanges code for: app_id, private_key (PEM), webhook_secret
5. Admin installs the App on their org/repos → installation_id returned via webhook
6. Credentials stored encrypted in core.github_app_config

````

**Token Generation (Per-Operation):**

```go
// Generate short-lived installation access token (1-hour expiry)
func (s *GitHubAppService) GetInstallationToken(ctx context.Context, installationID int64) (string, error) {
    jwt := s.generateJWT(s.appID, s.privateKey) // 10-min JWT from PEM
    token, _, err := s.client.Apps.CreateInstallationToken(ctx, installationID, nil)
    return token.GetToken(), err // Short-lived, auto-expires
}
````

**Clone Flow:**

```
1. Agent requests workspace with repo URL + branch
2. Server looks up installation_id for the repo's org in core.github_app_config
3. Server generates short-lived installation token (1-hour expiry)
4. Server executes: git clone https://x-access-token:${TOKEN}@github.com/org/repo.git
5. Token expires automatically — never persisted to workspace
6. Agent executes commands via tool API
```

**CLI Fallback (Air-Gapped / Self-Hosted):**

```bash
# For environments without browser access
emergent github setup --app-id 12345 --private-key-file ./app.pem --installation-id 67890
# Stores credentials encrypted in database via API
```

**Database Table:**

```sql
CREATE TABLE core.github_app_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id BIGINT NOT NULL,
    app_slug TEXT NOT NULL,
    private_key_encrypted BYTEA NOT NULL,  -- AES-256-GCM encrypted PEM
    webhook_secret_encrypted BYTEA NOT NULL,
    installation_id BIGINT,  -- NULL until app is installed
    installation_org TEXT,   -- GitHub org name
    owner_id UUID REFERENCES core.user_profiles(id),  -- Who connected it
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Rationale:**

- **No manual PEM files** — manifest flow handles everything via redirect
- **Short-lived tokens** — 1-hour expiry vs. long-lived PATs (90-day or indefinite)
- **Granular permissions** — App requests only `contents:read`, `contents:write` on selected repos
- **Org-level control** — Org admins control which repos the App can access
- **Audit trail** — GitHub logs all App token usage per-installation
- **Bot identity** — Commits authored as `emergent-app[bot]` (not a human user)
- **CLI fallback** — Air-gapped setups can paste credentials manually

**Alternatives Considered:**

- **GitHub PAT:** Simpler but less secure (long-lived, broad scope, tied to user account)
- **OAuth App:** Requires user-level auth, not suitable for server-to-server operations
- **Deploy Keys:** Per-repo SSH keys, doesn't scale, read-only by default

**Trade-offs:**

- More complex initial setup than PAT (manifest flow + installation)
- Requires GitHub connectivity for token refresh (tokens cached for ~55 minutes)
- App must be installed on each org (one-time per org)

### 9. Database Schema for Workspace Tracking

**Decision:** Add `kb.agent_workspaces` table with workspace metadata.

**Schema:**

```sql
CREATE TABLE kb.agent_workspaces (
    id UUID PRIMARY KEY,
    agent_session_id UUID REFERENCES kb.agent_sessions(id),
    container_type TEXT NOT NULL,  -- 'agent_workspace' or 'mcp_server'
    provider TEXT NOT NULL,  -- 'e2b', 'firecracker', or 'gvisor'
    provider_workspace_id TEXT NOT NULL,  -- Provider's internal ID
    repository_url TEXT,
    branch TEXT,
    deployment_mode TEXT NOT NULL,  -- 'managed' or 'self-hosted'
    lifecycle TEXT NOT NULL,  -- 'ephemeral' or 'persistent'
    status TEXT NOT NULL,  -- 'creating', 'ready', 'stopping', 'stopped'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,  -- TTL for auto-cleanup (NULL for persistent MCPs)
    resource_limits JSONB,  -- {cpu: "2", memory: "4G", disk: "10G"}
    snapshot_id TEXT,  -- For E2B snapshot-based persistence
    mcp_config JSONB,  -- For MCP servers: {stdio_bridge: true, restart_policy: "always"}
    metadata JSONB  -- Provider-specific data
);

-- Index for finding MCP servers that should be running
CREATE INDEX idx_workspaces_persistent ON kb.agent_workspaces(container_type, lifecycle, status)
    WHERE container_type = 'mcp_server' AND lifecycle = 'persistent';
```

**Rationale:**

- Single table for both workspaces and MCPs (shared lifecycle logic)
- `container_type` distinguishes use case
- `lifecycle` controls ephemeral vs. persistent behavior
- `mcp_config` stores stdio bridge and restart configuration
- TTL only applies to ephemeral workspaces (NULL for MCPs)

### 10. API Design

**Decision:** Dual API design - tool-based for workspaces, stdio bridge for MCPs.

**Agent Workspace Tool Endpoints (OpenCode-inspired):**

```
POST   /api/v1/agent/workspaces                  Create workspace
GET    /api/v1/agent/workspaces/:id              Get workspace status
DELETE /api/v1/agent/workspaces/:id              Destroy workspace
POST   /api/v1/agent/workspaces/:id/bash         Execute bash command
POST   /api/v1/agent/workspaces/:id/read         Read file content
POST   /api/v1/agent/workspaces/:id/write        Write file
POST   /api/v1/agent/workspaces/:id/edit         Edit file (string replace)
POST   /api/v1/agent/workspaces/:id/glob         Find files by pattern
POST   /api/v1/agent/workspaces/:id/grep         Search file contents
POST   /api/v1/agent/workspaces/:id/git          Git operations
GET    /api/v1/agent/workspaces                  List workspaces (with filters)
```

**MCP Server Endpoints:**

```
POST   /api/v1/mcp/servers                       Register MCP server
GET    /api/v1/mcp/servers/:id                   Get server status
DELETE /api/v1/mcp/servers/:id                   Stop server
POST   /api/v1/mcp/servers/:id/restart           Restart server
POST   /api/v1/mcp/servers/:id/call              Call MCP method (stdio bridge)
GET    /api/v1/mcp/servers                       List MCP servers
```

**Workspace Creation Request:**

```json
{
  "container_type": "agent_workspace",
  "repository_url": "https://github.com/org/repo",
  "branch": "main",
  "provider": "firecracker", // or "e2b", "gvisor", "auto"
  "deployment_mode": "self-hosted", // or "managed"
  "resource_limits": {
    "cpu": "2",
    "memory": "4G",
    "disk": "10G"
  },
  "warm_start": true
}
```

**MCP Server Registration Request:**

```json
{
  "container_type": "mcp_server",
  "name": "langfuse-mcp",
  "image": "emergent/mcp-langfuse:latest",
  "provider": "gvisor", // Prefer gVisor for persistent
  "lifecycle": "persistent",
  "stdio_bridge": true,
  "restart_policy": "always",
  "resource_limits": {
    "cpu": "0.5",
    "memory": "512M"
  },
  "environment": {
    "LANGFUSE_API_KEY": "..."
  }
}
```

**Tool Call Example (Bash):**

```json
POST /api/v1/agent/workspaces/abc-123/bash
{
  "command": "npm test",
  "workdir": "/workspace",
  "timeout_ms": 30000
}

Response:
{
  "stdout": "✓ All tests passed\n",
  "stderr": "",
  "exit_code": 0,
  "duration_ms": 1234
}
```

**Tool Call Example (Edit):**

```json
POST /api/v1/agent/workspaces/abc-123/edit
{
  "file_path": "/workspace/src/server.ts",
  "old_string": "const port = 3000",
  "new_string": "const port = 8080"
}

Response:
{
  "success": true,
  "lines_changed": 1
}
```

**MCP Call Example (stdio bridge):**

```json
POST /api/v1/mcp/servers/langfuse-001/call
{
  "method": "tools/call",
  "params": {
    "name": "get-traces",
    "arguments": {
      "project_id": "abc"
    }
  }
}

Response:
{
  "result": { ... },  // MCP response parsed from stdout
  "error": null
}
```

**Rationale:**

- **Tool-based interface:** Structured, auditable, type-safe
- **Separate MCP API:** Different lifecycle and interface needs
- **stdio bridge:** Transparent proxying for stdio-based MCPs
- **Provider selection:** Explicit or automatic
- **Synchronous tools:** Block until complete (or timeout)

**Alternatives Considered:**

- **WebSocket for streaming:** Over-engineered for v1
- **gRPC:** Better performance but harder to debug
- **Async-only creation:** Requires polling loop in agent code

### 11. Licensing Compliance Documentation

**Decision:** Document all licenses in `docs/licenses/AGENT_WORKSPACE.md`.

**Required Attributions:**

```markdown
# Agent Workspace Infrastructure Licenses

## Firecracker (Apache 2.0)

- Copyright: Amazon Web Services
- License: https://github.com/firecracker-microvm/firecracker/blob/main/LICENSE
- Usage: MicroVM isolation for self-hosted workspaces

## E2B SDK (Apache 2.0)

- Copyright: E2B Inc.
- License: https://github.com/e2b-dev/e2b/blob/main/LICENSE
- Usage: Managed sandbox infrastructure

## gVisor (Apache 2.0)

- Copyright: Google LLC
- License: https://github.com/google/gvisor/blob/master/LICENSE
- Usage: Application kernel isolation fallback
```

**Distribution:**

- All components can be distributed with Emergent (Apache 2.0 permits)
- Include LICENSE files in `licenses/` directory
- Add NOTICE file crediting projects

**Enterprise Safety:**

- ✅ All Apache 2.0 - no source code disclosure requirements
- ✅ Safe for closed-source enterprise versions
- ✅ No viral licensing effects
- ✅ Can modify and distribute without upstream contributions

### 12. Agent Workspace Configuration (Declarative Binding)

**Decision:** Agent types declaratively define workspace requirements. Workspaces are auto-provisioned on session start.

**Agent Type Definition:**

```go
// Defined in agent type configuration (database or YAML)
type AgentWorkspaceConfig struct {
    Enabled         bool              `json:"enabled"`
    RepoSource      RepoSource        `json:"repo_source"`       // Where to get the repo
    Tools           []string          `json:"tools"`              // Allowed tools: ["bash","read","write","edit","glob","grep","git"]
    ResourceLimits  ResourceLimits    `json:"resource_limits"`
    CheckoutOnStart bool              `json:"checkout_on_start"`  // Auto-clone on session start
    BaseImage       string            `json:"base_image"`         // Container image (default: emergent/workspace:latest)
    SetupCommands   []string          `json:"setup_commands"`     // Run after checkout (e.g., "npm install")
}

type RepoSource struct {
    Type   string `json:"type"`   // "task_context" | "fixed" | "none"
    URL    string `json:"url"`    // Only for "fixed" type
    Branch string `json:"branch"` // Default branch (overridden by task context)
}
```

**Task Context Binding:**

```json
// When a task is assigned to an agent, it carries context:
{
  "task_id": "task-123",
  "agent_type": "code-review",
  "context": {
    "repository_url": "https://github.com/org/repo",
    "branch": "feature/auth",
    "pull_request_number": 42,
    "base_branch": "main"
  }
}
```

**Auto-Provisioning Flow:**

```
1. Task assigned to agent with type "code-review"
2. System reads agent type's workspace config:
   - repo_source.type = "task_context" → use task.context.repository_url
   - checkout_on_start = true → clone after container creation
   - tools = ["bash","read","edit","grep","git"] → only these tools available
3. System auto-provisions workspace (no agent API call needed):
   a. Select provider via orchestrator
   b. Create container with resource limits from agent type
   c. Clone repo at branch from task context
   d. Run setup_commands (e.g., "npm install")
   e. Attach workspace to agent session
4. Agent receives workspace_id in session metadata
5. Agent uses tool API immediately — workspace is ready
```

**Workspace Templates (Presets):**

```yaml
# Example agent type configurations
code-review:
  workspace:
    enabled: true
    repo_source: { type: 'task_context' }
    checkout_on_start: true
    tools: ['bash', 'read', 'edit', 'grep', 'git']
    resource_limits: { cpu: '1', memory: '2G', disk: '5G' }
    setup_commands: ['npm install --frozen-lockfile']

deployment:
  workspace:
    enabled: true
    repo_source: { type: 'task_context' }
    checkout_on_start: true
    tools: ['bash', 'read', 'glob', 'grep']
    resource_limits: { cpu: '2', memory: '4G', disk: '10G' }

research:
  workspace:
    enabled: false # No workspace needed — uses web search, not code
```

**Rationale:**

- **No manual provisioning** — agents don't need to call `POST /workspaces`
- **Principle of least privilege** — agent type defines which tools are available
- **Task-driven context** — repo/branch comes from task metadata (PR, issue, etc.)
- **Consistent environments** — same agent type always gets same workspace config
- **Separation of concerns** — admin configures agent types, agents just work

**Alternatives Considered:**

- **Agent self-service:** Agent calls API to create workspace (more flexible, but loses declarative control)
- **Hardcoded configs:** Workspace config in code (not user-configurable)
- **Per-session override:** Agent can override workspace config per-session (too complex for v1)

**Trade-offs:**

- Less flexibility than self-service (agent can't customize workspace per-task)
- Agent type configuration must be maintained (admin responsibility)
- Setup commands add startup latency (mitigated by warm pools with pre-run setup)

### 13. GitHub App Admin UI Design

**Decision:** Admin settings page with one-click "Connect GitHub" and status display.

**Admin UI Flow:**

```
Settings → Integrations → GitHub

┌──────────────────────────────────────────────────────┐
│ GitHub Integration                                   │
│                                                      │
│ Status: ● Connected                                  │
│ App: emergent-dev (ID: 12345)                        │
│ Organization: my-org                                 │
│ Repositories: 15 accessible                          │
│ Connected by: admin@company.com on 2025-01-15       │
│                                                      │
│ [Reconnect]  [Disconnect]                           │
└──────────────────────────────────────────────────────┘

--- OR (if not connected) ---

┌──────────────────────────────────────────────────────┐
│ GitHub Integration                                   │
│                                                      │
│ Status: ○ Not Connected                              │
│                                                      │
│ Connect your GitHub account to enable:               │
│ • Private repository cloning in agent workspaces     │
│ • Automatic PR creation and code pushing             │
│ • Bot-authored commits (emergent-app[bot])           │
│                                                      │
│ [Connect GitHub]                                     │
│                                                      │
│ Self-hosted? Use CLI: emergent github setup           │
└──────────────────────────────────────────────────────┘
```

**API Endpoints:**

```
GET    /api/v1/settings/github          # Get connection status
POST   /api/v1/settings/github/connect  # Start manifest flow (returns redirect URL)
GET    /api/v1/settings/github/callback # Handle GitHub redirect (exchanges code for credentials)
DELETE /api/v1/settings/github          # Disconnect (revoke tokens, delete config)
POST   /api/v1/settings/github/cli      # CLI setup (accepts app_id, PEM, installation_id)
```

**Rationale:**

- **Familiar UX** — Same pattern as Vercel, Railway, Render ("Connect GitHub" button)
- **Zero-config** — No manual PEM copying, no env vars, no restart needed
- **Status visibility** — Admin can see connection health at a glance
- **CLI fallback** — Supports air-gapped deployments where browser isn't available

## Risks / Trade-offs

### Risk: Licensing Violation (MITIGATED - Daytona Removed)

**Original Risk:** AGPL-3.0 contamination from Daytona would require open-sourcing Emergent.

**Mitigation:** **ELIMINATED** - Switched to all-Apache 2.0 stack. No viral license risk.

### Risk: Firecracker Requires KVM (Linux-only)

### Risk: Firecracker Requires KVM (Linux-only)

**Description:** Firecracker requires `/dev/kvm` (Linux with hardware virtualization). Won't work on macOS/Windows dev machines.

**Mitigation:**

- Fallback to gVisor on non-Linux platforms (automatic detection)
- Document system requirements (Linux + KVM) for Firecracker provider
- Docker Desktop on Mac/Windows uses gVisor automatically
- Production deployments typically Linux (self-hosted VMs)

### Risk: E2B Managed Service Dependency

**Description:** Using E2B managed mode creates external dependency and potential vendor lock-in.

**Mitigation:**

- Offer self-hosted Firecracker as primary (no external dependencies)
- E2B is optional enhancement for managed deployments
- Can self-host E2B's orchestrator (Firecracker underneath)
- gVisor provides pure Docker fallback (no external services)

### Risk: Resource Exhaustion

**Description:** Agents create too many workspaces or consume excessive CPU/memory.

**Mitigation:**

- Hard limit: 10 concurrent workspaces per Emergent instance (configurable)
- Resource limits enforced via provider (Firecracker cgroups, E2B limits, gVisor Docker)
- 30-day TTL with aggressive cleanup of stale workspaces
- Monitoring alerts for resource usage >80%
- Cost controls for E2B managed mode (workspace quotas)

### Risk: Firecracker Manager Complexity

**Description:** Custom Firecracker manager service adds development and maintenance burden.

**Mitigation:**

- Use existing Firecracker Go SDK: `github.com/firecracker-microvm/firecracker-go-sdk`
- Reference implementations from E2B and Kata Containers
- Start with minimal feature set (create/destroy/exec only)
- Thorough integration testing with Firecracker API
- Consider E2B's self-hosted orchestrator in v2 (reduce custom code)

### Risk: Provider Downtime

**Description:** Provider becomes unavailable, blocking all agent operations.

**Mitigation:**

- Multi-provider fallback chain: Firecracker → E2B → gVisor
- Health check endpoints for each provider
- Workspace state persisted in database (can reconnect after restart)
- Graceful degradation (slower provider better than failure)
- Alert on provider failures

### Risk: Code Checkout Failures

**Description:** Private repository access fails due to invalid credentials, expired tokens, or network issues.

**Mitigation:**

- GitHub App installation tokens auto-generated and cached (~55 min refresh cycle)
- Token generation failure falls back to re-reading PEM and regenerating JWT
- Retry logic with exponential backoff (3 attempts)
- Clear error messages returned to agent (don't expose tokens)
- Fallback: workspace without code (agent can clone manually if needed)
- Admin UI shows GitHub connection health status

### Risk: Warm Pool Efficiency

**Description:** Warm pool sizing is challenging - too small causes cold starts, too large wastes resources.

**Mitigation:**

- Start with small pool (2 workspaces) and monitor hit rate
- Dynamic pool sizing based on demand patterns (v2 feature)
- Per-repository warm pools for frequently used repos (Emergent main)
- Graceful degradation (150ms start better than failure)

### Trade-off: Persistence vs. Ephemeral E2B

**Description:** E2B is ephemeral by default, doesn't match Firecracker's native persistence.

**Decision:**

- Accept ephemeral E2B for v1 (simpler implementation)
- v2: Implement snapshot-based persistence (E2B supports this)
- For persistence needs, prefer Firecracker provider
- Document limitation in API and provider selection guide

### Trade-off: Self-Hosted Complexity vs. Managed Simplicity

**Description:** Firecracker self-hosting requires KVM, networking, and storage configuration.

**Decision:**

- Provide clear documentation and reference docker-compose.yml
- Offer E2B managed option for users who prefer simplicity
- gVisor as "works everywhere" fallback (even on macOS/Windows)
- Future: Emergent Cloud with managed Firecracker (separate product)

### Trade-off: All-Apache Stack vs. Performance

**Description:** Dropped Daytona (claimed sub-90ms) for Firecracker (~125ms) and E2B (~150ms).

**Decision:**

- **Accept slower startup** for licensing safety and better isolation
- 125-150ms is still "sub-second" and acceptable for agent workflows
- Warm pools reduce frequency of cold starts
- Legal/enterprise safety > 50ms performance difference

**Rationale:**

- AGPL risk elimination is non-negotiable for enterprise strategy
- MicroVM isolation is inherently better than Docker (worth the tradeoff)
- User won't notice 50-100ms difference in agent workflows

## Migration Plan

### Phase 1: Infrastructure Setup (Week 1-2)

1. **Firecracker Integration**

   - Add `firecracker-manager` service to `docker-compose.yml`
   - Implement Go manager using `firecracker-go-sdk`
   - Test KVM access and basic VM lifecycle
   - Create database migration for `kb.agent_workspaces`

2. **gVisor Fallback**

   - Add gVisor runtime to Docker configuration
   - Test container creation with gVisor
   - Validate macOS/Windows fallback behavior

3. **E2B SDK Integration**
   - Add `e2b-go` dependency
   - Test managed sandbox creation
   - Configure API keys and quotas

### Phase 2: Core API Implementation (Week 3)

1. Implement `apps/server-go/internal/agent/workspace/` package
2. Create provider interface and three implementations:
   - `firecracker_provider.go`
   - `e2b_provider.go`
   - `gvisor_provider.go`
3. Add workspace orchestrator with provider selection logic
4. Implement REST endpoints (create, get, delete, exec)
5. Add database persistence layer

### Phase 3: Code Checkout & GitHub App Integration (Week 4)

1. Implement GitHub App manifest flow (admin UI + backend)
2. Create `core.github_app_config` migration and encrypted storage
3. Implement installation token generation service
4. Add git clone logic using GitHub App tokens
5. CLI setup command for air-gapped deployments
6. Integration tests for all three providers

### Phase 4: Agent Workspace Config & Production Hardening (Week 5)

1. Implement agent type workspace configuration schema
2. Auto-provisioning on session start
3. Task context binding (repo/branch from task metadata)
4. Resource monitoring and alerts
5. TTL-based cleanup job
6. Licensing documentation and attribution

### Phase 5: Testing & Rollout (Week 6)

1. Load testing (50 concurrent workspace creates)
2. E2E test suite for full workflow
3. Deployment runbook
4. Feature flag rollout (default: off)

### Rollback Strategy

- Feature flag `ENABLE_AGENT_WORKSPACES=false` disables new endpoints
- Database migration is additive (no data loss on rollback)
- Firecracker manager can be stopped without affecting main server
- All providers are optional (can disable individually)
- Existing agent APIs unchanged (backward compatible)

## Open Questions

1. **Firecracker SDK Maturity:** Is `firecracker-go-sdk` production-ready? _(Action: Review GitHub issues and adoption)_

2. **E2B Self-Hosting:** What's the effort to self-host E2B's Firecracker orchestrator vs. building our own? _(Action: Contact E2B team for self-hosted docs)_

3. **Workspace Networking:** Should workspaces have internet access? Isolated network with proxy? _(Decision needed: Security vs. functionality)_

4. **Multi-repo Workspaces:** Should a single workspace support cloning multiple repositories? _(Future enhancement, not v1)_

5. **Snapshot Storage:** Where to store E2B snapshots for persistence? S3? Database? _(v2 feature, defer decision)_

6. **Agent Authentication:** How does agent prove ownership of workspace? JWT? Session tokens? _(Action: Security review)_

7. **Cross-platform Testing:** How to test Firecracker locally without KVM? _(Answer: Use gVisor fallback for dev machines)_

8. **License Audit:** Final legal review of all Apache 2.0 licenses and attribution requirements. _(Action: Legal team sign-off before v1 release)_

9. **GitHub App Permissions:** What minimum permission set is needed? `contents:read+write` sufficient? Do we need `pull_requests`, `issues`? _(Action: Test with minimal permissions)_

10. **Workspace Config Storage:** Agent type workspace configs in database (dynamic) or YAML files (static)? _(Decision: Database for v1, with YAML import/export for version control)_

```

```
