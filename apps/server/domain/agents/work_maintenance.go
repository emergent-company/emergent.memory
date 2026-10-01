package agents

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// Work maintenance intervals and thresholds. These are the P2 defaults; the
// work-status reaper threshold deliberately matches the run reaper's
// staleRunThreshold so the two never race (design.md "Claim liveness").
const (
	defaultWorkReaperInterval     = 5 * time.Minute
	defaultWorkReaperThreshold    = 30 * time.Minute
	defaultWorkReconcilerInterval = 1 * time.Minute
)

// liveRunStatuses are the run states that still own their subject work object
// (queued/running/paused). A paused run (ask_user) is live: a human waiting on
// a question must not have the item reclaimed and run twice.
var workLiveRunStatuses = []string{
	string(RunStatusQueued),
	string(RunStatusRunning),
	string(RunStatusPaused),
}

// HasLiveRunForSubject reports whether any run for the subject object is live
// (queued, running, or paused).
func (r *Repository) HasLiveRunForSubject(ctx context.Context, canonicalID string) (bool, error) {
	var count int
	err := r.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_runs WHERE subject_object_id = ?::uuid AND status IN (?)`,
		canonicalID, bun.In(workLiveRunStatuses),
	).Scan(ctx, &count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// FindLatestRunForSubject returns the most recent run (by created_at) linked to
// the subject work object, or nil when none exists. It is used by the human
// actions to resolve which agent (and therefore which work config) owns a work
// item.
func (r *Repository) FindLatestRunForSubject(ctx context.Context, canonicalID string) (*AgentRun, error) {
	run := new(AgentRun)
	err := r.db.NewSelect().
		Model(run).
		Where("subject_object_id = ?", canonicalID).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return run, nil
}

// FindLiveRunsForSubject returns the live (queued/running/paused) runs linked to
// the subject work object, used by cancel to stop the in-flight run(s).
func (r *Repository) FindLiveRunsForSubject(ctx context.Context, canonicalID string) ([]*AgentRun, error) {
	var runs []*AgentRun
	err := r.db.NewSelect().
		Model(&runs).
		Where("subject_object_id = ?", canonicalID).
		Where("status IN (?)", bun.In(workLiveRunStatuses)).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// HasLiveJobForSubject reports whether any dispatch job for the subject object
// is live (pending or processing).
func (r *Repository) HasLiveJobForSubject(ctx context.Context, canonicalID string) (bool, error) {
	var count int
	err := r.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_run_jobs arj
		 JOIN kb.agent_runs ar ON ar.id = arj.run_id
		 WHERE ar.subject_object_id = ?::uuid
		   AND arj.status IN ('pending', 'processing')`,
		canonicalID,
	).Scan(ctx, &count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// FindReactionAgentsForType returns the enabled reaction agents in projectID
// whose trigger lists objectType (or a wildcard), filtered to those matching
// assignee when assignee is non-empty.
func (r *Repository) FindReactionAgentsForType(ctx context.Context, projectID, objectType, assignee string) ([]*Agent, error) {
	agents, err := r.FindEnabledByTriggerType(ctx, TriggerTypeReaction)
	if err != nil {
		return nil, err
	}
	var out []*Agent
	for _, a := range agents {
		if a == nil || a.ProjectID != projectID || a.ReactionConfig == nil {
			continue
		}
		if assignee != "" && a.Name != assignee {
			continue
		}
		if !reactionMatchesType(a.ReactionConfig, objectType) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// reactionMatchesType reports whether a reaction config listens for objectType
// (specific or wildcard).
func reactionMatchesType(rc *ReactionConfig, objectType string) bool {
	if len(rc.ObjectTypes) == 0 {
		return true // wildcard: listen for all object types
	}
	for _, t := range rc.ObjectTypes {
		if t == objectType || t == "*" {
			return true
		}
	}
	return false
}

// workMaintenance is the shared base for the periodic work-status reaper and
// reconciler.
type workMaintenance struct {
	repo        *Repository
	workObjects WorkObjectStore
	log         *slog.Logger
	interval    time.Duration

	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool
}

func (w *workMaintenance) start(ctx context.Context, fn func(context.Context)) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go func() {
		defer close(w.doneCh)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fn(ctx)
			case <-w.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *workMaintenance) stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	close(w.stopCh)
	<-w.doneCh
}

// WorkStatusReaper returns stranded in-progress work objects (whose subject run
// is missing or terminal past the threshold) to ready (budget-aware) or blocked.
type WorkStatusReaper struct {
	workMaintenance
	threshold time.Duration
}

// NewWorkStatusReaper creates a work-status reaper.
func NewWorkStatusReaper(repo *Repository, log *slog.Logger, interval, threshold time.Duration) *WorkStatusReaper {
	if interval <= 0 {
		interval = defaultWorkReaperInterval
	}
	if threshold <= 0 {
		threshold = defaultWorkReaperThreshold
	}
	return &WorkStatusReaper{
		workMaintenance: workMaintenance{
			repo:     repo,
			log:      log.With("component", "work-status-reaper"),
			interval: interval,
			stopCh:   make(chan struct{}),
			doneCh:   make(chan struct{}),
		},
		threshold: threshold,
	}
}

// SetWorkObjectStore injects the graph work-object surface after construction.
func (r *WorkStatusReaper) SetWorkObjectStore(store WorkObjectStore) {
	r.workObjects = store
}

// Start begins the periodic reaper goroutine.
func (r *WorkStatusReaper) Start(ctx context.Context) {
	r.start(ctx, r.reap)
}

// Stop signals the reaper to stop and waits for it.
func (r *WorkStatusReaper) Stop() { r.stop() }

// reap finds stranded in-progress work objects and releases them.
func (r *WorkStatusReaper) reap(ctx context.Context) {
	if r.workObjects == nil {
		return
	}
	cutoff := time.Now().Add(-r.threshold)
	heads, err := r.workObjects.ListWorkObjectsByStatus(ctx, "", "in_progress", cutoff, 500)
	if err != nil {
		r.log.Warn("failed to list in-progress work objects", slog.String("error", err.Error()))
		return
	}
	for _, h := range heads {
		live, err := r.repo.HasLiveRunForSubject(ctx, h.CanonicalID)
		if err != nil {
			r.log.Warn("failed to check live run", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
			continue
		}
		if live {
			continue // still owned by a queued/running/paused run
		}
		r.release(ctx, h)
	}
}

// release returns a stranded in-progress object to ready, or blocks it when its
// failure budget is exhausted (a crash consumes budget).
func (r *WorkStatusReaper) release(ctx context.Context, h *graph.WorkObjectHead) {
	count, err := r.repo.IncrementWorkItemFailure(ctx, h.ProjectID, h.CanonicalID, string(FailureClassDeterministic), nil)
	if err != nil {
		r.log.Warn("failed to record reaper failure", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
		return
	}
	if count > defaultWorkFailureLimit {
		if ok, err := r.workObjects.BlockWorkObject(ctx, h.ProjectID, h.CanonicalID, "in_progress", "blocked"); err != nil {
			r.log.Warn("failed to block stranded work object", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
		} else if ok {
			r.log.Warn("blocked stranded work object (budget exhausted)", slog.String("canonical_id", h.CanonicalID))
		}
		return
	}
	if ok, err := r.workObjects.TransitionWorkObject(ctx, h.ProjectID, h.CanonicalID, graph.WorkObjectTransition{
		FromStatus: "in_progress",
		ToStatus:   "ready",
	}); err != nil {
		r.log.Warn("failed to release stranded work object", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
	} else if ok {
		r.log.Info("released stranded work object to ready", slog.String("canonical_id", h.CanonicalID))
	}
}

// WorkReconciler enqueues ready board-enabled objects that have no live run and
// no live job.
type WorkReconciler struct {
	workMaintenance
}

// NewWorkReconciler creates a reconciler.
func NewWorkReconciler(repo *Repository, log *slog.Logger, interval time.Duration) *WorkReconciler {
	if interval <= 0 {
		interval = defaultWorkReconcilerInterval
	}
	return &WorkReconciler{
		workMaintenance: workMaintenance{
			repo:     repo,
			log:      log.With("component", "work-reconciler"),
			interval: interval,
			stopCh:   make(chan struct{}),
			doneCh:   make(chan struct{}),
		},
	}
}

// SetWorkObjectStore injects the graph work-object surface after construction.
func (rc *WorkReconciler) SetWorkObjectStore(store WorkObjectStore) {
	rc.workObjects = store
}

// Start begins the periodic reconciler goroutine.
func (rc *WorkReconciler) Start(ctx context.Context) {
	rc.start(ctx, rc.reconcile)
}

// Stop signals the reconciler to stop and waits for it.
func (rc *WorkReconciler) Stop() { rc.stop() }

// reconcile finds ready board-enabled objects with no live run/job and enqueues
// the listening agent.
func (rc *WorkReconciler) reconcile(ctx context.Context) {
	if rc.workObjects == nil {
		return
	}
	heads, err := rc.workObjects.ListWorkObjectsByStatus(ctx, "", "ready", time.Time{}, 500)
	if err != nil {
		rc.log.Warn("failed to list ready work objects", slog.String("error", err.Error()))
		return
	}
	for _, h := range heads {
		liveRun, err := rc.repo.HasLiveRunForSubject(ctx, h.CanonicalID)
		if err != nil {
			rc.log.Warn("failed to check live run", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
			continue
		}
		if liveRun {
			continue
		}
		liveJob, err := rc.repo.HasLiveJobForSubject(ctx, h.CanonicalID)
		if err != nil {
			rc.log.Warn("failed to check live job", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
			continue
		}
		if liveJob {
			continue // pending/processing job already covers it
		}
		rc.enqueue(ctx, h)
	}
}

// enqueue wakes the listening agent(s) for a ready object.
func (rc *WorkReconciler) enqueue(ctx context.Context, h *graph.WorkObjectHead) {
	agents, err := rc.repo.FindReactionAgentsForType(ctx, h.ProjectID, h.Type, h.Assignee)
	if err != nil {
		rc.log.Warn("failed to resolve listening agents", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
		return
	}
	if len(agents) == 0 {
		// Unroutable: no listener for the type/assignee. Left to the board's
		// derived unroutable predicate; not re-persisted.
		return
	}
	for _, agent := range agents {
		canonicalID := h.CanonicalID
		objectType := h.Type
		if _, err := rc.repo.CreateRunQueued(ctx, agent.ID, 1, CreateRunQueuedOptions{
			SubjectObjectID:   &canonicalID,
			SubjectObjectType: &objectType,
			TrustedInternal:   true,
		}); err != nil {
			rc.log.Warn("failed to enqueue reconciled work run", slog.String("canonical_id", h.CanonicalID), slog.String("agent_id", agent.ID), slog.String("error", err.Error()))
			continue
		}
		rc.log.Info("reconciled work object enqueued", slog.String("canonical_id", h.CanonicalID), slog.String("agent", agent.Name))
	}
}
