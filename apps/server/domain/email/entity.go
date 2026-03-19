package email

import (
	"time"

	"github.com/uptrace/bun"
)

// EmailDeliveryStatus represents the delivery status from Mailgun events
type EmailDeliveryStatus string

const (
	DeliveryStatusPending      EmailDeliveryStatus = "pending"
	DeliveryStatusDelivered    EmailDeliveryStatus = "delivered"
	DeliveryStatusOpened       EmailDeliveryStatus = "opened"
	DeliveryStatusClicked      EmailDeliveryStatus = "clicked"
	DeliveryStatusBounced      EmailDeliveryStatus = "bounced"
	DeliveryStatusSoftBounced  EmailDeliveryStatus = "soft_bounced"
	DeliveryStatusComplained   EmailDeliveryStatus = "complained"
	DeliveryStatusUnsubscribed EmailDeliveryStatus = "unsubscribed"
	DeliveryStatusFailed       EmailDeliveryStatus = "failed"
)

// JobStatus represents the processing status of an email job
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusSent       JobStatus = "sent"
	JobStatusFailed     JobStatus = "failed"
	JobStatusDeadLetter JobStatus = "dead_letter" // Permanently failed after max retries
)

// EmailJob represents a queued email to be sent.
// The email worker processes pending jobs and sends them via Mailgun.
// Failed jobs are retried with exponential backoff.
type EmailJob struct {
	bun.BaseModel `bun:"table:kb.email_jobs,alias:ej"`

	ID              string    `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	TemplateName    string    `bun:"template_name,notnull"`
	ToEmail         string    `bun:"to_email,notnull"`
	ToName          *string   `bun:"to_name"`
	Subject         string    `bun:"subject,notnull"`
	TemplateData    JSON      `bun:"template_data,type:jsonb,notnull,default:'{}'"`
	Status          JobStatus `bun:"status,notnull,default:'pending'"`
	Attempts        int       `bun:"attempts,notnull,default:0"`
	MaxAttempts     int       `bun:"max_attempts,notnull,default:3"`
	LastError       *string   `bun:"last_error"`
	MailgunMessageID *string  `bun:"mailgun_message_id"`
	CreatedAt       time.Time `bun:"created_at,notnull,default:now()"`
	ProcessedAt     *time.Time `bun:"processed_at"`
	NextRetryAt     *time.Time `bun:"next_retry_at"`
	SourceType      *string   `bun:"source_type"`
	SourceID        *string   `bun:"source_id,type:uuid"`

	// Delivery status fields (from Mailgun events sync)
	DeliveryStatus         *EmailDeliveryStatus `bun:"delivery_status"`
	DeliveryStatusAt       *time.Time           `bun:"delivery_status_at"`
	DeliveryStatusSyncedAt *time.Time           `bun:"delivery_status_synced_at"`
}

// JSON is a helper type for JSONB columns
type JSON map[string]interface{}
