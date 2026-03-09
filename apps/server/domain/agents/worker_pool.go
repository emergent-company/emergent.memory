package agents

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"
)

// WorkerPool executes queued agent runs using a fixed pool of goroutines.
// Workers poll kb.agent_run_jobs using FOR UPDATE SKIP LOCKED, claim jobs,
// execute them via AgentExecutor, and update status atomically.
type WorkerPool struct {
	repo         *Repository
	executor     *AgentExecutor
	log          *slog.Logger
	size         int
	pollInterval time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewWorkerPool creates a WorkerPool. size=0 disables the pool.
func NewWorkerPool(repo *Repository, executor *AgentExecutor, log *slog.Logger, size int, pollInterval time.Duration) *WorkerPool {
	return &WorkerPool{
		repo:         repo,
		executor:     executor,
		log:          log,
		size:         size,
		pollInterval: pollInterval,
	}
}

// Start launches the worker goroutines. It is idempotent — safe to call once.
func (p *WorkerPool) Start(ctx context.Context) error {
	if p.size <= 0 {
		p.log.Info("agent worker pool disabled (AGENT_WORKER_POOL_SIZE=0)")
		return nil
	}

	ctx, p.cancel = context.WithCancel(ctx)
	p.log.Info("starting agent worker pool", slog.Int("size", p.size), slog.Duration("poll_interval", p.pollInterval))

	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go p.runWorker(ctx, i)
	}
	return nil
}

// Stop signals all workers to stop and waits for them to finish.
func (p *WorkerPool) Stop() {
	if p.cancel == nil {
		return
	}
	p.cancel()
	p.wg.Wait()
	p.log.Info("agent worker pool stopped")
}

// runWorker is the main loop for a single worker goroutine.
func (p *WorkerPool) runWorker(ctx context.Context, workerID int) {
	defer p.wg.Done()
	log := p.log.With(slog.Int("worker", workerID))
	log.Debug("agent worker started")

	for {
		select {
		case <-ctx.Done():
			log.Debug("agent worker stopping")
			return
		default:
		}

		job, err := p.repo.ClaimNextJob(ctx)
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

	// Look up definition (optional)
	agentDef, _ := p.repo.FindDefinitionByName(ctx, agent.ProjectID, agent.Name)

	userMessage := "Execute agent tasks"
	if agent.Prompt != nil && *agent.Prompt != "" {
		userMessage = *agent.Prompt
	}

	_, execErr := p.executor.ExecuteWithRun(ctx, run, ExecuteRequest{
		Agent:           agent,
		AgentDefinition: agentDef,
		ProjectID:       agent.ProjectID,
		UserMessage:     userMessage,
	})
	if execErr != nil {
		log.Warn("queued agent run failed", slog.String("error", execErr.Error()))
		requeue := job.AttemptCount < job.MaxAttempts
		nextRunAt := time.Now().Add(backoff(job.AttemptCount))
		if err := p.repo.FailJob(ctx, job.ID, job.RunID, execErr.Error(), requeue, nextRunAt); err != nil {
			log.Warn("failed to update job status after failure", slog.String("error", err.Error()))
		}
		return
	}

	// Mark job and run as complete
	if err := p.repo.CompleteJob(ctx, job.ID, job.RunID); err != nil {
		log.Warn("failed to mark job completed", slog.String("error", err.Error()))
	} else {
		log.Info("queued agent run completed successfully")
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
