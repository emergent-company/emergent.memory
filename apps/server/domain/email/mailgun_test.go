package email

import (
	"log/slog"
	"os"
	"testing"

	"github.com/mailgun/mailgun-go/v4"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// TestNewMailgunSender_APIBasePrecedence proves MAILGUN_API_BASE overrides the
// SDK default and the EU region default, and that leaving it unset preserves
// the previous region-based behaviour exactly.
func TestNewMailgunSender_APIBasePrecedence(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name     string
		region   string
		apiBase  string
		wantBase string
	}{
		{
			name:     "override wins over eu region",
			region:   "eu",
			apiBase:  "http://mailgun-stub:8080/v3",
			wantBase: "http://mailgun-stub:8080/v3",
		},
		{
			name:     "override wins over us default",
			region:   "us",
			apiBase:  "http://mailgun-stub:8080/v3",
			wantBase: "http://mailgun-stub:8080/v3",
		},
		{
			name:     "override applies when region unset",
			region:   "",
			apiBase:  "http://mailgun-stub:8080/v3",
			wantBase: "http://mailgun-stub:8080/v3",
		},
		{
			name:     "unset override uses eu region base",
			region:   "eu",
			apiBase:  "",
			wantBase: mailgun.APIBaseEU,
		},
		{
			name:     "unset override uses us default",
			region:   "us",
			apiBase:  "",
			wantBase: mailgun.APIBaseUS,
		},
		{
			name:     "unset override and region uses us default",
			region:   "",
			apiBase:  "",
			wantBase: mailgun.APIBaseUS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				MailgunDomain:  "mg.example.com",
				MailgunAPIKey:  "key-abc123",
				MailgunRegion:  tt.region,
				MailgunAPIBase: tt.apiBase,
				FromEmail:      "noreply@example.com",
				FromName:       "Test",
			}

			sender := NewMailgunSender(cfg, log)
			if sender == nil {
				t.Fatal("NewMailgunSender returned nil for a configured sender")
			}
			if got := sender.client.APIBase(); got != tt.wantBase {
				t.Errorf("APIBase() = %q, want %q", got, tt.wantBase)
			}
		})
	}
}

// TestNewConfig_MirrorsMailgunAPIBase proves the email-package Config carries
// the app config's MAILGUN_API_BASE value through NewConfig.
func TestNewConfig_MirrorsMailgunAPIBase(t *testing.T) {
	appCfg := &config.Config{
		Email: config.EmailConfig{
			Enabled:        true,
			MailgunDomain:  "mg.example.com",
			MailgunAPIKey:  "key-abc123",
			MailgunRegion:  "eu",
			MailgunAPIBase: "http://mailgun-stub:8080/v3",
		},
	}

	cfg := NewConfig(appCfg)
	if cfg.MailgunAPIBase != "http://mailgun-stub:8080/v3" {
		t.Errorf("MailgunAPIBase = %q, want %q", cfg.MailgunAPIBase, "http://mailgun-stub:8080/v3")
	}
}
