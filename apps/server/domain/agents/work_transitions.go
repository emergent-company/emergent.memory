package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/tasks"
)

// FailureClass classifies a failed run for the object-driven failure policy
// (design.md "Failure policy"). Only retryable and deterministic failures
// consume the per-item budget.
type FailureClass string

const (
	FailureClassRetryable     FailureClass = "retryable"
	FailureClassDeterministic FailureClass = "deterministic"
	FailureClassHuman         FailureClass = "human"
	FailureClassQuota         FailureClass = "quota"
	FailureClassCapability    FailureClass = "capability"
	FailureClassProtocol      FailureClass = "protocol"
	FailureClassTimeout       FailureClass = "timeout"
)

// defaultWorkFailureLimit is the per-item failure budget applied when the agent
// definition's workConfig does not set failureLimit.
const defaultWorkFailureLimit = 3

// QuotaError is a typed provider quota/rate-limit error. It is the minimal
// net-new structured typing that lets the worker pool route quota failures to
// the agent-level breaker instead of consuming the item budget. The executor's
// in-run handling (RESOURCE_EXHAUSTED auto-disable, transient 429 retry) still
// happens first; this type is surfaced for failures that escape that path.
type QuotaError struct{ msg string }

func (e *QuotaError) Error() string { return e.msg }

// NewQuotaError wraps a provider quota/rate-limit failure.
func NewQuotaError(msg string) *QuotaError { return &QuotaError{msg: msg} }

// CapabilityError is a typed "wrong specialist" failure. When an assigned agent
// signals it cannot perform the work, the item is unassigned and returned to
// ready rather than consuming the item budget.
type CapabilityError struct{ msg string }

func (e *CapabilityError) Error() string { return e.msg }

// NewCapabilityError wraps a capability (wrong-specialist) failure.
func NewCapabilityError(msg string) *CapabilityError { return &CapabilityError{msg: msg} }

// isQuotaError reports whether err is a provider quota/rate-limit failure,
// either typed or matching the provider's textual signature.
func isQuotaError(err error) bool {
	if err == nil {
		return false
	}
	var q *QuotaError
	if errors.As(err, &q) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "RESOURCE_EXHAUSTED") ||
		strings.Contains(s, "spending cap") ||
		strings.Contains(s, "quota")
}

// isCapabilityError reports whether err is a capability (wrong-specialist)
// failure.
func isCapabilityError(err error) bool {
	if err == nil {
		return false
	}
	var c *CapabilityError
	if errors.As(err, &c) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "capability") || strings.Contains(s, "wrong specialist")
}

// classifyRunFailure maps a run's terminal outcome to a failure class.
func classifyRunFailure(execErr error, result *ExecuteResult) FailureClass {
	if execErr != nil {
		if isQuotaError(execErr) {
			return FailureClassQuota
		}
		if isCapabilityError(execErr) {
			return FailureClassCapability
		}
		if isRetryableErr(execErr) {
			return FailureClassRetryable
		}
		return FailureClassDeterministic
	}
	if result != nil && result.Status == RunStatusError {
		if r, ok := result.Summary["reason"].(string); ok && r == "timeout" {
			return FailureClassTimeout
		}
		return FailureClassDeterministic
	}
	return FailureClassDeterministic
}

// isRetryableErr reports a transient (provider 5xx / unavailable) failure that
// should be retried. Rate-limit (429) is excluded — that is quota, not retryable.
func isRetryableErr(err error) bool {
	s := err.Error()
	if strings.Contains(s, "23503") || strings.Contains(s, "foreign key constraint") {
		return false
	}
	return strings.Contains(s, "503") || strings.Contains(s, "UNAVAILABLE") || strings.Contains(s, "500")
}

// workConfigOf returns the agent definition's work config, or the zero value
// (defaults) when the definition is nil.
func workConfigOf(def *AgentDefinition) AgentWorkConfig {
	if def == nil {
		return AgentWorkConfig{}
	}
	return def.WorkConfig
}

// finishWorkRun applies the run-end → item-transition mapping (design.md
// "Run-end → item-transition mapping") for a subject-object run. It is the
// single place a work run's terminal outcome is translated into an item
// transition and a failure-budget effect. The caller returns immediately after.
func (p *WorkerPool) finishWorkRun(ctx context.Context, log *slog.Logger, job *AgentRunJob, run *AgentRun, agent *Agent, agentDef *AgentDefinition, result *ExecuteResult, execErr error) {
	if run.SubjectObjectID == nil || *run.SubjectObjectID == "" {
		return
	}
	canonicalID := *run.SubjectObjectID
	projectID := agent.ProjectID
	subjectType := ""
	if run.SubjectObjectType != nil {
		subjectType = *run.SubjectObjectType
	}

	// success with a terminator → done/review (already transitioned by the tool).
	if execErr == nil && result != nil && result.Status == RunStatusSuccess {
		if term, _ := result.Summary["work_terminator"].(string); term != "" {
			if err := p.repo.CompleteJob(ctx, job.ID, job.RunID); err != nil {
				log.Warn("failed to complete work run job", slog.String("error", err.Error()))
			}
			_ = p.repo.ResetFailureCounter(ctx, agent.ID)
			_ = p.repo.ClearWorkItemState(ctx, projectID, canonicalID)
			log.Info("work run finalized with terminator", slog.String("terminator", term), slog.String("canonical_id", canonicalID))
			return
		}
		// success without a terminator → protocol violation (bounded retry, else block).
		p.protocolViolation(ctx, log, job, agent, agentDef, canonicalID, projectID)
		return
	}

	// paused (ask_user) → item stays in-progress, no budget change.
	if result != nil && result.Status == RunStatusPaused {
		if err := p.repo.PauseJob(ctx, job.ID); err != nil {
			log.Warn("failed to pause work run job", slog.String("error", err.Error()))
		}
		return
	}

	// cancelled (durable cancel) → ready, or blocked when human-cancelled.
	if result != nil && result.Status == RunStatusCancelled {
		if err := p.repo.PauseJob(ctx, job.ID); err != nil {
			log.Warn("failed to retire cancelled work job", slog.String("error", err.Error()))
		}
		reason, _ := result.Summary["reason"].(string)
		if reason == "user_cancelled" {
			p.blockWorkObject(ctx, log, canonicalID, projectID, agentDef, "cancelled by user", FailureClassHuman)
		} else {
			p.transitionToReady(ctx, log, canonicalID, projectID, agentDef)
		}
		return
	}

	// failure — classify and route.
	cls := classifyRunFailure(execErr, result)
	errMsg := failureMessage(execErr, result)

	// Fail the job without requeue: the item transition below decides whether a
	// fresh run is enqueued (with backoff), so the job's own attempt budget is
	// not double-spent.
	if err := p.repo.FailJob(ctx, job.ID, job.RunID, errMsg, false, time.Time{}); err != nil {
		log.Warn("failed to mark work run job failed", slog.String("error", err.Error()))
	}

	switch cls {
	case FailureClassQuota:
		// agent-level only: no item change, no item budget.
		p.handleFailure(ctx, log, agent)
		log.Warn("work run failed with quota error; item unchanged", slog.String("canonical_id", canonicalID))
	case FailureClassCapability:
		// wrong specialist: unassign → ready with requeue backoff, budget 0.
		p.capabilityUnassign(ctx, log, agent, agentDef, canonicalID, projectID, subjectType, errMsg)
	case FailureClassTimeout:
		p.timeoutFailure(ctx, log, agent, agentDef, canonicalID, projectID, subjectType, errMsg)
	case FailureClassRetryable:
		p.retryableFailure(ctx, log, agent, agentDef, canonicalID, projectID, subjectType, errMsg)
	default: // deterministic / poison
		p.poisonFailure(ctx, log, agentDef, canonicalID, projectID, errMsg)
	}
}

func failureMessage(execErr error, result *ExecuteResult) string {
	if execErr != nil {
		return execErr.Error()
	}
	if result != nil {
		if e, ok := result.Summary["error"].(string); ok && e != "" {
			return e
		}
	}
	return "work run failed"
}

// protocolViolation handles a run that ended success without a terminator: it
// is a bounded retry against the item budget, otherwise the item is blocked as
// a protocol violation.
func (p *WorkerPool) protocolViolation(ctx context.Context, log *slog.Logger, job *AgentRunJob, agent *Agent, agentDef *AgentDefinition, canonicalID, projectID string) {
	if err := p.repo.CompleteJob(ctx, job.ID, job.RunID); err != nil {
		log.Warn("failed to complete job for protocol violation", slog.String("error", err.Error()))
	}
	p.handleFailure(ctx, log, agent)
	p.retryOrBlock(ctx, log, agent, agentDef, canonicalID, projectID, "", "run ended without a work terminator", FailureClassProtocol)
}

// retryableFailure: item → ready + re-enqueue with backoff; budget +1.
func (p *WorkerPool) retryableFailure(ctx context.Context, log *slog.Logger, agent *Agent, agentDef *AgentDefinition, canonicalID, projectID, subjectType, errMsg string) {
	p.handleFailure(ctx, log, agent)
	p.retryOrBlock(ctx, log, agent, agentDef, canonicalID, projectID, subjectType, errMsg, FailureClassRetryable)
}

// timeoutFailure: retry once with a longer backoff, else block; budget +1.
func (p *WorkerPool) timeoutFailure(ctx context.Context, log *slog.Logger, agent *Agent, agentDef *AgentDefinition, canonicalID, projectID, subjectType, errMsg string) {
	p.handleFailure(ctx, log, agent)
	p.retryOrBlock(ctx, log, agent, agentDef, canonicalID, projectID, subjectType, errMsg, FailureClassTimeout)
}

// poisonFailure: deterministic failure → block (no retry); budget +1. It does
// NOT touch the agent breaker — a poison item blocks the item, not the agent.
func (p *WorkerPool) poisonFailure(ctx context.Context, log *slog.Logger, agentDef *AgentDefinition, canonicalID, projectID, errMsg string) {
	p.recordFailure(ctx, log, canonicalID, projectID, agentDef, FailureClassDeterministic, nil)
	p.blockWorkObject(ctx, log, canonicalID, projectID, agentDef, errMsg, FailureClassDeterministic)
}

// capabilityUnassign clears the assignee and returns the item to ready with a
// requeue backoff; the item budget is unchanged and attempt history survives.
func (p *WorkerPool) capabilityUnassign(ctx context.Context, log *slog.Logger, agent *Agent, agentDef *AgentDefinition, canonicalID, projectID, subjectType, errMsg string) {
	wc := workConfigOf(agentDef)
	backoffAt := time.Now().Add(itemRequeueBackoff())
	if _, err := p.workObjects.UnassignWorkObject(ctx, projectID, canonicalID, wc.InProgressStatus(), wc.ReadyStatus()); err != nil {
		log.Warn("capability unassign transition failed", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
	}
	// Keep attempt history: record the failure but do not consume the budget.
	if _, err := p.repo.IncrementWorkItemFailure(ctx, projectID, canonicalID, string(FailureClassCapability), &backoffAt); err != nil {
		log.Warn("failed to record capability failure", slog.String("error", err.Error()))
	}
	p.reenqueueWork(ctx, log, agent, agentDef, canonicalID, projectID, subjectType, backoffAt)
	log.Info("work item unassigned and returned to ready", slog.String("canonical_id", canonicalID), slog.String("error", errMsg))
}

// retryOrBlock transitions the item back to ready and re-enqueues while the
// item budget remains; once exhausted the item is blocked (dead-letter).
func (p *WorkerPool) retryOrBlock(ctx context.Context, log *slog.Logger, agent *Agent, agentDef *AgentDefinition, canonicalID, projectID, subjectType, errMsg string, cls FailureClass) {
	wc := workConfigOf(agentDef)
	backoffAt := time.Now().Add(itemRequeueBackoff())
	count := p.recordFailure(ctx, log, canonicalID, projectID, agentDef, cls, &backoffAt)
	if count > wc.FailureLimitValue() {
		p.blockWorkObject(ctx, log, canonicalID, projectID, agentDef, errMsg, cls)
		return
	}
	if ok, err := p.workObjects.TransitionWorkObject(ctx, projectID, canonicalID, graph.WorkObjectTransition{
		FromStatus: wc.InProgressStatus(),
		ToStatus:   wc.ReadyStatus(),
	}); err != nil {
		log.Warn("work item ready transition failed", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
	} else if !ok {
		log.Warn("work item ready transition skipped (not in progress)", slog.String("canonical_id", canonicalID))
	}
	p.reenqueueWork(ctx, log, agent, agentDef, canonicalID, projectID, subjectType, backoffAt)
}

// recordFailure increments the per-item failure count and returns the new value.
func (p *WorkerPool) recordFailure(ctx context.Context, log *slog.Logger, canonicalID, projectID string, agentDef *AgentDefinition, cls FailureClass, backoffAt *time.Time) int {
	count, err := p.repo.IncrementWorkItemFailure(ctx, projectID, canonicalID, string(cls), backoffAt)
	if err != nil {
		log.Warn("failed to record work item failure", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
		return 0
	}
	return count
}

// transitionToReady returns an in-progress item to ready (used by the cancelled
// path).
func (p *WorkerPool) transitionToReady(ctx context.Context, log *slog.Logger, canonicalID, projectID string, agentDef *AgentDefinition) {
	wc := workConfigOf(agentDef)
	if ok, err := p.workObjects.TransitionWorkObject(ctx, projectID, canonicalID, graph.WorkObjectTransition{
		FromStatus: wc.InProgressStatus(),
		ToStatus:   wc.ReadyStatus(),
	}); err != nil {
		log.Warn("work item ready transition failed", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
	} else if !ok {
		log.Warn("work item ready transition skipped (not in progress)", slog.String("canonical_id", canonicalID))
	}
}

// blockWorkObject transitions an item to blocked and creates a human-facing
// dead-letter task.
func (p *WorkerPool) blockWorkObject(ctx context.Context, log *slog.Logger, canonicalID, projectID string, agentDef *AgentDefinition, errMsg string, cls FailureClass) {
	wc := workConfigOf(agentDef)
	if ok, err := p.workObjects.BlockWorkObject(ctx, projectID, canonicalID, wc.InProgressStatus(), wc.BlockedStatus()); err != nil {
		log.Warn("work item block transition failed", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
	} else if !ok {
		log.Warn("work item block transition skipped (not in progress)", slog.String("canonical_id", canonicalID))
	}
	desc := fmt.Sprintf("class=%s: %s", cls, errMsg)
	t := &tasks.Task{
		ProjectID:   projectID,
		Title:       fmt.Sprintf("Work item blocked (%s)", cls),
		Description: &desc,
		Type:        "work-escalation",
		Status:      "pending",
		SourceType:  strPtr("work_object"),
		SourceID:    &canonicalID,
	}
	if _, err := p.repo.CreateWorkTask(ctx, t); err != nil {
		log.Warn("failed to create dead-letter task", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
	}
}

// reenqueueWork enqueues a fresh run for the work object, optionally with a
// requeue backoff. A lost race with the reconciler is tolerated by the claim
// skip path.
func (p *WorkerPool) reenqueueWork(ctx context.Context, log *slog.Logger, agent *Agent, agentDef *AgentDefinition, canonicalID, projectID, subjectType string, backoffAt time.Time) {
	maxAttempts := 1
	if agentDef != nil && agentDef.WorkConfig.RetryPolicy.MaxAttempts > 0 {
		maxAttempts = agentDef.WorkConfig.RetryPolicy.MaxAttempts
	}
	var nextRunAt *time.Time
	if backoffAt.After(time.Now()) {
		nextRunAt = &backoffAt
	}
	subjectTypePtr := &subjectType
	if subjectType == "" {
		subjectTypePtr = nil
	}
	_, err := p.repo.CreateRunQueued(ctx, agent.ID, maxAttempts, CreateRunQueuedOptions{
		SubjectObjectID:   &canonicalID,
		SubjectObjectType: subjectTypePtr,
		TrustedInternal:   true,
		NextRunAt:         nextRunAt,
	})
	if err != nil {
		log.Warn("failed to re-enqueue work run", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
		return
	}
	log.Info("re-enqueued work run", slog.String("canonical_id", canonicalID))
}

// itemRequeueBackoff is the fixed requeue delay applied to a re-enqueued item.
// It keeps a failed/timed-out item from immediately hammering the queue while
// still bounded for tests.
func itemRequeueBackoff() time.Duration {
	return 30 * time.Second
}
