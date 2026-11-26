package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/runstatus"
)

// Work maintenance intervals and thresholds. These are the P2 defaults; the
// work-status reaper threshold deliberately matches the run reaper's
// staleRunThreshold so the two never race (design.md "Claim liveness").
const (
	defaultWorkReaperInterval     = 5 * time.Minute
	defaultWorkReaperThreshold    = 30 * time.Minute
	defaultWorkReconcilerInterval = 1 * time.Minute
)

// workLiveRunStatuses are the run states that still own their subject work
// object. A paused run (ask_user) is live: a human waiting on a question must
// not have the item reclaimed and run twice. The list comes from runstatus so
// the predicate cannot drift from the canonical vocabulary again (00113).
var workLiveRunStatuses = runstatus.LiveStrings()

// HasLiveRunForSubject reports whether any run for the subject object is live
// (submitted, working, or input-required).
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

// FindLiveRunsForSubject returns the live (submitted/working/input-required) runs linked to
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
// first, capped at limit. It backs the board card drawer's run history, so the
// Agent relation is preloaded: the drawer names which agent ran, a fact the run
// row itself does not carry (agent_id is a runtime kb.agents row, not the
// definition the human picked).
func (r *Repository) ListRunsForSubject(ctx context.Context, canonicalID string, limit int) ([]*AgentRun, error) {
	if limit <= 0 {
		limit = 20
	}
	var runs []*AgentRun
	err := r.db.NewSelect().
		Model(&runs).
		Relation("Agent").
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
		if !reactionSubscribesToWork(a.ReactionConfig) {
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
		if !reactionSubscribesToWork(a.ReactionConfig) {
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

// reactionSubscribesToWork reports whether a reaction config subscribes to a
// pickup signal that can wake an object-driven work run from the reconciler's
// scan: `created` (a new object) or `status_changed` (an object moved into a
// trigger status). `updated` and `assigned` are event-driven only and are not
// reconciled, so the safety-net scan ignores them.
func reactionSubscribesToWork(rc *ReactionConfig) bool {
	for _, e := range rc.Events {
		if e == EventTypeCreated || e == EventTypeStatusChanged {
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
		UNION
		SELECT DISTINCT
			COALESCE(NULLIF(r->'status'->>'ready',''), 'ready')        AS ready,
			COALESCE(NULLIF(r->'status'->>'inProgress',''), 'in_progress') AS in_progress
		FROM kb.agents a
		JOIN kb.agent_definitions ad ON ad.id = a.agent_definition_id
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(ad.work_config->'objectRules', '[]'::jsonb)) r
		WHERE a.enabled = true
		  AND a.trigger_type = 'reaction'
		  AND r ? 'status'
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

	// Trigger statuses (the status_changed signal) are reconciled as additional
	// "ready-like" statuses: an object stranded in a trigger status with no live
	// run is re-enqueued as a safety net.
	var triggerRows []struct {
		Status string `bun:"status"`
	}
	err = r.db.NewRaw(`
		SELECT DISTINCT jsonb_array_elements_text(
			COALESCE(ad.work_config->'triggerStatuses', '[]'::jsonb)
		) AS status
		FROM kb.agents a
		JOIN kb.agent_definitions ad ON ad.id = a.agent_definition_id
		WHERE a.enabled = true
		  AND a.trigger_type = 'reaction'
		  AND (ad.dispatch_mode = 'queued' OR (ad.work_config IS NOT NULL AND ad.work_config <> '{}'::jsonb))
		UNION
		SELECT DISTINCT t->>'status' AS status
		FROM kb.agents a
		JOIN kb.agent_definitions ad ON ad.id = a.agent_definition_id
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(ad.work_config->'objectRules', '[]'::jsonb)) r
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(r->'triggers', '[]'::jsonb)) t
		WHERE a.enabled = true
		  AND a.trigger_type = 'reaction'
		  AND COALESCE(t->>'status','') <> ''
	`).Scan(ctx, &triggerRows)
	if err != nil {
		return nil, nil, err
	}
	for _, tr := range triggerRows {
		if tr.Status != "" {
			readySet[tr.Status] = true
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
	repo           *Repository
	workObjects    WorkObjectStore
	workInvocation WorkInvocationResolver
	log            *slog.Logger
	interval       time.Duration

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
				continue // still owned by a live (submitted/working/input-required) run
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
func (rc *WorkReconciler) SetWorkInvocationResolver(r WorkInvocationResolver) {
	rc.workInvocation = r
}

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
	// One budget verdict per project per reconcile pass: a capped project may
	// have many parked items and the gate query must not be repeated per item.
	budgetErrs := map[string]error{}
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
			rc.enqueue(ctx, h, budgetErrs)
		}
	}
}

// enqueue wakes the listening agent(s) for a ready object.
func (rc *WorkReconciler) enqueue(ctx context.Context, h *graph.WorkObjectHead, budgetErrs map[string]error) {
	if block, err := rc.repo.GetWorkBlock(ctx, h.CanonicalID); err == nil && block != nil {
		if !isBudgetBlock(block) {
			return
		}
		// A budget park is auto-recoverable: clearing it is what makes a raised
		// cap or reset window resume the item without a human unblock. While
		// the project is still over cap, returning here is the reconciler's
		// stop condition — no run is attempted, so no refusal is logged per
		// tick.
		budgetErr, ok := budgetErrs[h.ProjectID]
		if !ok {
			budgetErr = rc.repo.checkBudgetGate(ctx, h.ProjectID)
			budgetErrs[h.ProjectID] = budgetErr
		}
		if budgetErr != nil {
			return
		}
		if _, cerr := rc.repo.ClearWorkBlock(ctx, h.CanonicalID); cerr != nil {
			rc.log.Warn("failed to clear budget work block",
				slog.String("canonical_id", h.CanonicalID),
				slog.String("error", cerr.Error()),
			)
			return
		}
		rc.log.Info("budget block cleared; work item is dispatchable again",
			slog.String("canonical_id", h.CanonicalID),
		)
	}
	if skipUnassignedIssue(h.Type, h.Assignee) {
		return
	}
	// An issue another lane claimed stays with that lane: the reconciler scans
	// every ready-status object, so without this it would re-dispatch the coding
	// agent on an issue a peer session owns (emergent.memory #990/#987).
	if skipForeignIssueClaim(h.Type, h) {
		rc.log.Info("skipping reconciled work object: issue claimed by another lane",
			slog.String("canonical_id", h.CanonicalID),
			slog.String("object_type", h.Type),
		)
		return
	}
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
		var def *AgentDefinition
		// Skip a listener whose rules do not pick up this status: the claim guard
		// would reject it anyway, but enqueuing a doomed run is noise.
		if resolved, derr := rc.repo.ResolveDefinitionForAgent(ctx, agent); derr == nil && resolved != nil {
			def = resolved
			if !def.TriggerAllowsStatus(h.Type, h.Status) {
				continue
			}
		}
		canonicalID := h.CanonicalID
		objectType := h.Type
		var trigger *AgentWorkTrigger
		if def != nil {
			trigger = def.MatchWorkTrigger(h.Type, h.Status)
		}
		inv := workRunInvocation(ctx, rc.workInvocation, h.ProjectID, h, trigger)
		opts := CreateRunQueuedOptions{
			SubjectObjectID:   &canonicalID,
			SubjectObjectType: &objectType,
			TrustedInternal:   workRunTrusted(h),
			Invocation:        inv,
			SourceTriggerID:   workSourceTriggerID(h),
			SourceSubject:     workSourceSubject(h),
			// Record the claim source as the object's current status: the
			// reconciler scans both ready and trigger statuses, and the claim must
			// CAS from whatever status the object is stranded in.
			TriggerMetadata: workTriggerMetadata(h),
		}
		if inv != nil {
			opts.Priority = inv.Priority
		}
		if trigger != nil && trigger.Prompt != "" {
			prompt := trigger.Prompt
			opts.TriggerMessage = &prompt
		}
		if _, err := rc.repo.CreateRunQueued(ctx, agent.ID, 1, opts); err != nil {
			if errors.Is(err, ErrWorkItemBusy) {
				continue
			}
			if isBudgetFailure(err) {
				rc.parkBudgetRefused(ctx, h, agent, err)
				continue
			}
			rc.log.Warn("failed to enqueue reconciled work run", slog.String("canonical_id", h.CanonicalID), slog.String("agent_id", agent.ID), slog.String("error", err.Error()))
			continue
		}
		rc.log.Info("reconciled work object enqueued", slog.String("canonical_id", h.CanonicalID), slog.String("agent", agent.Name))
	}
}

// isBudgetBlock reports whether a durable block was recorded by the spending
// gate (as opposed to a poison item or a human cancel).
func isBudgetBlock(block *WorkBlock) bool {
	return block != nil && block.FailureClass != nil && *block.FailureClass == string(FailureClassBudget)
}

// parkBudgetRefused records the same visible, durable park the trigger path
// uses, and notifies listeners so a dependent required check can publish an
// unavailable reason (#1032).
func (rc *WorkReconciler) parkBudgetRefused(ctx context.Context, h *graph.WorkObjectHead, agent *Agent, cause error) {
	reason := fmt.Sprintf("dispatch refused: %s", cause.Error())
	if perr := rc.repo.ParkWorkItemForBudget(ctx, h.ProjectID, h.CanonicalID, reason); perr != nil {
		rc.log.Warn("failed to park work item after budget refusal",
			slog.String("canonical_id", h.CanonicalID),
			slog.String("agent_id", agent.ID),
			slog.String("error", perr.Error()),
		)
	}
	rc.repo.NotifyWorkBudgetParked(ctx, WorkBudgetParkedEvent{
		ProjectID:   h.ProjectID,
		CanonicalID: h.CanonicalID,
		ObjectType:  h.Type,
		AgentID:     agent.ID,
		AgentName:   agent.Name,
		Reason:      reason,
		Head:        h,
	})
	rc.log.Warn("reconciled work dispatch refused by project budget; item parked",
		slog.String("canonical_id", h.CanonicalID),
		slog.String("object_type", h.Type),
		slog.String("agent_id", agent.ID),
		slog.String("project_id", h.ProjectID),
		slog.String("error", cause.Error()),
	)
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
	st := AgentWorkStatusConfig{}
	if agentDef != nil {
		st = agentDef.EffectiveWorkStatus(subjectType)
	} else {
		st = st.withDefaults()
	}
	pol := workObjectPolicy{
		ready:        st.Ready,
		inProgress:   st.InProgress,
		blocked:      st.Blocked,
		failureLimit: wc.FailureLimitValue(),
	}
	if workObjects != nil && subjectType != "" {
		if cfg, err := workObjects.GetObjectTypeWorkConfig(ctx, projectID, subjectType); err == nil && cfg != nil && cfg.FailureLimit > 0 {
			pol.failureLimit = cfg.FailureLimit
		}
	}
	return pol
}

func intProperty(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

// mergeWorkObjectMetadata copies identity fields (repo, issue number, source,
// subject) into a dispatch metadata map without overwriting the event/claim
// keys that describe why this dispatch happened.
func mergeWorkObjectMetadata(meta map[string]any, h *graph.WorkObjectHead) {
	if meta == nil || h == nil {
		return
	}
	for k, v := range workTriggerMetadata(h) {
		if k == "work_trigger_event" || k == "work_claim_status" {
			continue
		}
		meta[k] = v
	}
}

// workSourceSubject returns the verified source subject a source-derived work
// object carries, so a work-dispatched run binds its tools and its implicit
// credential to the source the dispatcher recorded. Only server-written
// properties are read; a model cannot set them. A review lane needs the pull
// request (repo#number@headSha) so review evidence and verdicts key on it even
// though the run was not enqueued by the webhook ingress directly (#1022). An
// issue coding lane needs the trusted issue subject for the same reason: review
// tools still use RepositoryValid, while the implicit source credential path
// requires IssueValid.
func workSourceSubject(h *graph.WorkObjectHead) *SourceSubject {
	if h == nil || h.Properties == nil {
		return nil
	}
	sourceID, _ := h.Properties["source_id"].(string)
	repo, _ := h.Properties["repo"].(string)
	if strings.TrimSpace(sourceID) == "" || strings.TrimSpace(repo) == "" {
		return nil
	}
	prNumber := intProperty(h.Properties["pr_number"])
	headSHA, _ := h.Properties["head_sha"].(string)
	if prNumber > 0 && strings.TrimSpace(headSHA) != "" {
		return &SourceSubject{
			SourceID: sourceID, Repo: repo, Kind: "pull_request",
			PRNumber: prNumber, HeadSHA: headSHA,
		}
	}
	if issueNumber := intProperty(h.Properties["issue_number"]); issueNumber > 0 {
		return &SourceSubject{
			SourceID: sourceID, Repo: repo, Kind: "issue",
			IssueNumber: issueNumber,
		}
	}
	return nil
}

func workTriggerMetadata(h *graph.WorkObjectHead) map[string]any {
	meta := map[string]any{
		"work_trigger_event": string(EventTypeStatusChanged),
		"work_claim_status":  h.Status,
	}
	if h.Properties == nil {
		return meta
	}
	repo, _ := h.Properties["repo"].(string)
	prNumber := intProperty(h.Properties["pr_number"])
	issueNumber := intProperty(h.Properties["issue_number"])
	if repo == "" || (prNumber <= 0 && issueNumber <= 0) {
		return meta
	}
	meta["repo"] = repo
	if sid, _ := h.Properties["source_id"].(string); sid != "" {
		meta["sourceId"] = sid
	}
	if prNumber > 0 {
		meta["prNumber"] = int(prNumber)
		meta["subjectType"] = "pull_request"
		meta["subjectRef"] = fmt.Sprintf("%s#%d", repo, int(prNumber))
		// A review lane carries head_sha directly. A coding issue linked to a
		// pull request carries the reviewed head as pr_head_sha and the branch
		// as pr_branch, so a rework run dispatched from a blocking verdict knows
		// which commit/branch the finding applies to (#1091).
		headSHA, _ := h.Properties["head_sha"].(string)
		if strings.TrimSpace(headSHA) == "" {
			headSHA, _ = h.Properties["pr_head_sha"].(string)
		}
		if strings.TrimSpace(headSHA) != "" {
			meta["headSha"] = headSHA
		}
		branch, _ := h.Properties["pr_branch"].(string)
		if strings.TrimSpace(branch) == "" {
			branch, _ = h.Properties["head_ref"].(string)
		}
		if strings.TrimSpace(branch) != "" {
			meta["branch"] = branch
		}
		return meta
	}
	meta["issueNumber"] = int(issueNumber)
	meta["subjectType"] = "issue"
	meta["subjectRef"] = fmt.Sprintf("%s#%d", repo, int(issueNumber))
	if url, _ := h.Properties["url"].(string); url != "" {
		meta["issueUrl"] = url
	}
	return meta
}
