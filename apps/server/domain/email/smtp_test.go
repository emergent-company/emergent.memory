package email

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"
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

	msg, messageID, err := buildMessage(cfg, opts)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
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
		want := `To: "Invitee" <invitee@example.com>`
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
		want := `From: "Test Sender" <noreply@example.com>`
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
		msg, _, err := buildMessage(cfg, opts)
		if err != nil {
			t.Fatalf("buildMessage: %v", err)
		}

		if !strings.Contains(string(msg), `From: "App" <noreply@example.com>`) {
			t.Errorf("expected default from email, got:\n%s", string(msg))
		}
	})

	t.Run("omits ToName when absent", func(t *testing.T) {
		cfg := &Config{FromName: "App"}
		opts := SendOptions{To: "user@example.com", Subject: "Hi"}
		msg, _, err := buildMessage(cfg, opts)
		if err != nil {
			t.Fatalf("buildMessage: %v", err)
		}

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

func TestSMTPSender_validateTLS(t *testing.T) {
	tests := []struct {
		name      string
		tls       string
		wantError bool
	}{
		{name: "none valid", tls: "none"},
		{name: "empty treated as none", tls: ""},
		{name: "starttls valid", tls: "starttls"},
		{name: "tls valid", tls: "tls"},
		{name: "unknown rejected", tls: "startls", wantError: true},
		{name: "typo rejected", tls: "tl", wantError: true},
		{name: "uppercase rejected", tls: "STARTTLS", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &SMTPSender{cfg: &Config{SMTPHost: "mailpit", SMTPTLS: tt.tls}}
			err := sender.validate()
			if tt.wantError && err == nil {
				t.Fatalf("validate() with SMTP_TLS=%q: expected error, got nil", tt.tls)
			}
			if !tt.wantError && err != nil {
				t.Fatalf("validate() with SMTP_TLS=%q: unexpected error %v", tt.tls, err)
			}
		})
	}
}

func TestSMTPSender_connectRejectsUnknownTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.ReadAll(c)
	}()

	sender := &SMTPSender{cfg: &Config{SMTPHost: "127.0.0.1", SMTPTLS: "tls_typo"}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = sender.connect(ctx, "127.0.0.1", ln.Addr().String())
	if err == nil {
		t.Fatal("connect() with unknown SMTP_TLS: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "SMTP_TLS") {
		t.Errorf("connect() error = %q, want mention of SMTP_TLS", err)
	}
}

func TestSMTPSender_deliverHonorsDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		// Accept the connection but never send the SMTP banner — simulate a host
		// that goes silent after the TCP handshake.
		buf := make([]byte, 1024)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	sender := &SMTPSender{cfg: &Config{SMTPHost: "127.0.0.1", SMTPPort: port, SMTPTLS: "none"}}

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = sender.deliver(ctx, "noreply@example.com", "invitee@example.com", []byte("test"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("deliver() with silent server: expected error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("deliver() took %v, want bounded by the context deadline", elapsed)
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
