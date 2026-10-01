package agents

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// WorkerPool executes queued agent runs using per-queue worker sets. A
// supervisor reconciles the enabled queues (kb.agent_queues) on an interval and
// maintains each queue's configured number of workers; each worker claims jobs
// from its own queue using FOR UPDATE SKIP LOCKED and executes them via
// AgentExecutor.
type WorkerPool struct {
	repo         *Repository
	executor     *AgentExecutor
	log          *slog.Logger
	globalSize   int
	pollInterval time.Duration

	// workObjects performs the versioned claim transition for object-driven
	// runs (nil disables the claim path, e.g. in unit tests).
	workObjects WorkObjectStore

	// refreshInterval is how often the supervisor reconciles queue configuration.
	refreshInterval time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	workers map[queueRef]*queueWorkerSet
	workWG  sync.WaitGroup
}

// SetWorkObjectStore injects the graph work-object surface used for the
// ready→in_progress claim transition of object-driven runs.
func (p *WorkerPool) SetWorkObjectStore(store WorkObjectStore) {
	p.workObjects = store
}

// queueRef identifies a queue by project + name. Queue names are unique per
// project, so workers and claims must be keyed by both to keep projects isolated.
type queueRef struct {
	ProjectID string
	Name      string
}

// queueWorkerSet is the running worker set for one queue.
type queueWorkerSet struct {
	cancel context.CancelFunc
	count  int
}

// NewWorkerPool creates a WorkerPool. size=0 disables the pool; otherwise size
// is the number of workers for the "default" queue (non-default queues use
// their own configured concurrency).
func NewWorkerPool(repo *Repository, executor *AgentExecutor, log *slog.Logger, size int, pollInterval time.Duration) *WorkerPool {
	return &WorkerPool{
		repo:            repo,
		executor:        executor,
		log:             log,
		globalSize:      size,
		pollInterval:    pollInterval,
		refreshInterval: 30 * time.Second,
		workers:         make(map[queueRef]*queueWorkerSet),
	}
}

// SetRefreshInterval overrides how often the supervisor reconciles queues.
func (p *WorkerPool) SetRefreshInterval(d time.Duration) {
	if d > 0 {
		p.refreshInterval = d
	}
}

// Start launches the supervisor. It is idempotent — safe to call once.
func (p *WorkerPool) Start(ctx context.Context) error {
	if p.globalSize <= 0 {
		p.log.Info("agent worker pool disabled (AGENT_WORKER_POOL_SIZE=0)")
		return nil
	}

	ctx, p.cancel = context.WithCancel(ctx)
	p.log.Info("starting agent worker pool",
		slog.Int("default_queue_size", p.globalSize),
		slog.Duration("poll_interval", p.pollInterval),
	)

	p.wg.Add(1)
	go p.supervise(ctx)
	return nil
}

// Stop signals the supervisor and all queue workers to stop and waits for them.
func (p *WorkerPool) Stop() {
	if p.cancel == nil {
		return
	}
	p.cancel()
	p.wg.Wait()
	p.workWG.Wait()
	p.log.Info("agent worker pool stopped")
}

// supervise reconciles per-queue worker sets on an interval.
func (p *WorkerPool) supervise(ctx context.Context) {
	defer p.wg.Done()

	interval := p.refreshInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	p.reconcile(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.stopAllWorkers()
			return
		case <-ticker.C:
			p.reconcile(ctx)
		}
	}
}

// reconcile sizes each enabled queue's worker set to its configured concurrency.
func (p *WorkerPool) reconcile(ctx context.Context) {
	if err := p.repo.EnsureDefaultQueues(ctx); err != nil {
		p.log.Warn("ensure default queues failed", slog.String("error", err.Error()))
	}
	queues, err := p.repo.ListEnabledQueues(ctx)
	if err != nil {
		p.log.Warn("queue refresh failed", slog.String("error", err.Error()))
		return
	}

	desired := make(map[queueRef]int, len(queues))
	for _, q := range queues {
		c := q.Concurrency
		if q.Name == DefaultQueueName && p.globalSize > 0 {
			c = p.globalSize
		}
		if c < 1 {
			c = 1
		}
		desired[queueRef{ProjectID: q.ProjectID, Name: q.Name}] = c
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop queues that were removed or disabled.
	for ref, set := range p.workers {
		if _, ok := desired[ref]; !ok {
			set.cancel()
			delete(p.workers, ref)
		}
	}
	// Start queues that are new, or resize those whose concurrency changed.
	for ref, count := range desired {
		if set, ok := p.workers[ref]; ok {
			if set.count == count {
				continue
			}
			set.cancel()
			delete(p.workers, ref)
		}
		qctx, qcancel := context.WithCancel(ctx)
		p.workers[ref] = &queueWorkerSet{cancel: qcancel, count: count}
		for i := 0; i < count; i++ {
			p.workWG.Add(1)
			go p.runQueueWorker(qctx, ref, i)
		}
		p.log.Info("started queue workers",
			slog.String("project_id", ref.ProjectID),
			slog.String("queue", ref.Name),
			slog.Int("workers", count),
		)
	}
}

func (p *WorkerPool) stopAllWorkers() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, set := range p.workers {
		set.cancel()
		delete(p.workers, name)
	}
}

// runQueueWorker is the main loop for a single queue worker.
func (p *WorkerPool) runQueueWorker(ctx context.Context, ref queueRef, workerID int) {
	defer p.workWG.Done()
	log := p.log.With(slog.String("project_id", ref.ProjectID), slog.String("queue", ref.Name), slog.Int("worker", workerID))
	log.Debug("queue worker started")

	for {
		select {
		case <-ctx.Done():
			log.Debug("queue worker stopping")
			return
		default:
		}

		job, err := p.repo.ClaimNextJobInQueue(ctx, ref.ProjectID, ref.Name)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("failed to claim next job", slog.String("error", err.Error()))
			p.sleep(ctx)
			continue
		}

		if job == nil {
			// No job available — idle sleep
			p.sleep(ctx)
			continue
		}

		p.executeJob(ctx, log, job)
	}
}

// executeJob runs a claimed job and updates its status.
func (p *WorkerPool) executeJob(ctx context.Context, log *slog.Logger, job *AgentRunJob) {
	log = log.With(slog.String("job_id", job.ID), slog.String("run_id", job.RunID))
	log.Info("executing queued agent run")

	// Look up the agent run
	run, err := p.repo.FindRunByID(ctx, job.RunID)
	if err != nil || run == nil {
		errMsg := "run not found"
		if err != nil {
			errMsg = err.Error()
		}
		log.Warn("job run not found; failing job", slog.String("error", errMsg))
		_ = p.repo.FailJob(ctx, job.ID, job.RunID, "run record not found: "+errMsg, false, time.Time{})
		return
	}

	// Look up the agent
	agent, err := p.repo.FindByID(ctx, run.AgentID, nil)
	if err != nil || agent == nil {
		errMsg := "agent not found"
		if err != nil {
			errMsg = err.Error()
		}
		log.Warn("job agent not found; failing job", slog.String("error", errMsg))
		_ = p.repo.FailJob(ctx, job.ID, job.RunID, "agent not found: "+errMsg, false, time.Time{})
		return
	}

	// Skip disabled agents — fail the job without executing so workers don't
	// waste cycles on stale queue entries left over from a now-disabled agent.
	if !agent.Enabled {
		log.Info("skipping job for disabled agent; failing without retry",
			slog.String("agent", agent.Name),
			slog.String("agent_id", agent.ID),
		)
		_ = p.repo.FailJob(ctx, job.ID, job.RunID, "agent is disabled", false, time.Time{})
		return
	}

	// Look up definition (optional)
	agentDef, _ := p.repo.ResolveDefinitionForAgent(ctx, agent)

	userMessage := "Execute agent tasks"
	if run.TriggerMessage != nil && *run.TriggerMessage != "" {
		userMessage = *run.TriggerMessage
	} else if agent.Prompt != nil && *agent.Prompt != "" {
		userMessage = *agent.Prompt
	}

	// Resolve org ID for the agent's project so the tracking model can attribute
	// LLM usage events to the correct tenant.
	orgID, _ := p.repo.GetOrgIDByProjectID(ctx, agent.ProjectID)

	// Object-driven claim: before executing, transition the work object
	// ready→in_progress under the versioned write model. A lost race (the object
	// was already claimed or is no longer ready) skips the run without burning
	// failure budget or touching the breaker.
	if run.SubjectObjectID != nil && *run.SubjectObjectID != "" {
		claimed, claimErr := p.claimWorkObject(ctx, run, agent, agentDef)
		if claimErr != nil {
			log.Warn("work object claim failed", slog.String("error", claimErr.Error()))
			_ = p.repo.FailJob(ctx, job.ID, job.RunID, claimErr.Error(), false, time.Time{})
			p.handleFailure(ctx, log, agent)
			p.reenqueueParent(ctx, log, run, agent.Name, "", "failed")
			return
		}
		if !claimed {
			log.Info("work object already claimed or not ready; skipping run",
				slog.String("run_id", run.ID),
				slog.String("canonical_id", *run.SubjectObjectID),
			)
			if err := p.repo.SkipRun(ctx, run.ID, "work object already claimed or not ready"); err != nil {
				log.Warn("failed to mark skipped run", slog.String("error", err.Error()))
			}
			if err := p.repo.CompleteJob(ctx, job.ID, job.RunID); err != nil {
				log.Warn("failed to complete skipped job", slog.String("error", err.Error()))
			}
			return
		}
	}

	result, execErr := p.executor.ExecuteWithRun(ctx, run, ExecuteRequest{
		Agent:           agent,
		AgentDefinition: agentDef,
		ProjectID:       agent.ProjectID,
		OrgID:           orgID,
		UserMessage:     userMessage,
		// Inherit the queued run's persisted trust marker rather than forcing
		// true: the trust is fixed at run creation by whichever transport enqueued
		// it (scheduler, trigger_agent, parent wake), and the worker is a deferred
		// executor, not a transport surface that may upgrade it.
		TrustedInternal: run.TrustedInternal,
	})
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}

	// Object-driven runs map their terminal outcome to an item transition and a
	// failure-budget effect (design.md "Run-end → item-transition mapping"),
	// which replaces the generic job/run completion below.
	if run.SubjectObjectID != nil && *run.SubjectObjectID != "" {
		p.finishWorkRun(ctx, log, job, run, agent, agentDef, result, execErr)
		return
	}

	if execErr != nil {
		log.Warn("queued agent run failed", slog.String("error", execErr.Error()))
		requeue := job.AttemptCount < job.MaxAttempts
		nextRunAt := time.Now().Add(backoff(job.AttemptCount))
		if err := p.repo.FailJob(ctx, job.ID, job.RunID, execErr.Error(), requeue, nextRunAt); err != nil {
			log.Warn("failed to update job status after failure", slog.String("error", err.Error()))
		}

		// Track consecutive failures and auto-disable the agent if threshold exceeded.
		p.handleFailure(ctx, log, agent)

		// Still wake parent on failure so it can handle the error
		p.reenqueueParent(ctx, log, run, agent.Name, "", "failed")
		return
	}

	// If the run terminated with an error status (e.g. MALFORMED_FUNCTION_CALL,
	// LLM safety block, timeout absorbed into the result), fail the job and
	// re-enqueue the parent with status:error so it can retry or escalate.
	// This must happen BEFORE CompleteJob, which would otherwise overwrite the
	// error status already set by FailRunWithSteps inside runPipeline.
	if result != nil && result.Status == RunStatusError {
		errMsg := ""
		if e, ok := result.Summary["error"].(string); ok {
			errMsg = e
		}
		log.Warn("queued agent run returned error status",
			slog.String("agent", agent.Name),
			slog.String("run_id", run.ID),
			slog.String("error", errMsg),
		)
		// Mark the job failed (no requeue — the parent decides whether to retry).
		if err := p.repo.FailJob(ctx, job.ID, job.RunID, errMsg, false, time.Time{}); err != nil {
			log.Warn("failed to mark job failed after run error", slog.String("error", err.Error()))
		}

		// Track consecutive failures and auto-disable the agent if threshold exceeded.
		p.handleFailure(ctx, log, agent)

		p.reenqueueParent(ctx, log, run, agent.Name, errMsg, "failed")
		return
	}

	// If the run paused awaiting human input, mark the job completed (to prevent
	// reprocessing) but do NOT overwrite the run status (already set to paused)
	// and do NOT re-enqueue the parent — the parent will be woken when the human
	// responds and the resumed run completes.
	if result != nil && result.Status == RunStatusPaused {
		log.Info("queued agent run paused awaiting human input",
			slog.String("agent", agent.Name),
			slog.String("run_id", run.ID),
		)
		if err := p.repo.PauseJob(ctx, job.ID); err != nil {
			log.Warn("failed to mark job paused", slog.String("error", err.Error()))
		}
		return
	}

	// A durable cancel — possibly served by another instance — is a normal
	// terminal outcome: the run is already finalized as cancelled. Retire the
	// job without overwriting the run, and wake the parent with a cancelled
	// status so it is not left waiting (issue #1166).
	if result != nil && result.Status == RunStatusCancelled {
		log.Info("queued agent run was cancelled",
			slog.String("agent", agent.Name),
			slog.String("run_id", run.ID),
		)
		if err := p.repo.PauseJob(ctx, job.ID); err != nil {
			log.Warn("failed to retire cancelled job", slog.String("error", err.Error()))
		}
		p.reenqueueParent(ctx, log, run, agent.Name, "", "cancelled")
		return
	}

	// Mark job and run as complete
	if err := p.repo.CompleteJob(ctx, job.ID, job.RunID); err != nil {
		log.Warn("failed to mark job completed", slog.String("error", err.Error()))
	} else {
		log.Info("queued agent run completed successfully")
	}

	// Reset consecutive failure counter on success.
	if resetErr := p.repo.ResetFailureCounter(ctx, agent.ID); resetErr != nil {
		log.Warn("failed to reset failure counter", slog.String("agent_id", agent.ID), slog.String("error", resetErr.Error()))
	}

	// Re-enqueue parent run (if any) with child's result as trigger_message.
	// If the agent's final response contains "DEFER_PARENT", the agent is
	// signalling that it has re-triggered a new child and does not want its
	// parent woken yet — the parent will be woken when that next child finishes.
	finalResponse := ""
	if result != nil {
		if fr, ok := result.Summary["final_response"].(string); ok {
			finalResponse = fr
		}
	}
	if strings.Contains(finalResponse, "DEFER_PARENT") {
		log.Info("agent deferred parent re-enqueue via DEFER_PARENT sentinel",
			slog.String("agent", agent.Name),
			slog.String("run_id", run.ID),
		)
		return
	}
	p.reenqueueParent(ctx, log, run, agent.Name, finalResponse, "completed")
}

// claimWorkObject performs the versioned ready→in_progress claim transition for
// an object-driven run. It returns claimed=false when the object was already
// claimed (a lost race) or is not in the ready status, so the caller can skip
// the run without consuming the failure budget.
func (p *WorkerPool) claimWorkObject(ctx context.Context, run *AgentRun, agent *Agent, agentDef *AgentDefinition) (bool, error) {
	if p.workObjects == nil {
		return false, fmt.Errorf("no work object store configured")
	}
	readyStatus := "ready"
	revisionStatus := "revision"
	inProgressStatus := "in_progress"
	if agentDef != nil {
		readyStatus = agentDef.WorkConfig.ReadyStatus()
		revisionStatus = agentDef.WorkConfig.RevisionStatus()
		inProgressStatus = agentDef.WorkConfig.InProgressStatus()
	}
	// A rework run (request-changes) claims from the revision status, not ready:
	// RequestChangesWorkObject moved the object to revision, and the claim would
	// otherwise be skipped, stranding the item in revision forever.
	if isReworkRun(run) {
		return p.workObjects.TransitionWorkObject(ctx, agent.ProjectID, *run.SubjectObjectID, graph.WorkObjectTransition{
			FromStatus: revisionStatus,
			ToStatus:   inProgressStatus,
		})
	}
	return p.workObjects.ClaimWorkObject(ctx, agent.ProjectID, *run.SubjectObjectID, readyStatus, inProgressStatus)
}

// isReworkRun reports whether a subject-object run is a rework run enqueued by a
// request-changes action. Rework runs carry the full feedback history in their
// trigger metadata under the "work_feedback" key.
func isReworkRun(run *AgentRun) bool {
	if run == nil || run.TriggerMetadata == nil {
		return false
	}
	_, ok := run.TriggerMetadata["work_feedback"]
	return ok
}

// reenqueueParent re-enqueues the parent run of this run (if any) with a
// trigger_message containing the child's result. This implements the
// "child-triggers-parent" async coordination pattern: the parent wakes up
// with the result already in its trigger_message, so it doesn't need to poll.
func (p *WorkerPool) reenqueueParent(ctx context.Context, log *slog.Logger, run *AgentRun, childAgentName, finalResponse, status string) {
	if run.ParentRunID == nil {
		return
	}

	parentRun, err := p.repo.FindRunByID(ctx, *run.ParentRunID)
	if err != nil || parentRun == nil {
		log.Warn("failed to find parent run for re-enqueue",
			slog.String("parent_run_id", *run.ParentRunID),
		)
		return
	}

	triggerMsg := fmt.Sprintf("AGENT_COMPLETE\nagent: %s\nstatus: %s\n\nResult:\n%s",
		childAgentName, status, finalResponse)

	// Append the original trigger message the parent sent to this child so the
	// parent can recover any IDs or context it embedded there (e.g. TASK_ID,
	// WP_ID, CODING_TASK_ID). We always use the FIRST run for this child+parent
	// pair — that run holds the original message from the parent, regardless of
	// how many times the child was subsequently re-enqueued by its own children.
	//
	// Strip any nested "Child trigger message:" section before appending so the
	// chain never grows beyond one level deep (prevents context-window ballooning
	// on deep pipelines).
	originalTrigger := run.TriggerMessage
	if firstRun, ferr := p.repo.FindFirstChildRunForAgent(ctx, *run.ParentRunID, run.AgentID); ferr == nil && firstRun != nil && firstRun.TriggerMessage != nil {
		originalTrigger = firstRun.TriggerMessage
	}
	if originalTrigger != nil && *originalTrigger != "" {
		childTrigger := *originalTrigger
		const nestedMarker = "\n\n---\nChild trigger message:"
		if idx := strings.Index(childTrigger, nestedMarker); idx != -1 {
			childTrigger = childTrigger[:idx]
		}
		triggerMsg += fmt.Sprintf("\n\n---\nChild trigger message:\n%s", childTrigger)
	}

	// Queue depth check — skip parent re-enqueue if the parent agent already has
	// too many pending jobs (prevents runaway queue explosion).
	maxPending := p.executor.safeguards.MaxPendingJobs
	if maxPending <= 0 {
		maxPending = 10 // safe fallback
	}
	pendingCount, countErr := p.repo.CountPendingJobsForAgent(ctx, parentRun.AgentID)
	if countErr != nil {
		log.Warn("failed to count pending jobs for parent agent, proceeding with re-enqueue",
			slog.String("parent_agent_id", parentRun.AgentID),
			slog.String("error", countErr.Error()),
		)
	} else if pendingCount >= maxPending {
		log.Warn("skipping parent re-enqueue: queue full",
			slog.String("parent_run_id", *run.ParentRunID),
			slog.String("parent_agent_id", parentRun.AgentID),
			slog.Int("pending_jobs", pendingCount),
			slog.Int("max_pending", maxPending),
			slog.String("child_agent", childAgentName),
			slog.String("child_status", status),
		)
		return
	}

	_, err = p.repo.CreateRunQueued(ctx, parentRun.AgentID, 1, CreateRunQueuedOptions{
		TriggerMessage:  &triggerMsg,
		ParentRunID:     parentRun.ParentRunID, // propagate grandparent so the chain continues
		RootRunID:       parentRun.RootRunID,   // propagate orchestration root so the tree is not split
		TriggerMetadata: parentRun.TriggerMetadata,
		MaxPendingJobs:  p.executor.safeguards.MaxPendingJobs,
		// Re-enqueued parents inherit the parent run's persisted trust, so a
		// suspended external-facing parent stays untrusted when re-woken.
		TrustedInternal: parentRun.TrustedInternal,
	})
	if err != nil {
		log.Warn("failed to re-enqueue parent run",
			slog.String("parent_run_id", *run.ParentRunID),
			slog.String("error", err.Error()),
		)
		return
	}
	log.Info("re-enqueued parent run after child completion",
		slog.String("parent_run_id", *run.ParentRunID),
		slog.String("child_agent", childAgentName),
		slog.String("child_status", status),
	)
}

// handleFailure increments the consecutive failure counter for an agent and
// auto-disables it when the threshold is reached.
func (p *WorkerPool) handleFailure(ctx context.Context, log *slog.Logger, agent *Agent) {
	if err := p.repo.IncrementFailureCounter(ctx, agent.ID); err != nil {
		log.Warn("failed to increment failure counter",
			slog.String("agent_id", agent.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	// Reload agent to get the updated consecutive_failures count.
	updated, err := p.repo.FindByID(ctx, agent.ID, nil)
	if err != nil || updated == nil {
		log.Warn("failed to reload agent after failure increment", slog.String("agent_id", agent.ID))
		return
	}

	threshold := p.executor.safeguards.ConsecutiveFailureThreshold
	if threshold <= 0 {
		threshold = 5 // safe fallback
	}
	if updated.ConsecutiveFailures >= threshold {
		reason := fmt.Sprintf("auto-disabled after %d consecutive failures", updated.ConsecutiveFailures)
		log.Error("auto-disabling agent due to consecutive failures",
			slog.String("agent_id", agent.ID),
			slog.String("agent_name", agent.Name),
			slog.Int("consecutive_failures", updated.ConsecutiveFailures),
			slog.Int("threshold", threshold),
		)
		if disableErr := p.repo.DisableAgent(ctx, agent.ID, reason); disableErr != nil {
			log.Error("failed to auto-disable agent",
				slog.String("agent_id", agent.ID),
				slog.String("error", disableErr.Error()),
			)
		}
	}
}

// sleep pauses the worker for the poll interval or until context is cancelled.
func (p *WorkerPool) sleep(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(p.pollInterval):
	}
}

// backoff returns exponential backoff duration: 2^attempt * 60s.
func backoff(attempt int) time.Duration {
	seconds := math.Pow(2, float64(attempt)) * 60
	if seconds > 3600 {
		seconds = 3600
	}
	return time.Duration(seconds) * time.Second
}
