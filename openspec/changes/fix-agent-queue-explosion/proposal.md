## Why

On 2026-03-18, the mcj-emergent server accumulated 29,369 pending agent jobs in an infinite retry loop. All jobs were hitting 429 "spending cap exceeded" errors but continuously retrying. Root cause analysis revealed 5 critical gaps in the agent execution system: (1) cron schedulers create new runs regardless of existing queue depth, (2) child agents re-enqueue parents creating exponential growth, (3) rate limit errors retry forever, (4) budget limits only alert but don't enforce, and (5) no minimum cron interval allows sub-minute scheduling that overwhelms the system. This change implements comprehensive safeguards to prevent queue explosions and enforce resource limits.

## What Changes

- **Add hard budget enforcement**: Block LLM API calls when project exceeds monthly budget (not just alerts)
- **Auto-disable agents after consecutive failures**: Disable agent after 5 consecutive run failures to prevent infinite retry loops
- **Enforce queue depth limit**: Reject new runs when agent already has 10+ pending jobs
- **Enforce minimum cron interval**: Validate cron schedules to prevent intervals shorter than 15 minutes
- **Prevent duplicate cron runs**: Check for existing pending/running jobs before creating new cron-triggered run
- **Treat budget 429 errors as permanent**: Stop retrying when hitting spending cap errors; disable agent immediately

## Capabilities

### New Capabilities

- `agent-queue-limits`: Queue depth enforcement preventing multiple pending runs for same agent
- `agent-failure-tracking`: Consecutive failure tracking and auto-disable mechanism for unstable agents
- `budget-enforcement`: Hard budget limits that block API calls when project is over monthly spend cap
- `cron-validation`: Minimum interval validation for cron schedules to prevent resource exhaustion

### Modified Capabilities

- `agent-execution`: Add budget pre-flight check before LLM calls; treat RESOURCE_EXHAUSTED as permanent failure
- `agent-triggers`: Add pending job check before cron execution; validate cron intervals on agent create/update

## Impact

### Code Changes
- `apps/server/domain/agents/triggers.go`: Add pending job check in `executeTriggeredAgent`, validate cron intervals in `registerCronTrigger`
- `apps/server/domain/agents/repository.go`: Add `CountPendingJobsForAgent`, `GetConsecutiveFailures`, `IncrementFailureCounter`, `ResetFailureCounter` methods
- `apps/server/domain/agents/handler.go`: Add cron interval validation in create/update endpoints
- `apps/server/domain/agents/executor.go`: Add budget pre-flight check; treat RESOURCE_EXHAUSTED as non-retryable
- `apps/server/domain/agents/worker_pool.go`: Track consecutive failures; auto-disable after threshold
- `apps/server/domain/agents/entity.go`: Add `consecutive_failures` column to agents table
- `apps/server/domain/provider/usage_service.go`: Add `CheckBudgetExceeded` method for pre-flight enforcement

### Database Changes
- Migration: Add `consecutive_failures INT DEFAULT 0` to `kb.agents` table
- Migration: Add index on `kb.agent_run_jobs(agent_id, status)` for efficient queue depth queries

### API Changes
- Agent creation/update returns 400 if cron schedule interval < 15 minutes
- Agent runs return 429 when queue depth exceeds 10 for that agent
- LLM API calls return 402 Payment Required when project budget exceeded

### Configuration
- New env var: `AGENT_MAX_PENDING_JOBS` (default: 10)
- New env var: `AGENT_MIN_CRON_INTERVAL_MINUTES` (default: 15)
- New env var: `AGENT_CONSECUTIVE_FAILURE_THRESHOLD` (default: 5)
- New env var: `BUDGET_ENFORCEMENT_ENABLED` (default: true)

### Operational Impact
- Existing agents with cron < 15 minutes will be rejected on next update (gradual enforcement)
- Agents with 29k+ queued jobs will auto-disable on first failure after deployment
- Projects over budget will have agent execution blocked until budget increased or month resets
- Observability: New metrics for queue depth, consecutive failures, budget blocks

### Testing Requirements
- Unit tests for queue depth counting
- Unit tests for cron interval validation
- Unit tests for consecutive failure tracking
- Integration test: verify agent auto-disables after 5 failures
- Integration test: verify budget enforcement blocks LLM calls
- Integration test: verify cron respects pending job check
