package email

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// DeliveryStore ingests Mailgun delivery events (bounces, complaints, failures,
// deliveries) and reconciles them onto the originating kb.email_jobs row.
//
// Ingestion is idempotent: events are deduplicated by the Mailgun event id via
// the unique index on kb.email_logs.mailgun_event_id (migration 00200), so a
// Mailgun retry cannot double-apply an event.
type DeliveryStore struct {
	db  bun.IDB
	log *slog.Logger
}

// NewDeliveryStore creates a delivery-event store.
func NewDeliveryStore(db bun.IDB, log *slog.Logger) *DeliveryStore {
	return &DeliveryStore{
		db:  db,
		log: log.With(logger.Scope("email.delivery")),
	}
}

// DeliveryEvent is one normalised Mailgun event ready for persistence.
type DeliveryEvent struct {
	// EventID is the Mailgun event id; when empty a deterministic synthetic id
	// is derived so retries still dedupe.
	EventID string
	// EventType is the Mailgun event name (e.g. "failed", "complained").
	EventType string
	// Severity is the Mailgun failure severity ("permanent" / "temporary").
	Severity string
	// MessageID is the Mailgun message id from message.headers.message-id.
	MessageID string
	// Recipient is the event recipient, for the audit trail.
	Recipient string
	// Reason is the Mailgun failure reason, for the audit trail.
	Reason string
	// Timestamp is the event time.
	Timestamp time.Time
	// Details is the raw event-data JSON for the audit trail.
	Details json.RawMessage
}

// IngestDeliveryEvent records one event and applies it to the matched email job.
//
// It returns applied=true when the event was newly recorded (false for a
// duplicate), so the caller can report how many events were actually ingested.
// A duplicate is not an error: the handler still answers 200 so Mailgun stops
// retrying.
func (s *DeliveryStore) IngestDeliveryEvent(ctx context.Context, ev DeliveryEvent) (bool, error) {
	if ev.EventID == "" {
		ev.EventID = SyntheticEventID(ev)
	}

	messageID := NormalizeMessageID(ev.MessageID)

	var applied bool
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		applied = false

		// Resolve the originating job (if any) by the normalised message id.
		var jobID *string
		if messageID != "" {
			var id string
			err := tx.NewRaw(`SELECT id FROM kb.email_jobs WHERE btrim(mailgun_message_id, '<>') = ? LIMIT 1`, messageID).
				Scan(ctx, &id)
			if err != nil && err != sql.ErrNoRows {
				return fmt.Errorf("resolve email job for message id %q: %w", messageID, err)
			}
			if err == nil {
				jobID = &id
			}
		}

		details := ev.Details
		if len(details) == 0 {
			details = json.RawMessage(`{}`)
		}

		// Insert-first idempotency gate. On conflict the RETURNING clause yields
		// no row; sql.ErrNoRows means "already ingested" and we skip the job
		// update so the event is applied exactly once.
		var logID string
		insertErr := tx.NewRaw(`INSERT INTO kb.email_logs
				(email_job_id, event_type, mailgun_event_id, details, created_at)
			VALUES (?, ?, ?, ?, now())
			ON CONFLICT (mailgun_event_id) WHERE mailgun_event_id IS NOT NULL DO NOTHING
			RETURNING id`,
			jobID, ev.EventType, ev.EventID, string(details),
		).Scan(ctx, &logID)
		if insertErr == sql.ErrNoRows {
			return nil
		}
		if insertErr != nil {
			return fmt.Errorf("insert email log: %w", insertErr)
		}

		applied = true

		if jobID == nil {
			return nil
		}

		status, ok := deliveryStatusForEvent(ev.EventType, ev.Severity)
		if !ok {
			return nil
		}

		// Newest event wins: an out-of-order retry must not regress the state.
		if _, err := tx.NewRaw(`UPDATE kb.email_jobs
			SET delivery_status = ?, delivery_status_at = ?, delivery_status_synced_at = now()
			WHERE id = ? AND (delivery_status_at IS NULL OR delivery_status_at <= ?)`,
			status, ev.Timestamp, *jobID, ev.Timestamp,
		).Exec(ctx); err != nil {
			return fmt.Errorf("update email job delivery status: %w", err)
		}

		s.log.Debug("applied delivery event",
			slog.String("job_id", *jobID),
			slog.String("event", ev.EventType),
			slog.String("status", string(status)))

		return nil
	})
	if err != nil {
		return false, err
	}

	return applied, nil
}

// deliveryStatusForEvent maps a Mailgun event (and severity, for failures) to a
// stored delivery status. Unmapped events are audited but do not change state.
func deliveryStatusForEvent(event, severity string) (EmailDeliveryStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "delivered":
		return DeliveryStatusDelivered, true
	case "opened":
		return DeliveryStatusOpened, true
	case "clicked":
		return DeliveryStatusClicked, true
	case "failed":
		if strings.EqualFold(strings.TrimSpace(severity), "temporary") {
			return DeliveryStatusSoftBounced, true
		}
		return DeliveryStatusBounced, true
	case "bounced":
		return DeliveryStatusBounced, true
	case "rejected", "dropped":
		return DeliveryStatusFailed, true
	case "complained":
		return DeliveryStatusComplained, true
	case "unsubscribed":
		return DeliveryStatusUnsubscribed, true
	default:
		return "", false
	}
}

// NormalizeMessageID strips surrounding angle brackets so the stored
// mailgun_message_id (SDK send response, e.g. "<id@domain>") matches the
// webhook header value (e.g. "id@domain").
func NormalizeMessageID(messageID string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(messageID), "<>"))
}

// SyntheticEventID derives a stable dedupe id for events Mailgun did not give an
// id, so a retry of the same event still collides on the unique index.
func SyntheticEventID(ev DeliveryEvent) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		ev.MessageID,
		ev.EventType,
		ev.Timestamp.UTC().Format(time.RFC3339Nano),
		ev.Recipient,
	}, "|")))
	return "synth-" + hex.EncodeToString(sum[:])
}
