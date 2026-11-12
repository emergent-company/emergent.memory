package vertex

import (
	"context"
	"encoding/json"
	"io"
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
	if DefaultModel != "gemini-embedding-2-preview" {
		t.Errorf("DefaultModel = %q, want gemini-embedding-2-preview", DefaultModel)
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
			Model:     "gemini-embedding-2-preview",
			Timeout:   60 * time.Second,
		}

		if cfg.ProjectID != "my-project" {
			t.Errorf("ProjectID = %q, want my-project", cfg.ProjectID)
		}
		if cfg.Location != "us-central1" {
			t.Errorf("Location = %q, want us-central1", cfg.Location)
		}
		if cfg.Model != "gemini-embedding-2-preview" {
			t.Errorf("Model = %q, want gemini-embedding-2-preview", cfg.Model)
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

// recordingSender captures the marshalled :predict body and returns a canned
// response, so a test can assert the exact wire shape without calling Google.
type recordingSender struct {
	bodies [][]byte
	err    error
	values []float32
	tokens int
}

func (r *recordingSender) send(_ context.Context, _ string, body []byte) (*predictResponse, error) {
	r.bodies = append(r.bodies, append([]byte(nil), body...))
	if r.err != nil {
		return nil, r.err
	}
	resp := &predictResponse{Predictions: []prediction{{
		Embeddings: embeddingResult{
			Values:     r.values,
			Statistics: embeddingStats{TokenCount: r.tokens},
		},
	}}}
	return resp, nil
}

func newRecordingClient(model string, rec *recordingSender) *Client {
	c := &Client{
		projectID:  "test-project",
		location:   "us-central1",
		model:      model,
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		maxRetries: 0,
		baseDelay:  time.Millisecond,
		maxDelay:   time.Millisecond,
	}
	c.send = rec.send
	return c
}

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	return got
}

func TestBuildPredictRequest(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		texts    []string
		taskType string
		wantText string
		wantTask string // "" means the field must be absent
	}{
		{
			name:     "GA gemini-embedding-2 query uses the documented prefix",
			model:    "gemini-embedding-2",
			texts:    []string{"why is the sky blue"},
			taskType: TaskTypeQuery,
			wantText: "task: search result | query: why is the sky blue",
			wantTask: "",
		},
		{
			name:     "GA gemini-embedding-2 document uses the documented prefix",
			model:    "gemini-embedding-2",
			texts:    []string{"the sky is blue"},
			taskType: TaskTypeDocument,
			wantText: "title: none | text: the sky is blue",
			wantTask: "",
		},
		{
			name:     "GA id with a models/ prefix is still recognised",
			model:    "publishers/google/models/gemini-embedding-2",
			texts:    []string{"q"},
			taskType: TaskTypeQuery,
			wantText: "task: search result | query: q",
			wantTask: "",
		},
		{
			name:     "gemini-embedding-001 query keeps task_type and the raw text",
			model:    "gemini-embedding-001",
			texts:    []string{"why is the sky blue"},
			taskType: TaskTypeQuery,
			wantText: "why is the sky blue",
			wantTask: TaskTypeQuery,
		},
		{
			name:     "gemini-embedding-001 document keeps task_type and the raw text",
			model:    "gemini-embedding-001",
			texts:    []string{"the sky is blue"},
			taskType: TaskTypeDocument,
			wantText: "the sky is blue",
			wantTask: TaskTypeDocument,
		},
		{
			name:     "the preview family is not treated as GA",
			model:    "gemini-embedding-2-preview",
			texts:    []string{"x"},
			taskType: TaskTypeQuery,
			wantText: "x",
			wantTask: TaskTypeQuery,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildPredictRequest(tt.model, tt.texts, tt.taskType)
			if len(req.Instances) != 1 {
				t.Fatalf("instances = %d, want 1", len(req.Instances))
			}
			inst := req.Instances[0]
			if inst.Content != tt.wantText {
				t.Errorf("content = %q, want %q", inst.Content, tt.wantText)
			}
			if inst.TaskType != tt.wantTask {
				t.Errorf("task_type = %q, want %q", inst.TaskType, tt.wantTask)
			}
			if req.Parameters == nil || req.Parameters.OutputDimensionality != DefaultDimension {
				t.Errorf("outputDimensionality = %v, want %d", req.Parameters, DefaultDimension)
			}
		})
	}
}

// TestPredictBodyOmitsTaskTypeForGA pins the wire contract: Google rejects the
// field for gemini-embedding-2, so the JSON must not carry it at all — a
// "task_type": "" would still be a field.
func TestPredictBodyOmitsTaskTypeForGA(t *testing.T) {
	rec := &recordingSender{values: []float32{0.1, 0.2}}
	c := newRecordingClient("gemini-embedding-2", rec)

	if _, err := c.EmbedQuery(context.Background(), "hello"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if len(rec.bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(rec.bodies))
	}

	body := decodeBody(t, rec.bodies[0])
	instances, ok := body["instances"].([]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("instances = %#v, want one entry", body["instances"])
	}
	inst := instances[0].(map[string]any)
	if _, present := inst["task_type"]; present {
		t.Errorf("task_type present in GA request: %#v", inst)
	}
	if got, want := inst["content"], "task: search result | query: hello"; got != want {
		t.Errorf("content = %v, want %q", got, want)
	}
}

func TestEmbedQuerySendsQueryTask(t *testing.T) {
	rec := &recordingSender{values: []float32{0.1}, tokens: 7}
	c := newRecordingClient("gemini-embedding-001", rec)

	res, err := c.EmbedQueryWithUsage(context.Background(), "find me")
	if err != nil {
		t.Fatalf("EmbedQueryWithUsage: %v", err)
	}
	if len(res.Embedding) != 1 || res.Embedding[0] != 0.1 {
		t.Errorf("embedding = %v, want [0.1]", res.Embedding)
	}
	if res.Usage == nil || res.Usage.TotalTokens != 7 {
		t.Errorf("usage = %#v, want 7 tokens", res.Usage)
	}
	if res.Provider != "vertex" || res.Model != "gemini-embedding-001" {
		t.Errorf("provider/model = %q/%q", res.Provider, res.Model)
	}

	body := decodeBody(t, rec.bodies[0])
	inst := body["instances"].([]any)[0].(map[string]any)
	if got := inst["task_type"]; got != TaskTypeQuery {
		t.Errorf("query task_type = %v, want %s", got, TaskTypeQuery)
	}
}

func TestEmbedDocumentsSendDocumentTask(t *testing.T) {
	rec := &recordingSender{values: []float32{0.5}, tokens: 3}
	c := newRecordingClient("gemini-embedding-001", rec)

	if _, err := c.EmbedDocumentsWithUsage(context.Background(), []string{"a"}); err != nil {
		t.Fatalf("EmbedDocumentsWithUsage: %v", err)
	}

	body := decodeBody(t, rec.bodies[0])
	inst := body["instances"].([]any)[0].(map[string]any)
	if got := inst["task_type"]; got != TaskTypeDocument {
		t.Errorf("document task_type = %v, want %s", got, TaskTypeDocument)
	}
}
