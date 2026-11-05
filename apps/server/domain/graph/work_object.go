package graph

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// WorkObjectHead is the HEAD projection of a work object the agents domain
// needs for dispatch dedup and work-item claims. It carries only the fields the
// claim/dedup/transition paths key on — the object's canonical identity, type,
// key, status, version, and assignee.
type WorkObjectHead struct {
	ProjectID   string
	CanonicalID string
	Type        string
	Key         string
	Status      string
	Version     int
	Assignee    string
}

// workObjectHead builds a WorkObjectHead from a HEAD GraphObject.
func workObjectHead(obj *GraphObject) *WorkObjectHead {
	head := &WorkObjectHead{
		ProjectID:   obj.ProjectID.String(),
		CanonicalID: obj.CanonicalID.String(),
		Type:        obj.Type,
		Version:     obj.Version,
	}
	if obj.Key != nil {
		head.Key = *obj.Key
	}
	if obj.Status != nil {
		head.Status = *obj.Status
	}
	if obj.Assignee != nil {
		head.Assignee = *obj.Assignee
	}
	return head
}

// WorkItem is the Kanban projection of a board-enabled work object joined to
// its latest run (the execution badge). It carries the HEAD's work state
// (status, assignee, key, type) plus the latest run's execution status and
// failure class, the total run count, the review flag, and the derived
// unroutable flag (set by the agents domain — a board-enabled object whose type
// or assignee matches no listening agent).
type WorkItem struct {
	ProjectID   string
	CanonicalID string
	Type        string
	Key         string
	Status      string
	Assignee    string
	Version     int
	UpdatedAt   time.Time
	NeedsReview bool

	LatestRunStatus       string
	LatestRunFailureClass string
	LatestRunID           string
	LatestRunAgentName    string
	// IsWorking is true while the latest run is live (queued/running/paused).
	IsWorking bool
	// NextRunAt is the scheduled retry time for a queued run.
	NextRunAt *time.Time
	RunCount  int

	// Blocked is a durable execution state; the workflow status is preserved.
	Blocked     bool
	BlockReason string
	BlockedAt   *time.Time

	Unroutable bool
}

// WorkObjectTransition describes a single status transition applied through the
// platform's single-work-status write path. It always carries a from/to status;
// the optional fields express review and unassign side effects.
type WorkObjectTransition struct {
	// FromStatus is the status the HEAD must currently be in for the transition
	// to apply (a defensive assert, mirroring the claim's ready assert). An empty
	// value skips the assert (caller owns the check).
	FromStatus string
	// ToStatus is the new status written to both the status column and
	// properties["status"]. When KeepStatus is true it is ignored and the HEAD's
	// current status is preserved (used by the reassign path, which changes the
	// assignee without a status move).
	ToStatus string
	// KeepStatus, when true, preserves the HEAD's current status as the new
	// version's status instead of ToStatus.
	KeepStatus bool
	// SetNeedsReview, when non-nil, sets the needs_review column on the new
	// version (used by work_complete with requiresReview, and cleared by approve
	// and request-changes).
	SetNeedsReview *bool
	// ClearAssignee, when true, nulls the assignee on the new version (used by
	// the capability-failure unassign path).
	ClearAssignee bool
	// SetAssignee, when non-nil, sets the assignee on the new version; an empty
	// string clears it. Takes precedence over ClearAssignee.
	SetAssignee *string
	// SetReviewedBy, when non-nil, sets reviewed_by on the new version (approve).
	SetReviewedBy *uuid.UUID
	// SetReviewedAt, when non-nil, sets reviewed_at on the new version (approve).
	SetReviewedAt *time.Time
	// EmitAssigned, when true, emits an entity.assigned event after commit
	// instead of the status_changed event (used by the reassign path, which
	// preserves status). The event is emitted only after a successful commit.
	EmitAssigned bool
	// ReworkAgentID, when non-nil, marks the transition as a request-changes
	// rework owned by the given agent. It is carried on the emitted
	// status_changed payload under "rework_agent_id" so the event-driven
	// dispatch can suppress the owning agent's re-dispatch (the request-changes
	// action has already enqueued the feedback-carrying rework run directly).
	ReworkAgentID *uuid.UUID
}

// GetHeadObject returns the HEAD version of a work object identified by its
// canonical ID (main graph, project-scoped). Returns (nil, nil) when the object
// is not found.
func (s *Service) GetHeadObject(ctx context.Context, projectID, canonicalID string) (*WorkObjectHead, error) {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil, err
	}
	cid, err := uuid.Parse(canonicalID)
	if err != nil {
		return nil, err
	}
	obj, err := s.repo.GetHeadByCanonicalID(ctx, s.repo.DB(), pid, cid, nil)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if obj == nil {
		return nil, nil
	}
	return workObjectHead(obj), nil
}

// FindHeadByTypeAndKey returns the HEAD version of an object identified by
// (project, type, key) on the main graph, or (nil, nil) when not found. It
// resolves a declared work-contract deliverable to an existing object (P6).
func (s *Service) FindHeadByTypeAndKey(ctx context.Context, projectID, objType, key string) (*WorkObjectHead, error) {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil, err
	}
	obj, err := s.repo.FindHeadByTypeAndKey(ctx, s.repo.DB(), pid, nil, objType, key)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, nil
	}
	return workObjectHead(obj), nil
}

// ClaimWorkObject atomically transitions a work object from readyStatus to
// inProgressStatus under the versioned write model: it acquires the object's
// advisory upsert lock in a transaction, re-reads the HEAD, asserts the status
// is still readyStatus, and then creates the next version. It returns
// claimed=false (nil error) when the object is not found, has no key, or is no
// longer in readyStatus (a lost race) — the caller must skip the run without
// consuming any failure budget.
func (s *Service) ClaimWorkObject(ctx context.Context, projectID, canonicalID, readyStatus, inProgressStatus string) (bool, error) {
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		FromStatus: readyStatus,
		ToStatus:   inProgressStatus,
	})
}

// TransitionWorkObject is the single-work-status write path: advisory lock +
// read HEAD + CreateVersion, keeping status and properties["status"] consistent.
// It returns transitioned=false (nil error) when the object is not found, has no
// key, or is not in fromStatus (a lost race).
func (s *Service) TransitionWorkObject(ctx context.Context, projectID, canonicalID string, t WorkObjectTransition) (bool, error) {
	return s.transitionWorkObject(ctx, projectID, canonicalID, t)
}

// CompleteWorkObject finalizes a work object: → doneStatus, or → reviewStatus
// with needs_review=true when requiresReview. It is the transition invoked by
// the work_complete terminator.
func (s *Service) CompleteWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, doneStatus, reviewStatus string, requiresReview bool) (bool, error) {
	if requiresReview {
		nr := true
		return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
			FromStatus:     inProgressStatus,
			ToStatus:       reviewStatus,
			SetNeedsReview: &nr,
		})
	}
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		FromStatus: inProgressStatus,
		ToStatus:   doneStatus,
	})
}

// BlockWorkObject transitions a work object to the blocked status. It is the
// transition invoked by the work_block terminator (and the dead-letter path).
func (s *Service) BlockWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, blockedStatus string) (bool, error) {
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		FromStatus: inProgressStatus,
		ToStatus:   blockedStatus,
	})
}

// UnassignWorkObject clears the assignee and returns the object to the ready
// status (capability failure: wrong specialist). Attempt history survives.
func (s *Service) UnassignWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, readyStatus string) (bool, error) {
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		FromStatus:    inProgressStatus,
		ToStatus:      readyStatus,
		ClearAssignee: true,
	})
}

// ApproveWorkObject finalizes a review: it transitions review→done, sets
// reviewed_by/reviewed_at (reviewerID, now), and clears needs_review. The
// review-status assert guarantees a non-review item cannot be approved. This is
// the write path for the previously dormant reviewed_by/reviewed_at columns.
func (s *Service) ApproveWorkObject(ctx context.Context, projectID, canonicalID, reviewStatus, doneStatus, reviewerID string) (bool, error) {
	reviewedBy, err := uuid.Parse(reviewerID)
	if err != nil {
		return false, err
	}
	now := time.Now()
	nr := false
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		FromStatus:     reviewStatus,
		ToStatus:       doneStatus,
		SetNeedsReview: &nr,
		SetReviewedBy:  &reviewedBy,
		SetReviewedAt:  &now,
	})
}

// RequestChangesWorkObject sends a review back for rework: it transitions
// review→revision and clears needs_review (the item is no longer awaiting
// review; the next work_complete re-sets it). Feedback recording and the rework
// enqueue are the agents domain's responsibility (they need the run/queue
// surface). reworkAgentID, when non-nil, records the agent that owns the rework
// dispatch so the emitted status_changed event can suppress that agent's own
// re-dispatch (the request-changes action already enqueued the feedback-carrying
// rework run).
func (s *Service) RequestChangesWorkObject(ctx context.Context, projectID, canonicalID, reviewStatus, revisionStatus string, reworkAgentID *uuid.UUID) (bool, error) {
	nr := false
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		FromStatus:     reviewStatus,
		ToStatus:       revisionStatus,
		SetNeedsReview: &nr,
		ReworkAgentID:  reworkAgentID,
	})
}

// UpdateWorkObjectStatus sets a work object to an exact status (the human
// set-status action): an any→status transition via the single-work-status
// writer. needs_review is set true when the target equals reviewStatus, else
// cleared; reviewed_by/reviewed_at are left untouched. An agent-originated
// status write on a board-enabled type is rejected.
func (s *Service) UpdateWorkObjectStatus(ctx context.Context, projectID, canonicalID, status, reviewStatus string) (bool, error) {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return false, err
	}
	cid, err := uuid.Parse(canonicalID)
	if err != nil {
		return false, err
	}
	// Resolve the object's type so the agent-actor guard can apply the real
	// per-type board-enabled flag (rather than the removed assignee heuristic).
	head, err := s.repo.GetHeadByCanonicalID(ctx, s.repo.DB(), pid, cid, nil)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if head == nil {
		return false, nil
	}
	cfg, err := s.objectTypeWorkConfig(ctx, pid, head.Type)
	if err != nil {
		return false, err
	}
	actorType, _ := actorFromContext(ctx, nil)
	if err := rejectAgentWorkStatusWrite(actorType, isBoardEnabledConfig(cfg), status != ""); err != nil {
		return false, err
	}
	nr := status == reviewStatus
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		ToStatus:       status,
		SetNeedsReview: &nr,
	})
}

// ReassignWorkObject sets or clears the assignee on a work object without a
// status move (the HEAD's current status is preserved). assignee empty clears.
func (s *Service) ReassignWorkObject(ctx context.Context, projectID, canonicalID string, assignee *string) (bool, error) {
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		KeepStatus:   true,
		SetAssignee:  assignee,
		EmitAssigned: true,
	})
}

// CancelWorkObject closes a work item by transitioning it (from any status) to
// the blocked status. Cancelling the in-flight run is the agents domain's
// responsibility.
func (s *Service) CancelWorkObject(ctx context.Context, projectID, canonicalID, blockedStatus string) (bool, error) {
	return s.transitionWorkObject(ctx, projectID, canonicalID, WorkObjectTransition{
		ToStatus: blockedStatus,
	})
}

// transitionWorkObject is the shared single-status writer. Board-enabled types
// never mutate status in place: every transition creates the next version under
// the per-object advisory lock and writes both the status column and
// properties["status"], so the two copies can never diverge (the ContentHash is
// computed from properties+status+key+labels, repository.go:computeContentHash).
func (s *Service) transitionWorkObject(ctx context.Context, projectID, canonicalID string, t WorkObjectTransition) (bool, error) {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return false, err
	}
	cid, err := uuid.Parse(canonicalID)
	if err != nil {
		return false, err
	}

	// Resolve the object's (type, key) from its canonical identity so the
	// transition can take the stable upsert lock and find the HEAD by (type, key).
	head, err := s.repo.GetHeadByCanonicalID(ctx, s.repo.DB(), pid, cid, nil)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if head == nil {
		return false, nil
	}
	if head.Key == nil || *head.Key == "" {
		s.log.Warn("cannot transition work object without a key",
			slog.String("canonical_id", canonicalID),
			slog.String("type", head.Type))
		return false, nil
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.repo.AcquireObjectUpsertLock(ctx, tx.Tx, pid, head.Type, *head.Key); err != nil {
		return false, err
	}

	// Re-read the HEAD inside the lock so the from-status assert is serialized
	// against any concurrent writer creating a new version.
	lockedHead, err := s.repo.FindHeadByTypeAndKey(ctx, tx.Tx, pid, nil, head.Type, *head.Key)
	if err != nil {
		return false, err
	}
	if lockedHead == nil {
		return false, nil
	}

	if t.FromStatus != "" {
		status := ""
		if lockedHead.Status != nil {
			status = *lockedHead.Status
		}
		if status != t.FromStatus {
			return false, nil // lost race: another writer already transitioned it
		}
	}

	actorType := ActorSystem
	assignee := lockedHead.Assignee
	if t.ClearAssignee {
		assignee = nil
	}
	if t.SetAssignee != nil {
		if *t.SetAssignee == "" {
			assignee = nil
		} else {
			v := *t.SetAssignee
			assignee = &v
		}
	}
	toStatus := t.ToStatus
	if t.KeepStatus && lockedHead.Status != nil {
		toStatus = *lockedHead.Status
	}
	prevStatus := ""
	if lockedHead.Status != nil {
		prevStatus = *lockedHead.Status
	}
	newProps := withStatusProperty(lockedHead.Properties, toStatus)
	newVersion := &GraphObject{
		Type:       lockedHead.Type,
		Key:        lockedHead.Key,
		Namespace:  lockedHead.Namespace,
		Status:     &toStatus,
		Assignee:   assignee,
		Properties: newProps,
		Labels:     lockedHead.Labels,
		ActorType:  &actorType,
		// Preserve versioned metadata from the locked HEAD so a status transition
		// never silently drops provenance, extraction confidence, review fields,
		// schema version, the merge ledger, or the migration archive.
		ExtractionConfidence: lockedHead.ExtractionConfidence,
		NeedsReview:          lockedHead.NeedsReview,
		ReviewedBy:           lockedHead.ReviewedBy,
		ReviewedAt:           lockedHead.ReviewedAt,
		SchemaVersion:        lockedHead.SchemaVersion,
		MigrationArchive:     lockedHead.MigrationArchive,
		MergedToCanonicalID:  lockedHead.MergedToCanonicalID,
	}
	if t.SetNeedsReview != nil {
		newVersion.NeedsReview = t.SetNeedsReview
	}
	if t.SetReviewedBy != nil {
		newVersion.ReviewedBy = t.SetReviewedBy
	}
	if t.SetReviewedAt != nil {
		newVersion.ReviewedAt = t.SetReviewedAt
	}
	// Record the explicit change summary for this transition. The work path is
	// the only writer of work status, so without this every board-history entry
	// for a claim/complete/block/approve/request-changes/reassign/set-status move
	// could only be *derived* by diffing versions (#1850). The summary is built
	// from the same property diff the general update path uses, plus the assignee
	// (a built-in column, appended as a synthetic `/assignee` path) so the two
	// surfaces never disagree; it is nil when nothing changed.
	newVersion.ChangeSummary = workTransitionChangeSummary(lockedHead.Properties, prevStatus, newProps, lockedHead.Assignee, assignee)
	if err := s.repo.CreateVersion(ctx, tx.Tx, lockedHead, newVersion); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}

	// Emit after commit (never inside the transaction): the event is advisory
	// and must not roll back with the write. The reassign path emits `assigned`;
	// every other status transition emits `status_changed` only when the status
	// actually moved.
	if t.EmitAssigned {
		s.emitObjectAssigned(ctx, projectID, canonicalID, head.Type, newVersion.Version, toStatus, prevStatus, assignee)
	} else if toStatus != prevStatus {
		s.emitObjectStatusChanged(ctx, projectID, canonicalID, head.Type, newVersion.Version, toStatus, prevStatus, assigneeOrEmpty(assignee), t.ReworkAgentID)
	}
	return true, nil
}

// emitObjectStatusChanged emits a graph_object entity.status_changed event
// (no-op if events not wired). The payload carries the new status, the previous
// status, and the assignee so reaction dispatch can match the status_changed
// signal without a second graph read. When reworkAgentID is non-nil it is
// carried as "rework_agent_id" so the owning agent can be suppressed from
// re-dispatch.
func (s *Service) emitObjectStatusChanged(ctx context.Context, projectID, canonicalID, objType string, version int, newStatus, previousStatus, assignee string, reworkAgentID *uuid.UUID) {
	if s.events == nil {
		return
	}
	s.events.EmitStatusChanged(ctx, events.EntityGraphObject, canonicalID, projectID, &events.EmitOptions{
		Version:    &version,
		ObjectType: objType,
		Data:       statusChangeData(newStatus, previousStatus, assignee, reworkAgentID),
	})
}

// emitObjectAssigned emits a graph_object entity.assigned event (no-op if events
// not wired). The payload carries the new status, the previous status (equal
// under a status-preserving reassign), and the new assignee.
func (s *Service) emitObjectAssigned(ctx context.Context, projectID, canonicalID, objType string, version int, newStatus, previousStatus string, assignee *string) {
	if s.events == nil {
		return
	}
	s.events.EmitAssigned(ctx, events.EntityGraphObject, canonicalID, projectID, &events.EmitOptions{
		Version:    &version,
		ObjectType: objType,
		Data:       statusChangeData(newStatus, previousStatus, assigneeOrEmpty(assignee), nil),
	})
}

// statusChangeData builds the event payload carrying the new status, previous
// status, and assignee for status_changed/assigned events. reworkAgentID, when
// non-nil, adds the "rework_agent_id" marker used to suppress the owning
// agent's re-dispatch on a request-changes transition.
func statusChangeData(newStatus, previousStatus, assignee string, reworkAgentID *uuid.UUID) map[string]any {
	data := make(map[string]any, 4)
	if newStatus != "" {
		data["status"] = newStatus
	}
	if previousStatus != "" {
		data["previousStatus"] = previousStatus
	}
	if assignee != "" {
		data["assignee"] = assignee
	}
	if reworkAgentID != nil {
		data["rework_agent_id"] = reworkAgentID.String()
	}
	return data
}

// assigneeOrEmpty dereferences a nil-able assignee pointer to a string.
func assigneeOrEmpty(a *string) string {
	if a == nil {
		return ""
	}
	return *a
}

// withStatusProperty returns a copy of props with the built-in "status" key set
// to status, keeping the status column and properties["status"] consistent so a
// later write cannot clobber the transition back to the old value.
func withStatusProperty(props map[string]any, status string) map[string]any {
	out := make(map[string]any, len(props)+1)
	for k, v := range props {
		out[k] = v
	}
	out["status"] = status
	return out
}

// workTransitionChangeSummary builds the explicit change summary for a
// single-work-status transition. It reuses computeChangeSummary for the property
// diff — the status mirror in properties is the only property a transition
// changes — but normalizes the previous status onto the old properties first, so
// the status *move* is recorded as an updated `/status` (prev → new) even on the
// first transition, when the create path stored the status only in the column.
// The assignee is a built-in column, so when it moves it is appended as a
// synthetic `/assignee` path in the same RFC 6901 shape. Recording both keeps
// the stored summary — which the board history prefers over its derived
// status/assignee diff (gateway object_preview.go) — complete, so a transition
// can never hide the assignee change. Returns nil when neither the status nor
// the assignee changed (a no-op transition records no change).
func workTransitionChangeSummary(oldProps map[string]any, oldStatus string, newProps map[string]any, oldAssignee, newAssignee *string) map[string]any {
	summary := computeChangeSummary(withStatusProperty(oldProps, oldStatus), newProps)
	if assigneeOrEmpty(oldAssignee) == assigneeOrEmpty(newAssignee) {
		return summary
	}
	if summary == nil {
		summary = map[string]any{
			"added":   map[string]any{},
			"removed": []string{},
			"updated": map[string]any{},
			"paths":   []string{},
			"meta":    map[string]any{"added": 0, "removed": 0, "updated": 0},
		}
	}
	updated, _ := summary["updated"].(map[string]any)
	if updated == nil {
		updated = map[string]any{}
		summary["updated"] = updated
	}
	updated["/assignee"] = map[string]any{
		"from": assigneeSummaryValue(oldAssignee),
		"to":   assigneeSummaryValue(newAssignee),
	}
	paths, _ := summary["paths"].([]string)
	summary["paths"] = append(paths, "/assignee")
	if meta, ok := summary["meta"].(map[string]any); ok {
		meta["updated"] = len(updated)
	}
	return summary
}

// assigneeSummaryValue renders a nullable assignee for the change summary: nil
// becomes JSON null so a cleared assignee reads as an explicit change rather
// than a missing one.
func assigneeSummaryValue(a *string) any {
	if a == nil {
		return nil
	}
	return *a
}

// ListWorkObjectsByStatus returns the HEAD projections of board-enabled work
// objects currently in the given status, on the main graph. projectID empty
// means all projects; olderThan non-zero restricts to objects whose HEAD was
// last written before olderThan.
//
// Board-enabled detection (P4): an object is board-enabled when its type carries
// the per-type `boardEnabled` schema flag, resolved from the schema packs. This
// replaces the P2 `assignee IS NOT NULL` proxy.
func (s *Service) ListWorkObjectsByStatus(ctx context.Context, projectID, status string, olderThan time.Time, limit int) ([]*WorkObjectHead, error) {
	var pid *uuid.UUID
	if projectID != "" {
		p, err := uuid.Parse(projectID)
		if err != nil {
			return nil, err
		}
		pid = &p
	}
	boardRefs, err := s.repo.listBoardEnabledTypeRefs(ctx, pid)
	if err != nil {
		return nil, err
	}
	objs, err := s.repo.listWorkObjectsByStatus(ctx, boardRefs, status, olderThan, limit)
	if err != nil {
		return nil, err
	}
	heads := make([]*WorkObjectHead, len(objs))
	for i, o := range objs {
		heads[i] = workObjectHead(o)
	}
	return heads, nil
}

// ListWorkItems returns the board projection of board-enabled work objects:
// the HEAD joined to its latest run (execution status, failure class, run
// count) via subject_object_id = canonical_id. status empty returns every
// status; typeName empty returns every board-enabled type. Items are ordered by
// updated_at ascending. The Unroutable flag is left false here — it is a
// derived, agents-domain predicate the caller overlays.
func (s *Service) ListWorkItems(ctx context.Context, projectID, status, typeName string, limit int) ([]*WorkItem, error) {
	var pid *uuid.UUID
	if projectID != "" {
		p, err := uuid.Parse(projectID)
		if err != nil {
			return nil, err
		}
		pid = &p
	}
	boardRefs, err := s.repo.listBoardEnabledTypeRefs(ctx, pid)
	if err != nil {
		return nil, err
	}
	objs, err := s.repo.listBoardWorkObjects(ctx, boardRefs, status, typeName, limit)
	if err != nil {
		return nil, err
	}

	canonicalIDs := make([]uuid.UUID, 0, len(objs))
	for _, o := range objs {
		canonicalIDs = append(canonicalIDs, o.CanonicalID)
	}
	runs, err := s.repo.listLatestRunsForSubjects(ctx, canonicalIDs)
	if err != nil {
		return nil, err
	}
	blocks, err := s.repo.listWorkBlocksForSubjects(ctx, canonicalIDs)
	if err != nil {
		return nil, err
	}

	items := make([]*WorkItem, 0, len(objs))
	for _, o := range objs {
		item := &WorkItem{
			ProjectID:   o.ProjectID.String(),
			CanonicalID: o.CanonicalID.String(),
			Type:        o.Type,
			Version:     o.Version,
			UpdatedAt:   o.UpdatedAt,
		}
		if o.Key != nil {
			item.Key = *o.Key
		}
		if o.Status != nil {
			item.Status = *o.Status
		}
		if o.Assignee != nil {
			item.Assignee = *o.Assignee
		}
		if o.NeedsReview != nil {
			item.NeedsReview = *o.NeedsReview
		}
		if run := runs[o.CanonicalID]; run != nil {
			item.LatestRunStatus = run.Status
			item.LatestRunID = run.RunID
			item.LatestRunAgentName = run.AgentName
			item.NextRunAt = run.NextRunAt
			item.RunCount = run.RunCount
			item.IsWorking = run.Status == "queued" || run.Status == "running" || run.Status == "paused"
			if run.FailureClass != nil {
				item.LatestRunFailureClass = *run.FailureClass
			}
		}
		if block := blocks[o.CanonicalID]; block != nil {
			item.Blocked = true
			item.BlockReason = block.Reason
			item.BlockedAt = block.BlockedAt
		}
		items = append(items, item)
	}
	return items, nil
}
