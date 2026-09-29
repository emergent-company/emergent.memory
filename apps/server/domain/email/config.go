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
	// MailgunSigningKey is the Mailgun webhook signing key, used to verify the
	// HMAC signature on the public delivery webhook. Empty disables the webhook
	// (fail closed).
	MailgunSigningKey string
	// MailgunWebhookTolerance is the maximum age (and future skew) accepted for
	// a webhook signature timestamp. Zero falls back to 5 minutes.
	MailgunWebhookTolerance time.Duration
	// MailgunWebhookRatePerMin caps webhook requests per client IP per minute.
	// Zero falls back to 120.
	MailgunWebhookRatePerMin int
	// MailgunWebhookRateBurst is the per-IP burst allowance. Zero falls back to 40.
	MailgunWebhookRateBurst int
	// MailgunWebhookGlobalRatePerMin caps total webhook requests per minute
	// across all client IPs. Zero falls back to 1200.
	MailgunWebhookGlobalRatePerMin int
	// MailgunWebhookGlobalRateBurst is the global burst allowance. Zero falls
	// back to 300.
	MailgunWebhookGlobalRateBurst int
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
	// MailgunAPIBase overrides the Mailgun API base URL (including the /v3 path
	// segment). When empty the SDK default is used, or the EU base when
	// MailgunRegion is "eu".
	MailgunAPIBase string
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
		Enabled:                        cfg.Email.Enabled,
		MailgunDomain:                  cfg.Email.MailgunDomain,
		MailgunAPIKey:                  cfg.Email.MailgunAPIKey,
		MailgunSigningKey:              cfg.Email.MailgunSigningKey,
		MailgunWebhookTolerance:        cfg.Email.MailgunWebhookTolerance,
		MailgunWebhookRatePerMin:       cfg.Email.MailgunWebhookRatePerMin,
		MailgunWebhookRateBurst:        cfg.Email.MailgunWebhookRateBurst,
		MailgunWebhookGlobalRatePerMin: cfg.Email.MailgunWebhookGlobalRatePerMin,
		MailgunWebhookGlobalRateBurst:  cfg.Email.MailgunWebhookGlobalRateBurst,
		FromEmail:                      cfg.Email.FromEmail,
		FromName:                       cfg.Email.FromName,
		MaxRetries:                     cfg.Email.MaxRetries,
		RetryDelaySec:                  cfg.Email.RetryDelaySec,
		WorkerIntervalMs:               cfg.Email.WorkerIntervalMs,
		WorkerBatchSize:                cfg.Email.WorkerBatchSize,
		MailgunRegion:                  cfg.Email.MailgunRegion,
		MailgunAPIBase:                 cfg.Email.MailgunAPIBase,
		Transport:                      cfg.Email.Transport,
		SMTPHost:                       cfg.Email.SMTPHost,
		SMTPPort:                       cfg.Email.SMTPPort,
		SMTPUsername:                   cfg.Email.SMTPUsername,
		SMTPPassword:                   cfg.Email.SMTPPassword,
		SMTPTLS:                        cfg.Email.SMTPTLS,
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

// Defaults for the public Mailgun webhook hardening knobs. They are applied
// when a Config is constructed without the corresponding env-backed field
// (notably in tests), so the endpoint is safe-by-default.
const (
	defaultWebhookTolerance        = 5 * time.Minute
	defaultWebhookRatePerMin       = 120
	defaultWebhookRateBurst        = 40
	defaultWebhookGlobalRatePerMin = 1200
	defaultWebhookGlobalRateBurst  = 300
)

// WebhookTolerance returns the accepted signature-timestamp window.
func (c *Config) WebhookTolerance() time.Duration {
	if c.MailgunWebhookTolerance <= 0 {
		return defaultWebhookTolerance
	}
	return c.MailgunWebhookTolerance
}

// WebhookRatePerMin returns the per-IP request budget per minute.
func (c *Config) WebhookRatePerMin() int {
	if c.MailgunWebhookRatePerMin <= 0 {
		return defaultWebhookRatePerMin
	}
	return c.MailgunWebhookRatePerMin
}

// WebhookRateBurst returns the per-IP burst allowance.
func (c *Config) WebhookRateBurst() int {
	if c.MailgunWebhookRateBurst <= 0 {
		return defaultWebhookRateBurst
	}
	return c.MailgunWebhookRateBurst
}

// WebhookGlobalRatePerMin returns the global request budget per minute.
func (c *Config) WebhookGlobalRatePerMin() int {
	if c.MailgunWebhookGlobalRatePerMin <= 0 {
		return defaultWebhookGlobalRatePerMin
	}
	return c.MailgunWebhookGlobalRatePerMin
}

// WebhookGlobalRateBurst returns the global burst allowance.
func (c *Config) WebhookGlobalRateBurst() int {
	if c.MailgunWebhookGlobalRateBurst <= 0 {
		return defaultWebhookGlobalRateBurst
	}
	return c.MailgunWebhookGlobalRateBurst
}
