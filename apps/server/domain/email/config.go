package email

import (
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// Config contains email service configuration
type Config struct {
	// Enabled determines if email sending is enabled
	Enabled bool
	// MailgunDomain is the Mailgun domain
	MailgunDomain string
	// MailgunAPIKey is the Mailgun API key
	MailgunAPIKey string
	// FromEmail is the default from email address
	FromEmail string
	// FromName is the default from name
	FromName string
	// MaxRetries is the maximum number of retry attempts (default: 3)
	MaxRetries int
	// RetryDelaySec is the base delay in seconds for retries (default: 60)
	RetryDelaySec int
	// WorkerIntervalMs is the polling interval in milliseconds (default: 5000)
	WorkerIntervalMs int
	// WorkerBatchSize is the number of jobs to process per poll (default: 10)
	WorkerBatchSize int
	// MailgunRegion is the Mailgun region ("us" or "eu")
	MailgunRegion string
	// Transport selects the email transport ("mailgun" or "smtp")
	Transport string
	// SMTPHost is the SMTP server host
	SMTPHost string
	// SMTPPort is the SMTP server port
	SMTPPort int
	// SMTPUsername is the SMTP auth username (optional)
	SMTPUsername string
	// SMTPPassword is the SMTP auth password (optional)
	SMTPPassword string
	// SMTPTLS is the SMTP TLS mode ("none", "starttls", or "tls")
	SMTPTLS string
}

// NewConfig creates email configuration from the app config
func NewConfig(cfg *config.Config) *Config {
	return &Config{
		Enabled:          cfg.Email.Enabled,
		MailgunDomain:    cfg.Email.MailgunDomain,
		MailgunAPIKey:    cfg.Email.MailgunAPIKey,
		FromEmail:        cfg.Email.FromEmail,
		FromName:         cfg.Email.FromName,
		MaxRetries:       cfg.Email.MaxRetries,
		RetryDelaySec:    cfg.Email.RetryDelaySec,
		WorkerIntervalMs: cfg.Email.WorkerIntervalMs,
		WorkerBatchSize:  cfg.Email.WorkerBatchSize,
		MailgunRegion:    cfg.Email.MailgunRegion,
		Transport:        cfg.Email.Transport,
		SMTPHost:         cfg.Email.SMTPHost,
		SMTPPort:         cfg.Email.SMTPPort,
		SMTPUsername:     cfg.Email.SMTPUsername,
		SMTPPassword:     cfg.Email.SMTPPassword,
		SMTPTLS:          cfg.Email.SMTPTLS,
	}
}

// WorkerInterval returns the worker interval as a Duration
func (c *Config) WorkerInterval() time.Duration {
	return time.Duration(c.WorkerIntervalMs) * time.Millisecond
}

// IsConfigured returns true if Mailgun is configured
func (c *Config) IsConfigured() bool {
	return c.MailgunDomain != "" && c.MailgunAPIKey != ""
}
