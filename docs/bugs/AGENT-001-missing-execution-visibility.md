# AGENT-001: Missing Agent Execution Visibility & Controls

**Status:** Confirmed  
**Priority:** High  
**Reporter:** Client Feedback (Janitor Agent Development)  
**Date:** 2026-02-16  
**Component:** Agent Execution System

## Summary

Client developing a "janitor" maintenance agent reports critical gaps in execution visibility and control. While backend has robust execution limits and metrics tracking, these are not exposed to users through the frontend or API, creating a "black box" experience.

## Problem Report

### Client's Experience

1. **11-Minute Mystery Run:**

   - Run took 652 seconds (~11 minutes)
   - No visibility into what happened during that time
   - Cannot see step count, token usage, or tool calls
   - No way to track progress in real-time

2. **Stuck Runs:**

   - Two runs stuck in "running" state for 1+ hour
   - No way to cancel them via API
   - Unclear if they hit a timeout or error

3. **Unknown Limits:**
   - No visibility into max_steps configuration
   - No token budget settings visible
   - Cannot see what limits are enforced
   - Forced to implement application-level limits as workaround

### Client's Proposed Workaround

Since they can't rely on Emergent's limits (don't know what they are), they're implementing self-imposed limits:

```
Janitor Agent (hourly: 0 * * * *)
│
├─ Phase 1: Fix Approved (7 min, 12 calls)
│  └─ Execute 3 MaintenanceIssues max
│
├─ Phase 2: Detect New (7 min, 8 calls)
│  └─ Create 5 MaintenanceIssues max
│
└─ Phase 3: Report & Exit (1 min)
   └─ Total: ~15 minutes, 20 tool calls
```

This is **good engineering practice** but shouldn't be necessary due to lack of visibility.

## Investigation Results

### What Backend Has (But Frontend Doesn't Expose)

✅ **Step Limits** (`apps/server-go/domain/agents/executor.go:255-267`):

- Global default: 500 steps
- Per-definition `maxSteps` configuration
- Per-run tracking in database
- Automatic pause when limit reached
- Resume capability

✅ **Timeout Enforcement** (`executor.go:187-192`):

- Per-definition `defaultTimeout` (seconds)
- Per-execution timeout override
- Context-based cancellation

✅ **Token Limits** (`entity.go:201`):

- Configurable via `model.maxTokens`
- Applied to LLM generation config

✅ **Stuck Run Detection** (`executor.go:701-748`):

- Doom loop detector (consecutive identical tool calls)
- Warn after 3 identical calls, stop after 5
- Step limit enforcement
- Timeout enforcement

✅ **Comprehensive Metrics** (Database):

```sql
kb.agent_runs:
  - step_count (INT)
  - max_steps (INT, nullable)
  - duration_ms (INT)
  - parent_run_id (UUID, nullable)
  - resumed_from (UUID, nullable)

kb.agent_run_messages:
  - Full conversation history
  - Role, content, step_number

kb.agent_run_tool_calls:
  - Every tool invocation
  - Input/output, duration_ms
  - Status (completed/error)
```

### What's Missing

❌ **Frontend Type Definitions** (`apps/admin/src/api/agents.ts:96-106`):

```typescript
export interface AgentRun {
  id: string;
  agentId: string;
  status: 'running' | 'success' | 'completed' | 'error' | 'failed' | 'skipped';
  startedAt: string;
  completedAt: string | null;
  durationMs: number | null;
  summary: Record<string, any> | null;
  errorMessage: string | null;
  skipReason: string | null;
  // ❌ MISSING: stepCount, maxSteps, parentRunId, resumedFrom
}
```

**Backend DTO includes these** (`dto.go:27-43`):

```go
type AgentRunDTO struct {
    // ... existing fields ...
    ParentRunID *string `json:"parentRunId,omitempty"`
    StepCount   int     `json:"stepCount"`
    MaxSteps    *int    `json:"maxSteps,omitempty"`
    ResumedFrom *string `json:"resumedFrom,omitempty"`
}
```

❌ **No Messages/Tool Calls Endpoints:**

- Backend has the data in database
- No HTTP endpoints to retrieve:
  - `/api/projects/:projectId/agent-runs/:runId/messages`
  - `/api/projects/:projectId/agent-runs/:runId/tool-calls`

❌ **No Run Cancellation Endpoint:**

- `CancelRun()` exists in repository but no HTTP endpoint
- Cannot manually stop a running agent via API

❌ **No Token Usage Tracking:**

- Only max output tokens configured
- No actual input+output token tracking
- No cumulative token usage metrics
- No cost estimation

❌ **No Real-Time Progress:**

- No streaming progress updates during execution
- No live step count visibility while running
- All metrics only available after completion

❌ **No Agent Definition Configuration UI:**

- `maxSteps` and `defaultTimeout` exist in backend
- No way to set them in frontend
- No visibility into current settings

## Impact

### For Users

1. **Reduced Trust:**

   - Runs are a "black box"
   - Can't tell if agent is making progress or stuck
   - Forced to implement redundant safeguards

2. **Poor Debugging Experience:**

   - Can't see what agent did during run
   - No tool call history to review
   - Error messages lack context

3. **No Cost Control:**

   - Can't track token usage
   - No budget alerts
   - No cost estimation before runs

4. **Stuck Runs:**
   - No way to cancel via API
   - Have to wait for timeout (unknown duration)
   - Wastes resources

### For Emergent

1. **Increased Support Load:**

   - Users asking "how long will it run?"
   - "What are the limits?"
   - "How do I stop it?"

2. **Feature Perception:**

   - Backend has great features, but invisible
   - Looks less capable than competitors

3. **Client Workarounds:**
   - Adding application-level limits
   - Implementing own progress tracking
   - Duplicate effort

## Reproduction

1. Create any agent that runs for >1 minute
2. Trigger it via API
3. Try to:
   - See progress (step count) → ❌ Not visible
   - View tool calls → ❌ Not available
   - Cancel the run → ❌ No API endpoint
   - Check limits → ❌ Not exposed

## Recommended Fixes

### Phase 1: Expose Existing Backend Features (High Priority)

#### 1.1 Update Frontend Types

**File:** `apps/admin/src/api/agents.ts`

```typescript
export interface AgentRun {
  id: string;
  agentId: string;
  status: 'running' | 'success' | 'error' | 'paused' | 'cancelled' | 'skipped';
  startedAt: string;
  completedAt: string | null;
  durationMs: number | null;
  summary: Record<string, any> | null;
  errorMessage: string | null;
  skipReason: string | null;

  // ✅ Add these from backend DTO:
  stepCount: number;
  maxSteps: number | null;
  parentRunId: string | null;
  resumedFrom: string | null;
}

export interface AgentRunMessage {
  id: string;
  runId: string;
  role: 'system' | 'user' | 'assistant' | 'tool_result';
  content: Record<string, any>;
  stepNumber: number;
  createdAt: string;
}

export interface AgentRunToolCall {
  id: string;
  runId: string;
  messageId: string | null;
  toolName: string;
  input: Record<string, any>;
  output: Record<string, any>;
  status: 'completed' | 'error';
  durationMs: number;
  stepNumber: number;
  createdAt: string;
}
```

**Estimate:** 30 minutes

#### 1.2 Add Messages & Tool Calls Endpoints

**File:** `apps/server-go/domain/agents/handler.go`

Add handlers for:

- `GET /api/projects/:projectId/agent-runs/:runId/messages`
- `GET /api/projects/:projectId/agent-runs/:runId/tool-calls`

**Estimate:** 2 hours

#### 1.3 Add Run Cancellation Endpoint

**File:** `apps/server-go/domain/agents/handler.go`

```go
// POST /api/admin/agents/:id/runs/:runId/cancel
func (h *Handler) CancelRun(c echo.Context) error {
    agentID := c.Param("id")
    runID := c.Param("runId")

    // Verify agent ownership via project context
    // Call h.repo.CancelRun(ctx, runID)
    // Return success response
}
```

**Estimate:** 1 hour

#### 1.4 Update Frontend API Client

**File:** `apps/admin/src/api/agents.ts`

```typescript
export interface AgentsClient {
  // ... existing methods ...

  getRunMessages(projectId: string, runId: string): Promise<AgentRunMessage[]>;
  getRunToolCalls(
    projectId: string,
    runId: string
  ): Promise<AgentRunToolCall[]>;
  cancelRun(agentId: string, runId: string): Promise<void>;
}
```

**Estimate:** 1 hour

**Total Phase 1:** ~5 hours

### Phase 2: Agent Configuration UI (Medium Priority)

#### 2.1 Update Agent Definition Types

**File:** `apps/admin/src/api/agents.ts`

```typescript
export interface ModelConfig {
  name: string;
  temperature?: number;
  maxTokens?: number; // ✅ Already exists in backend
}

export interface Agent {
  // ... existing fields ...

  // ✅ Add configuration options:
  maxSteps?: number;
  defaultTimeout?: number; // seconds
  model?: ModelConfig;
}
```

**Estimate:** 30 minutes

#### 2.2 Add Configuration Fields to Agent Form

Show in agent creation/edit UI:

- Max Steps (with explanation: "Limit total LLM turns per run, default: 500")
- Default Timeout (with explanation: "Maximum execution time in seconds, default: none")
- Max Tokens (with explanation: "Maximum tokens per LLM response, default: model limit")

**Estimate:** 3 hours

**Total Phase 2:** ~4 hours

### Phase 3: Enhanced Visibility (Medium Priority)

#### 3.1 Run Details Page

Create `/agents/:agentId/runs/:runId` page showing:

- Basic info (status, duration, started/completed times)
- Progress: `stepCount / maxSteps` with progress bar
- Messages table (role, content preview, step number)
- Tool calls table (name, duration, status, expand for I/O)
- Action buttons: Cancel (if running), Resume (if paused)

**Estimate:** 8 hours

#### 3.2 Agent Runs List Enhancement

Update agents runs table to show:

- Step count (e.g., "42 / 500 steps")
- Duration with step rate (e.g., "5m 23s (8 steps/min)")
- Visual indicator for paused/stuck runs
- Quick action: Cancel running, Resume paused

**Estimate:** 4 hours

**Total Phase 3:** ~12 hours

### Phase 4: Token Usage & Cost Tracking (Low Priority)

#### 4.1 Database Schema Updates

Add to `kb.agent_runs`:

```sql
ALTER TABLE kb.agent_runs
  ADD COLUMN total_input_tokens INT DEFAULT 0,
  ADD COLUMN total_output_tokens INT DEFAULT 0,
  ADD COLUMN estimated_cost_usd DECIMAL(10,4);
```

**Estimate:** 1 hour (including migration)

#### 4.2 Token Tracking in Executor

Update executor callbacks to extract and sum token usage from LLM responses.

**Estimate:** 3 hours

#### 4.3 Cost Estimation

Add cost calculator based on model and token usage.

**Estimate:** 2 hours

**Total Phase 4:** ~6 hours

### Phase 5: Real-Time Progress (Low Priority)

#### 5.1 Server-Sent Events Endpoint

```go
GET /api/projects/:projectId/agent-runs/:runId/progress
```

Stream progress updates as SSE:

```
event: step
data: {"stepCount": 5, "maxSteps": 500, "status": "running"}

event: tool_call
data: {"toolName": "spec_list_files", "status": "completed", "durationMs": 234}

event: completed
data: {"status": "success", "stepCount": 42, "durationMs": 15234}
```

**Estimate:** 6 hours

#### 5.2 Frontend Progress Component

Real-time progress bar with live step count and tool calls.

**Estimate:** 4 hours

**Total Phase 5:** ~10 hours

## Total Effort Estimate

| Phase     | Description                       | Priority | Effort  |
| --------- | --------------------------------- | -------- | ------- |
| 1         | Expose existing backend features  | High     | 5h      |
| 2         | Agent configuration UI            | Medium   | 4h      |
| 3         | Enhanced visibility (run details) | Medium   | 12h     |
| 4         | Token usage & cost tracking       | Low      | 6h      |
| 5         | Real-time progress                | Low      | 10h     |
| **Total** |                                   |          | **37h** |

## Minimal Viable Fix (Phase 1 Only)

To address immediate client needs:

1. Update frontend types to match backend DTOs (30 min)
2. Add messages/tool-calls endpoints (2h)
3. Add cancellation endpoint (1h)
4. Update frontend API client (1h)

**Total: ~5 hours** to make backend features visible.

## Client Feedback Questions

From client's investigation section:

> Contact Emergent team to understand:
>
> - What are the actual server-side limits?
> - How to access run metrics (tokens, steps)?
> - Why do runs get stuck in "running" state?
> - Can we configure timeout/token budgets?

**Answers:**

1. **Server-side limits:**

   - Default max steps: 500
   - Configurable per agent via `maxSteps` (not exposed in UI yet)
   - Timeout: configurable via `defaultTimeout` (not exposed in UI yet)
   - Doom loop detector: stops after 5 consecutive identical tool calls

2. **How to access run metrics:**

   - ✅ Backend tracks: steps, duration, messages, tool calls
   - ❌ Frontend doesn't expose messages/tool-calls endpoints yet
   - ❌ Token usage not tracked yet
   - **Fix:** Phase 1 (5 hours) will expose these

3. **Why runs get stuck:**

   - If `defaultTimeout` not set, runs could theoretically run forever
   - Step limit (500) should pause runs, not leave in "running"
   - Possible bug: timeout context not canceling properly
   - **Fix:** Add cancellation endpoint, investigate timeout bug

4. **Can we configure timeout/token budgets:**
   - ✅ Yes, backend supports `defaultTimeout` and `model.maxTokens`
   - ❌ Not exposed in UI yet
   - **Fix:** Phase 2 (4 hours) will add configuration UI

## Related Issues

- Frontend type definitions lag behind backend DTOs
- No agent configuration UI for advanced settings
- Token usage tracking not implemented
- Real-time progress not available

## References

- Backend executor: `apps/server-go/domain/agents/executor.go`
- Backend DTOs: `apps/server-go/domain/agents/dto.go`
- Frontend types: `apps/admin/src/api/agents.ts`
- Database schema: `apps/server-go/migrations/00018_create_agents.sql`
