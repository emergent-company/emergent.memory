package agents

import (
	"context"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// WorkObjectStore is the minimal graph surface the agents domain needs to
// dispatch to, and correctly claim, work objects under the versioned write
// model. It is implemented by the graph service and injected via fx.
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
}
