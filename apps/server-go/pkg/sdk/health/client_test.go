package health_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/health"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func newClient(t *testing.T, mock *testutil.MockServer) *sdk.Client {
	t.Helper()
	client, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

func TestHealthHealth(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureHealth := testutil.FixtureHealthResponse()
	mock.OnJSON("GET", "/health", http.StatusOK, fixtureHealth)

	client := newClient(t, mock)

	result, err := client.Health.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	if result.Status != fixtureHealth.Status {
		t.Errorf("expected status %s, got %s", fixtureHealth.Status, result.Status)
	}
	if result.Version != fixtureHealth.Version {
		t.Errorf("expected version %s, got %s", fixtureHealth.Version, result.Version)
	}
}

func TestHealthAPIHealth(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureHealth := testutil.FixtureHealthResponse()
	mock.OnJSON("GET", "/api/health", http.StatusOK, fixtureHealth)

	client := newClient(t, mock)

	result, err := client.Health.APIHealth(context.Background())
	if err != nil {
		t.Fatalf("APIHealth() error = %v", err)
	}

	if result.Status != fixtureHealth.Status {
		t.Errorf("expected status %s, got %s", fixtureHealth.Status, result.Status)
	}
}

func TestHealthReady(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/ready", http.StatusOK, map[string]string{"status": "ready"})

	client := newClient(t, mock)

	result, err := client.Health.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready() error = %v", err)
	}

	if result.Status != "ready" {
		t.Errorf("expected status ready, got %s", result.Status)
	}
}

func TestHealthIsReady(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/ready", http.StatusOK, map[string]string{"status": "ready"})

	client := newClient(t, mock)

	ready, err := client.Health.IsReady(context.Background())
	if err != nil {
		t.Fatalf("IsReady() error = %v", err)
	}

	if !ready {
		t.Error("expected service to be ready, got not ready")
	}
}

func TestHealthReadyNotReady(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/ready", http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "message": "Database connection failed"})

	client := newClient(t, mock)

	result, err := client.Health.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready() error = %v", err)
	}

	if result.Status != "not_ready" {
		t.Errorf("expected status not_ready, got %s", result.Status)
	}
}

func TestHealthHealthz(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	client := newClient(t, mock)

	err := client.Health.Healthz(context.Background())
	if err != nil {
		t.Fatalf("Healthz() error = %v", err)
	}
}

func TestHealthDebug(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	debugResp := map[string]any{
		"environment": "development",
		"debug":       true,
		"go_version":  "go1.23.0",
		"goroutines":  42,
		"memory": map[string]any{
			"alloc_mb":       float64(128),
			"total_alloc_mb": float64(512),
			"sys_mb":         float64(256),
			"num_gc":         float64(100),
		},
		"database": map[string]any{
			"host":        "localhost",
			"port":        float64(5432),
			"database":    "emergent",
			"pool_total":  float64(10),
			"pool_idle":   float64(5),
			"pool_in_use": float64(3),
		},
	}
	mock.OnJSON("GET", "/debug", http.StatusOK, debugResp)

	client := newClient(t, mock)

	result, err := client.Health.Debug(context.Background())
	if err != nil {
		t.Fatalf("Debug() error = %v", err)
	}

	if result.Environment != "development" {
		t.Errorf("expected environment development, got %s", result.Environment)
	}
	if result.GoVersion != "go1.23.0" {
		t.Errorf("expected go_version go1.23.0, got %s", result.GoVersion)
	}
	if result.Goroutines != 42 {
		t.Errorf("expected goroutines 42, got %d", result.Goroutines)
	}
}

func TestHealthDebugNotFoundInProduction(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/debug", http.StatusNotFound, map[string]string{"message": "Not found"})

	client := newClient(t, mock)

	_, err := client.Health.Debug(context.Background())
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

func TestHealthJobMetrics(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	metricsResp := health.AllJobMetrics{
		Queues: []health.JobQueueMetrics{
			{
				Queue:       "document_parsing",
				Pending:     5,
				Processing:  2,
				Completed:   100,
				Failed:      3,
				Total:       110,
				LastHour:    10,
				Last24Hours: 50,
			},
			{
				Queue:       "chunk_embedding",
				Pending:     0,
				Processing:  1,
				Completed:   200,
				Failed:      0,
				Total:       201,
				LastHour:    20,
				Last24Hours: 100,
			},
		},
		Timestamp: "2025-01-01T00:00:00Z",
	}
	mock.OnJSON("GET", "/api/metrics/jobs", http.StatusOK, metricsResp)

	client := newClient(t, mock)

	result, err := client.Health.JobMetrics(context.Background())
	if err != nil {
		t.Fatalf("JobMetrics() error = %v", err)
	}

	if len(result.Queues) != 2 {
		t.Fatalf("expected 2 queues, got %d", len(result.Queues))
	}
	if result.Queues[0].Queue != "document_parsing" {
		t.Errorf("expected first queue document_parsing, got %s", result.Queues[0].Queue)
	}
	if result.Queues[0].Pending != 5 {
		t.Errorf("expected pending 5, got %d", result.Queues[0].Pending)
	}
}

func TestHealthSchedulerStatus(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/api/metrics/scheduler", http.StatusOK, map[string]string{
		"message": "Scheduler metrics endpoint - wire up to scheduler service for task info",
	})

	client := newClient(t, mock)

	result, err := client.Health.SchedulerStatus(context.Background())
	if err != nil {
		t.Fatalf("SchedulerStatus() error = %v", err)
	}

	if result.Message == "" {
		t.Error("expected non-empty message from scheduler metrics")
	}
}
