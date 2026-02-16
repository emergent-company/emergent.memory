# Investigation: Stuck Runs & Timeout Issues

**Date:** 2026-02-16  
**Status:** Investigation Complete  
**Priority:** Medium

## Problem Statement

Client reported runs getting "stuck" in `running` state for 1+ hours with no apparent timeout enforcement, despite backend having timeout configuration support.

## Investigation Findings

### 1. Timeout Configuration Exists

**Code:** `apps/server-go/domain/agents/executor.go:187-192`

```go
// Apply timeout if specified
if req.Timeout != nil && *req.Timeout > 0 {
    var cancel context.CancelFunc
    ctx, cancel = context.WithTimeout(ctx, *req.Timeout)
    defer cancel()
}
```

**How it works:**

- `defaultTimeout` can be set on `AgentDefinition` (in seconds)
- Timeout is applied via `context.WithTimeout()`
- Context cancellation should propagate to `r.Run()` call

### 2. Potential Issue: Context Cancellation Not Detected

**Code:** `apps/server-go/domain/agents/executor.go:376-401`

```go
for event, eventErr := range r.Run(ctx, "system", sess.ID(), userContent, agent.RunConfig{}) {
    if eventErr != nil {
        steps := tracker.current()
        _ = ae.repo.FailRunWithSteps(ctx, run.ID, eventErr.Error(), steps)
        return &ExecuteResult{
            RunID:    run.ID,
            Status:   RunStatusError,
            Summary:  map[string]any{"error": eventErr.Error()},
            Steps:    steps,
            Duration: time.Since(startTime),
        }, nil
    }

    // ... process event ...
}
```

**Problem:**

- If `r.Run()` doesn't properly handle context cancellation, it may continue running even after timeout
- The ADK (Agent Development Kit) `r.Run()` might not be respecting the context cancellation
- No explicit check for `ctx.Err() == context.DeadlineExceeded` after the loop

### 3. Missing Context Check

**What's missing:**

```go
// After r.Run() completes, should check if context was cancelled
for event, eventErr := range r.Run(ctx, ...) {
    // ... existing code ...
}

// ❌ MISSING: Check if we exited due to timeout
if ctx.Err() != nil {
    steps := tracker.current()
    errMsg := "Run cancelled: " + ctx.Err().Error()
    _ = ae.repo.FailRunWithSteps(ctx, run.ID, errMsg, steps)
    return &ExecuteResult{
        RunID:    run.ID,
        Status:   RunStatusError,
        Summary:  map[string]any{"error": errMsg, "reason": "timeout"},
        Steps:    steps,
        Duration: time.Since(startTime),
    }, nil
}
```

### 4. ADK Context Handling

The issue might also be in the ADK's `r.Run()` implementation:

- If ADK doesn't check context cancellation between tool calls
- If ADK doesn't propagate context to tool executions
- If ADK's event stream doesn't respect context cancellation

**Need to verify:** Does Google's ADK properly handle `context.WithTimeout()`?

## Root Cause Hypothesis

**Most Likely:** ADK's `r.Run()` method may not be properly checking the context during long-running operations, especially:

1. During LLM API calls (if Vertex AI client doesn't respect context)
2. During tool execution (if tools don't check context)
3. Between agent turns (no periodic context check)

**Secondary:** Even if ADK eventually returns an error, our code doesn't check `ctx.Err()` after the event loop, so timeout errors might not be caught.

## Evidence Needed

To confirm the root cause, we need:

1. **Check ADK source code:**

   - Does `agent.Runner.Run()` check context cancellation?
   - Does it propagate context to LLM calls?
   - Does it propagate context to tool calls?

2. **Test timeout enforcement:**

   - Create agent with `defaultTimeout: 60` (1 minute)
   - Trigger agent with a task that takes >1 minute
   - Check if run is marked as error/cancelled after 1 minute
   - Check database for timeout error message

3. **Add logging:**
   - Log when timeout context is created
   - Log when context.Err() is not nil
   - Track if ADK returns an error when context is cancelled

## Recommended Fixes

### Fix 1: Add Explicit Context Check (Immediate)

**File:** `apps/server-go/domain/agents/executor.go:401`

```go
for event, eventErr := range r.Run(ctx, "system", sess.ID(), userContent, agent.RunConfig{}) {
    // ... existing code ...
}

// ✅ ADD: Explicit context check after run completes
if ctx.Err() != nil {
    steps := tracker.current()
    errMsg := "Run cancelled"
    reason := "unknown"

    if ctx.Err() == context.DeadlineExceeded {
        reason = "timeout"
        errMsg = "Run cancelled: timeout exceeded"
    } else if ctx.Err() == context.Canceled {
        reason = "cancelled"
        errMsg = "Run cancelled: context cancelled"
    }

    ae.log.Warn("run cancelled by context",
        slog.String("run_id", run.ID),
        slog.String("reason", reason),
        slog.Int("steps", steps),
    )

    _ = ae.repo.FailRunWithSteps(ctx, run.ID, errMsg, steps)
    return &ExecuteResult{
        RunID:    run.ID,
        Status:   RunStatusError,
        Summary:  map[string]any{"error": errMsg, "reason": reason},
        Steps:    steps,
        Duration: time.Since(startTime),
    }, nil
}

// Continue with existing success path...
```

**Estimate:** 30 minutes

### Fix 2: Add Periodic Context Check in Callbacks (Medium Priority)

**File:** `apps/server-go/domain/agents/executor.go:252-275`

```go
beforeModelCb := func(cbCtx agent.CallbackContext, llmReq *model.LLMRequest) (*model.LLMResponse, error) {
    // ✅ ADD: Check if context was cancelled
    if ctx.Err() != nil {
        ae.log.Warn("context cancelled, stopping agent",
            slog.String("run_id", run.ID),
            slog.String("reason", ctx.Err().Error()),
        )
        return nil, fmt.Errorf("agent stopped: %w", ctx.Err())
    }

    currentStep := tracker.increment()

    // ... existing code ...
}
```

**Estimate:** 15 minutes

### Fix 3: Propagate Context to Tool Calls (If Needed)

Check if tools respect context cancellation. If not, may need to wrap tool execution.

**Estimate:** 1-2 hours (requires investigation)

### Fix 4: Add Timeout Monitoring & Auto-Cleanup Job

Create a background job that:

1. Finds runs in `running` state for > 2 hours
2. Marks them as `error` with message "exceeded maximum runtime"
3. Runs every 15 minutes

**File:** `apps/server-go/domain/agents/cleanup.go` (new file)

```go
package agents

import (
    "context"
    "time"
)

// CleanupStuckRuns marks runs stuck in running state as failed
func (r *Repository) CleanupStuckRuns(ctx context.Context, maxDuration time.Duration) (int, error) {
    threshold := time.Now().Add(-maxDuration)

    res, err := r.db.NewUpdate().
        Model((*AgentRun)(nil)).
        Set("status = ?", RunStatusError).
        Set("completed_at = ?", time.Now()).
        Set("error_message = ?", "Run exceeded maximum runtime and was automatically cancelled").
        Where("status = ?", RunStatusRunning).
        Where("started_at < ?", threshold).
        Exec(ctx)

    if err != nil {
        return 0, err
    }

    affected, _ := res.RowsAffected()
    return int(affected), nil
}
```

**Estimate:** 2 hours (including cron job setup)

## Testing Plan

1. **Create test agent:**

   ```json
   {
     "name": "Timeout Test Agent",
     "systemPrompt": "Sleep for 2 minutes by calling a slow tool repeatedly",
     "defaultTimeout": 60,
     "maxSteps": 100
   }
   ```

2. **Trigger run and observe:**

   - Should fail after 60 seconds
   - Should have error message mentioning timeout
   - Should NOT be stuck in `running` state

3. **Check database:**
   ```sql
   SELECT id, status, error_message, duration_ms, started_at, completed_at
   FROM kb.agent_runs
   WHERE agent_id = 'test-agent-id'
   ORDER BY started_at DESC
   LIMIT 1;
   ```

## Priority Recommendation

1. **Immediate (Fix 1):** Add explicit context check after `r.Run()` - 30 minutes
2. **Short-term (Fix 2):** Add context check in callbacks - 15 minutes
3. **Medium-term (Fix 4):** Add cleanup job - 2 hours
4. **Investigate (Fix 3):** Verify ADK context handling - TBD

**Total for immediate fixes:** ~45 minutes

## Workaround for Client

Until fixes are deployed, client can:

1. **Set default timeout on agent definition:**

   ```bash
   curl -X PATCH https://api.dev.emergent-company.ai/api/admin/agent-definitions/{id} \
     -H "Authorization: Bearer $TOKEN" \
     -d '{"defaultTimeout": 1200}'  # 20 minutes
   ```

2. **Use SQL script to cancel stuck runs:**

   ```sql
   -- In scripts/sql/agent-monitoring-helpers.sql
   UPDATE kb.agent_runs
   SET status = 'cancelled', completed_at = NOW()
   WHERE id = 'stuck-run-id';
   ```

3. **Monitor runs via SQL:**
   ```sql
   SELECT * FROM kb.v_active_agent_runs
   WHERE running_duration > INTERVAL '30 minutes';
   ```

## Related Issues

- Potential ADK context handling issue
- Need better visibility into why runs fail (more detailed error messages)
- Consider adding "max_runtime" metric to track longest runs
