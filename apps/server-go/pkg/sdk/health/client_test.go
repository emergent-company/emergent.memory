package health_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent/emergent-core/pkg/sdk"
	"github.com/emergent/emergent-core/pkg/sdk/testutil"
)

func TestHealthHealth(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureHealth := testutil.FixtureHealthResponse()
	mock.OnJSON("GET", "/health", http.StatusOK, fixtureHealth)

	client, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth: sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: "test_key",
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

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

func TestHealthReady(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/ready", http.StatusOK, map[string]string{"status": "ready"})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	ready, err := client.Health.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready() error = %v", err)
	}

	if !ready {
		t.Error("expected service to be ready, got not ready")
	}
}

func TestHealthReadyNotReady(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/ready", http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	ready, err := client.Health.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready() error = %v", err)
	}

	if ready {
		t.Error("expected service to not be ready, got ready")
	}
}

func TestHealthHealthz(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	err := client.Health.Healthz(context.Background())
	if err != nil {
		t.Fatalf("Healthz() error = %v", err)
	}
}
