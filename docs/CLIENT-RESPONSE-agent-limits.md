# Response to Agent Execution Limits Feedback

**Date:** 2026-02-16  
**Re:** Janitor Agent Development - Execution Limits & Visibility

Dear Client,

Thank you for the comprehensive feedback on your janitor agent development experience. Your detailed analysis of the 11-minute run mystery and stuck runs has been extremely valuable. I've investigated thoroughly and have **excellent news**: the backend already has most features you need, and I've now made them fully accessible via the API.

## TL;DR: What's Fixed

✅ **Step limits** - Configurable, auto-pause, resumable  
✅ **Timeout enforcement** - Configurable, bug fixed  
✅ **Token limits** - Configurable per agent  
✅ **Run cancellation** - NEW API endpoint added  
✅ **Full visibility** - Messages, tool calls, metrics all via API  
✅ **Configuration** - Set limits via API endpoints

❌ **No SQL required** - Everything manageable through the API

## Answers to Your Questions

### 1. What are the actual server-side limits?

**Step Limits:**

- **Default:** 500 steps (cumulative across resumes)
- **Configurable:** Via agent definition API
- **Behavior:** Auto-pause when limit reached (not failed!)
- **Resumable:** Yes, from paused state

**Timeout:**

- **Default:** None (use step limit as safety net)
- **Configurable:** Via agent definition API (seconds)
- **Enforcement:** Context-based cancellation (bug now fixed)

**Token Limits:**

- **Configurable:** Via agent definition `model.maxTokens`
- **Applied:** Max output tokens per LLM response

**Doom Loop Detection:**

- **Warns:** After 3 consecutive identical tool calls
- **Stops:** After 5 consecutive identical tool calls

### 2. How to access run metrics?

**All available via API:**

```bash
# Get run with metrics
GET /api/projects/{projectId}/agent-runs/{runId}
# Returns: stepCount, maxSteps, status, duration, etc.

# Get conversation history
GET /api/projects/{projectId}/agent-runs/{runId}/messages
# Returns: Full message history with roles and content

# Get tool calls
GET /api/projects/{projectId}/agent-runs/{runId}/tool-calls
# Returns: All tool invocations with I/O and timing

# List all runs with filtering
GET /api/projects/{projectId}/agent-runs?status=running&limit=20
# Returns: Paginated list with filters
```

### 3. Why do runs get stuck in "running" state?

**Root Cause Found & Fixed:**

The timeout context wasn't being properly checked after the agent run completed. Even though timeouts were configured, context cancellation errors weren't being detected.

**Fixes Applied:**

1. Added explicit context check after agent execution loop
2. Added proactive context check before each LLM call
3. Proper error messages when timeout occurs

**Result:** Runs will NO LONGER get stuck indefinitely. They will properly fail with a timeout error message.

### 4. Can we configure timeout/token budgets?

**Yes! Everything is configurable via API.**

## Complete API Guide for Client

### Configuration: Set Limits on Your Agent

```bash
# Get current agent definition (to see current limits)
curl https://api.dev.emergent-company.ai/api/admin/agent-definitions/{definitionId} \
  -H "Authorization: Bearer $TOKEN"

# Response shows current configuration:
{
  "data": {
    "id": "...",
    "name": "Janitor Agent",
    "maxSteps": null,              # Currently unlimited
    "defaultTimeout": null,        # Currently no timeout
    "model": {
      "name": "claude-3-5-sonnet-20241022",
      "maxTokens": null            # Currently using model default
    }
  }
}

# Update agent definition with limits
curl -X PATCH https://api.dev.emergent-company.ai/api/admin/agent-definitions/{definitionId} \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "maxSteps": 100,                    # Limit to 100 steps
    "defaultTimeout": 1200,             # 20 minutes max
    "model": {
      "name": "claude-3-5-sonnet-20241022",
      "maxTokens": 4096                 # 4K tokens per response
    }
  }'

# Response confirms update:
{
  "success": true,
  "data": {
    "maxSteps": 100,
    "defaultTimeout": 1200,
    "model": {"maxTokens": 4096}
  }
}
```

### Monitoring: Check Running Agents

```bash
# List all running agents for your project
curl "https://api.dev.emergent-company.ai/api/projects/{projectId}/agent-runs?status=running" \
  -H "Authorization: Bearer $TOKEN"

# Response:
{
  "success": true,
  "data": {
    "items": [
      {
        "id": "run-123",
        "agentId": "agent-456",
        "status": "running",
        "stepCount": 42,
        "maxSteps": 100,
        "startedAt": "2026-02-16T10:00:00Z",
        "durationMs": 350000  # 5.8 minutes
      }
    ],
    "totalCount": 1
  }
}

# Find potentially stuck runs (client-side filter)
# Just check if durationMs > acceptable threshold (e.g., 1 hour = 3600000ms)
```

### Debugging: View Run Details

```bash
# Get full run details
curl https://api.dev.emergent-company.ai/api/projects/{projectId}/agent-runs/{runId} \
  -H "Authorization: Bearer $TOKEN"

# Get conversation history
curl https://api.dev.emergent-company.ai/api/projects/{projectId}/agent-runs/{runId}/messages \
  -H "Authorization: Bearer $TOKEN"

# Response shows full conversation:
{
  "data": [
    {
      "id": "msg-1",
      "role": "user",
      "content": {"text": "Execute janitor tasks"},
      "stepNumber": 0,
      "createdAt": "..."
    },
    {
      "id": "msg-2",
      "role": "assistant",
      "content": {"text": "I'll start by listing maintenance issues..."},
      "stepNumber": 1,
      "createdAt": "..."
    }
  ]
}

# Get tool call history
curl https://api.dev.emergent-company.ai/api/projects/{projectId}/agent-runs/{runId}/tool-calls \
  -H "Authorization: Bearer $TOKEN"

# Response shows all tool invocations:
{
  "data": [
    {
      "toolName": "spec_list_maintenance_issues",
      "input": {"limit": 5},
      "output": {"issues": [...]},
      "status": "completed",
      "durationMs": 234,
      "stepNumber": 2
    }
  ]
}
```

### Emergency: Cancel Stuck Runs

```bash
# Cancel a specific run
curl -X POST https://api.dev.emergent-company.ai/api/admin/agents/{agentId}/runs/{runId}/cancel \
  -H "Authorization: Bearer $TOKEN"

# Response:
{
  "success": true,
  "data": {
    "message": "Run cancelled successfully",
    "runId": "run-123"
  }
}
```

## Your Proposed Architecture: Perfect!

Your two-phase bounded workflow is **exactly the right approach**:

```
Janitor Agent (hourly: 0 * * * *)
├─ Phase 1: Fix Approved (7 min, 12 calls)
│  └─ Execute 3 MaintenanceIssues max
├─ Phase 2: Detect New (7 min, 8 calls)
│  └─ Create 5 MaintenanceIssues max
└─ Phase 3: Report & Exit (1 min)
   └─ Total: ~15 minutes, 20 tool calls
```

**Recommended Backend Safety Limits:**

```bash
curl -X PATCH .../agent-definitions/{id} \
  -d '{
    "maxSteps": 100,         # 20 expected, 100 for safety (5x buffer)
    "defaultTimeout": 1800,  # 30 minutes (15 expected, 2x buffer)
    "model": {
      "maxTokens": 4096
    }
  }'
```

This gives you:

- ✅ Application-level bounded work (your design)
- ✅ Platform-level safety nets (our enforcement)
- ✅ Clear visibility into progress
- ✅ Emergency stop capability

## Frontend API Client (TypeScript)

If you're using our TypeScript client:

```typescript
import { createAgentsClient } from '@/api/agents';

const client = createAgentsClient(apiBase, fetchJson);

// Set limits
await client.updateDefinition(definitionId, {
  maxSteps: 100,
  defaultTimeout: 1800,
  model: { maxTokens: 4096 },
});

// Monitor runs
const runs = await client.listProjectRuns(projectId, {
  status: 'running',
  limit: 20,
});

// Find stuck runs (client-side)
const stuckRuns = runs.items.filter((run) => {
  const durationMinutes = (run.durationMs || 0) / 60000;
  return durationMinutes > 30; // Running > 30 minutes
});

// Cancel stuck runs
for (const run of stuckRuns) {
  await client.cancelRun(run.agentId, run.id);
}

// View run details
const messages = await client.getRunMessages(projectId, runId);
const toolCalls = await client.getRunToolCalls(projectId, runId);
```

## What's New in Phase 1

### API Endpoints Added

- ✅ `POST /api/admin/agents/:id/runs/:runId/cancel` - Cancel runs
- ✅ `GET /api/projects/:projectId/agent-runs` - List with filtering (already existed)
- ✅ `GET /api/projects/:projectId/agent-runs/:runId/messages` - Messages (already existed)
- ✅ `GET /api/projects/:projectId/agent-runs/:runId/tool-calls` - Tool calls (already existed)
- ✅ `GET /api/admin/agent-definitions/:id` - Get configuration (already existed)
- ✅ `PATCH /api/admin/agent-definitions/:id` - Update limits (already existed)

### Frontend Types Updated

- ✅ `AgentRun` now includes: `stepCount`, `maxSteps`, `parentRunId`, `resumedFrom`
- ✅ New types: `AgentRunMessage`, `AgentRunToolCall`
- ✅ New types: `AgentDefinition`, `ModelConfig`
- ✅ New methods: `cancelRun()`, `getRunMessages()`, `getRunToolCalls()`, etc.

### Backend Bugs Fixed

- ✅ Timeout enforcement now works properly
- ✅ Stuck runs will be marked as errors with timeout message
- ✅ Context cancellation properly detected

## No SQL Access Required

Everything is manageable through the API. The SQL scripts in `scripts/sql/agent-monitoring-helpers.sql` are **for internal debugging only** - you should never need to touch the database directly.

## Workflow: How to Use These Features

### 1. Initial Setup (One Time)

```bash
# Set limits on your janitor agent definition
curl -X PATCH .../agent-definitions/{definitionId} \
  -d '{
    "maxSteps": 100,
    "defaultTimeout": 1800,
    "model": {"maxTokens": 4096}
  }'
```

### 2. Monitoring (As Needed)

```typescript
// Check for stuck runs
const runs = await client.listProjectRuns(projectId, { status: 'running' });
const longRunning = runs.items.filter((r) => (r.durationMs || 0) > 1800000);

if (longRunning.length > 0) {
  console.warn(`Found ${longRunning.length} runs exceeding 30 minutes`);
}
```

### 3. Debugging Failed Runs

```typescript
// Get failed run details
const run = await client.getProjectRun(projectId, runId);
console.log(`Failed after ${run.stepCount} steps: ${run.errorMessage}`);

// View what happened
const messages = await client.getRunMessages(projectId, runId);
const toolCalls = await client.getRunToolCalls(projectId, runId);

// Check for doom loop
const consecutiveCalls = toolCalls.reduce((acc, call, i) => {
  if (i > 0 && call.toolName === toolCalls[i - 1].toolName) {
    acc[call.toolName] = (acc[call.toolName] || 1) + 1;
  }
  return acc;
}, {});
```

### 4. Emergency Stop

```bash
# Cancel a run immediately
curl -X POST .../agents/{agentId}/runs/{runId}/cancel
```

## Next Steps

### This Week (Phase 1 Complete)

- ✅ All API endpoints functional
- ✅ Timeout bug fixed
- ✅ Frontend types updated
- ✅ Full visibility via API

### Next Week (Phase 2 - Optional)

- ⏳ UI for setting limits (no manual API calls needed)
- ⏳ Run details page with progress visualization
- ⏳ Cancel buttons in UI

**You can proceed with your janitor agent now** - everything you need is available via API!

## Summary

**What you can do right now (all via API):**

✅ Configure step limits, timeouts, token limits  
✅ List all running agents  
✅ View run progress (step count)  
✅ Get conversation history  
✅ Get tool call details  
✅ Cancel stuck runs  
✅ Filter runs by status

**What's automatic:**

✅ Timeout enforcement  
✅ Step limit auto-pause  
✅ Doom loop detection  
✅ Comprehensive metrics tracking

**What you don't need:**

❌ SQL access  
❌ Database queries  
❌ Manual cleanup scripts

Everything is handled through our REST API.

## Questions?

If you have any questions or need help with API integration, please let me know. All endpoints are documented and ready to use.

**— Emergent Team**

---

## API Quick Reference

| Operation         | Endpoint                                                | Method |
| ----------------- | ------------------------------------------------------- | ------ |
| **Configuration** |
| Get definition    | `/api/admin/agent-definitions/:id`                      | GET    |
| Update limits     | `/api/admin/agent-definitions/:id`                      | PATCH  |
| **Monitoring**    |
| List runs         | `/api/projects/:projectId/agent-runs?status=running`    | GET    |
| Get run details   | `/api/projects/:projectId/agent-runs/:runId`            | GET    |
| **Debugging**     |
| Get messages      | `/api/projects/:projectId/agent-runs/:runId/messages`   | GET    |
| Get tool calls    | `/api/projects/:projectId/agent-runs/:runId/tool-calls` | GET    |
| **Control**       |
| Cancel run        | `/api/admin/agents/:id/runs/:runId/cancel`              | POST   |
