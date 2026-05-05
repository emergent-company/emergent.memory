package graph

import (
	"context"

	"github.com/google/uuid"
)

// Event type constants — mirror journal package values so graph does not import journal.
const (
	EventTypeCreated  = "created"
	EventTypeUpdated  = "updated"
	EventTypeDeleted  = "deleted"
	EventTypeRestored = "restored"
	EventTypeRelated  = "related"
	EventTypeBatch    = "batch"
	EventTypeMerge    = "merge"
	EventTypeMoved    = "moved"
)

// Actor type constants.
const (
	ActorUser   = "user"
	ActorSystem = "system"
)

// Entity type constants.
const (
	EntityObject       = "graph_object"
	EntityRelationship = "graph_relationship"
)

// LogParams carries the parameters for a single journal log entry.
// It mirrors journal.LogParams but is defined here so domain/graph does not
// import domain/journal.
type LogParams struct {
	ProjectID  uuid.UUID
	BranchID   *uuid.UUID
	EventType  string
	EntityType *string
	EntityID   *uuid.UUID
	ObjectType *string
	ActorType  string
	ActorID    *uuid.UUID
	Metadata   map[string]any
}

// EventSink receives graph mutation events. Implementations include:
//   - journal.GraphEventSinkAdapter  — writes to the journal (production)
//   - NoopEventSink                  — discards all events (lite / test builds)
type EventSink interface {
	Log(ctx context.Context, params LogParams) error
}

// NoopEventSink implements EventSink by discarding all events.
// Used when journal is disabled or in builds without the journal module.
type NoopEventSink struct{}

// Log discards the event and returns nil.
func (NoopEventSink) Log(_ context.Context, _ LogParams) error { return nil }
