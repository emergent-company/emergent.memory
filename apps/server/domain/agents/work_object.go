package agents

import (
	"context"
	"time"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
)

// WorkObjectStore is the minimal graph surface the agents domain needs to
// dispatch to, and correctly claim and transition, work objects under the
// versioned write model. It is implemented by the graph service and injected
// via fx.
type WorkObjectStore interface {
	// GetHeadObject returns the HEAD version of the object identified by its
	// canonical ID (project-scoped). Returns (nil, nil) when not found.
	GetHeadObject(ctx context.Context, projectID, canonicalID string) (*graph.WorkObjectHead, error)
	// ClaimWorkObject atomically transitions a work object from readyStatus to
	// inProgressStatus under the object's advisory upsert lock, creating a new
	// version. It returns claimed=false (and a nil error) when the object's
	// current status is not readyStatus, when the object has no key, or when the
	// object is not found — the caller must then skip the run without consuming
	// any failure budget.
	ClaimWorkObject(ctx context.Context, projectID, canonicalID, readyStatus, inProgressStatus string) (claimed bool, err error)
	// TransitionWorkObject is the single-work-status write path (advisory lock +
	// CreateVersion, status and properties["status"] kept consistent). It returns
	// transitioned=false when the object is missing, has no key, or is not in the
	// from status.
	TransitionWorkObject(ctx context.Context, projectID, canonicalID string, t graph.WorkObjectTransition) (bool, error)
	// CompleteWorkObject finalizes a work object: → doneStatus, or → reviewStatus
	// with needs_review=true when requiresReview.
	CompleteWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, doneStatus, reviewStatus string, requiresReview bool) (bool, error)
	// BlockWorkObject transitions a work object to the blocked status.
	BlockWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, blockedStatus string) (bool, error)
	// UnassignWorkObject clears the assignee and returns the object to ready.
	UnassignWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, readyStatus string) (bool, error)
	// ApproveWorkObject finalizes a review: review→done, sets reviewed_by/at,
	// clears needs_review.
	ApproveWorkObject(ctx context.Context, projectID, canonicalID, reviewStatus, doneStatus, reviewerID string) (bool, error)
	// RequestChangesWorkObject sends a review back for rework: review→revision.
	RequestChangesWorkObject(ctx context.Context, projectID, canonicalID, reviewStatus, revisionStatus string) (bool, error)
	// ReassignWorkObject sets/clears the assignee without a status move.
	ReassignWorkObject(ctx context.Context, projectID, canonicalID string, assignee *string) (bool, error)
	// CancelWorkObject closes a work item: any→blocked.
	CancelWorkObject(ctx context.Context, projectID, canonicalID, blockedStatus string) (bool, error)
	// ListWorkObjectsByStatus returns the HEAD projections of board-enabled work
	// objects currently in the given status (projectID empty = all projects;
	// olderThan non-zero restricts to objects last written before olderThan).
	ListWorkObjectsByStatus(ctx context.Context, projectID, status string, olderThan time.Time, limit int) ([]*graph.WorkObjectHead, error)
	// ListWorkItems returns the Kanban projection of board-enabled work objects
	// joined to their latest run (status empty = all statuses; typeName empty =
	// all board-enabled types).
	ListWorkItems(ctx context.Context, projectID, status, typeName string, limit int) ([]*graph.WorkItem, error)
	// GetObjectTypeWorkConfig resolves the per-type object-driven work config for
	// a project+type (P4). Returns (nil, nil) for unknown/unconfigured types. It
	// is the surface used to apply per-type failureLimit/retryPolicy overrides
	// (P4.3).
	GetObjectTypeWorkConfig(ctx context.Context, projectID, typeName string) (*agents.ObjectTypeWorkConfig, error)
}
