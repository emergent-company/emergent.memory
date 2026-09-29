package email

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// maxWebhookBodySize bounds the request body so an unauthenticated caller cannot
// force unbounded allocation before the signature is checked.
const maxWebhookBodySize = 1 << 20 // 1 MiB

// MailgunWebhookHandler receives Mailgun delivery events (bounces, complaints,
// failures, deliveries) and reconciles them onto the originating email jobs.
//
// This is a PUBLIC endpoint: the Mailgun HMAC signature is the authentication,
// and a request without a valid signature is rejected fail-closed.
type MailgunWebhookHandler struct {
	store *DeliveryStore
	cfg   *Config
	log   *slog.Logger
}

// NewMailgunWebhookHandler creates the Mailgun delivery webhook handler.
func NewMailgunWebhookHandler(db bun.IDB, cfg *Config, log *slog.Logger) *MailgunWebhookHandler {
	return &MailgunWebhookHandler{
		store: NewDeliveryStore(db, log),
		cfg:   cfg,
		log:   log.With(logger.Scope("email.webhook")),
	}
}

// mailgunSignature is the Mailgun request signature envelope.
type mailgunSignature struct {
	Timestamp string `json:"timestamp"`
	Token     string `json:"token"`
	Signature string `json:"signature"`
}

// mailgunWebhookBody is the Mailgun v3 webhook envelope.
type mailgunWebhookBody struct {
	Signature mailgunSignature `json:"signature"`
	EventData json.RawMessage  `json:"event-data"`
}

// mailgunEventData is the subset of a Mailgun event we act on.
type mailgunEventData struct {
	Event     string  `json:"event"`
	ID        string  `json:"id"`
	Timestamp float64 `json:"timestamp"`
	Severity  string  `json:"severity"`
	Recipient string  `json:"recipient"`
	Reason    string  `json:"reason"`
	Message   struct {
		Headers struct {
			MessageID string `json:"message-id"`
		} `json:"headers"`
	} `json:"message"`
}

// VerifyMailgunSignature reports whether signature is the valid Mailgun HMAC.
//
// Mailgun signs timestamp+token with HMAC-SHA256 under the domain signing key
// and sends the hex digest. Verification is constant-time and fails closed:
// an unconfigured key, a missing field, or a malformed digest all return false.
func VerifyMailgunSignature(signingKey, timestamp, token, signature string) bool {
	if signingKey == "" || timestamp == "" || token == "" || signature == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(token))

	provided, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	return hmac.Equal(mac.Sum(nil), provided)
}

// Handle implements POST /api/webhooks/mailgun.
func (h *MailgunWebhookHandler) Handle(c echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxWebhookBodySize))
	if err != nil {
		return apperror.NewBadRequest("failed to read webhook body")
	}

	var envelope mailgunWebhookBody
	if err := json.Unmarshal(body, &envelope); err != nil {
		return apperror.NewBadRequest("invalid Mailgun webhook payload")
	}

	if !VerifyMailgunSignature(h.cfg.MailgunSigningKey,
		envelope.Signature.Timestamp, envelope.Signature.Token, envelope.Signature.Signature) {
		h.log.Warn("rejected Mailgun webhook with invalid signature")
		return apperror.New(http.StatusUnauthorized, "invalid_signature", "Invalid Mailgun signature")
	}

	events, err := decodeMailgunEvents(envelope.EventData)
	if err != nil {
		return apperror.NewBadRequest("invalid Mailgun event data")
	}

	applied := 0
	for _, raw := range events {
		ev, err := deliveryEventFromMailgun(raw)
		if err != nil {
			return apperror.NewBadRequest("invalid Mailgun event data")
		}
		ok, err := h.store.IngestDeliveryEvent(c.Request().Context(), ev)
		if err != nil {
			h.log.Error("failed to ingest Mailgun delivery event",
				slog.String("event", ev.EventType),
				slog.String("error", err.Error()))
			return apperror.NewInternal("ingest Mailgun delivery event", err)
		}
		if ok {
			applied++
		}
	}

	return c.JSON(http.StatusOK, map[string]int{
		"received": len(events),
		"applied":  applied,
	})
}

// decodeMailgunEvents accepts both a single event object and a JSON array of
// events (Mailgun may batch).
func decodeMailgunEvents(raw json.RawMessage) ([]mailgunEventData, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty event-data")
	}

	switch trimmed[0] {
	case '[':
		var events []mailgunEventData
		if err := json.Unmarshal(trimmed, &events); err != nil {
			return nil, err
		}
		return events, nil
	case '{':
		var event mailgunEventData
		if err := json.Unmarshal(trimmed, &event); err != nil {
			return nil, err
		}
		return []mailgunEventData{event}, nil
	default:
		return nil, fmt.Errorf("unexpected event-data shape")
	}
}

// deliveryEventFromMailgun normalises one Mailgun event for the store.
func deliveryEventFromMailgun(raw mailgunEventData) (DeliveryEvent, error) {
	details, err := json.Marshal(raw)
	if err != nil {
		return DeliveryEvent{}, err
	}

	ts := time.Unix(0, 0).UTC()
	if raw.Timestamp > 0 {
		sec := int64(raw.Timestamp)
		nsec := int64((raw.Timestamp - float64(sec)) * float64(time.Second))
		ts = time.Unix(sec, nsec).UTC()
	}

	return DeliveryEvent{
		EventID:   raw.ID,
		EventType: raw.Event,
		Severity:  raw.Severity,
		MessageID: raw.Message.Headers.MessageID,
		Recipient: raw.Recipient,
		Reason:    raw.Reason,
		Timestamp: ts,
		Details:   details,
	}, nil
}

// RegisterWebhookRoutes registers the public Mailgun delivery webhook.
//
// The route is intentionally unauthenticated at the transport layer: Mailgun
// cannot present a session, so the handler verifies the HMAC signature and fails
// closed. There is no exemption from signature verification.
func RegisterWebhookRoutes(e *echo.Echo, h *MailgunWebhookHandler) {
	e.POST("/api/webhooks/mailgun", h.Handle)
}
