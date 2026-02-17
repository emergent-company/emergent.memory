## Why

AI agents in Emergent currently lack isolated workspaces to safely execute code, run scripts, and perform operations. Additionally, Emergent needs infrastructure to host persistent MCP servers (stdio-based) that currently must be externally managed. This creates two critical gaps:

1. **Agent Capabilities:** No isolated execution environment with rich tooling (file operations, git, bash)
2. **MCP Hosting:** No way to run persistent, stdio-based MCP servers alongside Emergent

We need a fast, stateful, self-hosted sandbox infrastructure that enables:

- Agents to work in isolated workspaces with OpenCode-style tools (not raw shell)
- Persistent MCP server hosting with stdio bridging
- Sub-second startup times with proper isolation
- All Apache 2.0 licensed components (safe for enterprise/closed-source)

## What Changes

- Add **E2B + Firecracker + gVisor** agent workspace infrastructure (all Apache 2.0 licensed)
- **Critical:** Drop Daytona due to AGPL-3.0 license incompatibility with enterprise/closed-source plans
- Implement tri-provider architecture with automatic selection based on deployment mode and platform
- Create workspace lifecycle management (create, persist, attach, destroy)
- Enable automatic code checkout into agent workspaces from private repositories
- Support persistent workspace state across agent sessions (Firecracker and gVisor)
- Provide warm pool technology for sub-150ms cold starts
- Deploy self-hosted Firecracker manager for microVM isolation
- Support E2B managed mode as optional enhancement
- Add workspace monitoring and resource management
- Replace PAT-based repository auth with **GitHub App manifest flow** (one-click setup)
- Add **declarative agent-workspace configuration** (auto-provisioning on session start)
- **All components Apache 2.0 - safe for enterprise distribution**

## Capabilities

### New Capabilities

- `agent-sandbox-lifecycle`: Workspace creation, assignment, persistence, and cleanup. Manages the full lifecycle of isolated agent workspaces AND persistent MCP server containers. Includes warm pools, state snapshots, resource limits, and separate lifecycle management for ephemeral (workspaces) vs persistent (MCPs).

- `agent-workspace-persistence`: Stateful workspace management across agent sessions. Enables agents to resume work where previous agents left off, maintaining file systems, running processes, and installed dependencies. Handles both workspace persistence and MCP server state.

- `workspace-tool-interface`: OpenCode-inspired tool API for agent-workspace interaction. Provides structured tools (bash, read, write, edit, glob, grep, git) instead of raw shell access. Ensures security, auditability, and type safety. No SSH/TTY exposure.

- `mcp-server-hosting`: Infrastructure for hosting persistent, stdio-based MCP servers in isolated containers. Includes stdio-to-HTTP bridging, automatic restart on crash, lifecycle management (start with Emergent, restart on failure), and resource controls.

- `code-checkout-automation`: Automated repository cloning and code synchronization into agent workspaces. Handles private repository authentication, branch selection, and keeping code up-to-date without manual intervention. Not applicable to MCP servers (image-based).

- `firecracker-integration`: Direct Firecracker microVM integration for self-hosted deployments. Manages Firecracker binary (Apache 2.0), creates/destroys microVMs via Firecracker API, handles KVM access and networking. Provides sub-125ms startup with hardware-backed isolation.

- `e2b-integration`: Integration with E2B Firecracker-based sandboxes (Apache 2.0) for managed deployments. Offers enterprise-grade isolation (~150ms startup) with optional self-hosting support. Used by Fortune 100 companies.

- `gvisor-integration`: Google gVisor application kernel integration (Apache 2.0) for lightweight, cross-platform isolation. Provides Docker-compatible sandboxing that works on macOS/Windows dev machines where Firecracker requires KVM.

- `workspace-orchestration`: Multi-provider sandbox orchestration that selects appropriate backend (Firecracker vs E2B vs gVisor) based on deployment mode (managed/self-hosted), platform capabilities (KVM availability), and performance requirements. Includes fallback chain for high availability.

- `github-app-integration`: GitHub App-based authentication for private repository access. Replaces PAT-based auth with a GitHub App manifest flow — admin clicks "Connect GitHub" and credentials (app_id, private_key, installation_id) are auto-created via redirect callback. Encrypted credential storage in `core.github_app_config`. Short-lived installation access tokens generated per-operation. CLI fallback (`emergent github setup`) for air-gapped self-hosted deployments. Commits authored as GitHub App bot identity.

- `agent-workspace-config`: Declarative agent-to-workspace binding via agent type definitions. Each agent type declares workspace requirements (repository source, tools needed, resource limits, `checkout_on_start`). Task context provides runtime binding (which repo/branch, e.g., from PR metadata). The system auto-provisions the workspace on session start — agents don't manually request workspaces. Supports workspace templates for common configurations (code review agent, deployment agent, etc.).

### Modified Capabilities

None - this is a new infrastructure capability with no changes to existing specs.

## Impact

**Backend (Go Server)**

- New `apps/server-go/internal/agent/workspace/` package for workspace management
- New API endpoints: `POST /api/v1/agent/workspaces`, `GET /api/v1/agent/workspaces/:id`, `DELETE /api/v1/agent/workspaces/:id`, `POST /api/v1/agent/workspaces/:id/exec`
- New database schema in `kb` schema for tracking workspace state, agent assignments, and resource usage
- New `core.github_app_config` table for encrypted GitHub App credential storage

**Infrastructure**

- **Firecracker Manager:** Custom Go service to manage Firecracker microVMs (requires KVM)
- **gVisor Runtime:** Docker runtime configuration for cross-platform support
- **E2B SDK Integration:** Optional managed service integration
- Network configuration for isolated agent workspaces
- Storage volumes for persistent workspace data
- No Docker-in-Docker (removed due to licensing)

**Dependencies (All Apache 2.0)**

- **Firecracker** (AWS, Apache 2.0) - Primary self-hosted provider
- **E2B SDK** (E2B Inc, Apache 2.0) - Managed/optional provider
- **gVisor** (Google, Apache 2.0) - Cross-platform fallback
- **firecracker-go-sdk** (Apache 2.0) - Go bindings for Firecracker
- KVM access for Firecracker (`/dev/kvm` on Linux)

**Licensing & Distribution**

- ✅ **All components Apache 2.0** - Safe for enterprise/closed-source
- ✅ **Can distribute with Emergent** - No viral licensing
- ✅ **Enterprise-ready** - No AGPL contamination risk
- 📄 Requires attribution in `docs/licenses/AGENT_WORKSPACE.md`

**Security Considerations**

- Firecracker microVMs provide hardware-backed isolation (stronger than Docker)
- Resource limits per workspace (CPU, memory, disk)
- Network isolation policies for agent workspaces
- Private repository credential management via GitHub App (short-lived installation tokens, encrypted storage, no PATs)
- Agent workspace auto-provisioning scoped by agent type definitions (principle of least privilege)
