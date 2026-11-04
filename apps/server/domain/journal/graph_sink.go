package journal

import (
	"context"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// GraphEventSinkAdapter wraps journal.Service to satisfy graph.EventSink.
// It translates graph.LogParams into journal.LogParams so domain/graph does not
// import domain/journal.
type GraphEventSinkAdapter struct {
	svc *Service
}

// NewGraphEventSinkAdapter creates a new adapter.
func NewGraphEventSinkAdapter(svc *Service) graph.EventSink {
	return &GraphEventSinkAdapter{svc: svc}
}

// Log translates graph.LogParams to journal.LogParams and calls the underlying service.
func (a *GraphEventSinkAdapter) Log(ctx context.Context, p graph.LogParams) error {
	a.svc.Log(ctx, LogParams{
		ProjectID:  p.ProjectID,
		BranchID:   p.BranchID,
		EventType:  p.EventType,
		EntityType: p.EntityType,
		EntityID:   p.EntityID,
		ObjectType: p.ObjectType,
		ActorType:  p.ActorType,
		ActorID:    p.ActorID,
		Metadata:   p.Metadata,
	})
	return nil
}
