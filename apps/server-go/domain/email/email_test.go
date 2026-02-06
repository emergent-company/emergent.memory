package email

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aymerick/raymond"
	"github.com/emergent/emergent-core/internal/config"
)

func TestMailgunSenderValidate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *Config
		wantError string
	}{
		{
			name: "all fields valid",
			cfg: &Config{
				MailgunDomain: "mg.example.com",
				MailgunAPIKey: "key-abc123",
				FromEmail:     "noreply@example.com",
				FromName:      "Test App",
			},
			wantError: "",
		},
		{
			name: "missing MailgunDomain",
			cfg: &Config{
				MailgunDomain: "",
				MailgunAPIKey: "key-abc123",
				FromEmail:     "noreply@example.com",
				FromName:      "Test App",
			},
			wantError: "MAILGUN_DOMAIN is required",
		},
		{
			name: "missing MailgunAPIKey",
			cfg: &Config{
				MailgunDomain: "mg.example.com",
				MailgunAPIKey: "",
				FromEmail:     "noreply@example.com",
				FromName:      "Test App",
			},
			wantError: "MAILGUN_API_KEY is required",
		},
		{
			name: "missing FromEmail",
			cfg: &Config{
				MailgunDomain: "mg.example.com",
				MailgunAPIKey: "key-abc123",
				FromEmail:     "",
				FromName:      "Test App",
			},
			wantError: "EMAIL_FROM_ADDRESS is required",
		},
		{
			name: "missing FromName",
			cfg: &Config{
				MailgunDomain: "mg.example.com",
				MailgunAPIKey: "key-abc123",
				FromEmail:     "noreply@example.com",
				FromName:      "",
			},
			wantError: "EMAIL_FROM_NAME is required",
		},
		{
			name: "all fields empty",
			cfg: &Config{
				MailgunDomain: "",
				MailgunAPIKey: "",
				FromEmail:     "",
				FromName:      "",
			},
			wantError: "MAILGUN_DOMAIN is required", // First check fails
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a sender with the test config (we don't need a real client for validate)
			sender := &MailgunSender{cfg: tt.cfg}

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

func TestConfigIsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected bool
	}{
		{
			name: "both domain and key present",
			cfg: &Config{
				MailgunDomain: "mg.example.com",
				MailgunAPIKey: "key-abc123",
			},
			expected: true,
		},
		{
			name: "missing domain",
			cfg: &Config{
				MailgunDomain: "",
				MailgunAPIKey: "key-abc123",
			},
			expected: false,
		},
		{
			name: "missing api key",
			cfg: &Config{
				MailgunDomain: "mg.example.com",
				MailgunAPIKey: "",
			},
			expected: false,
		},
		{
			name: "both missing",
			cfg: &Config{
				MailgunDomain: "",
				MailgunAPIKey: "",
			},
			expected: false,
		},
		{
			name:     "empty config",
			cfg:      &Config{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.IsConfigured()
			if result != tt.expected {
				t.Errorf("IsConfigured() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConfigWorkerInterval(t *testing.T) {
	tests := []struct {
		name             string
		workerIntervalMs int
		expected         time.Duration
	}{
		{
			name:             "5000ms",
			workerIntervalMs: 5000,
			expected:         5 * time.Second,
		},
		{
			name:             "1000ms",
			workerIntervalMs: 1000,
			expected:         1 * time.Second,
		},
		{
			name:             "100ms",
			workerIntervalMs: 100,
			expected:         100 * time.Millisecond,
		},
		{
			name:             "zero",
			workerIntervalMs: 0,
			expected:         0,
		},
		{
			name:             "large value",
			workerIntervalMs: 60000,
			expected:         time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{WorkerIntervalMs: tt.workerIntervalMs}
			result := cfg.WorkerInterval()
			if result != tt.expected {
				t.Errorf("WorkerInterval() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short message",
			input:    "short error",
			expected: "short error",
		},
		{
			name:     "empty message",
			input:    "",
			expected: "",
		},
		{
			name:     "exactly 1000 chars",
			input:    string(make([]byte, 1000)),
			expected: string(make([]byte, 1000)),
		},
		{
			name:     "1001 chars truncated to 1000",
			input:    string(make([]byte, 1001)),
			expected: string(make([]byte, 1000)),
		},
		{
			name:     "very long message truncated",
			input:    string(make([]byte, 5000)),
			expected: string(make([]byte, 1000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateError(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("truncateError() length = %d, want %d", len(result), len(tt.expected))
			}
		})
	}
}

func TestWorkerMetrics(t *testing.T) {
	// Create a worker with minimal config (no dependencies needed for metrics tests)
	w := &Worker{}

	t.Run("initial metrics are zero", func(t *testing.T) {
		m := w.Metrics()
		if m.Processed != 0 {
			t.Errorf("initial Processed = %d, want 0", m.Processed)
		}
		if m.Succeeded != 0 {
			t.Errorf("initial Succeeded = %d, want 0", m.Succeeded)
		}
		if m.Failed != 0 {
			t.Errorf("initial Failed = %d, want 0", m.Failed)
		}
	})

	t.Run("incrementSuccess increments processed and success", func(t *testing.T) {
		w := &Worker{}
		w.incrementSuccess()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("after incrementSuccess Processed = %d, want 1", m.Processed)
		}
		if m.Succeeded != 1 {
			t.Errorf("after incrementSuccess Succeeded = %d, want 1", m.Succeeded)
		}
		if m.Failed != 0 {
			t.Errorf("after incrementSuccess Failed = %d, want 0", m.Failed)
		}
	})

	t.Run("incrementFailure increments processed and failure", func(t *testing.T) {
		w := &Worker{}
		w.incrementFailure()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("after incrementFailure Processed = %d, want 1", m.Processed)
		}
		if m.Succeeded != 0 {
			t.Errorf("after incrementFailure Succeeded = %d, want 0", m.Succeeded)
		}
		if m.Failed != 1 {
			t.Errorf("after incrementFailure Failed = %d, want 1", m.Failed)
		}
	})

	t.Run("multiple increments accumulate correctly", func(t *testing.T) {
		w := &Worker{}
		// Simulate 5 successes and 3 failures
		for i := 0; i < 5; i++ {
			w.incrementSuccess()
		}
		for i := 0; i < 3; i++ {
			w.incrementFailure()
		}

		m := w.Metrics()
		if m.Processed != 8 {
			t.Errorf("after mixed increments Processed = %d, want 8", m.Processed)
		}
		if m.Succeeded != 5 {
			t.Errorf("after mixed increments Succeeded = %d, want 5", m.Succeeded)
		}
		if m.Failed != 3 {
			t.Errorf("after mixed increments Failed = %d, want 3", m.Failed)
		}
	})
}

func TestWorkerIsRunning(t *testing.T) {
	t.Run("initially not running", func(t *testing.T) {
		w := &Worker{}
		if w.IsRunning() {
			t.Error("new worker should not be running")
		}
	})

	t.Run("running state can be set", func(t *testing.T) {
		w := &Worker{}
		w.mu.Lock()
		w.running = true
		w.mu.Unlock()
		if !w.IsRunning() {
			t.Error("worker should be running after setting running=true")
		}
	})
}

func TestSendErrorInterface(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "simple message",
			message: "send failed",
			want:    "send failed",
		},
		{
			name:    "empty message",
			message: "",
			want:    "",
		},
		{
			name:    "detailed message",
			message: "mailgun API error: 401 unauthorized",
			want:    "mailgun API error: 401 unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &sendError{message: tt.message}
			if err.Error() != tt.want {
				t.Errorf("sendError.Error() = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestWorkerMetricsStruct(t *testing.T) {
	// Test JSON tags on WorkerMetrics struct
	m := WorkerMetrics{
		Processed: 100,
		Succeeded: 85,
		Failed:    15,
	}

	if m.Processed != 100 {
		t.Errorf("WorkerMetrics.Processed = %d, want 100", m.Processed)
	}
	if m.Succeeded != 85 {
		t.Errorf("WorkerMetrics.Succeeded = %d, want 85", m.Succeeded)
	}
	if m.Failed != 15 {
		t.Errorf("WorkerMetrics.Failed = %d, want 15", m.Failed)
	}
}

func TestGenerateFallbackHTML(t *testing.T) {
	w := &Worker{}

	tests := []struct {
		name           string
		job            *EmailJob
		ctx            TemplateContext
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "basic email with subject",
			job: &EmailJob{
				Subject: "Welcome Email",
			},
			ctx: TemplateContext{},
			wantContains: []string{
				"<title>Welcome Email</title>",
				"Hello,",
				"This email was sent by Emergent.",
			},
		},
		{
			name: "with recipient name",
			job: &EmailJob{
				Subject: "Hello",
			},
			ctx: TemplateContext{
				"recipientName": "John Doe",
			},
			wantContains: []string{
				"Hello John Doe,",
			},
		},
		{
			name: "with message",
			job: &EmailJob{
				Subject: "Update",
			},
			ctx: TemplateContext{
				"message": "Your account has been updated.",
			},
			wantContains: []string{
				"Your account has been updated.",
			},
		},
		{
			name: "with CTA button",
			job: &EmailJob{
				Subject: "Action Required",
			},
			ctx: TemplateContext{
				"ctaUrl":  "https://example.com/action",
				"ctaText": "Take Action",
			},
			wantContains: []string{
				`href="https://example.com/action"`,
				"Take Action",
			},
		},
		{
			name: "with CTA URL but no text uses default",
			job: &EmailJob{
				Subject: "Action",
			},
			ctx: TemplateContext{
				"ctaUrl": "https://example.com/click",
			},
			wantContains: []string{
				`href="https://example.com/click"`,
				"Click Here",
			},
		},
		{
			name: "no CTA URL means no button",
			job: &EmailJob{
				Subject: "Info",
			},
			ctx:            TemplateContext{},
			wantNotContain: []string{"<a href="},
		},
		{
			name: "full email with all fields",
			job: &EmailJob{
				Subject: "Welcome to Our Service",
			},
			ctx: TemplateContext{
				"recipientName": "Jane Smith",
				"message":       "Thank you for signing up!",
				"ctaUrl":        "https://example.com/start",
				"ctaText":       "Get Started",
			},
			wantContains: []string{
				"<title>Welcome to Our Service</title>",
				"Hello Jane Smith,",
				"Thank you for signing up!",
				`href="https://example.com/start"`,
				"Get Started",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := w.generateFallbackHTML(tt.job, tt.ctx)

			for _, want := range tt.wantContains {
				if !contains(result, want) {
					t.Errorf("generateFallbackHTML() missing %q in result", want)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if contains(result, notWant) {
					t.Errorf("generateFallbackHTML() should not contain %q", notWant)
				}
			}

			// Should always be valid HTML
			if !contains(result, "<!DOCTYPE html>") {
				t.Error("generateFallbackHTML() missing DOCTYPE")
			}
			if !contains(result, "</html>") {
				t.Error("generateFallbackHTML() missing closing </html>")
			}
		})
	}
}

func TestGenerateFallbackText(t *testing.T) {
	w := &Worker{}

	tests := []struct {
		name           string
		job            *EmailJob
		ctx            TemplateContext
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "basic email",
			job: &EmailJob{
				Subject: "Welcome",
			},
			ctx: TemplateContext{},
			wantContains: []string{
				"Hello,",
				"This email was sent by Emergent.",
			},
		},
		{
			name: "with recipient name",
			job: &EmailJob{
				Subject: "Hello",
			},
			ctx: TemplateContext{
				"recipientName": "Alice",
			},
			wantContains: []string{
				"Hello Alice,",
			},
		},
		{
			name: "with message",
			job: &EmailJob{
				Subject: "Update",
			},
			ctx: TemplateContext{
				"message": "Your password was changed.",
			},
			wantContains: []string{
				"Your password was changed.",
			},
		},
		{
			name: "with CTA URL",
			job: &EmailJob{
				Subject: "Verify",
			},
			ctx: TemplateContext{
				"ctaUrl": "https://example.com/verify",
			},
			wantContains: []string{
				"Link: https://example.com/verify",
			},
		},
		{
			name: "no CTA URL means no link line",
			job: &EmailJob{
				Subject: "Info",
			},
			ctx:            TemplateContext{},
			wantNotContain: []string{"Link:"},
		},
		{
			name: "full email with all fields",
			job: &EmailJob{
				Subject: "Complete Profile",
			},
			ctx: TemplateContext{
				"recipientName": "Bob",
				"message":       "Please complete your profile.",
				"ctaUrl":        "https://example.com/profile",
			},
			wantContains: []string{
				"Hello Bob,",
				"Please complete your profile.",
				"Link: https://example.com/profile",
				"---",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := w.generateFallbackText(tt.job, tt.ctx)

			for _, want := range tt.wantContains {
				if !contains(result, want) {
					t.Errorf("generateFallbackText() missing %q in result:\n%s", want, result)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if contains(result, notWant) {
					t.Errorf("generateFallbackText() should not contain %q", notWant)
				}
			}
		})
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsSubstr(s, substr)))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// TemplateService.generatePlainText Tests
// =============================================================================

func TestTemplateService_generatePlainText(t *testing.T) {
	// Create a minimal TemplateService (no dependencies needed for generatePlainText)
	ts := &TemplateService{}

	tests := []struct {
		name           string
		ctx            TemplateContext
		wantContains   []string
		wantNotContain []string
		wantEmpty      bool
	}{
		{
			name: "plainText field provided - used as-is",
			ctx: TemplateContext{
				"plainText": "This is the plain text version.",
			},
			wantContains: []string{
				"This is the plain text version.",
			},
		},
		{
			name: "plainText takes precedence over other fields",
			ctx: TemplateContext{
				"plainText": "Custom plain text",
				"title":     "Title",
				"message":   "Message",
			},
			wantContains: []string{
				"Custom plain text",
			},
			wantNotContain: []string{
				"Title",
				"Message",
			},
		},
		{
			name: "title only",
			ctx: TemplateContext{
				"title": "Welcome to Our Service",
			},
			wantContains: []string{
				"Welcome to Our Service",
			},
		},
		{
			name: "previewText only",
			ctx: TemplateContext{
				"previewText": "Check out what's new!",
			},
			wantContains: []string{
				"Check out what's new!",
			},
		},
		{
			name: "message only",
			ctx: TemplateContext{
				"message": "Your account has been updated successfully.",
			},
			wantContains: []string{
				"Your account has been updated successfully.",
			},
		},
		{
			name: "ctaUrl only",
			ctx: TemplateContext{
				"ctaUrl": "https://example.com/action",
			},
			wantContains: []string{
				"Link: https://example.com/action",
			},
		},
		{
			name: "dashboardUrl only",
			ctx: TemplateContext{
				"dashboardUrl": "https://example.com/dashboard",
			},
			wantContains: []string{
				"Dashboard: https://example.com/dashboard",
			},
		},
		{
			name: "all fields combined",
			ctx: TemplateContext{
				"title":        "Important Update",
				"previewText":  "Your account requires attention",
				"message":      "Please review your settings.",
				"ctaUrl":       "https://example.com/settings",
				"dashboardUrl": "https://example.com/dashboard",
			},
			wantContains: []string{
				"Important Update",
				"Your account requires attention",
				"Please review your settings.",
				"Link: https://example.com/settings",
				"Dashboard: https://example.com/dashboard",
			},
		},
		{
			name:      "empty context returns empty string",
			ctx:       TemplateContext{},
			wantEmpty: true,
		},
		{
			name: "empty string fields are ignored",
			ctx: TemplateContext{
				"title":   "",
				"message": "",
			},
			wantEmpty: true,
		},
		{
			name: "nil context is handled",
			ctx:  nil,
			// Should not panic, may return empty
		},
		{
			name: "non-string plainText is ignored",
			ctx: TemplateContext{
				"plainText": 12345,
				"message":   "Fallback message",
			},
			wantContains: []string{
				"Fallback message",
			},
		},
		{
			name: "empty plainText string is ignored",
			ctx: TemplateContext{
				"plainText": "",
				"title":     "Title Used",
			},
			wantContains: []string{
				"Title Used",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.generatePlainText(tt.ctx)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("generatePlainText() = %q, want empty string", result)
				}
				return
			}

			for _, want := range tt.wantContains {
				if !contains(result, want) {
					t.Errorf("generatePlainText() missing %q in result:\n%s", want, result)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if contains(result, notWant) {
					t.Errorf("generatePlainText() should not contain %q", notWant)
				}
			}
		})
	}
}

func TestTemplateService_ClearCache(t *testing.T) {
	// Create a TemplateService with initialized caches
	ts := &TemplateService{
		templateCache: make(map[string]*raymond.Template),
		layoutCache:   make(map[string]*raymond.Template),
	}

	// Add some dummy entries to the caches
	ts.templateCache["test1"] = nil
	ts.templateCache["test2"] = nil
	ts.layoutCache["layout1"] = nil

	// Verify caches have entries
	if len(ts.templateCache) != 2 {
		t.Fatalf("templateCache should have 2 entries, got %d", len(ts.templateCache))
	}
	if len(ts.layoutCache) != 1 {
		t.Fatalf("layoutCache should have 1 entry, got %d", len(ts.layoutCache))
	}

	// Clear the caches
	ts.ClearCache()

	// Verify caches are empty
	if len(ts.templateCache) != 0 {
		t.Errorf("templateCache should be empty after ClearCache, got %d entries", len(ts.templateCache))
	}
	if len(ts.layoutCache) != 0 {
		t.Errorf("layoutCache should be empty after ClearCache, got %d entries", len(ts.layoutCache))
	}
}

func TestNewConfig(t *testing.T) {
	t.Run("copies all fields from app config", func(t *testing.T) {
		appCfg := &config.Config{
			Email: config.EmailConfig{
				Enabled:          true,
				MailgunDomain:    "mg.example.com",
				MailgunAPIKey:    "key-abc123",
				FromEmail:        "test@example.com",
				FromName:         "Test Sender",
				MaxRetries:       5,
				RetryDelaySec:    120,
				WorkerIntervalMs: 10000,
				WorkerBatchSize:  20,
			},
		}

		cfg := NewConfig(appCfg)

		if cfg.Enabled != true {
			t.Error("Enabled should be true")
		}
		if cfg.MailgunDomain != "mg.example.com" {
			t.Errorf("MailgunDomain = %q, want %q", cfg.MailgunDomain, "mg.example.com")
		}
		if cfg.MailgunAPIKey != "key-abc123" {
			t.Errorf("MailgunAPIKey = %q, want %q", cfg.MailgunAPIKey, "key-abc123")
		}
		if cfg.FromEmail != "test@example.com" {
			t.Errorf("FromEmail = %q, want %q", cfg.FromEmail, "test@example.com")
		}
		if cfg.FromName != "Test Sender" {
			t.Errorf("FromName = %q, want %q", cfg.FromName, "Test Sender")
		}
		if cfg.MaxRetries != 5 {
			t.Errorf("MaxRetries = %d, want 5", cfg.MaxRetries)
		}
		if cfg.RetryDelaySec != 120 {
			t.Errorf("RetryDelaySec = %d, want 120", cfg.RetryDelaySec)
		}
		if cfg.WorkerIntervalMs != 10000 {
			t.Errorf("WorkerIntervalMs = %d, want 10000", cfg.WorkerIntervalMs)
		}
		if cfg.WorkerBatchSize != 20 {
			t.Errorf("WorkerBatchSize = %d, want 20", cfg.WorkerBatchSize)
		}
	})

	t.Run("handles disabled config", func(t *testing.T) {
		appCfg := &config.Config{
			Email: config.EmailConfig{
				Enabled: false,
			},
		}

		cfg := NewConfig(appCfg)

		if cfg.Enabled != false {
			t.Error("Enabled should be false")
		}
	})

	t.Run("handles empty strings", func(t *testing.T) {
		appCfg := &config.Config{
			Email: config.EmailConfig{
				Enabled:       true,
				MailgunDomain: "",
				MailgunAPIKey: "",
			},
		}

		cfg := NewConfig(appCfg)

		if cfg.MailgunDomain != "" {
			t.Errorf("MailgunDomain should be empty, got %q", cfg.MailgunDomain)
		}
		if cfg.MailgunAPIKey != "" {
			t.Errorf("MailgunAPIKey should be empty, got %q", cfg.MailgunAPIKey)
		}
		// Should not be configured
		if cfg.IsConfigured() {
			t.Error("IsConfigured should return false for empty credentials")
		}
	})
}

// =============================================================================
// NewSender Tests
// =============================================================================

func TestNewSender(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("returns noOpSender when not configured", func(t *testing.T) {
		cfg := &Config{
			Enabled:       true,
			MailgunDomain: "",
			MailgunAPIKey: "",
		}

		sender := NewSender(log, cfg)

		// Verify we got a noOpSender by checking the Send behavior
		result, err := sender.Send(context.Background(), SendOptions{
			To:      "test@example.com",
			Subject: "Test",
		})

		if err != nil {
			t.Errorf("noOpSender.Send() error = %v, want nil", err)
		}
		if result == nil {
			t.Fatal("noOpSender.Send() result = nil, want non-nil")
		}
		if !result.Success {
			t.Error("noOpSender.Send() result.Success = false, want true")
		}
		if result.MessageID != "noop-test@example.com" {
			t.Errorf("noOpSender.Send() result.MessageID = %q, want %q", result.MessageID, "noop-test@example.com")
		}
	})

	t.Run("returns noOpSender when email disabled", func(t *testing.T) {
		cfg := &Config{
			Enabled:       false,
			MailgunDomain: "mg.example.com",
			MailgunAPIKey: "key-abc123",
		}

		sender := NewSender(log, cfg)

		result, err := sender.Send(context.Background(), SendOptions{
			To:      "disabled@example.com",
			Subject: "Disabled Test",
		})

		if err != nil {
			t.Errorf("noOpSender.Send() error = %v, want nil", err)
		}
		if result == nil {
			t.Fatal("noOpSender.Send() result = nil, want non-nil")
		}
		// Should be noOpSender, so message ID has noop prefix
		if result.MessageID != "noop-disabled@example.com" {
			t.Errorf("noOpSender.Send() result.MessageID = %q, want %q", result.MessageID, "noop-disabled@example.com")
		}
	})

	t.Run("returns MailgunSender when configured and enabled", func(t *testing.T) {
		cfg := &Config{
			Enabled:       true,
			MailgunDomain: "mg.example.com",
			MailgunAPIKey: "key-abc123",
			FromEmail:     "noreply@example.com",
			FromName:      "Test",
		}

		sender := NewSender(log, cfg)

		// We can't easily test that it's a MailgunSender without type assertion
		// but we can verify sender is not nil
		if sender == nil {
			t.Fatal("NewSender returned nil when configured")
		}
	})
}

// =============================================================================
// noOpSender Tests
// =============================================================================

func TestNoOpSender_Send(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	sender := &noOpSender{log: log}

	t.Run("returns success", func(t *testing.T) {
		result, err := sender.Send(context.Background(), SendOptions{
			To:      "user@example.com",
			Subject: "Test Subject",
		})

		if err != nil {
			t.Errorf("Send() error = %v, want nil", err)
		}
		if result == nil {
			t.Fatal("Send() result = nil, want non-nil")
		}
		if !result.Success {
			t.Error("Send() result.Success = false, want true")
		}
	})

	t.Run("returns noop message ID with recipient", func(t *testing.T) {
		result, err := sender.Send(context.Background(), SendOptions{
			To:      "special@test.org",
			Subject: "Test",
		})

		if err != nil {
			t.Errorf("Send() error = %v, want nil", err)
		}
		if result.MessageID != "noop-special@test.org" {
			t.Errorf("Send() result.MessageID = %q, want %q", result.MessageID, "noop-special@test.org")
		}
	})

	t.Run("handles empty options", func(t *testing.T) {
		result, err := sender.Send(context.Background(), SendOptions{})

		if err != nil {
			t.Errorf("Send() error = %v, want nil", err)
		}
		if result == nil {
			t.Fatal("Send() result = nil, want non-nil")
		}
		if result.MessageID != "noop-" {
			t.Errorf("Send() result.MessageID = %q, want %q", result.MessageID, "noop-")
		}
	})
}

// =============================================================================
// NewMailgunSender Tests
// =============================================================================

func TestNewMailgunSender(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("returns nil when not configured", func(t *testing.T) {
		cfg := &Config{
			MailgunDomain: "",
			MailgunAPIKey: "",
		}

		sender := NewMailgunSender(cfg, log)

		if sender != nil {
			t.Error("NewMailgunSender should return nil when not configured")
		}
	})

	t.Run("returns nil when domain is empty", func(t *testing.T) {
		cfg := &Config{
			MailgunDomain: "",
			MailgunAPIKey: "key-abc123",
		}

		sender := NewMailgunSender(cfg, log)

		if sender != nil {
			t.Error("NewMailgunSender should return nil when domain is empty")
		}
	})

	t.Run("returns nil when API key is empty", func(t *testing.T) {
		cfg := &Config{
			MailgunDomain: "mg.example.com",
			MailgunAPIKey: "",
		}

		sender := NewMailgunSender(cfg, log)

		if sender != nil {
			t.Error("NewMailgunSender should return nil when API key is empty")
		}
	})

	t.Run("returns sender when configured", func(t *testing.T) {
		cfg := &Config{
			MailgunDomain: "mg.example.com",
			MailgunAPIKey: "key-abc123",
			FromEmail:     "noreply@example.com",
			FromName:      "Test",
		}

		sender := NewMailgunSender(cfg, log)

		if sender == nil {
			t.Fatal("NewMailgunSender should return sender when configured")
		}
		if sender.cfg != cfg {
			t.Error("NewMailgunSender should use the provided config")
		}
		if sender.client == nil {
			t.Error("NewMailgunSender should initialize the mailgun client")
		}
	})
}
