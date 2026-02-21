package vertex

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestClientOptions(t *testing.T) {
	t.Run("WithMaxRetries", func(t *testing.T) {
		c := &Client{}
		opt := WithMaxRetries(5)
		opt(c)

		if c.maxRetries != 5 {
			t.Errorf("maxRetries = %d, want 5", c.maxRetries)
		}
	})

	t.Run("WithBaseDelay", func(t *testing.T) {
		c := &Client{}
		opt := WithBaseDelay(500 * time.Millisecond)
		opt(c)

		if c.baseDelay != 500*time.Millisecond {
			t.Errorf("baseDelay = %v, want 500ms", c.baseDelay)
		}
	})

	t.Run("WithMaxDelay", func(t *testing.T) {
		c := &Client{}
		opt := WithMaxDelay(30 * time.Second)
		opt(c)

		if c.maxDelay != 30*time.Second {
			t.Errorf("maxDelay = %v, want 30s", c.maxDelay)
		}
	})

	t.Run("WithLogger", func(t *testing.T) {
		c := &Client{}
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		opt := WithLogger(logger)
		opt(c)

		if c.log != logger {
			t.Error("logger was not set correctly")
		}
	})

	t.Run("chained options", func(t *testing.T) {
		c := &Client{}
		opts := []ClientOption{
			WithMaxRetries(3),
			WithBaseDelay(100 * time.Millisecond),
			WithMaxDelay(5 * time.Second),
		}

		for _, opt := range opts {
			opt(c)
		}

		if c.maxRetries != 3 {
			t.Errorf("maxRetries = %d, want 3", c.maxRetries)
		}
		if c.baseDelay != 100*time.Millisecond {
			t.Errorf("baseDelay = %v, want 100ms", c.baseDelay)
		}
		if c.maxDelay != 5*time.Second {
			t.Errorf("maxDelay = %v, want 5s", c.maxDelay)
		}
	})
}

func TestRetryableError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   string
	}{
		{
			name:       "500 error",
			statusCode: 500,
			body:       "internal server error",
			expected:   "retryable API error 500: internal server error",
		},
		{
			name:       "503 error",
			statusCode: 503,
			body:       "service unavailable",
			expected:   "retryable API error 503: service unavailable",
		},
		{
			name:       "429 rate limit",
			statusCode: 429,
			body:       "rate limit exceeded",
			expected:   "retryable API error 429: rate limit exceeded",
		},
		{
			name:       "empty body",
			statusCode: 502,
			body:       "",
			expected:   "retryable API error 502: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &retryableError{
				statusCode: tt.statusCode,
				body:       tt.body,
			}

			result := err.Error()
			if result != tt.expected {
				t.Errorf("Error() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	// Verify constants have sensible values
	if DefaultModel != "gemini-embedding-001" {
		t.Errorf("DefaultModel = %q, want gemini-embedding-001", DefaultModel)
	}
	if DefaultDimension != 768 {
		t.Errorf("DefaultDimension = %d, want 768", DefaultDimension)
	}
	if DefaultMaxRetries != 3 {
		t.Errorf("DefaultMaxRetries = %d, want 3", DefaultMaxRetries)
	}
	if DefaultBaseDelay != 100*time.Millisecond {
		t.Errorf("DefaultBaseDelay = %v, want 100ms", DefaultBaseDelay)
	}
	if DefaultMaxDelay != 10*time.Second {
		t.Errorf("DefaultMaxDelay = %v, want 10s", DefaultMaxDelay)
	}
	if DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v, want 30s", DefaultTimeout)
	}
	if DefaultBatchSize != 100 {
		t.Errorf("DefaultBatchSize = %d, want 100", DefaultBatchSize)
	}
}

func TestConfig(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		cfg := Config{}

		if cfg.ProjectID != "" {
			t.Errorf("ProjectID should be empty by default")
		}
		if cfg.Location != "" {
			t.Errorf("Location should be empty by default")
		}
		if cfg.Model != "" {
			t.Errorf("Model should be empty by default")
		}
		if cfg.Timeout != 0 {
			t.Errorf("Timeout should be 0 by default")
		}
	})

	t.Run("custom values", func(t *testing.T) {
		cfg := Config{
			ProjectID: "my-project",
			Location:  "us-central1",
			Model:     "gemini-embedding-001",
			Timeout:   60 * time.Second,
		}

		if cfg.ProjectID != "my-project" {
			t.Errorf("ProjectID = %q, want my-project", cfg.ProjectID)
		}
		if cfg.Location != "us-central1" {
			t.Errorf("Location = %q, want us-central1", cfg.Location)
		}
		if cfg.Model != "gemini-embedding-001" {
			t.Errorf("Model = %q, want gemini-embedding-001", cfg.Model)
		}
		if cfg.Timeout != 60*time.Second {
			t.Errorf("Timeout = %v, want 60s", cfg.Timeout)
		}
	})
}

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name      string
		baseDelay time.Duration
		maxDelay  time.Duration
		attempt   int
		expected  time.Duration
	}{
		{
			name:      "first attempt",
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			attempt:   1,
			expected:  100 * time.Millisecond, // base * 2^(1-1) = base * 1
		},
		{
			name:      "second attempt",
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			attempt:   2,
			expected:  200 * time.Millisecond, // base * 2^(2-1) = base * 2
		},
		{
			name:      "third attempt",
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			attempt:   3,
			expected:  400 * time.Millisecond, // base * 2^(3-1) = base * 4
		},
		{
			name:      "fourth attempt",
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			attempt:   4,
			expected:  800 * time.Millisecond, // base * 2^(4-1) = base * 8
		},
		{
			name:      "capped at max delay",
			baseDelay: 1 * time.Second,
			maxDelay:  5 * time.Second,
			attempt:   10, // 1s * 2^9 = 512s, but capped at 5s
			expected:  5 * time.Second,
		},
		{
			name:      "zero attempt",
			baseDelay: 100 * time.Millisecond,
			maxDelay:  10 * time.Second,
			attempt:   0,
			expected:  50 * time.Millisecond, // base * 2^(-1) = base * 0.5
		},
		{
			name:      "large delay hitting cap",
			baseDelay: 500 * time.Millisecond,
			maxDelay:  2 * time.Second,
			attempt:   5, // 500ms * 2^4 = 8s, but capped at 2s
			expected:  2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				baseDelay: tt.baseDelay,
				maxDelay:  tt.maxDelay,
			}

			result := c.calculateBackoff(tt.attempt)
			if result != tt.expected {
				t.Errorf("calculateBackoff(%d) = %v, want %v", tt.attempt, result, tt.expected)
			}
		})
	}
}

func TestCalculateBackoff_ExponentialGrowth(t *testing.T) {
	c := &Client{
		baseDelay: 100 * time.Millisecond,
		maxDelay:  10 * time.Second,
	}

	// Verify exponential growth pattern
	prev := c.calculateBackoff(1)
	for i := 2; i <= 5; i++ {
		curr := c.calculateBackoff(i)
		// Each attempt should double (within some tolerance for float precision)
		if curr < prev {
			t.Errorf("calculateBackoff(%d) = %v should be >= calculateBackoff(%d) = %v", i, curr, i-1, prev)
		}
		expectedRatio := 2.0
		actualRatio := float64(curr) / float64(prev)
		if actualRatio < expectedRatio*0.99 || actualRatio > expectedRatio*1.01 {
			t.Errorf("backoff ratio between attempt %d and %d: got %v, want ~%v", i-1, i, actualRatio, expectedRatio)
		}
		prev = curr
	}
}
