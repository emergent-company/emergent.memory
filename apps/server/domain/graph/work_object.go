package graph

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

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

// WorkObjectTransition describes a single status transition applied through the
// platform's single-work-status write path. It always carries a from/to status;
// the optional fields express review and unassign side effects.
type WorkObjectTransition struct {
	// FromStatus is the status the HEAD must currently be in for the transition
	// to apply (a defensive assert, mirroring the claim's ready assert). An empty
	// value skips the assert (caller owns the check).
	FromStatus string
	// ToStatus is the new status written to both the status column and
	// properties["status"].
	ToStatus string
	// SetNeedsReview, when non-nil, sets the needs_review column on the new
	// version (used by work_complete with requiresReview).
	SetNeedsReview *bool
	// ClearAssignee, when true, nulls the assignee on the new version (used by
	// the capability-failure unassign path).
	ClearAssignee bool
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
	defer tx.Rollback()

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
	newVersion := &GraphObject{
		Type:       lockedHead.Type,
		Key:        lockedHead.Key,
		Status:     &t.ToStatus,
		Assignee:   assignee,
		Properties: withStatusProperty(lockedHead.Properties, t.ToStatus),
		Labels:     lockedHead.Labels,
		ActorType:  &actorType,
	}
	if t.SetNeedsReview != nil {
		newVersion.NeedsReview = t.SetNeedsReview
	}
	if err := s.repo.CreateVersion(ctx, tx.Tx, lockedHead, newVersion); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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

// ListWorkObjectsByStatus returns the HEAD projections of board-enabled work
// objects currently in the given status, on the main graph. projectID empty
// means all projects; olderThan non-zero restricts to objects whose HEAD was
// last written before olderThan.
//
// Board-enabled detection (P2 minimal mechanism): an object whose `assignee` is
// non-null is treated as board-enabled. This is the pragmatic, self-contained
// signal available before the per-type `boardEnabled` schema flag lands (P4);
// null-assignee "any-listener" work objects are not yet distinguishable as
// board-enabled without that flag. See design.md "Object-type configuration".
func (s *Service) ListWorkObjectsByStatus(ctx context.Context, projectID, status string, olderThan time.Time, limit int) ([]*WorkObjectHead, error) {
	var pid *uuid.UUID
	if projectID != "" {
		p, err := uuid.Parse(projectID)
		if err != nil {
			return nil, err
		}
		pid = &p
	}
	objs, err := s.repo.listWorkObjectsByStatus(ctx, pid, status, olderThan, limit)
	if err != nil {
		return nil, err
	}
	heads := make([]*WorkObjectHead, len(objs))
	for i, o := range objs {
		heads[i] = workObjectHead(o)
	}
	return heads, nil
}
