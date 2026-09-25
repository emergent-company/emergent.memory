package email

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestSMTPSender_validate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *Config
		wantError string
	}{
		{
			name:      "host present",
			cfg:       &Config{SMTPHost: "mailpit"},
			wantError: "",
		},
		{
			name:      "missing host",
			cfg:       &Config{SMTPHost: ""},
			wantError: "SMTP_HOST is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &SMTPSender{cfg: tt.cfg}
			err := sender.validate()

			if tt.wantError == "" {
				if err != nil {
					t.Errorf("validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("validate() expected error, got nil")
				} else if err.Error() != tt.wantError {
					t.Errorf("validate() error = %q, want %q", err.Error(), tt.wantError)
				}
			}
		})
	}
}

func TestBuildMessage(t *testing.T) {
	cfg := &Config{
		FromName:  "Test Sender",
		FromEmail: "noreply@example.com",
		SMTPHost:  "mailpit",
	}

	opts := SendOptions{
		To:      "invitee@example.com",
		ToName:  "Invitee",
		Subject: "You're invited",
		Text:    "Hello plain text body",
		HTML:    "<p>Hello <b>HTML</b> body</p>",
	}

	msg, messageID := buildMessage(cfg, opts)
	raw := string(msg)

	t.Run("contains multipart/alternative", func(t *testing.T) {
		if !strings.Contains(raw, "multipart/alternative") {
			t.Errorf("message missing multipart/alternative Content-Type:\n%s", raw)
		}
	})

	t.Run("contains text/plain part with Text body", func(t *testing.T) {
		if !strings.Contains(raw, "Content-Type: text/plain; charset=utf-8") {
			t.Error("message missing text/plain part header")
		}
		if !strings.Contains(raw, opts.Text) {
			t.Errorf("message missing text body %q", opts.Text)
		}
	})

	t.Run("contains text/html part with HTML body", func(t *testing.T) {
		if !strings.Contains(raw, "Content-Type: text/html; charset=utf-8") {
			t.Error("message missing text/html part header")
		}
		if !strings.Contains(raw, opts.HTML) {
			t.Errorf("message missing html body %q", opts.HTML)
		}
	})

	t.Run("contains correct To header", func(t *testing.T) {
		want := "To: Invitee <invitee@example.com>"
		if !strings.Contains(raw, want) {
			t.Errorf("message missing %q:\n%s", want, raw)
		}
	})

	t.Run("contains correct Subject header", func(t *testing.T) {
		want := "Subject: You're invited"
		if !strings.Contains(raw, want) {
			t.Errorf("message missing %q:\n%s", want, raw)
		}
	})

	t.Run("contains Message-ID header matching returned id", func(t *testing.T) {
		if !strings.HasPrefix(messageID, "<") || !strings.HasSuffix(messageID, ">") {
			t.Errorf("message ID %q not angle-bracketed", messageID)
		}
		want := "Message-ID: " + messageID
		if !strings.Contains(raw, want) {
			t.Errorf("message missing %q:\n%s", want, raw)
		}
	})

	t.Run("contains From header with name", func(t *testing.T) {
		want := "From: Test Sender <noreply@example.com>"
		if !strings.Contains(raw, want) {
			t.Errorf("message missing %q:\n%s", want, raw)
		}
	})

	t.Run("contains MIME-Version and Date headers", func(t *testing.T) {
		for _, want := range []string{"MIME-Version: 1.0", "Date: "} {
			if !strings.Contains(raw, want) {
				t.Errorf("message missing %q", want)
			}
		}
	})
}

func TestBuildMessageDefaults(t *testing.T) {
	t.Run("defaults FromEmail to noreply@example.com", func(t *testing.T) {
		cfg := &Config{FromName: "App"}
		opts := SendOptions{To: "user@example.com", Subject: "Hi"}
		msg, _ := buildMessage(cfg, opts)

		if !strings.Contains(string(msg), "From: App <noreply@example.com>") {
			t.Errorf("expected default from email, got:\n%s", string(msg))
		}
	})

	t.Run("omits ToName when absent", func(t *testing.T) {
		cfg := &Config{FromName: "App"}
		opts := SendOptions{To: "user@example.com", Subject: "Hi"}
		msg, _ := buildMessage(cfg, opts)

		if !strings.Contains(string(msg), "To: user@example.com") {
			t.Errorf("expected bare To header, got:\n%s", string(msg))
		}
	})
}

func TestNewSenderSMTPSelection(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("returns SMTPSender when transport is smtp", func(t *testing.T) {
		cfg := &Config{
			Enabled:   true,
			Transport: "smtp",
			SMTPHost:  "mailpit",
			SMTPPort:  1025,
		}

		sender := NewSender(log, cfg)

		if _, ok := sender.(*SMTPSender); !ok {
			t.Errorf("NewSender() = %T, want *SMTPSender", sender)
		}
	})

	t.Run("returns MailgunSender when transport is mailgun and configured", func(t *testing.T) {
		cfg := &Config{
			Enabled:       true,
			Transport:     "mailgun",
			MailgunDomain: "mg.example.com",
			MailgunAPIKey: "key-abc123",
			FromEmail:     "noreply@example.com",
			FromName:      "Test",
		}

		sender := NewSender(log, cfg)

		if _, ok := sender.(*MailgunSender); !ok {
			t.Errorf("NewSender() = %T, want *MailgunSender", sender)
		}
	})

	t.Run("returns noOpSender when disabled even with smtp transport", func(t *testing.T) {
		cfg := &Config{
			Enabled:   false,
			Transport: "smtp",
			SMTPHost:  "mailpit",
		}

		sender := NewSender(log, cfg)

		if _, ok := sender.(*noOpSender); !ok {
			t.Errorf("NewSender() = %T, want *noOpSender", sender)
		}
	})

	t.Run("returns noOpSender when transport is mailgun but not configured", func(t *testing.T) {
		cfg := &Config{
			Enabled:   true,
			Transport: "mailgun",
		}

		sender := NewSender(log, cfg)

		if _, ok := sender.(*noOpSender); !ok {
			t.Errorf("NewSender() = %T, want *noOpSender", sender)
		}
	})
}

func TestSMTPSender_SendDisabled(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	sender := &SMTPSender{
		cfg: &Config{Enabled: false, SMTPHost: "mailpit"},
		log: log,
	}

	result, err := sender.Send(context.Background(), SendOptions{To: "user@example.com"})
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("Send() result = nil, want non-nil")
	}
	if result.Success {
		t.Error("Send() result.Success = true, want false when disabled")
	}
}

func TestSMTPSender_SendMissingHost(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	sender := &SMTPSender{
		cfg: &Config{Enabled: true, SMTPHost: ""},
		log: log,
	}

	result, err := sender.Send(context.Background(), SendOptions{To: "user@example.com"})
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("Send() result = nil, want non-nil")
	}
	if result.Success {
		t.Error("Send() result.Success = true, want false when SMTPHost empty")
	}
	if result.Error == "" {
		t.Error("Send() result.Error should be non-empty when SMTPHost empty")
	}
}
