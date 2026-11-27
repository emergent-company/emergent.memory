package mcp

import (
	"encoding/json"
	"sync"
	"time"
)

// SSEEvent represents a single SSE event with metadata for resumability
type SSEEvent struct {
	ID        int64           // Sequential event ID
	EventType string          // "message" for JSON-RPC messages
	Data      json.RawMessage // JSON-RPC message payload
	Timestamp time.Time       // When the event was created
}

// EventStore stores SSE events per session to support resumability via Last-Event-ID
// According to MCP spec 2025-11-25, servers SHOULD support resuming disconnected streams
type EventStore struct {
	mu        sync.RWMutex
	events    map[string][]*SSEEvent // sessionID -> ordered list of events
	nextID    map[string]int64       // sessionID -> next event ID to assign
	maxEvents int                    // Max events to keep per session (for memory management)
}

// NewEventStore creates a new event store with the specified max events per session
func NewEventStore(maxEvents int) *EventStore {
	return &EventStore{
		events:    make(map[string][]*SSEEvent),
		nextID:    make(map[string]int64),
		maxEvents: maxEvents,
	}
}

// GetEventsSince returns all events after the given ID for resumability support
// Per spec: "If the Last-Event-ID header is present, the server SHOULD send all events
// that occurred after the specified event ID."
//
// NOTE: nothing currently records events into this store (the only producer,
// AddEvent, was removed with SendServerMessage, which had no callers), so this
// returns an empty slice and a resuming client replays nothing. Tracked as a
// follow-up: either wire a producer or drop the Last-Event-ID path.
func (es *EventStore) GetEventsSince(sessionID string, lastEventID int64) []*SSEEvent {
	es.mu.RLock()
	defer es.mu.RUnlock()

	events := es.events[sessionID]
	result := make([]*SSEEvent, 0)

	// Find all events with ID > lastEventID
	for _, event := range events {
		if event.ID > lastEventID {
			result = append(result, event)
		}
	}

	return result
}

// ClearSession removes all stored events for a session (called on session termination)
func (es *EventStore) ClearSession(sessionID string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	delete(es.events, sessionID)
	delete(es.nextID, sessionID)
}

// GetNextEventID allocates the next event ID for a session
// Used for priming events which have no data payload
func (es *EventStore) GetNextEventID(sessionID string) int64 {
	es.mu.Lock()
	defer es.mu.Unlock()
	id := es.nextID[sessionID]
	es.nextID[sessionID]++
	return id
}
