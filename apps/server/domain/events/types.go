package events

import (
	"net/http"
	"time"
)

// EntityEventType represents the type of entity event
type EntityEventType string

const (
	EventTypeCreated EntityEventType = "entity.created"
	EventTypeUpdated EntityEventType = "entity.updated"
	EventTypeDeleted EntityEventType = "entity.deleted"
	EventTypeBatch   EntityEventType = "entity.batch"
)

// EntityType represents supported entity types for real-time events
type EntityType string

const (
	EntityDocument      EntityType = "document"
	EntityChunk         EntityType = "chunk"
	EntityExtractionJob EntityType = "extraction_job"
	EntityGraphObject   EntityType = "graph_object"
	EntityNotification  EntityType = "notification"
	EntitySyncJob       EntityType = "sync_job"
)

// ActorType represents the type of actor making a change
type ActorType string

const (
	ActorUser   ActorType = "user"
	ActorAgent  ActorType = "agent"
	ActorSystem ActorType = "system"
)

// ActorContext tracks who made the change (for loop prevention in reaction agents)
type ActorContext struct {
	ActorType ActorType `json:"actorType"`
	ActorID   string    `json:"actorId,omitempty"`
}

// EntityEvent is the payload sent via SSE
type EntityEvent struct {
	Type       EntityEventType        `json:"type"`
	Entity     EntityType             `json:"entity"`
	ID         *string                `json:"id"`         // nil for batch events
	IDs        []string               `json:"ids,omitempty"`
	ProjectID  string                 `json:"projectId"`
	Data       map[string]any         `json:"data,omitempty"`
	Timestamp  string                 `json:"timestamp"`
	Actor      *ActorContext          `json:"actor,omitempty"`
	Version    *int                   `json:"version,omitempty"`    // for graph objects
	ObjectType string                 `json:"objectType,omitempty"` // for graph objects
}

// SSEConnection represents an SSE connection
type SSEConnection struct {
	ConnectionID  string
	UserID        string
	ProjectID     string
	Writer        http.ResponseWriter
	Flusher       http.Flusher
	Done          chan struct{}
	LastHeartbeat time.Time
}

// ConnectedEvent is sent when a client connects
type ConnectedEvent struct {
	ConnectionID string `json:"connectionId"`
	ProjectID    string `json:"projectId"`
}

// HealthStatus included in heartbeat events
type HealthStatus struct {
	OK             bool    `json:"ok"`
	Model          *string `json:"model"`
	DB             string  `json:"db"`
	Embeddings     string  `json:"embeddings"`
	RLSPoliciesOK  *bool   `json:"rls_policies_ok,omitempty"`
	RLSPolicyCount *int    `json:"rls_policy_count,omitempty"`
	RLSPolicyHash  *string `json:"rls_policy_hash,omitempty"`
}

// HeartbeatEvent is sent periodically to keep connections alive
type HeartbeatEvent struct {
	Timestamp string        `json:"timestamp"`
	Health    *HealthStatus `json:"health,omitempty"`
}

// SSEEventPayload is the data portion of an entity event sent to clients
type SSEEventPayload struct {
	Entity    EntityType     `json:"entity"`
	ID        *string        `json:"id,omitempty"`
	IDs       []string       `json:"ids,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp string         `json:"timestamp"`
}

// EmitOptions are optional parameters for emitting events
type EmitOptions struct {
	Data       map[string]any
	Actor      *ActorContext
	Version    *int
	ObjectType string
}
