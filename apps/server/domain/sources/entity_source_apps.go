package sources

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Source event states.
const (
	SourceEventReceived   = "received"
	SourceEventDispatched = "dispatched"
	SourceEventSkipped    = "skipped"
	SourceEventFailed     = "failed"
	SourceEventSuperseded = "superseded"
	SourceEventCancelled  = "cancelled"
)

// SourceApp is an org-scoped provider App credential. One App can back many
// project sources and installations. The sealed columns are write-only.
type SourceApp struct {
	bun.BaseModel `bun:"table:kb.source_apps,alias:sa"`

	ID                     uuid.UUID `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	OrgID                  uuid.UUID `bun:"org_id,notnull,type:uuid" json:"orgId"`
	Provider               string    `bun:"provider,notnull" json:"provider"`
	AppID                  int64     `bun:"app_id,notnull" json:"appId"`
	Slug                   string    `bun:"slug,notnull" json:"slug"`
	PrivateKeyEncrypted    []byte    `bun:"private_key_encrypted,notnull,type:bytea" json:"-"`
	WebhookSecretEncrypted []byte    `bun:"webhook_secret_encrypted,notnull,type:bytea" json:"-"`
	WebhookKey             uuid.UUID `bun:"webhook_key,notnull,type:uuid,default:gen_random_uuid()" json:"webhookKey"`
	CreatedAt              time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt              time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// SourceTrigger binds one source event type to a runtime agent and queue.
type SourceTrigger struct {
	bun.BaseModel `bun:"table:kb.source_triggers,alias:st"`

	ID          uuid.UUID      `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID   uuid.UUID      `bun:"project_id,notnull,type:uuid" json:"projectId"`
	SourceID    uuid.UUID      `bun:"source_id,notnull,type:uuid" json:"sourceId"`
	EventType   string         `bun:"event_type,notnull" json:"eventType"`
	AgentID     uuid.UUID      `bun:"agent_id,notnull,type:uuid" json:"agentId"`
	Queue       string         `bun:"queue,notnull,default:'default'" json:"queue"`
	EventFilter map[string]any `bun:"event_filter,type:jsonb,notnull" json:"eventFilter"`
	Enabled     bool           `bun:"enabled,notnull" json:"enabled"`
	CreatedAt   time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt   time.Time      `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// SourceEvent is one verified delivery and its dispatch claim. Payload is a
// bounded, provider-normalized subset, never the raw secrets-bearing body.
type SourceEvent struct {
	bun.BaseModel `bun:"table:kb.source_events,alias:se"`

	ID          uuid.UUID      `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID   uuid.UUID      `bun:"project_id,notnull,type:uuid" json:"projectId"`
	SourceID    uuid.UUID      `bun:"source_id,notnull,type:uuid" json:"sourceId"`
	DeliveryID  string         `bun:"delivery_id,notnull" json:"deliveryId"`
	EventType   string         `bun:"event_type,notnull" json:"eventType"`
	Action      string         `bun:"action,notnull" json:"action"`
	SubjectType string         `bun:"subject_type,notnull" json:"subjectType"`
	SubjectRef  string         `bun:"subject_ref,notnull" json:"subjectRef"`
	HeadSHA     string         `bun:"head_sha,notnull" json:"headSha"`
	Payload     map[string]any `bun:"payload,type:jsonb,notnull" json:"payload"`
	State       string         `bun:"state,notnull" json:"state"`
	RunID       *uuid.UUID     `bun:"run_id,type:uuid" json:"runId,omitempty"`
	Error       *string        `bun:"error" json:"error,omitempty"`
	CreatedAt   time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt   time.Time      `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// Source event claim states.
const (
	SourceClaimPending    = "pending"
	SourceClaimRunning    = "running"
	SourceClaimDone       = "done"
	SourceClaimSuperseded = "superseded"
	SourceClaimCancelled  = "cancelled"
)

// SourceEventClaim is the per-subject/head idempotency unit. One active claim
// per (source, subject_ref, head_sha); a newer head supersedes and cancels the
// prior run.
type SourceEventClaim struct {
	bun.BaseModel `bun:"table:kb.source_event_claims,alias:sc"`

	ID         uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID  uuid.UUID  `bun:"project_id,notnull,type:uuid" json:"projectId"`
	SourceID   uuid.UUID  `bun:"source_id,notnull,type:uuid" json:"sourceId"`
	SubjectRef string     `bun:"subject_ref,notnull" json:"subjectRef"`
	HeadSHA    string     `bun:"head_sha,notnull" json:"headSha"`
	RunID      *uuid.UUID `bun:"run_id,type:uuid" json:"runId,omitempty"`
	State      string     `bun:"state,notnull,default:'pending'" json:"state"`
	CreatedAt  time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt  time.Time  `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}
