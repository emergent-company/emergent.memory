package graph

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// WorkObjectHead is the HEAD projection of a work object the agents domain
// needs for dispatch dedup and work-item claims. It carries only the fields the
// claim/dedup paths key on — the object's canonical identity, type, key, status,
// version, and assignee.
type WorkObjectHead struct {
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
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return false, err
	}
	cid, err := uuid.Parse(canonicalID)
	if err != nil {
		return false, err
	}

	// Resolve the object's (type, key) from its canonical identity so the claim
	// can take the stable upsert lock and find the HEAD by (type, key).
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
		s.log.Warn("cannot claim work object without a key",
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

	// Re-read the HEAD inside the lock so the status assert is serialized
	// against any concurrent claimant writing a new version.
	lockedHead, err := s.repo.FindHeadByTypeAndKey(ctx, tx.Tx, pid, nil, head.Type, *head.Key)
	if err != nil {
		return false, err
	}
	if lockedHead == nil {
		return false, nil
	}
	status := ""
	if lockedHead.Status != nil {
		status = *lockedHead.Status
	}
	if status != readyStatus {
		return false, nil // lost race: another claimant already transitioned it
	}

	actorType := ActorSystem
	newVersion := &GraphObject{
		Type:       lockedHead.Type,
		Key:        lockedHead.Key,
		Status:     &inProgressStatus,
		Assignee:   lockedHead.Assignee,
		Properties: withStatusProperty(lockedHead.Properties, inProgressStatus),
		Labels:     lockedHead.Labels,
		ActorType:  &actorType,
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
