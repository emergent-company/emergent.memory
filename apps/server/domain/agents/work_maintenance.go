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

// ListRunsForSubject returns the runs linked to the subject work object, newest
// first, capped at limit. It backs the board card drawer's run history.
func (r *Repository) ListRunsForSubject(ctx context.Context, canonicalID string, limit int) ([]*AgentRun, error) {
	if limit <= 0 {
		limit = 20
	}
	var runs []*AgentRun
	err := r.db.NewSelect().
		Model(&runs).
		Where("subject_object_id = ?", canonicalID).
		Order("created_at DESC").
		Limit(limit).
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
// whose trigger lists objectType (or a wildcard) and is subscribed to the
// `created` event, filtered to those matching assignee when assignee is
// non-empty, and restricted to object-work agents (a definition with a work
// config or queued dispatch). Legacy inline agents and `updated`/`deleted`-only
// listeners are never woken by the reconciler — mirroring the object-driven
// dispatch gate.
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
		if !reactionSubscribesToCreated(a.ReactionConfig) {
			continue
		}
		def, err := r.ResolveDefinitionForAgent(ctx, a)
		if err != nil || def == nil {
			continue
		}
		if def.DispatchMode != DispatchModeQueued && def.WorkConfig.IsZero() {
			continue // legacy inline agent: not object-driven work
		}
		out = append(out, a)
	}
	return out, nil
}

// workListenerIndex is the project's routable-work footprint: for each object
// type (and "*" for wildcard listeners), the set of enabled work-agent names
// subscribed to the created event. It backs the board's derived unroutable
// predicate without per-item queries.
type workListenerIndex struct {
	typeListeners map[string]map[string]bool
}

// buildWorkListenerIndex loads every enabled reaction agent in the project that
// is an object-driven work listener (queued dispatch or a work config) and
// indexes its name by the object types it subscribes to.
func (r *Repository) buildWorkListenerIndex(ctx context.Context, projectID string) (*workListenerIndex, error) {
	agents, err := r.FindEnabledByTriggerType(ctx, TriggerTypeReaction)
	if err != nil {
		return nil, err
	}
	idx := &workListenerIndex{typeListeners: map[string]map[string]bool{}}
	add := func(objType, name string) {
		if idx.typeListeners[objType] == nil {
			idx.typeListeners[objType] = map[string]bool{}
		}
		idx.typeListeners[objType][name] = true
	}
	for _, a := range agents {
		if a == nil || a.ProjectID != projectID || a.ReactionConfig == nil {
			continue
		}
		if !reactionSubscribesToCreated(a.ReactionConfig) {
			continue
		}
		def, err := r.ResolveDefinitionForAgent(ctx, a)
		if err != nil || def == nil {
			continue
		}
		if def.DispatchMode != DispatchModeQueued && def.WorkConfig.IsZero() {
			continue // legacy inline agent: not object-driven work
		}
		if len(a.ReactionConfig.ObjectTypes) == 0 {
			add("*", a.Name)
		}
		for _, t := range a.ReactionConfig.ObjectTypes {
			add(t, a.Name)
		}
	}
	return idx, nil
}

// isRoutable reports whether a work object with the given type and assignee has
// a listening agent. assignee empty means any listener; non-empty means a
// listener matching that name.
func (idx *workListenerIndex) isRoutable(objType, assignee string) bool {
	if idx == nil {
		return false
	}
	names := map[string]bool{}
	for n := range idx.typeListeners[objType] {
		names[n] = true
	}
	for n := range idx.typeListeners["*"] {
		names[n] = true
	}
	if len(names) == 0 {
		return false
	}
	return assignee == "" || names[assignee]
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

// reactionSubscribesToCreated reports whether a reaction config is subscribed to
// the `created` event (the only event that wakes an object-driven work run).
func reactionSubscribesToCreated(rc *ReactionConfig) bool {
	for _, e := range rc.Events {
		if e == EventTypeCreated {
			return true
		}
	}
	return false
}

// listWorkAgentStatuses returns the distinct ready and in-progress status values
// configured by object-work reaction agents (definitions with a work config or
// queued dispatch), always including the built-in defaults. The reaper and
// reconciler use these to scan for work objects under custom status mappings.
func (r *Repository) listWorkAgentStatuses(ctx context.Context) (ready, inProgress []string, err error) {
	type row struct {
		Ready      string `bun:"ready"`
		InProgress string `bun:"in_progress"`
	}
	var rows []row
	err = r.db.NewRaw(`
		SELECT DISTINCT
			COALESCE(NULLIF(ad.work_config->'status'->>'ready',''), 'ready')        AS ready,
			COALESCE(NULLIF(ad.work_config->'status'->>'inProgress',''), 'in_progress') AS in_progress
		FROM kb.agents a
		JOIN kb.agent_definitions ad ON ad.id = a.agent_definition_id
		WHERE a.enabled = true
		  AND a.trigger_type = 'reaction'
		  AND (ad.dispatch_mode = 'queued' OR (ad.work_config IS NOT NULL AND ad.work_config <> '{}'::jsonb))
	`).Scan(ctx, &rows)
	if err != nil {
		return nil, nil, err
	}
	readySet := map[string]bool{"ready": true}
	inProgressSet := map[string]bool{"in_progress": true}
	for _, r := range rows {
		if r.Ready != "" {
			readySet[r.Ready] = true
		}
		if r.InProgress != "" {
			inProgressSet[r.InProgress] = true
		}
	}
	for s := range readySet {
		ready = append(ready, s)
	}
	for s := range inProgressSet {
		inProgress = append(inProgress, s)
	}
	return ready, inProgress, nil
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
	_, inProgressStatuses, err := r.repo.listWorkAgentStatuses(ctx)
	if err != nil {
		r.log.Warn("failed to resolve in-progress statuses", slog.String("error", err.Error()))
		return
	}
	for _, status := range inProgressStatuses {
		heads, err := r.workObjects.ListWorkObjectsByStatus(ctx, "", status, cutoff, 500)
		if err != nil {
			r.log.Warn("failed to list in-progress work objects", slog.String("status", status), slog.String("error", err.Error()))
			continue
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
}

// release returns a stranded in-progress object to ready, or blocks it when its
// failure budget is exhausted (a crash consumes budget). The owning listener's
// effective work policy (type > agent > default) supplies the statuses and the
// failure limit so custom status mappings are honoured.
func (r *WorkStatusReaper) release(ctx context.Context, h *graph.WorkObjectHead) {
	_, def := resolveOwningAgentForHead(ctx, r.repo, h)
	pol := resolveWorkObjectPolicy(ctx, r.workObjects, def, h.ProjectID, h.Type)
	count, err := r.repo.IncrementWorkItemFailure(ctx, h.ProjectID, h.CanonicalID, string(FailureClassDeterministic), nil)
	if err != nil {
		r.log.Warn("failed to record reaper failure", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
		return
	}
	if count > pol.failureLimit {
		if ok, err := r.workObjects.BlockWorkObject(ctx, h.ProjectID, h.CanonicalID, pol.inProgress, pol.blocked); err != nil {
			r.log.Warn("failed to block stranded work object", slog.String("canonical_id", h.CanonicalID), slog.String("error", err.Error()))
		} else if ok {
			r.log.Warn("blocked stranded work object (budget exhausted)", slog.String("canonical_id", h.CanonicalID))
		}
		return
	}
	if ok, err := r.workObjects.TransitionWorkObject(ctx, h.ProjectID, h.CanonicalID, graph.WorkObjectTransition{
		FromStatus: pol.inProgress,
		ToStatus:   pol.ready,
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
// the listening agent. It scans each object-work agent's configured ready status
// (not only the literal "ready") so custom status mappings are honoured.
func (rc *WorkReconciler) reconcile(ctx context.Context) {
	if rc.workObjects == nil {
		return
	}
	readyStatuses, _, err := rc.repo.listWorkAgentStatuses(ctx)
	if err != nil {
		rc.log.Warn("failed to resolve ready statuses", slog.String("error", err.Error()))
		return
	}
	for _, status := range readyStatuses {
		heads, err := rc.workObjects.ListWorkObjectsByStatus(ctx, "", status, time.Time{}, 500)
		if err != nil {
			rc.log.Warn("failed to list ready work objects", slog.String("status", status), slog.String("error", err.Error()))
			continue
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

// resolveOwningAgentForHead resolves the agent (and its definition) that owns a
// work object's HEAD: the agent of the latest subject run, falling back to the
// assignee. Returns nil when neither resolves.
func resolveOwningAgentForHead(ctx context.Context, repo *Repository, h *graph.WorkObjectHead) (*Agent, *AgentDefinition) {
	var agent *Agent
	if run, err := repo.FindLatestRunForSubject(ctx, h.CanonicalID); err == nil && run != nil {
		agent, _ = repo.FindByID(ctx, run.AgentID, &h.ProjectID)
	}
	if agent == nil && h.Assignee != "" {
		agent, _ = repo.FindByName(ctx, h.ProjectID, h.Assignee)
	}
	if agent == nil {
		return nil, nil
	}
	def, _ := repo.ResolveDefinitionForAgent(ctx, agent)
	return agent, def
}

// workObjectPolicy is the resolved status mapping and failure budget for a work
// object, layering per-type failure-limit overrides over the owning agent's work
// config (type > agent > default).
type workObjectPolicy struct {
	ready        string
	inProgress   string
	blocked      string
	failureLimit int
}

// resolveWorkObjectPolicy resolves the effective statuses and failure budget for
// a work object from its owning agent's definition, with per-type overrides
// (type > agent > default).
func resolveWorkObjectPolicy(ctx context.Context, workObjects WorkObjectStore, agentDef *AgentDefinition, projectID, subjectType string) workObjectPolicy {
	wc := workConfigOf(agentDef)
	pol := workObjectPolicy{
		ready:        wc.ReadyStatus(),
		inProgress:   wc.InProgressStatus(),
		blocked:      wc.BlockedStatus(),
		failureLimit: wc.FailureLimitValue(),
	}
	if workObjects != nil && subjectType != "" {
		if cfg, err := workObjects.GetObjectTypeWorkConfig(ctx, projectID, subjectType); err == nil && cfg != nil && cfg.FailureLimit > 0 {
			pol.failureLimit = cfg.FailureLimit
		}
	}
	return pol
}
