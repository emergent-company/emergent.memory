# Agent Queue Explosion Investigation - 2026-03-18

## Problem Summary

The mcj-emergent server had 29,369 pending jobs in the `kb.agent_run_jobs` table, all failing with 429 rate limit errors but continuously retrying, creating an infinite loop.

## Root Causes Identified

### 1. **Cron Scheduler Creates New Jobs Regardless of Existing Queue** ❌

**File:** `apps/server/domain/agents/triggers.go:189`

The cron scheduler (`registerCronTrigger`) creates a new agent run every time the cron fires, with **no check** for:
- Whether the previous run is still pending
- Whether the agent is currently failing
- Whether there are already pending jobs for this agent

```go
err := ts.scheduler.AddCronTask(taskName, agent.CronSchedule, func(ctx context.Context) error {
    return ts.executeTriggeredAgent(ctx, agentID, projectID)  // ← Always executes
})
```

The `e2e-orchestrator` project had a cron of `0 * * * * *` (every minute), so it queued a new run every 60 seconds regardless of failures.

**Impact:** 20,565 pending orchestrator jobs accumulated over time.

---

### 2. **Child Agents Re-enqueue Parent Agents, Creating Exponential Growth** ❌

**File:** `apps/server/domain/agents/worker_pool.go:233-287`

When a child agent completes (or fails), it automatically re-enqueues its parent agent with the result:

```go
func (p *WorkerPool) reenqueueParent(ctx context.Context, log *slog.Logger, run *AgentRun, ...) {
    // ...
    _, err = p.repo.CreateRunQueued(ctx, parentRun.AgentID, 1, CreateRunQueuedOptions{
        TriggerMessage:  &triggerMsg,
        ParentRunID:     parentRun.ParentRunID,
        TriggerMetadata: parentRun.TriggerMetadata,
    })
    // ← Creates a new queued run for the parent, even if parent already has pending runs
}
```

This is called **on both success AND error** (line 160, 183).

**Impact:** 
- Orchestrator spawns review-manager, coding-manager, etc.
- Each child fails with 429
- Each child re-enqueues orchestrator
- Orchestrator gets re-queued multiple times per failed run
- Result: 7,069 review-manager jobs, 825 coding-manager jobs, etc.

---

### 3. **Jobs Retry Up to maxAttempts, But Then Parent Re-queues Them Anyway** ⚠️

**File:** `apps/server/domain/agents/worker_pool.go:154-161`

When a job fails:
```go
requeue := job.AttemptCount < job.MaxAttempts  // ← Requeues up to maxAttempts
nextRunAt := time.Now().Add(backoff(job.AttemptCount))
if err := p.repo.FailJob(ctx, job.ID, job.RunID, execErr.Error(), requeue, nextRunAt); err != nil {
    // ...
}
p.reenqueueParent(ctx, log, run, agent.Name, "", "error")  // ← Also wakes parent
```

The job retries up to `maxAttempts` (default 1), but **then the parent gets woken anyway** and can re-trigger the same failing child.

---

### 4. **429 Rate Limit Errors Are Treated as Transient, Not Permanent** ⚠️

**File:** `apps/server/domain/agents/executor.go:1097-1110`

The executor has retry logic for 429 errors:

```go
if strings.Contains(errStr, "429") || 
   strings.Contains(errStr, "RESOURCE_EXHAUSTED") ||
   strings.Contains(errStr, "spending cap") {
    // Retry with 5s delay
    time.Sleep(5 * time.Second)
    continue  // ← Retry immediately
}
```

**But:** It retries up to 5 times **within the same run**, then fails the run. The run then gets re-queued by the parent or cron scheduler, starting the cycle again.

**Expected behavior:** If a spending cap error persists after retries, the agent should:
- Mark itself as disabled
- Stop accepting new cron triggers
- Not re-enqueue itself or its parent

---

### 5. **Budget Mechanism Only Sends Alerts, Does Not Enforce Limits** ❌

**File:** `apps/server/domain/provider/usage_service.go:145-245`

The budget system (`checkBudget`) only:
- Checks if spend > threshold
- Sends a notification
- **Does NOT prevent further API calls**

```go
func (s *UsageService) checkBudget(ctx context.Context, projectID string) {
    // 1. Fetch budget
    // 2. Calculate spend
    // 3. Send notification if threshold crossed
    // ← NO enforcement, NO blocking
}
```

**Impact:** Even if you set a budget, agents continue making API calls until the **provider** (Vertex AI) enforces the spending cap with 429 errors.

---

## Immediate Fix (Completed)

✅ Deleted all 29,369 pending jobs:
```sql
DELETE FROM kb.agent_run_jobs WHERE status = 'pending';
```

---

## Required Fixes

### High Priority

1. **Prevent Cron from Queuing Duplicate Runs**
   - Before creating a new run, check if there's already a pending/running job for this agent
   - Skip cron execution if agent has pending work
   - Location: `apps/server/domain/agents/triggers.go:189`

2. **Prevent Parent Re-enqueue if Child Repeatedly Fails**
   - Track consecutive failures for child → parent chains
   - After N consecutive failures (e.g., 3), disable the agent or break the chain
   - Location: `apps/server/domain/agents/worker_pool.go:233`

3. **Treat Spending Cap Errors as Permanent**
   - If a 429 "spending cap exceeded" error persists after retries, mark the agent as disabled
   - Do not re-enqueue
   - Location: `apps/server/domain/agents/executor.go:1097`

4. **Enforce Budget Limits (Not Just Alerts)**
   - Before making an LLM call, check if project is over budget
   - Return early with a budget error instead of calling the provider
   - Location: `apps/server/domain/provider/usage_service.go` (new function)

### Medium Priority

5. **Add Queue Depth Check**
   - Before creating a new queued run, count existing pending jobs for this agent
   - Reject if count > threshold (e.g., 10)
   - Location: `apps/server/domain/agents/repository.go:1645`

6. **Add Job Age Limit**
   - Automatically fail jobs that have been pending for > X hours (e.g., 24h)
   - Prevents stale jobs from accumulating
   - Location: New background job in `apps/server/domain/agents/worker_pool.go`

---

## Data from mcj-emergent

All 29,369 jobs were from the `e2e-orchestrator-1773568814007` project:

| Agent | Pending Jobs |
|-------|--------------|
| orchestrator | 20,565 |
| review-manager | 7,069 |
| coding-manager | 825 |
| reviewer | 566 |
| janitor | 162 |
| research-manager | 70 |
| designer | 64 |
| code-researcher | 34 |
| python-coder | 10 |
| web-researcher | 2 |

The orchestrator cron: `0 * * * * *` (every minute)
Status: Disabled (but jobs were already queued)

---

## Questions for User

1. Should the budget enforcement be **hard** (block API calls) or **soft** (continue with warnings)?
2. Should cron-triggered agents auto-disable after N consecutive failures?
3. What's an acceptable queue depth per agent before rejecting new runs?
4. Should we backfill detection for spending cap errors and auto-disable affected agents?
