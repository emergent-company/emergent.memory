package email

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mailgun/mailgun-go/v4"

	"github.com/emergent/emergent-core/pkg/logger"
)

// MailgunSender sends emails via Mailgun API.
// This is a thin wrapper around the Mailgun SDK.
type MailgunSender struct {
	cfg    *Config
	log    *slog.Logger
	client *mailgun.MailgunImpl
}

// NewMailgunSender creates a new Mailgun email sender.
// Returns nil if Mailgun is not configured.
func NewMailgunSender(cfg *Config, log *slog.Logger) *MailgunSender {
	if !cfg.IsConfigured() {
		return nil
	}

	client := mailgun.NewMailgun(cfg.MailgunDomain, cfg.MailgunAPIKey)

	return &MailgunSender{
		cfg:    cfg,
		log:    log.With(logger.Scope("email.mailgun")),
		client: client,
	}
}

// Send sends an email via Mailgun.
func (s *MailgunSender) Send(ctx context.Context, opts SendOptions) (*SendResult, error) {
	if !s.cfg.Enabled {
		s.log.Warn("email sending is disabled (EMAIL_ENABLED=false)")
		return &SendResult{
			Success: false,
			Error:   "Email sending is disabled",
		}, nil
	}

	if err := s.validate(); err != nil {
		s.log.Error("email configuration invalid", slog.String("error", err.Error()))
		return &SendResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Format recipient with name if provided
	to := opts.To
	if opts.ToName != "" {
		to = fmt.Sprintf("%s <%s>", opts.ToName, opts.To)
	}

	// Format sender with name
	from := fmt.Sprintf("%s <%s>", s.cfg.FromName, s.cfg.FromEmail)

	// Create message
	message := s.client.NewMessage(from, opts.Subject, opts.Text, to)
	if opts.HTML != "" {
		message.SetHtml(opts.HTML)
	}

	s.log.Debug("sending email",
		slog.String("to", opts.To),
		slog.String("subject", opts.Subject))

	// Send with timeout
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, messageID, err := s.client.Send(sendCtx, message)
	if err != nil {
		s.log.Error("failed to send email",
			slog.String("to", opts.To),
			slog.String("error", err.Error()))
		return &SendResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.log.Info("email sent successfully",
		slog.String("to", opts.To),
		slog.String("message_id", messageID))

	return &SendResult{
		Success:   true,
		MessageID: messageID,
	}, nil
}

// validate checks that the configuration is valid
func (s *MailgunSender) validate() error {
	if s.cfg.MailgunDomain == "" {
		return fmt.Errorf("MAILGUN_DOMAIN is required")
	}
	if s.cfg.MailgunAPIKey == "" {
		return fmt.Errorf("MAILGUN_API_KEY is required")
	}
	if s.cfg.FromEmail == "" {
		return fmt.Errorf("EMAIL_FROM_ADDRESS is required")
	}
	if s.cfg.FromName == "" {
		return fmt.Errorf("EMAIL_FROM_NAME is required")
	}
	return nil
}

// GetEventsForMessage retrieves events for a specific message from Mailgun Logs API.
// Used to track delivery status (delivered, opened, bounced, etc.)
func (s *MailgunSender) GetEventsForMessage(ctx context.Context, messageID string, sentAt *time.Time) ([]MailgunEvent, error) {
	if !s.cfg.Enabled {
		return nil, fmt.Errorf("email service is disabled")
	}

	// Calculate time range
	now := time.Now()
	lookbackDays := 7
	msPerDay := 24 * time.Hour

	var start, end time.Time
	if sentAt != nil {
		start = *sentAt
		endFromSent := sentAt.Add(time.Duration(lookbackDays) * msPerDay)
		if endFromSent.Before(now) {
			end = endFromSent
		} else {
			end = now
		}
	} else {
		start = now.Add(-time.Duration(lookbackDays) * msPerDay)
		end = now
	}

	// Create event iterator
	it := s.client.ListEvents(&mailgun.ListEventOptions{
		Begin: start,
		End:   end,
		Filter: map[string]string{
			"message-id": messageID,
		},
	})

	var events []MailgunEvent
	var page []mailgun.Event

	// Iterate through pages
	for it.Next(ctx, &page) {
		for _, e := range page {
			events = append(events, MailgunEvent{
				ID:        e.GetID(),
				Event:     e.GetName(),
				Timestamp: e.GetTimestamp().Unix(),
			})
		}
	}

	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	return events, nil
}

// MailgunEvent represents an event from Mailgun's logs
type MailgunEvent struct {
	ID        string `json:"id"`
	Event     string `json:"event"`
	Timestamp int64  `json:"timestamp"`
}
