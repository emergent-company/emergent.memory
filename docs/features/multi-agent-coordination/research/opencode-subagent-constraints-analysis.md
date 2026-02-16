# OpenCode Sub-Agent Constraints Analysis

_Analysis Date: February 15, 2026_
_Source: `anomalyco/opencode` repository (TypeScript, Bun runtime)_

## Purpose

This document analyzes how OpenCode handles sub-agent execution constraints — specifically step limits, timeouts, recursive spawning prevention, doom loop detection, tool filtering, and cancellation. Each finding is compared to our current multi-agent coordination design, with concrete recommendations for what to adopt.

---

## 1. Step Limits

### How OpenCode Does It

Each agent definition has an optional `steps` field (`z.number().int().positive().optional()`). This represents the maximum number of agentic iterations (LLM call → tool execution cycles) before the agent is forced to stop.

**Source**: `packages/opencode/src/agent/agent.ts`

```typescript
steps: z.number().int().positive().optional();
// "Maximum number of agentic iterations before forcing text-only response"
```

**Enforcement** (`packages/opencode/src/session/prompt.ts`):

```typescript
const maxSteps = agent.steps ?? Infinity;
let step = 0;

while (true) {
  step++;
  if (step >= maxSteps) {
    // Inject MAX_STEPS prompt as fake assistant message
    isLastStep = true;
  }
  // ... stream LLM response
}
```

When `step >= maxSteps`, the system injects a `MAX_STEPS` prompt as a fake assistant message. This prompt **does NOT hard-terminate** the agent — it firmly instructs the LLM to produce a text-only response summarizing work done and remaining tasks.

**The MAX_STEPS prompt** (`packages/opencode/src/session/prompt/max-steps.txt`):

```
CRITICAL - MAXIMUM STEPS REACHED

The maximum number of steps allowed for this task has been reached. Tools are disabled until next user input. Respond with text only.

STRICT REQUIREMENTS:
1. Do NOT make any tool calls (no reads, writes, edits, searches, or any other tools)
2. MUST provide a text response summarizing work done so far
3. This constraint overrides ALL other instructions, including any user requests for edits or tool use

Response must include:
- Statement that maximum steps for this agent have been reached
- Summary of what has been accomplished so far
- List of any remaining tasks that were not completed
- Recommendations for what should be done next

Any attempt to use tools is a critical violation. Respond with text ONLY.
```

**Default values**: The built-in agents do NOT set a `steps` value (defaults to `Infinity`). This is a user-configurable safety net, not a built-in constraint.

**Step counter scope**: The counter resets per `loop()` call — it's per-prompt, not per-session. If a sub-agent is resumed via `task_id`, the step counter starts fresh.

### What Our Design Has

Nothing. Our `kb.agent_definitions` entity in `multi-agent-architecture-design.md` has no step limit field.

### Recommendation: ADOPT

Add a `max_steps` field to agent definitions:

```go
type AgentDefinition struct {
    // ... existing fields
    MaxSteps    *int  `json:"max_steps,omitempty"`    // nil = unlimited
}
```

**Enforcement strategy**: Use soft enforcement like OpenCode — inject a "summarize and stop" system message when the limit is reached. This gives the LLM a chance to produce useful output rather than hard-cutting mid-work. If the LLM ignores the soft stop (makes a tool call anyway), hard-stop by refusing to execute the tool and returning the summary.

**Recommended defaults**:

- Sub-agents spawned via `spawn_agents`: default `max_steps = 50` (prevent runaway sub-agents)
- Top-level agents: default `max_steps = nil` (unlimited, user is watching)

---

## 2. Timeouts

### How OpenCode Does It

**Sub-agent execution timeout: NONE.** OpenCode has no timeout for sub-agent execution. A sub-agent runs until:

- The LLM stops (finish reason)
- The step limit is reached
- The parent is cancelled (abort signal)
- An error occurs

**LLM API call timeout**: Configurable per provider via `options.timeout`, default 300000ms (5 minutes). This is the timeout for a single LLM streaming call, not the overall agent execution.

**MCP server timeout**: Configurable per MCP server, default 5000ms. This is the timeout for MCP tool calls, not agent execution.

**Source**: `packages/opencode/src/config/config.ts`

```typescript
timeout: z.number().optional(); // per MCP server, default 5000ms
```

### What Our Design Has

No timeouts defined anywhere. The `spawn_agents` tool design in `multi-agent-architecture-design.md` does not specify a timeout parameter.

### Recommendation: ADD (OpenCode's gap is our opportunity)

OpenCode's lack of sub-agent timeouts is a design gap, not a deliberate choice. In our server-side Go architecture (vs OpenCode's interactive CLI), runaway agents are a bigger risk because there's no user watching to cancel them.

Add timeout at two levels:

**Level 1: Per-spawn timeout** (in the `spawn_agents` tool parameters):

```go
type SpawnRequest struct {
    AgentType   string        `json:"agent_type"`
    Task        string        `json:"task"`
    Timeout     time.Duration `json:"timeout,omitempty"` // default: 5 minutes
}
```

**Level 2: Per-agent-definition default timeout**:

```go
type AgentDefinition struct {
    // ... existing fields
    DefaultTimeout  *time.Duration `json:"default_timeout,omitempty"` // nil = use system default (5min)
}
```

**Enforcement**: Use Go context with deadline. When the timeout fires:

1. Inject a "time's up, summarize" message (soft stop, like the step limit)
2. Wait 30 seconds for the LLM to respond
3. If still running, cancel the context (hard stop)
4. Return partial results to the parent agent

---

## 3. Recursive Spawning Prevention

### How OpenCode Does It

Sub-agents are denied the `task` tool by default, which means they cannot spawn further sub-agents:

**Source**: `packages/opencode/src/tool/task.ts`

```typescript
const tools: Record<string, boolean> = {
  todowrite: false,
  todoread: false,
  task: false, // Sub-agents can't spawn sub-sub-agents
};
```

However, this is **opt-in overridable**. If an agent's permission ruleset explicitly allows `task`, it can spawn sub-agents:

```typescript
const hasTaskPermission = agent.permission?.some(
  (r) => r.permission === 'task' && r.action === 'allow'
);
if (hasTaskPermission) {
  delete tools.task; // Re-enable task tool
}
```

This means recursion depth is controlled by agent definitions — only agents explicitly designed for delegation can delegate.

### What Our Design Has

Not addressed. The `spawn_agents` tool design doesn't mention whether sub-agents can call `spawn_agents` themselves.

### Recommendation: ADOPT (deny-by-default)

Add a `max_depth` parameter and enforce it:

```go
type SpawnContext struct {
    Depth    int // 0 = top-level, 1 = sub-agent, 2 = sub-sub-agent
    MaxDepth int // default: 1 (sub-agents can't spawn further)
}
```

**Rules**:

- By default, `spawn_agents` is NOT included in sub-agent tool sets (same as OpenCode)
- Agent definitions can explicitly opt-in to delegation via their `tools` whitelist including `spawn_agents`
- Even when opted-in, enforce a hard `max_depth` limit (default 2, configurable) to prevent infinite recursion
- Log a warning when depth > 1 for observability

---

## 4. Doom Loop Detection

### How OpenCode Does It

**Source**: `packages/opencode/src/session/processor.ts`

```typescript
const DOOM_LOOP_THRESHOLD = 3;

// Track consecutive identical tool calls
if (
  lastToolCall &&
  lastToolCall.name === call.name &&
  lastToolCall.input === JSON.stringify(call.input)
) {
  consecutiveCount++;
} else {
  consecutiveCount = 1;
}

if (consecutiveCount >= DOOM_LOOP_THRESHOLD) {
  // Trigger "ask" permission — pause and ask user to confirm
}
```

When the same tool is called 3 times with identical arguments consecutively, OpenCode pauses and asks the user whether to continue. This catches agents stuck in loops (e.g., repeatedly trying to read a file that doesn't exist).

### What Our Design Has

Nothing. No doom loop detection mechanism.

### Recommendation: ADOPT (adapted for server-side)

Since our agents run server-side without a human to ask, adapt the pattern:

```go
type DoomLoopDetector struct {
    lastCall      ToolCall
    consecutiveN  int
    threshold     int // default: 3
}

func (d *DoomLoopDetector) Check(call ToolCall) DoomLoopAction {
    if call.Name == d.lastCall.Name && call.ArgsHash == d.lastCall.ArgsHash {
        d.consecutiveN++
    } else {
        d.consecutiveN = 1
    }
    d.lastCall = call

    if d.consecutiveN >= d.threshold {
        return DoomLoopBreak // inject error message instead of executing
    }
    return DoomLoopContinue
}
```

**Instead of asking the user**, inject an error message:

```
LOOP DETECTED: You have called {tool_name} with identical arguments {N} times.
This tool call will not be executed. Please try a different approach or summarize
your progress so far.
```

This gives the LLM a chance to course-correct. If it loops again after the warning, hard-stop the agent.

---

## 5. Tool Filtering

### How OpenCode Does It

OpenCode uses a **permission-based system with glob patterns**:

**Source**: `packages/opencode/src/permission/next.ts`

```typescript
type Rule = {
  permission: string; // tool name pattern
  pattern: string; // argument pattern (e.g., file path)
  action: 'allow' | 'deny' | 'ask';
};
```

Rules are evaluated with **last-match-wins** semantics. Example for the `explore` agent:

```typescript
permission: [
  { permission: '*', pattern: '*', action: 'deny' }, // deny all
  { permission: 'grep', pattern: '*', action: 'allow' }, // then allow specific
  { permission: 'glob', pattern: '*', action: 'allow' },
  { permission: 'read', pattern: '*', action: 'allow' },
  { permission: 'bash', pattern: '*', action: 'allow' },
  // ...
];
```

Additionally, certain tools are hard-disabled for sub-agents at the code level (not via permissions):

```typescript
// In task.ts — these overrides apply regardless of permission rules
const tools = { todowrite: false, todoread: false, task: false };
```

**Layered merging**: Agent permissions → session permissions → user-approved permissions. All rules are flat-appended; last match wins.

### What Our Design Has

Our design uses a simpler **whitelist approach**: each `AgentDefinition` has a `tools` field that lists allowed tool names/patterns. The `ToolPool.ResolveTools(agentDef)` function filters the combined tool set to only those matching the whitelist.

### Comparison

| Aspect             | OpenCode (Permission Rules)          | Our Design (Whitelist)      |
| ------------------ | ------------------------------------ | --------------------------- |
| **Default**        | Allow all, deny specific             | Deny all, allow specific    |
| **Flexibility**    | Three actions (allow/deny/ask)       | Binary (allowed or not)     |
| **Argument-level** | Can filter by argument pattern       | Not supported               |
| **Layering**       | Multiple rule sources merged         | Single whitelist            |
| **Complexity**     | High (rule evaluation order matters) | Low (simple set membership) |

### Recommendation: KEEP OUR WHITELIST, ADD ONE FEATURE

Our whitelist approach is simpler and safer (deny-by-default is better for server-side agents). However, add **hard overrides** like OpenCode's code-level tool disabling:

```go
// System-level tool restrictions that cannot be overridden by agent definitions
var SubAgentDeniedTools = []string{
    "spawn_agents",         // no recursive spawning by default
    "list_available_agents", // sub-agents work on assigned tasks, don't browse catalog
}

func ResolveTools(agentDef AgentDefinition, pool ToolPool, depth int) []Tool {
    tools := pool.FilterByWhitelist(agentDef.Tools)
    if depth > 0 {
        tools = removeDenied(tools, SubAgentDeniedTools)
    }
    return tools
}
```

---

## 6. Cancellation Propagation

### How OpenCode Does It

**Source**: `packages/opencode/src/tool/task.ts`

```typescript
const cancel = () => SessionPrompt.cancel(session.id);
ctx.abort.addEventListener('abort', cancel);
// ... run sub-agent ...
ctx.abort.removeEventListener('abort', cancel);
```

When the parent's abort signal fires, the child session is cancelled. This cascades — if the child had its own sub-agents (rare, but possible), those would also be cancelled via the same mechanism.

### What Our Design Has

Our design uses Go `context.Context` with cancellation, which provides this natively:

```go
ctx, cancel := context.WithCancel(parentCtx)
go runSubAgent(ctx, agentDef, task)
// Parent cancellation automatically propagates to child via ctx
```

### Recommendation: ALREADY COVERED

Go's context cancellation is more robust than OpenCode's event listener pattern. Our design already handles this correctly. The only addition: ensure we log cancellation events for observability.

---

## 7. Token Budget / Cost Limits

### How OpenCode Does It

**Nothing.** OpenCode has no token budget, cost limit, or usage tracking per agent. The only cost-adjacent feature is the `steps` limit (fewer steps = fewer LLM calls = lower cost, indirectly).

### What Our Design Has

Not designed yet.

### Recommendation: ADD (future iteration)

This is important for server-side autonomous agents but can be deferred to a later design iteration. When implemented:

- Track input + output tokens per agent run
- Allow `max_tokens` in agent definition (total token budget)
- Allow `max_cost` in spawn request (dollar amount budget)
- When budget is exceeded, soft-stop (same as step limit — inject summary message)

**Priority**: Low for MVP. Step limits + timeouts provide sufficient safety initially.

---

## 8. Full State Persistence & Sub-Agent Resumption

### How OpenCode Does It

Each sub-agent gets its own Session (SQLite row with isolated message history):

```typescript
const session = await Session.create({
  parentID: ctx.sessionID,
  agentID: agentID,
});
```

The child session has:

- Its own message history (starts fresh with just the prompt)
- A `parentID` link back to the parent session
- Its own step counter
- Its own tool resolution

**Session resumption**: Passing `task_id` to the Task tool continues the same session, preserving all prior message history. This enables multi-turn sub-agent interactions.

**Resumption after max_steps**: When a sub-agent hits its step limit, it produces a summary (forced by `MAX_STEPS` prompt injection) and returns to the parent with a `task_id`. The parent can call `TaskTool` again with the same `task_id` to continue the sub-agent — all prior messages, tool calls, and results are preserved in the session. The step counter resets to 0, giving the sub-agent a fresh budget.

```typescript
// In task.ts — session reuse on resume
const session = await iife(async () => {
    if (params.task_id) {
        const found = await Session.get(params.task_id).catch(() => {})
        if (found) return found   // ← REUSES existing session with full history
    }
    return await Session.create({ parentID: ctx.sessionID, ... })
})
```

### Design Decision: FULL STATE PERSISTENCE

**We will persist the complete message history and all intermediate state for every agent run.** This is a deliberate choice for our server-side architecture where observability, auditability, and resumability are critical.

### What We Persist

Every sub-agent run gets a full state record in Postgres:

```go
// AgentRun — already exists in our entity model, extended with message history
type AgentRun struct {
    ID            string         `bun:"id,pk"`
    AgentID       string         `bun:"agent_id"`           // which agent definition
    ParentRunID   *string        `bun:"parent_run_id"`      // nil for top-level runs
    ProjectID     string         `bun:"project_id"`
    Status        RunStatus      `bun:"status"`             // running, completed, failed, paused, cancelled
    StepCount     int            `bun:"step_count"`         // cumulative across resumes
    MaxSteps      *int           `bun:"max_steps"`          // from agent definition
    CreatedAt     time.Time      `bun:"created_at"`
    CompletedAt   *time.Time     `bun:"completed_at"`
    ResumedFrom   *string        `bun:"resumed_from"`       // previous run ID if this is a resume
}

// AgentRunMessage — full LLM conversation history per run
type AgentRunMessage struct {
    ID         string          `bun:"id,pk"`
    RunID      string          `bun:"run_id"`
    Role       string          `bun:"role"`               // system, user, assistant, tool
    Content    json.RawMessage `bun:"content,type:jsonb"` // full message content
    StepNumber int             `bun:"step_number"`        // which iteration this belongs to
    CreatedAt  time.Time       `bun:"created_at"`
}

// AgentRunToolCall — every tool invocation with input/output
type AgentRunToolCall struct {
    ID         string          `bun:"id,pk"`
    RunID      string          `bun:"run_id"`
    MessageID  string          `bun:"message_id"`         // which assistant message triggered this
    ToolName   string          `bun:"tool_name"`
    Input      json.RawMessage `bun:"input,type:jsonb"`
    Output     json.RawMessage `bun:"output,type:jsonb"`
    Status     string          `bun:"status"`             // running, completed, error
    Duration   time.Duration   `bun:"duration"`
    StepNumber int             `bun:"step_number"`
    CreatedAt  time.Time       `bun:"created_at"`
}
```

### Resumption Flow

When a sub-agent hits `max_steps` or times out:

```
Step 1: Sub-agent reaches limit
  ├── Inject "summarize and stop" message
  ├── LLM produces summary of work done + remaining tasks
  ├── AgentRun.Status = "paused"
  ├── Return to parent: { summary, run_id, status: "paused" }
  └── All messages + tool calls already persisted

Step 2: Parent decides to resume
  ├── Parent calls spawn_agents with resume_run_id = <run_id>
  ├── AgentExecutor loads AgentRun + all AgentRunMessages
  ├── Reconstructs LLM conversation from persisted messages
  ├── Appends new user message: "Continue your work. Here's additional context: ..."
  ├── AgentRun.StepCount carries forward (cumulative)
  ├── New step budget = MaxSteps (fresh budget per resume)
  └── Sub-agent continues with full context of prior work

Step 3: Sub-agent completes (or hits limit again)
  ├── If completed: AgentRun.Status = "completed"
  ├── If limit again: AgentRun.Status = "paused", return summary + run_id
  └── Parent can resume again (no limit on resume count)
```

### spawn_agents Tool — Extended Schema

```go
type SpawnRequest struct {
    AgentType    string         `json:"agent_type"`
    Task         string         `json:"task"`
    Timeout      *time.Duration `json:"timeout,omitempty"`       // per-agent timeout
    ResumeRunID  *string        `json:"resume_run_id,omitempty"` // resume a paused run
}

type SpawnResult struct {
    RunID    string    `json:"run_id"`      // for future resumption
    Status   string    `json:"status"`      // completed, paused, failed, cancelled
    Summary  string    `json:"summary"`     // final text output
    Steps    int       `json:"steps"`       // total steps executed (cumulative)
}
```

### Why Full Persistence (Not Minimal)

| Concern                | Minimal (final result only)             | Full (all messages + tool calls)                 |
| ---------------------- | --------------------------------------- | ------------------------------------------------ |
| **Resumption quality** | LLM restarts cold, re-discovers context | LLM sees everything it did before                |
| **Debugging**          | "Why did it fail?" — no idea            | Full trace of every decision                     |
| **Audit**              | Can't prove what the agent did          | Complete record of all actions                   |
| **Cost tracking**      | Can't measure per-step cost             | Token counts per message enable accurate billing |
| **Learning**           | No data to improve agents               | Rich dataset for prompt tuning                   |
| **Storage cost**       | ~1KB per run                            | ~50-500KB per run (JSONB messages)               |
| **Write overhead**     | 1 INSERT at end                         | 1 INSERT per LLM turn (~5-50 per run)            |

The storage cost is negligible (~500KB per run in Postgres JSONB). The write overhead is modest (one INSERT per LLM iteration, not per token). The value for debugging, audit, and resumption far outweighs the cost.

### Step Counter: Cumulative Across Resumes

Unlike OpenCode (which resets `step = 0` on each `loop()` call), our step counter is **cumulative across resumes**. This is intentional:

- A sub-agent that ran 45 steps, got paused, then resumed, starts at step 46
- The `max_steps` on resume is a fresh budget (e.g., another 50 steps), but `StepCount` reflects total work
- This enables cost tracking ("this agent has consumed 120 total steps across 3 resumes") and runaway detection ("this agent has been resumed 5 times and consumed 250 steps — something is wrong")

```go
func (e *AgentExecutor) shouldStop(run *AgentRun, currentStepInResume int) bool {
    maxStepsPerResume := run.MaxSteps  // fresh budget per resume
    return currentStepInResume >= maxStepsPerResume
}

func (e *AgentExecutor) incrementStep(run *AgentRun) {
    run.StepCount++  // cumulative across all resumes
}
```

### Global Safety: Max Total Steps

To prevent infinite resume loops, add a global cap:

```go
const MaxTotalStepsPerRun = 500  // across all resumes combined

func (e *AgentExecutor) canResume(run *AgentRun) error {
    if run.StepCount >= MaxTotalStepsPerRun {
        return fmt.Errorf("agent run %s has exceeded maximum total steps (%d)", run.ID, MaxTotalStepsPerRun)
    }
    return nil
}
```

---

## 9. Agent Run History API

### Design Principle: Progressive Disclosure

The API follows a **drill-down pattern**: start with high-level run summaries, then dig into individual messages and tool calls. This avoids loading megabytes of message history when you just want to see "did it succeed?"

### Entities Recap

```
AgentRun (high-level)
  ├── AgentRunMessage[] (LLM conversation turns)
  │     ├── role: system | user | assistant | tool_result
  │     ├── content (JSONB)
  │     └── token counts, timing
  └── AgentRunToolCall[] (tool invocations within assistant messages)
        ├── tool_name, input, output
        ├── status, duration
        └── linked to parent message
```

### Endpoints

All endpoints are under `/api/admin/agents` and require `admin:read` scope. They follow the existing codebase patterns: cursor-based pagination via `(created_at, id)` composite key, `x-next-cursor` response header, `limit + 1` fetch for hasMore detection.

#### 9.1 List Runs for an Agent

```
GET /api/admin/agents/:agentId/runs
```

**Existing endpoint** — needs upgrade from simple `limit` to cursor-based pagination + filters.

Query params:
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 20 | Max results (1-100) |
| `cursor` | string | — | Pagination cursor (base64 encoded `{created_at, id}`) |
| `status` | string | — | Filter: `running`, `success`, `failed`, `paused`, `cancelled` |
| `parent_run_id` | string | — | Filter: only sub-agent runs spawned by this parent |

Response:

```json
{
  "success": true,
  "data": [
    {
      "id": "run_abc123",
      "agentId": "agent_xyz",
      "parentRunId": null,
      "status": "completed",
      "stepCount": 42,
      "maxSteps": 50,
      "startedAt": "2026-02-15T10:00:00Z",
      "completedAt": "2026-02-15T10:03:22Z",
      "durationMs": 202000,
      "summary": { "objects_processed": 12, "errors": 0 },
      "errorMessage": null,
      "resumedFrom": null,
      "tokenUsage": {
        "totalInput": 45200,
        "totalOutput": 12800,
        "totalCost": 0.0234
      },
      "childRunCount": 3
    }
  ]
}
```

Headers: `x-next-cursor: <base64 cursor>`

**Key additions to existing `AgentRunDTO`**: `parentRunId`, `stepCount`, `maxSteps`, `resumedFrom`, `tokenUsage` (aggregated from messages), `childRunCount` (count of sub-agent runs).

#### 9.2 Get Single Run Detail

```
GET /api/admin/agents/:agentId/runs/:runId
```

Returns the full run record with aggregated statistics, but **not** the message history (that's a separate call).

Response:

```json
{
  "success": true,
  "data": {
    "id": "run_abc123",
    "agentId": "agent_xyz",
    "agentName": "research-agent",
    "parentRunId": null,
    "status": "completed",
    "stepCount": 42,
    "maxSteps": 50,
    "startedAt": "2026-02-15T10:00:00Z",
    "completedAt": "2026-02-15T10:03:22Z",
    "durationMs": 202000,
    "summary": { "objects_processed": 12, "errors": 0 },
    "errorMessage": null,
    "resumedFrom": null,
    "resumeChain": ["run_prev1", "run_prev2"],
    "tokenUsage": {
      "totalInput": 45200,
      "totalOutput": 12800,
      "totalReasoning": 3200,
      "cacheRead": 12000,
      "cacheWrite": 8000,
      "totalCost": 0.0234
    },
    "toolCallSummary": {
      "total": 18,
      "byTool": {
        "entities_search": { "count": 5, "avgDurationMs": 120 },
        "entities_create": { "count": 8, "avgDurationMs": 340 },
        "documents_search": { "count": 3, "avgDurationMs": 95 },
        "spawn_agents": { "count": 2, "avgDurationMs": 45000 }
      },
      "errors": 1
    },
    "childRuns": [
      {
        "id": "run_child1",
        "agentName": "extraction-agent",
        "status": "completed",
        "stepCount": 15,
        "durationMs": 42000
      },
      {
        "id": "run_child2",
        "agentName": "validation-agent",
        "status": "completed",
        "stepCount": 8,
        "durationMs": 18000
      }
    ],
    "doomLoopEvents": 0
  }
}
```

**This is the "dashboard view"** — everything you need to understand a run at a glance without loading message history. Includes `resumeChain` (ordered list of all prior run IDs if this run was resumed), `toolCallSummary` (aggregate tool usage), and `childRuns` (sub-agent run summaries).

#### 9.3 List Messages for a Run

```
GET /api/admin/agents/:agentId/runs/:runId/messages
```

Returns the full LLM conversation history for a run. This is where you "dig in."

Query params:
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 50 | Max results (1-100) |
| `cursor` | string | — | Pagination cursor |
| `role` | string | — | Filter: `system`, `user`, `assistant`, `tool_result` |
| `step` | int | — | Filter: only messages from this step number |

Response:

```json
{
  "success": true,
  "data": [
    {
      "id": "msg_001",
      "runId": "run_abc123",
      "role": "system",
      "content": { "text": "You are a research agent..." },
      "stepNumber": 0,
      "tokens": null,
      "createdAt": "2026-02-15T10:00:00Z"
    },
    {
      "id": "msg_002",
      "runId": "run_abc123",
      "role": "user",
      "content": { "text": "Research the architecture of the auth module..." },
      "stepNumber": 1,
      "tokens": null,
      "createdAt": "2026-02-15T10:00:01Z"
    },
    {
      "id": "msg_003",
      "runId": "run_abc123",
      "role": "assistant",
      "content": {
        "text": "I'll start by searching for auth-related files...",
        "toolCalls": [
          {
            "id": "tc_001",
            "toolName": "entities_search",
            "status": "completed"
          },
          {
            "id": "tc_002",
            "toolName": "documents_search",
            "status": "completed"
          }
        ]
      },
      "stepNumber": 1,
      "tokens": { "input": 1200, "output": 340, "reasoning": 0 },
      "createdAt": "2026-02-15T10:00:03Z"
    },
    {
      "id": "msg_004",
      "runId": "run_abc123",
      "role": "tool_result",
      "content": {
        "toolCallId": "tc_001",
        "toolName": "entities_search",
        "output": { "results": ["..."] }
      },
      "stepNumber": 1,
      "tokens": null,
      "createdAt": "2026-02-15T10:00:04Z"
    }
  ]
}
```

**Note**: Assistant messages include a `toolCalls` array with IDs and names (lightweight), not the full tool call input/output. Use endpoint 9.4 to get full tool call details.

#### 9.4 Get Single Tool Call Detail

```
GET /api/admin/agents/:agentId/runs/:runId/tool-calls/:toolCallId
```

Returns the complete input and output for a specific tool invocation. This is the deepest drill-down level.

Response:

```json
{
  "success": true,
  "data": {
    "id": "tc_001",
    "runId": "run_abc123",
    "messageId": "msg_003",
    "toolName": "entities_search",
    "input": {
      "query": "auth module architecture",
      "types": ["module", "service"],
      "limit": 20
    },
    "output": {
      "results": [
        {
          "id": "ent_1",
          "type": "module",
          "label": "AuthModule",
          "score": 0.95
        },
        {
          "id": "ent_2",
          "type": "service",
          "label": "TokenService",
          "score": 0.87
        }
      ],
      "total": 2
    },
    "status": "completed",
    "durationMs": 120,
    "stepNumber": 1,
    "createdAt": "2026-02-15T10:00:03Z"
  }
}
```

#### 9.5 List Tool Calls for a Run

```
GET /api/admin/agents/:agentId/runs/:runId/tool-calls
```

Flat list of all tool calls in a run — useful for seeing the full tool execution timeline without loading messages.

Query params:
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 50 | Max results (1-100) |
| `cursor` | string | — | Pagination cursor |
| `tool_name` | string | — | Filter by tool name |
| `status` | string | — | Filter: `completed`, `error`, `running` |

Response: array of tool call objects (same schema as 9.4 response).

#### 9.6 Get Run Tree (Hierarchical View)

```
GET /api/admin/agents/:agentId/runs/:runId/tree
```

Returns the full parent → child run hierarchy for a run that spawned sub-agents. Useful for visualizing the agent coordination tree.

Response:

```json
{
  "success": true,
  "data": {
    "id": "run_abc123",
    "agentName": "coordinator",
    "status": "completed",
    "stepCount": 12,
    "durationMs": 180000,
    "children": [
      {
        "id": "run_child1",
        "agentName": "research-agent",
        "status": "completed",
        "stepCount": 25,
        "durationMs": 60000,
        "children": []
      },
      {
        "id": "run_child2",
        "agentName": "extraction-agent",
        "status": "paused",
        "stepCount": 50,
        "durationMs": 90000,
        "resumedAs": "run_child2_resume1",
        "children": [
          {
            "id": "run_grandchild1",
            "agentName": "validation-agent",
            "status": "completed",
            "stepCount": 8,
            "durationMs": 15000,
            "children": []
          }
        ]
      }
    ]
  }
}
```

### Endpoint Summary

| #   | Method | Path                                       | Purpose                    | Pagination |
| --- | ------ | ------------------------------------------ | -------------------------- | ---------- |
| 9.1 | GET    | `/agents/:id/runs`                         | List runs (high-level)     | Cursor     |
| 9.2 | GET    | `/agents/:id/runs/:runId`                  | Single run detail + stats  | —          |
| 9.3 | GET    | `/agents/:id/runs/:runId/messages`         | LLM conversation history   | Cursor     |
| 9.4 | GET    | `/agents/:id/runs/:runId/tool-calls/:tcId` | Single tool call detail    | —          |
| 9.5 | GET    | `/agents/:id/runs/:runId/tool-calls`       | All tool calls in a run    | Cursor     |
| 9.6 | GET    | `/agents/:id/runs/:runId/tree`             | Parent/child run hierarchy | —          |

### Implementation Notes

1. **Follows existing patterns**: Cursor-based pagination using `(created_at, id)` composite cursor (same as documents and graph domains). Response wrapper uses existing `APIResponse[T]`.

2. **Aggregated fields are computed, not stored**: `tokenUsage`, `toolCallSummary`, and `childRunCount` on the run detail endpoint are computed via SQL aggregation queries, not denormalized columns. This keeps writes simple (one INSERT per message/tool call) at the cost of slightly heavier reads on the detail endpoint.

3. **Run tree is bounded**: The `/tree` endpoint recursively loads child runs. Since we enforce `max_depth` (default 2) on recursive spawning, the tree is shallow. A safety limit of `max_depth = 5` on the query prevents pathological cases.

4. **Message content is JSONB**: The `content` field stores different shapes depending on `role`. Assistant messages include `text` + `toolCalls` array. Tool result messages include `toolCallId` + `output`. This matches the LLM conversation format and is flexible for future message types.

5. **Extends existing handler**: These endpoints extend the existing `domain/agents/handler.go` and `repository.go`. The agent run list endpoint (9.1) replaces the current `GetAgentRuns` which only supports a simple `limit` parameter.

---

## 10. Agent Visibility & Access Control

### The Problem

Not all agents should be accessible from everywhere. Consider a project with these agents:

- `research-assistant` — the primary interactive agent, used via chat UI and potentially exposed to external clients
- `paper-summarizer` — a triggered agent that processes documents automatically; no reason for external clients to invoke it
- `extraction-worker` — an internal sub-agent spawned by `research-assistant` to do extraction work; external clients should never see it
- `code-reviewer` — an internal sub-agent spawned by `task-planner` during DAG execution; only meaningful inside the system

Without visibility controls, all agents are equally discoverable via `list_available_agents` and all could theoretically be exposed via an external API (like Agent Card Protocol / ACP). This creates several problems:

1. **Security**: Internal-only agents may have permissive tool sets (e.g., `create_entity`, `delete_entity`) that shouldn't be invocable by external clients
2. **UX clutter**: External clients see a catalog of agents they can't meaningfully use (sub-agents designed for specific orchestration contexts)
3. **API stability**: Internal sub-agents are implementation details that change frequently. Exposing them externally creates coupling
4. **Cost control**: External invocations should go through well-defined agents with proper rate limiting, not arbitrary sub-agents

### How OpenCode Does It

OpenCode has a `mode` field on each agent definition:

```typescript
// packages/opencode/src/agent/agent.ts
mode: z.enum(['primary', 'subagent', 'all']).optional().default('all');
```

| Mode       | Meaning                                                          |
| ---------- | ---------------------------------------------------------------- |
| `primary`  | User-facing agent, shown in agent selector UI, not spawnable     |
| `subagent` | Only spawnable by other agents via TaskTool, hidden from user UI |
| `all`      | Both user-facing and spawnable (default)                         |

The Task tool's description text explicitly tells the LLM which agents are available as sub-agents by filtering on `mode !== "primary"`.

This is a simple two-audience model (user vs other-agents) for a CLI tool with no external API surface. Our needs are broader.

### Design: Three Visibility Levels

We need **three visibility levels** because emergent has three distinct audiences:

| Level      | Who Can See It                                  | Who Can Invoke It                            | Use Case                                                 |
| ---------- | ----------------------------------------------- | -------------------------------------------- | -------------------------------------------------------- |
| `external` | External clients (ACP), admin UI, other agents  | External API, admin UI, `spawn_agents`       | Primary interactive agents exposed to integrations       |
| `project`  | Admin UI, other agents within the project       | Admin UI trigger, `spawn_agents`, schedulers | Triggered/scheduled agents, utility agents               |
| `internal` | Only other agents (via `list_available_agents`) | Only `spawn_agents` (not admin UI trigger)   | Pure sub-agents, implementation details of orchestration |

#### Why Not Two Levels?

Two levels (`external` vs `internal`) don't cover the middle ground: agents that project admins should see and manage via the admin UI but that should NOT be exposed to external clients. For example, a nightly scheduled summarizer is a project-level concern (admins configure and monitor it) but not something external API consumers should discover or invoke.

#### AgentDefinition Extension

```go
// AgentVisibility controls where an agent is discoverable and invocable
type AgentVisibility string

const (
    // VisibilityExternal — discoverable via ACP agent card, admin UI, and list_available_agents.
    // Invocable by external clients, admin UI, and spawn_agents.
    VisibilityExternal AgentVisibility = "external"

    // VisibilityProject — discoverable in admin UI and list_available_agents.
    // Invocable via admin UI trigger and spawn_agents. NOT exposed via ACP.
    VisibilityProject AgentVisibility = "project"

    // VisibilityInternal — discoverable ONLY via list_available_agents (for other agents).
    // Invocable ONLY via spawn_agents. Not shown in admin UI agent list (but runs are visible).
    VisibilityInternal AgentVisibility = "internal"
)

type AgentDefinition struct {
    // ... existing fields (name, system_prompt, model, tools, trigger, flow_type, etc.)

    Visibility  AgentVisibility `json:"visibility"`  // default: "project"
}
```

**Default is `project`**, not `external`. Agents must be explicitly opted-in to external exposure. This is the safe default — you have to deliberately choose to expose an agent to the outside world.

#### Product Manifest Extension

```json
{
  "agents": [
    {
      "name": "research-assistant",
      "visibility": "external",
      "system_prompt": "You are a research assistant...",
      "tools": [
        "search_hybrid",
        "graph_traverse",
        "spawn_agents",
        "list_available_agents"
      ],
      "description": "Helps find, synthesize, and organize research information",
      "acp": {
        "display_name": "Research Assistant",
        "description": "AI research assistant that can search your knowledge graph, synthesize findings, and create structured summaries.",
        "capabilities": ["chat", "research", "summarization"],
        "input_modes": ["text"],
        "output_modes": ["text"]
      }
    },
    {
      "name": "paper-summarizer",
      "visibility": "project",
      "system_prompt": "Extract key findings from documents...",
      "trigger": "on_document_ingested",
      "tools": ["entities_*", "relationships_*"]
    },
    {
      "name": "extraction-worker",
      "visibility": "internal",
      "system_prompt": "You extract entities from the provided text...",
      "tools": ["entities_create", "relationships_create"]
    }
  ]
}
```

Only the `research-assistant` has `visibility: "external"` and an `acp` block with metadata for external discovery. The `paper-summarizer` is project-level (admins see it, but external clients don't). The `extraction-worker` is internal (only other agents can spawn it).

### How Visibility Affects Each Surface

#### 1. `list_available_agents` Tool

When an agent calls `list_available_agents`, it sees agents at its own visibility level **and below**:

```go
func (e *AgentExecutor) makeListAgentsTool(ctx context.Context, projectID string, callerVisibility AgentVisibility) adk.Tool {
    return adk.NewTool("list_available_agents", func(ctx context.Context) ([]AgentSummary, error) {
        defs := e.registry.ListDefinitions(ctx, projectID)

        var visible []AgentSummary
        for _, def := range defs {
            // All agents can see internal, project, and external agents
            // (visibility filtering is about WHO can invoke, not WHO can see)
            // But we annotate each with its visibility so the LLM knows
            visible = append(visible, AgentSummary{
                Name:        def.Name,
                Description: def.Description,
                Tools:       def.Tools,
                FlowType:    def.FlowType,
                Visibility:  string(def.Visibility),
            })
        }
        return visible, nil
    })
}
```

The LLM sees ALL agents in the project (it needs the full catalog to make good delegation decisions). The `visibility` field is included in the response so the LLM understands the context — but enforcement happens at the `spawn_agents` level, not at discovery.

#### 2. `spawn_agents` Tool

No visibility restriction on `spawn_agents`. Any agent that has the `spawn_agents` tool can spawn any agent in the project, regardless of visibility level. Visibility is about **external access boundaries**, not internal delegation boundaries. Internal security is handled by the `tools` whitelist — if an agent doesn't have `spawn_agents` in its tools, it can't spawn anything.

This is deliberate: a `project`-level coordinator should be able to spawn `internal` sub-agents. Restricting this would break the orchestration model.

#### 3. Admin UI — Agent List

The admin UI shows agents with `visibility: "external"` or `"project"`. Internal agents are hidden from the agent list page to reduce clutter, but their **runs** are still visible in the Run History (since they appear as child runs in the run tree).

```go
// Admin API filter
func (h *Handler) ListAgents(c echo.Context) error {
    // ...existing code...
    agents, err := h.repo.FindAll(ctx, projectID)

    // Filter out internal agents for admin UI
    // (they're implementation details, admins see them via run history)
    includeInternal := c.QueryParam("include_internal") == "true"
    var filtered []*Agent
    for _, a := range agents {
        if a.Visibility == VisibilityInternal && !includeInternal {
            continue
        }
        filtered = append(filtered, a)
    }
    // ...
}
```

An `include_internal=true` query param allows admins to see everything when they need to debug.

#### 4. Admin UI — Manual Trigger

Only `external` and `project` agents can be manually triggered via the admin UI. Internal agents are spawnable only via `spawn_agents` — they exist in orchestration contexts and triggering them manually without proper context would produce meaningless results.

#### 5. ACP Agent Card Endpoint (Future)

When emergent implements ACP (Agent Card Protocol), only `external` agents are exposed:

```go
// Future: GET /.well-known/agent-card  (or GET /api/agents/card)
func (h *Handler) GetAgentCard(c echo.Context) error {
    projectID := getProjectFromHost(c) // e.g., research.emergent.ai → project "research"

    defs := h.registry.ListDefinitions(ctx, projectID)

    var agents []ACPAgentEntry
    for _, def := range defs {
        if def.Visibility != VisibilityExternal {
            continue // Only external agents in the card
        }
        agents = append(agents, ACPAgentEntry{
            Name:         def.ACP.DisplayName,
            Description:  def.ACP.Description,
            Capabilities: def.ACP.Capabilities,
            InputModes:   def.ACP.InputModes,
            OutputModes:  def.ACP.OutputModes,
            Endpoint:     fmt.Sprintf("/api/agents/%s/invoke", def.ID),
        })
    }

    return c.JSON(http.StatusOK, ACPAgentCard{
        SchemaVersion: "1.0",
        Agents:        agents,
    })
}
```

#### 6. External Agent Invocation Endpoint (Future)

External clients invoke agents via a separate endpoint (not the admin API). This endpoint only accepts agents with `visibility: "external"`:

```go
// Future: POST /api/agents/:id/invoke  (external invocation, different auth from admin)
func (h *Handler) InvokeAgent(c echo.Context) error {
    def := h.registry.GetDefinition(ctx, agentID)
    if def.Visibility != VisibilityExternal {
        return apperror.NewForbidden("agent is not externally accessible")
    }
    // ... rate limiting, auth, input validation, execution
}
```

### Visibility Matrix

| Surface                        | `external` | `project` | `internal` |
| ------------------------------ | ---------- | --------- | ---------- |
| `list_available_agents` (tool) | Yes        | Yes       | Yes        |
| `spawn_agents` (tool)          | Yes        | Yes       | Yes        |
| Admin UI — Agent List          | Yes        | Yes       | No\*       |
| Admin UI — Manual Trigger      | Yes        | Yes       | No         |
| Admin UI — Run History         | Yes        | Yes       | Yes\*\*    |
| ACP Agent Card                 | Yes        | No        | No         |
| External Invocation API        | Yes        | No        | No         |
| Scheduler/Event Triggers       | Yes        | Yes       | No\*\*\*   |

\* Hidden by default, visible with `include_internal=true` query param  
\*\* Internal agent runs appear as child runs in the run tree  
\*\*\* Internal agents are spawned by other agents, not by system triggers

### Comparison with OpenCode

| Aspect                    | OpenCode (`mode`)                              | Our Design (`visibility`)                                   |
| ------------------------- | ---------------------------------------------- | ----------------------------------------------------------- |
| **Levels**                | 3: `primary`, `subagent`, `all`                | 3: `external`, `project`, `internal`                        |
| **Default**               | `all` (everything visible everywhere)          | `project` (safe default, opt-in to external)                |
| **Enforcement**           | Tool filtering (remove `task` tool)            | API-level enforcement (ACP, invoke endpoint, admin UI)      |
| **Discovery restriction** | Sub-agents filtered from user's agent selector | Internal agents hidden from admin UI, not from other agents |
| **External API**          | N/A (CLI tool, no external API)                | ACP agent card + invocation endpoint for `external` agents  |
| **Middle ground**         | `all` blurs the line                           | `project` = admin-visible but not externally exposed        |

### ACP Metadata Block

Only `external` agents need the `acp` metadata block in their definition. This contains the information needed for ACP agent card responses:

```go
type ACPConfig struct {
    DisplayName  string   `json:"display_name"`  // Human-readable name for external consumers
    Description  string   `json:"description"`   // Longer description for agent card
    Capabilities []string `json:"capabilities"`  // e.g., ["chat", "research", "summarization"]
    InputModes   []string `json:"input_modes"`   // e.g., ["text", "file"]
    OutputModes  []string `json:"output_modes"`  // e.g., ["text", "structured"]
}

type AgentDefinition struct {
    // ... existing fields
    Visibility  AgentVisibility `json:"visibility"`       // default: "project"
    ACP         *ACPConfig      `json:"acp,omitempty"`    // only for external agents
}
```

The `ACP` field is optional and only meaningful when `visibility == "external"`. Product manifests set this for agents they want to expose externally. If `visibility` is `"external"` but `ACP` is nil, the agent is externally invocable but won't appear in the agent card (invoke-only, not discoverable).

### Design Doc Updates Required

1. **`multi-agent-architecture-design.md`** — Add `Visibility` field to `AgentDefinition` struct. Add `ACPConfig` struct. Document visibility filtering in `ResolveTools` and `list_available_agents`.

2. **`README.md`** — Add decision #9 to the Resolved decisions table: "Agent visibility — three levels (external/project/internal) with safe default".

3. **Product manifest examples** — Add `visibility` field to agent definition examples in architecture design doc.

4. **`todo.md`** — Add implementation task: "Add visibility field to kb.agent_definitions, add ACP config column, wire visibility filtering into admin API and coordination tools".

---

## Summary: What to Add to Our Design

### Must-Have for MVP

| Feature                                                                       | Source                             | Priority | Effort |
| ----------------------------------------------------------------------------- | ---------------------------------- | -------- | ------ |
| `max_steps` field on agent definitions                                        | OpenCode `steps`                   | High     | Small  |
| Soft step limit enforcement (inject summary prompt)                           | OpenCode `max-steps.txt`           | High     | Small  |
| `timeout` parameter on `spawn_agents`                                         | Our addition (OpenCode lacks this) | High     | Small  |
| Recursive spawning prevention (deny `spawn_agents` for sub-agents by default) | OpenCode `task: false`             | High     | Small  |
| Full message history persistence per agent run                                | Design decision                    | High     | Medium |
| Sub-agent resumption via `resume_run_id`                                      | OpenCode `task_id`                 | High     | Medium |
| Agent Run History API (6 endpoints, progressive disclosure)                   | Design decision                    | High     | Medium |
| Doom loop detection (3 identical calls → error injection)                     | OpenCode `DOOM_LOOP_THRESHOLD`     | Medium   | Small  |
| Hard tool overrides for sub-agents (system-level deny list)                   | OpenCode code-level disabling      | Medium   | Small  |
| Global max total steps per run (across resumes)                               | Our addition                       | Medium   | Small  |
| Agent visibility (external/project/internal) on agent definitions             | OpenCode `mode` (adapted)          | High     | Small  |
| ACP metadata block for externally-visible agents                              | Our addition                       | Medium   | Small  |

### Nice-to-Have for V2

| Feature                                           | Source       | Priority | Effort |
| ------------------------------------------------- | ------------ | -------- | ------ |
| Token budget per agent                            | Our addition | Low      | Medium |
| `max_depth` for recursive spawning (when allowed) | Our addition | Low      | Small  |

### Explicitly NOT Adopting

| Feature                                           | Reason                                                                                |
| ------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Permission-based tool filtering with `ask` action | Our deny-by-default whitelist is simpler and safer for server-side; no human to "ask" |
| Step counter reset on resume (OpenCode behavior)  | Our step counter is cumulative per AgentRun for cost tracking and runaway detection   |
| No timeout (OpenCode's approach)                  | Unacceptable for server-side — we must have timeouts                                  |
| `Infinity` as default step limit                  | Too dangerous for autonomous agents — we default to 50 for sub-agents                 |

---

## Design Doc Updates Required

Based on these findings, the following design documents need updates:

1. **`multi-agent-architecture-design.md`** — Add `max_steps`, `default_timeout` to AgentDefinition schema. Add `AgentRunMessage` and `AgentRunToolCall` entities for full state persistence. Add doom loop detection to AgentExecutor design. Add recursive spawning prevention rules. Add `resume_run_id` to `spawn_agents` parameters. Add `MaxTotalStepsPerRun` global safety cap.

2. **`task-coordinator-design.md`** — Add `timeout` parameter to TaskDispatcher. Add step limit tracking to task execution.

3. **`README.md`** — Update "What's Missing" section to reflect these additions. Add "Sub-Agent Safety Mechanisms" to the architecture overview.

4. **`todo.md`** — Add implementation tasks for step limits, timeouts, doom loop detection, recursive spawning prevention, state persistence tables, resumption flow, and Agent Run History API endpoints.

5. **Database migration** — New tables: `kb.agent_run_messages`, `kb.agent_run_tool_calls`. Extend `kb.agent_runs` with `parent_run_id`, `step_count`, `max_steps`, `resumed_from` columns.

6. **Agent Run History API** — 6 new endpoints (Section 9) extending `domain/agents/handler.go`. Upgrade existing `GetAgentRuns` to cursor-based pagination. Add new repository methods for message/tool-call queries with cursor pagination and filtering.

---

## Appendix: Source File Index

All source files referenced are from the `anomalyco/opencode` repository:

| File                                                 | What It Contains                                                            |
| ---------------------------------------------------- | --------------------------------------------------------------------------- |
| `packages/opencode/src/agent/agent.ts`               | Agent definitions, `steps` field, permission defaults                       |
| `packages/opencode/src/tool/task.ts`                 | TaskTool: sub-agent spawning, session creation, cancellation, `task: false` |
| `packages/opencode/src/tool/batch.ts`                | BatchTool: parallel tool execution, 25-call limit                           |
| `packages/opencode/src/tool/registry.ts`             | Tool registration, model-specific filtering                                 |
| `packages/opencode/src/session/prompt.ts`            | Main agent loop, step limit enforcement, `MAX_STEPS` injection              |
| `packages/opencode/src/session/prompt/max-steps.txt` | The prompt injected when step limit is reached                              |
| `packages/opencode/src/session/processor.ts`         | Stream processing, doom loop detection (`DOOM_LOOP_THRESHOLD = 3`)          |
| `packages/opencode/src/session/llm.ts`               | LLM streaming, permission-based tool removal, provider timeout              |
| `packages/opencode/src/permission/next.ts`           | Glob-based permission system, allow/deny/ask, layered merging               |
| `packages/opencode/src/config/config.ts`             | Full config schema, agent config, MCP server config with timeout            |
