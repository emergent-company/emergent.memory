## 1. Database Migration

- [x] 1.1 Create migration file `20260318_add_agent_consecutive_failures.sql` in `apps/server/migrations/`
- [x] 1.2 Add column `consecutive_failures INT DEFAULT 0` to `kb.agents` table
- [x] 1.3 Create partial index `idx_agents_consecutive_failures` on `kb.agents(consecutive_failures) WHERE consecutive_failures > 0`
- [x] 1.4 Verify migration up/down with Goose locally
- [ ] 1.5 Test migration on mcj-emergent test server

## 2. Environment Variables & Configuration

- [x] 2.1 Add `AGENT_MAX_PENDING_JOBS` env var (default: 10) to `.env.example`
- [x] 2.2 Add `AGENT_CONSECUTIVE_FAILURE_THRESHOLD` env var (default: 5) to `.env.example`
- [x] 2.3 Add `AGENT_MIN_CRON_INTERVAL_MINUTES` env var (default: 15) to `.env.example`
- [x] 2.4 Add `BUDGET_ENFORCEMENT_ENABLED` env var (default: true) to `.env.example`
- [x] 2.5 Add `AGENT_EXECUTION_ENABLED` env var (default: true, emergency kill switch) to `.env.example`
- [x] 2.6 Update config loading in `apps/server/domain/agents/module.go` to read new env vars

## 3. Agent Entity Updates

- [x] 3.1 Add `ConsecutiveFailures int` field to `Agent` struct in `apps/server/domain/agents/entity.go`
- [x] 3.2 Add `bun:"consecutive_failures"` tag to new field
- [x] 3.3 Update `AgentDTO` to include consecutive_failures field for API responses
- [x] 3.4 Verify entity serialization in existing handlers

## 4. Repository - Queue Depth Queries

- [x] 4.1 Add `CountPendingJobsForAgent(ctx context.Context, agentID string) (int, error)` method to `repository.go`
- [x] 4.2 Implement query: `SELECT COUNT(*) FROM kb.agent_run_jobs j JOIN kb.agent_runs r ON j.run_id = r.id WHERE r.agent_id = ? AND j.status IN ('pending', 'processing')`
- [ ] 4.3 Add unit test for CountPendingJobsForAgent with various statuses
- [ ] 4.4 Verify query uses existing indexes efficiently (< 50ms execution time)

## 5. Repository - Failure Tracking Methods

- [x] 5.1 Add `IncrementFailureCounter(ctx context.Context, agentID string) error` method to `repository.go`
- [x] 5.2 Implement atomic increment: `UPDATE kb.agents SET consecutive_failures = consecutive_failures + 1 WHERE id = ?`
- [x] 5.3 Add `ResetFailureCounter(ctx context.Context, agentID string) error` method to `repository.go`
- [x] 5.4 Implement reset: `UPDATE kb.agents SET consecutive_failures = 0 WHERE id = ?`
- [x] 5.5 Add `DisableAgent(ctx context.Context, agentID string, reason string) error` method to `repository.go`
- [x] 5.6 Implement disable: `UPDATE kb.agents SET enabled = false WHERE id = ?` with log entry
- [ ] 5.7 Add unit tests for all three failure tracking methods

## 6. Usage Service - Budget Enforcement

- [x] 6.1 Add `CheckBudgetExceeded(ctx context.Context, projectID string) (bool, error)` method to `apps/server/domain/provider/usage_service.go`
- [x] 6.2 Implement query to get project budget_usd from kb.projects
- [x] 6.3 Implement query to sum current month spending: `SELECT COALESCE(SUM(estimated_cost_usd), 0) FROM kb.llm_usage_events WHERE project_id = ? AND created_at >= date_trunc('month', CURRENT_TIMESTAMP)`
- [x] 6.4 Return true if spend >= budget, false otherwise (null budget = no limit)
- [ ] 6.5 Add unit test with mock data for various budget scenarios
- [ ] 6.6 Verify query performance with indexes on (project_id, created_at)

## 7. Agent Executor - Budget Pre-flight Check

- [x] 7.1 Add `BudgetExceededError` struct to `apps/server/domain/agents/executor.go` with ProjectID and Message fields
- [x] 7.2 Add budget pre-flight check at start of `Execute()` method before runPipeline
- [x] 7.3 Call `usageService.CheckBudgetExceeded(ctx, req.ProjectID)` before LLM calls
- [x] 7.4 If exceeded and BUDGET_ENFORCEMENT_ENABLED=true, return BudgetExceededError
- [x] 7.5 If budget check query fails, log warning and proceed (fail-open)
- [ ] 7.6 Add test case for budget exceeded blocking execution
- [ ] 7.7 Add test case for budget check failure allowing execution

## 8. Agent Executor - RESOURCE_EXHAUSTED Handling

- [x] 8.1 Locate retry logic in `executor.go` around line 1097 where errors are classified
- [x] 8.2 Add check for `strings.Contains(errStr, "RESOURCE_EXHAUSTED") || strings.Contains(errStr, "spending cap")`
- [x] 8.3 On first RESOURCE_EXHAUSTED error (attempt=0), log warning and retry once after 5 second sleep
- [x] 8.4 On second RESOURCE_EXHAUSTED error (attempt>0), call `repo.DisableAgent(ctx, agent.ID, "spending cap exceeded")`
- [x] 8.5 Return error with message "spending cap exceeded, agent disabled"
- [ ] 8.6 Add test case for RESOURCE_EXHAUSTED retry-then-disable flow
- [ ] 8.7 Add test case for other errors continuing normal retry logic

## 9. Agent Executor - Emergency Kill Switch

- [x] 9.1 Add check at start of `Execute()` for `AGENT_EXECUTION_ENABLED` env var
- [x] 9.2 If set to false, return error "agent execution is disabled system-wide"
- [x] 9.3 Add log entry for kill switch activation
- [ ] 9.4 Add test case for kill switch blocking execution

## 10. Worker Pool - Failure Tracking Integration

- [x] 10.1 Locate job failure handling in `apps/server/domain/agents/worker_pool.go` around line 233
- [x] 10.2 After job failure, call `repo.IncrementFailureCounter(ctx, agent.ID)`
- [x] 10.3 Reload agent from database to get updated consecutive_failures count
- [x] 10.4 If `agent.ConsecutiveFailures >= failureThreshold`, call `repo.DisableAgent(ctx, agent.ID, fmt.Sprintf("auto-disabled after %d consecutive failures", agent.ConsecutiveFailures))`
- [x] 10.5 Log auto-disable event with agent_id and failure count
- [x] 10.6 Locate job success handling in worker_pool.go
- [x] 10.7 After job success, call `repo.ResetFailureCounter(ctx, agent.ID)`
- [ ] 10.8 Add test case for failure counter increment and auto-disable at threshold
- [ ] 10.9 Add test case for failure counter reset on success

## 11. Worker Pool - Parent Re-enqueue Queue Check

- [x] 11.1 Locate `reenqueueParent()` function in `worker_pool.go`
- [x] 11.2 Add `repo.CountPendingJobsForAgent(ctx, parentRun.AgentID)` call before creating new run
- [x] 11.3 If count >= AGENT_MAX_PENDING_JOBS, log "skipping parent re-enqueue, queue full" with child result details
- [x] 11.4 Return early without calling `CreateRunQueued`
- [ ] 11.5 Add test case for parent re-enqueue skipped when queue full
- [ ] 11.6 Add test case for parent re-enqueue proceeding when queue has capacity

## 12. Repository - Queue Depth Check in CreateRunQueued

- [x] 12.1 Locate `CreateRunQueued()` method in `repository.go`
- [x] 12.2 Add queue depth check at start: `count, err := r.CountPendingJobsForAgent(ctx, agentID)`
- [x] 12.3 If count >= maxPendingJobs, return error `fmt.Errorf("agent has %d pending jobs (max %d)", count, maxPendingJobs)`
- [ ] 12.4 Add test case for CreateRunQueued rejecting when queue full
- [ ] 12.5 Add test case for CreateRunQueued accepting when queue has capacity

## 13. Trigger Service - Cron Validation

- [x] 13.1 Add `validateCronInterval(cronExpr string, minMinutes int) error` function to `apps/server/domain/agents/triggers.go`
- [x] 13.2 Parse cron expression using `github.com/robfig/cron/v3` parser
- [x] 13.3 Calculate next two execution times using `schedule.Next(now)` and `schedule.Next(next1)`
- [x] 13.4 Compute interval as `next2.Sub(next1)`
- [x] 13.5 Return error if interval < minDuration: `fmt.Errorf("cron interval %.0fm is below minimum %dm", interval.Minutes(), minMinutes)`
- [x] 13.6 Add unit tests for validateCronInterval with various cron patterns (every minute, every 5 min, every 15 min, hourly, daily)
- [x] 13.7 Add test for invalid cron syntax returning parse error

## 14. Trigger Service - Cron Registration Validation

- [x] 14.1 Locate `registerCronTrigger()` method in `triggers.go`
- [x] 14.2 Add call to `validateCronInterval(cronSchedule, minCronIntervalMinutes)` before `scheduler.AddCronTask`
- [x] 14.3 If validation fails, return error without registering trigger
- [ ] 14.4 Add test case for invalid cron rejected during registration
- [ ] 14.5 Add test case for valid cron accepted during registration

## 15. Trigger Service - Pending Jobs Check Before Execution

- [x] 15.1 Locate `executeTriggeredAgent()` function in `triggers.go` (cron callback)
- [x] 15.2 Add `count, err := ts.repo.CountPendingJobsForAgent(ctx, agentID)` at start of function
- [x] 15.3 If count >= maxPendingJobs, log "skipping cron trigger, queue full" with agent_id and count, return nil
- [x] 15.4 If count query fails, log warning and proceed with execution (fail-open)
- [ ] 15.5 Add test case for cron skipping execution when queue full
- [ ] 15.6 Add test case for cron proceeding when queue has capacity

## 16. Trigger Service - SyncAgentTrigger Updates

- [x] 16.1 Locate `SyncAgentTrigger()` method in `triggers.go`
- [x] 16.2 When agent.Enabled=false, ensure `RemoveAgentTrigger()` is called
- [x] 16.3 When agent.Enabled=true and trigger_type=schedule, validate cron before registering
- [x] 16.4 If cron validation fails, log error and skip trigger registration
- [x] 16.5 Add test case for disabled agent having trigger removed
- [x] 16.6 Add test case for re-enabled agent having trigger re-registered

## 17. Handler - Cron Validation on Create/Update

- [x] 17.1 Locate agent create handler in `apps/server/domain/agents/handler.go`
- [x] 17.2 Add validation call to `validateCronInterval()` if trigger_type=schedule
- [x] 17.3 Return HTTP 400 Bad Request with validation error message
- [x] 17.4 Locate agent update handler
- [x] 17.5 Add same cron validation for updates when cron_schedule changes
- [x] 17.6 Add test case for POST /api/agents with invalid cron returning 400
- [x] 17.7 Add test case for PATCH /api/agents/:id with invalid cron returning 400

## 18. Error Types and HTTP Status

- [x] 18.1 Ensure BudgetExceededError returns HTTP 402 Payment Required in handler
- [x] 18.2 Ensure queue full errors return HTTP 429 Too Many Requests in handler
- [x] 18.3 Ensure cron validation errors return HTTP 400 Bad Request
- [x] 18.4 Add error message improvements: include actionable guidance for users
- [x] 18.5 Add test cases for correct HTTP status codes

## 19. Testing - Unit Tests

- [ ] 19.1 Run `go test ./apps/server/domain/agents/...` to verify all agent domain tests pass
- [ ] 19.2 Run `go test ./apps/server/domain/provider/...` to verify usage service tests pass
- [ ] 19.3 Verify test coverage for new functions is > 80%
- [ ] 19.4 Add integration test for full queue explosion scenario (cron + parent re-enqueue + failures)

## 20. Testing - End-to-End Validation

- [ ] 20.1 Deploy to mcj-emergent test server with all features enabled
- [ ] 20.2 Create test agent with cron "* * * * *" — verify rejected with validation error
- [ ] 20.3 Create test agent with cron "*/15 * * * *" — verify accepted
- [ ] 20.4 Create 10 pending jobs for agent, attempt 11th — verify rejected with queue full error
- [ ] 20.5 Set project budget to $1, create agent run — verify blocked with budget exceeded error
- [ ] 20.6 Trigger agent to fail 5 times consecutively — verify auto-disabled
- [ ] 20.7 Verify auto-disabled agent does not fire cron schedule
- [ ] 20.8 Re-enable agent, verify cron re-registers and fires correctly
- [ ] 20.9 Test RESOURCE_EXHAUSTED error disables agent after 1 retry
- [ ] 20.10 Verify emergency kill switch blocks all agent execution when AGENT_EXECUTION_ENABLED=false

## 21. Monitoring & Observability

- [x] 21.1 Add log entries for queue depth checks (debug level)
- [x] 21.2 Add log entries for budget enforcement blocks (warn level)
- [x] 21.3 Add log entries for auto-disable events (error level)
- [x] 21.4 Add log entries for cron validation failures (warn level)
- [ ] 21.5 Add Prometheus metrics counter `agent_runs_rejected_queue_full_total`
- [ ] 21.6 Add Prometheus metrics counter `agent_runs_rejected_budget_exceeded_total`
- [ ] 21.7 Add Prometheus metrics counter `agents_auto_disabled_total`
- [ ] 21.8 Verify metrics appear in Grafana on mcj-emergent

## 22. Documentation

- [x] 22.1 Update `apps/server/domain/agents/AGENT.md` with new queue limits behavior
- [x] 22.2 Document consecutive failure tracking and auto-disable threshold
- [x] 22.3 Document budget enforcement pre-flight checks
- [x] 22.4 Document cron validation minimum interval requirement
- [ ] 22.5 Add migration notes to `apps/server/migrations/README.md`
- [x] 22.6 Update environment variable documentation
- [ ] 22.7 Add troubleshooting section for common scenarios (queue full, budget exceeded, auto-disabled agents)

## 23. Production Readiness

- [x] 23.1 Run full test suite: `task test`
- [ ] 23.2 Run e2e tests: `task test:e2e`
- [x] 23.3 Run linter: `task lint`
- [x] 23.4 Build server binary: `task build`
- [ ] 23.5 Verify hot reload works with `air` after all changes
- [ ] 23.6 Review all TODOs and FIXMEs added during implementation
- [ ] 23.7 Verify no breaking changes to existing well-behaved agents

## 24. Cleanup & Rollout

- [ ] 24.1 Remove any debug logging added during development
- [ ] 24.2 Verify all environment variables have sensible defaults
- [ ] 24.3 Confirm feature flags can disable all new behavior if needed
- [ ] 24.4 Prepare rollback plan documentation
- [ ] 24.5 Create Grafana dashboard for monitoring queue depths, failures, budget blocks
- [ ] 24.6 Schedule production deployment with rollout plan from design.md Phase 1-3
