## Context

The agent execution system consists of:
- **TriggerService** (`triggers.go`): Manages cron schedules and event listeners
- **WorkerPool** (`worker_pool.go`): Polls `kb.agent_run_jobs`, claims jobs with FOR UPDATE SKIP LOCKED
- **AgentExecutor** (`executor.go`): Executes agent runs, retries transient errors
- **Repository** (`repository.go`): Database operations for agents and runs
- **UsageService** (`usage_service.go`): Tracks LLM usage and checks budgets

Current state issues:
1. Cron creates runs without checking queue depth → exponential job accumulation
2. Child agents re-enqueue parents on failure → cascading re-queues
3. 429 RESOURCE_EXHAUSTED errors retry indefinitely → infinite loops
4. Budget system only alerts, doesn't block → API calls continue until provider blocks
5. No minimum cron interval → sub-minute scheduling overwhelms workers

The mcj-emergent incident (29,369 queued jobs) demonstrates these gaps compound catastrophically.

## Goals / Non-Goals

**Goals:**
- Prevent queue explosions by enforcing depth limits per agent
- Stop infinite retries by auto-disabling failing agents
- Enforce budget limits before making expensive API calls
- Prevent resource exhaustion from high-frequency cron schedules
- Maintain backward compatibility for existing well-behaved agents

**Non-Goals:**
- Global queue depth limits (limit is per-agent, not system-wide)
- Retry backoff changes (keep existing exponential backoff)
- Parent/child orchestration redesign (fix within current architecture)
- Budget prediction or forecasting (only enforce hard limits)

## Decisions

### Decision 1: Queue Depth Check in CreateRunQueued vs Cron Trigger

**Options:**
- A) Check queue depth in `CreateRunQueued` (centralized enforcement)
- B) Check queue depth in cron trigger `executeTriggeredAgent` (trigger-specific)
- C) Check in both places

**Choice:** **C - Both places**

**Rationale:**
- `CreateRunQueued` provides last line of defense for all run creation paths (cron, manual, parent re-enqueue)
- Cron trigger check avoids wasted work (don't even call executor if queue is full)
- Parent re-enqueue in `reenqueueParent` also needs to check before creating new run
- Cost: One extra DB query per run creation, but prevents much more expensive queue buildup

**Implementation:**
```go
// In repository.go
func (r *Repository) CreateRunQueued(...) (*AgentRun, error) {
    count, err := r.CountPendingJobsForAgent(ctx, agentID)
    if err != nil { return nil, err }
    if count >= maxPendingJobs {
        return nil, fmt.Errorf("agent has %d pending jobs (max %d)", count, maxPendingJobs)
    }
    // ... existing logic
}

// In triggers.go executeTriggeredAgent
func (ts *TriggerService) executeTriggeredAgent(...) error {
    count, err := ts.repo.CountPendingJobsForAgent(ctx, agentID)
    if count >= maxPendingJobs {
        ts.log.Warn("skipping cron trigger, queue full", ...)
        return nil // Don't fail, just skip
    }
    // ... existing logic
}

// In worker_pool.go reenqueueParent
func (p *WorkerPool) reenqueueParent(...) {
    count, err := p.repo.CountPendingJobsForAgent(ctx, parentRun.AgentID)
    if count >= maxPendingJobs {
        log.Info("skipping parent re-enqueue, queue full", ...)
        return
    }
    // ... existing logic
}
```

### Decision 2: Consecutive Failure Tracking Location

**Options:**
- A) Store counter in `kb.agents.consecutive_failures` column
- B) Query recent runs from `kb.agent_runs` to calculate streak
- C) Separate `kb.agent_failure_log` table

**Choice:** **A - Column in agents table**

**Rationale:**
- Simple atomic increment/reset operations
- No joins required to check failure count
- Naturally scoped to agent lifecycle
- Migration is straightforward (`ALTER TABLE ADD COLUMN`)
- Tradeoff: Doesn't preserve history (acceptable - we only need current streak)

**Implementation:**
```sql
-- Migration
ALTER TABLE kb.agents ADD COLUMN consecutive_failures INT DEFAULT 0;
CREATE INDEX idx_agents_consecutive_failures ON kb.agents(consecutive_failures) 
WHERE consecutive_failures > 0; -- Partial index for monitoring
```

```go
// In worker_pool.go after job failure
if err := p.repo.IncrementFailureCounter(ctx, agent.ID); err != nil {
    log.Warn("failed to increment failure counter", ...)
}

// Check if threshold exceeded
agent, _ = p.repo.FindByID(ctx, agent.ID, nil)
if agent.ConsecutiveFailures >= failureThreshold {
    if err := p.repo.DisableAgent(ctx, agent.ID, "auto-disabled after 5 consecutive failures"); err != nil {
        log.Error("failed to auto-disable agent", ...)
    }
}

// In worker_pool.go after job success
if err := p.repo.ResetFailureCounter(ctx, agent.ID); err != nil {
    log.Warn("failed to reset failure counter", ...)
}
```

### Decision 3: Budget Enforcement Point

**Options:**
- A) Check budget in UsageService before persisting event (post-call)
- B) Check budget in AgentExecutor before calling LLM (pre-call)
- C) Check budget in TrackingModel wrapper (low-level)

**Choice:** **B - Pre-flight check in AgentExecutor**

**Rationale:**
- Prevents wasted API calls and retries when budget exhausted
- Clear error message to user before attempting expensive operation
- Executor already loads projectID and orgID for other purposes
- Can return specific error type that prevents retries
- Tradeoff: Requires budget query on every run (but query is fast indexed lookup)

**Implementation:**
```go
// In executor.go before runPipeline
func (e *AgentExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
    // Pre-flight budget check
    exceeded, err := e.usageService.CheckBudgetExceeded(ctx, req.ProjectID)
    if err != nil {
        e.log.Warn("failed to check budget", ...)
        // Don't block on budget check failure - fail open
    } else if exceeded {
        return nil, &BudgetExceededError{
            ProjectID: req.ProjectID,
            Message: "Project has exceeded its monthly budget. Increase budget to continue.",
        }
    }
    // ... existing logic
}

// In usage_service.go
func (s *UsageService) CheckBudgetExceeded(ctx context.Context, projectID string) (bool, error) {
    var budget struct {
        BudgetUSD *float64 `bun:"budget_usd"`
    }
    err := s.db.NewSelect().
        Model((*Project)(nil)).
        Column("budget_usd").
        Where("id = ?", projectID).
        Scan(ctx, &budget)
    if err != nil || budget.BudgetUSD == nil || *budget.BudgetUSD <= 0 {
        return false, err // No budget set = no limit
    }

    var spend float64
    err = s.db.NewSelect().
        Model((*LLMUsageEvent)(nil)).
        ColumnExpr("COALESCE(SUM(estimated_cost_usd), 0)").
        Where("project_id = ?", projectID).
        Where("created_at >= date_trunc('month', CURRENT_TIMESTAMP)").
        Scan(ctx, &spend)
    if err != nil {
        return false, err
    }

    return spend >= *budget.BudgetUSD, nil
}
```

### Decision 4: Cron Interval Validation Strategy

**Options:**
- A) Parse cron, simulate next N executions, measure intervals
- B) Reject known patterns (e.g., `* * * * *`)
- C) Use cron parser library to calculate minimum interval

**Choice:** **C - Calculate minimum interval from parsed cron**

**Rationale:**
- Robust: catches all patterns that violate 15-min minimum
- User-friendly: can show "this cron fires every X minutes" in error
- Existing cron parser (`github.com/robfig/cron/v3`) supports this
- Tradeoff: Slightly more complex than pattern matching, but comprehensive

**Implementation:**
```go
// In handler.go validation
func validateCronInterval(cronExpr string, minMinutes int) error {
    parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
    schedule, err := parser.Parse(cronExpr)
    if err != nil {
        return fmt.Errorf("invalid cron expression: %w", err)
    }

    // Calculate interval by checking next 2 executions
    now := time.Now()
    next1 := schedule.Next(now)
    next2 := schedule.Next(next1)
    interval := next2.Sub(next1)

    minDuration := time.Duration(minMinutes) * time.Minute
    if interval < minDuration {
        return fmt.Errorf("cron interval %.0fm is below minimum %dm", 
            interval.Minutes(), minMinutes)
    }
    return nil
}
```

### Decision 5: RESOURCE_EXHAUSTED Error Handling

**Options:**
- A) Treat as non-retryable in executor, fail immediately
- B) Add to retry logic but with shorter max attempts
- C) Retry once, then auto-disable agent

**Choice:** **C - Retry once, then auto-disable**

**Rationale:**
- Distinguishes transient quota issues from hard spending caps
- Gives benefit of doubt (maybe quota resets soon)
- But prevents infinite loops by disabling after single retry
- Clear signal to user (agent disabled = investigate budget)

**Implementation:**
```go
// In executor.go retry logic
if strings.Contains(errStr, "RESOURCE_EXHAUSTED") || 
   strings.Contains(errStr, "spending cap") {
    if attempt > 0 {
        // Already retried once, this is a hard limit
        e.log.Error("spending cap persists after retry, disabling agent", ...)
        _ = e.repo.DisableAgent(ctx, agent.ID, "spending cap exceeded")
        return fmt.Errorf("spending cap exceeded, agent disabled: %w", err)
    }
    // First encounter - retry once after brief delay
    e.log.Warn("spending cap error, retrying once", ...)
    time.Sleep(5 * time.Second)
    continue
}
```

## Risks / Trade-offs

### Risk 1: Queue depth check race conditions
**Risk:** Between checking count and inserting job, another worker could insert jobs  
**Mitigation:** Acceptable - worst case is 1-2 jobs over limit. Check is best-effort guardrail, not strict guarantee.

### Risk 2: Budget query adds latency to every agent run
**Risk:** Budget check query on every execution adds ~10-50ms overhead  
**Mitigation:** 
- Query is simple indexed lookup (`WHERE id = ?`)
- Can add caching layer if needed (1-minute TTL)
- Budget enforcement can be disabled via env var for high-throughput use cases

### Risk 3: Existing agents with high-frequency crons break
**Risk:** Existing agents with `* * * * *` (every minute) will fail validation on update  
**Mitigation:**
- Validation only applies on create/update, not retroactively
- Clear error message guides users to fix their schedule
- Document migration: "Agents with cron < 15 min must be updated"

### Risk 4: Auto-disable on 5 failures might be too aggressive
**Risk:** Transient issues (network blip, provider downtime) could disable healthy agents  
**Mitigation:**
- Threshold is configurable via env var
- Failure counter resets on first success
- Disable includes reason in logs
- Admin can re-enable via UI/API
- Consider: Add "last disabled reason" column for debugging

### Risk 5: Parent re-enqueue blocking may break multi-agent pipelines
**Risk:** If parent queue is full, child results are lost  
**Mitigation:**
- Log skipped parent re-enqueue with child result in message
- Child run still completes successfully (data not lost)
- Parent can query child run status when it eventually runs
- Consider: Add "pending child results" metadata to parent run

## Migration Plan

### Phase 1: Deploy with feature flags (Week 1)
1. Merge all code changes behind env vars (all disabled by default)
2. Deploy to mcj-emergent test server
3. Enable features one by one:
   - Day 1: Enable queue depth limits (`AGENT_MAX_PENDING_JOBS=10`)
   - Day 2: Enable cron validation (`AGENT_MIN_CRON_INTERVAL_MINUTES=15`)
   - Day 3: Enable failure tracking (`AGENT_CONSECUTIVE_FAILURE_THRESHOLD=5`)
   - Day 4: Enable budget enforcement (`BUDGET_ENFORCEMENT_ENABLED=true`)
4. Monitor logs for blocked runs, auto-disabled agents, budget blocks
5. Verify no unexpected impacts on existing agents

### Phase 2: Production rollout (Week 2)
1. Run database migration to add `consecutive_failures` column
2. Enable all features in production with default values
3. Add Grafana dashboards:
   - Queue depth per agent (alert if > 8)
   - Agents disabled due to failures (alert if any)
   - Budget enforcement blocks per day
4. Notify users with agents that will need cron schedule updates

### Phase 3: Cleanup (Week 3)
1. Identify and update/disable agents with invalid cron schedules
2. Review auto-disabled agents, investigate root causes
3. Document new limits in agent creation documentation
4. Remove feature flags, make behavior standard

### Rollback Strategy
- If queue depth limits cause issues: Set `AGENT_MAX_PENDING_JOBS=999999` (effectively unlimited)
- If budget enforcement causes issues: Set `BUDGET_ENFORCEMENT_ENABLED=false`
- If auto-disable too aggressive: Set `AGENT_CONSECUTIVE_FAILURE_THRESHOLD=999999`
- Database migration is safe (adds column with default, no data loss)

### Monitoring
- **Alert:** Agent queue depth > 8 (warning sign before hitting 10)
- **Alert:** Agent auto-disabled (investigate root cause)
- **Alert:** Budget enforcement blocks > 100/day (may need budget increase)
- **Metric:** `agent_runs_rejected_queue_full_total` counter
- **Metric:** `agent_runs_rejected_budget_exceeded_total` counter
- **Metric:** `agents_auto_disabled_total` counter

## Open Questions

1. **Should we backfill consecutive_failures for currently failing agents?**
   - Option A: Start from 0 for all agents on migration
   - Option B: Query last 5 runs, set initial value
   - Recommendation: **A** - simpler, gives clean slate

2. **Should budget enforcement return 402 Payment Required or 429 Too Many Requests?**
   - 402 is semantic but rarely used
   - 429 is more common and better client library support
   - Recommendation: **402** - accurately represents payment issue

3. **Should parent re-enqueue skip persist child result metadata?**
   - Could add `child_results` JSONB column to agent_runs
   - Stores pending results when parent queue full
   - Adds complexity but prevents data loss
   - Recommendation: **Defer** - log result in skip message for now, add column if users report issues

4. **Should we add a global emergency kill switch for all agent execution?**
   - Useful for incidents like the 29k queue explosion
   - Could be env var `AGENT_EXECUTION_ENABLED=false`
   - Recommendation: **Yes** - add in this change for operational safety
