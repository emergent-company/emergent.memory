## Context

The `cli-assistant-agent` was introduced as a read-only information assistant — it could answer questions about the CLI and Memory platform, but could not take actions. The MCP tool system already has 63 registered tools covering full CRUD for graph entities, agent definitions, agents, schemas, and MCP servers. The only missing piece is a `create_project` MCP tool (project creation has only existed as a REST endpoint).

Auth context already flows end-to-end through the agent executor pipeline: `projectID` and `orgID` are injected into the pipeline context via `ContextWithProjectID` / `ContextWithOrgID`, and every tool call inherits the calling user's permissions via RLS. There is nothing to wire differently — tools just need to be added to the agent's whitelist.

## Goals / Non-Goals

**Goals:**
- Add `create_project` MCP tool so the agent can create projects on behalf of the user
- Expand `EnsureCliAssistantAgent` tool whitelist with write tools across all major resource types
- Update the system prompt to reflect action-taking capability with appropriate guardrails
- Add e2e tests that verify the agent actually executes write actions (not just gives guidance)

**Non-Goals:**
- Adding `delete_project` — too destructive for an assistant to perform without a dedicated confirmation flow
- User-facing confirmation dialogs / approval flows — out of scope; the agent's system prompt will carry guardrail instructions
- Changing the MCP auth model — existing per-project RLS is sufficient
- Frontend changes

## Decisions

### 1. `create_project` belongs in `mcp/service.go`, not a new file

The existing `mcp/service.go` already handles both `GetToolDefinitions()` and `ExecuteTool()` for all built-in tools. Adding `create_project` there keeps the pattern consistent. The service has access to the project store via its dependencies; adding an org store dependency is the only wiring change.

Alternative considered: add project tools in a new `mcp/project_tools.go`. Rejected — the split would add complexity without benefit since there's only one new tool.

### 2. `create_project` takes `name` + `org_id` as required params

The REST `POST /api/projects` requires `orgId`. The agent has `get_project_info` which returns the current project's org ID, and the auth context injects `orgID` on the pipeline context. The tool can resolve `org_id` from context if not supplied, falling back to the first org the user belongs to.

Alternative: look up the org ID automatically without requiring it. Chosen — reduces friction; the tool resolves org from `auth.OrgIDFromContext(ctx)` and only errors if it cannot be resolved.

### 3. Write tools added selectively — not `"*"` wildcard

The tool pool supports a `"*"` wildcard that gives an agent every tool. This is too permissive for the assistant (it would gain `delete_project`-adjacent tools that don't exist yet, and any future tools added to the registry). A curated whitelist keeps the blast radius predictable and the system prompt honest about capabilities.

### 4. System prompt updated to "action agent" posture with soft guardrails

The prompt will instruct the agent to: confirm intent before destructive operations (delete), prefer targeted updates over recreation, and explain what it is about to do before calling a write tool. Hard enforcement (human-in-the-loop approval) is out of scope.

## Risks / Trade-offs

- [Risk] Agent creates resources the user didn't intend → Mitigation: system prompt instructs the agent to describe the action before executing it; e2e tests verify the agent actually creates resources rather than just talking about it.
- [Risk] `create_project` org resolution silently picks the wrong org in multi-org accounts → Mitigation: the tool description documents that it uses the authenticated user's default org; users can supply `org_id` explicitly.
- [Risk] Existing `cli-assistant-agent` rows in production have the old (read-only) tool list → Mitigation: `EnsureCliAssistantAgent` is an upsert keyed on project ID + name; it overwrites the tool list on next call. The agent is re-created on first ask after deploy with no migration needed.

## Migration Plan

1. Deploy updated server — `EnsureCliAssistantAgent` upsert runs on first `/api/projects/:id/ask` call per project, updating the tool list in place.
2. No DB schema changes, no Goose migration.
3. Rollback: revert the `Tools` slice in `EnsureCliAssistantAgent` and redeploy; next ask recreates the agent with the old tool list.

## Open Questions

- Should `create_project` also install a default template pack? Deferred — keep the tool minimal for now.
- Should `trigger_agent` be included? Yes — the assistant being able to trigger agents it creates is a natural follow-on capability.
