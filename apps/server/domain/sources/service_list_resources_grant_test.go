package sources

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// newListResourcesTestService builds a DB-backed Service around one provider so
// the saved-source listing path can be exercised without a real Drive.
func newListResourcesTestService(t *testing.T, db *testdb.TestDB, p SourceProvider) *Service {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(NewRepository(db.DB, log), cfg, log, NewRegistry(p))
}

// createGithubSource creates a source through the service so its credentials are
// encrypted the way the listing path expects to decrypt them.
func createGithubSource(t *testing.T, svc *Service, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	created, err := svc.Create(context.Background(), projectID, &CreateIntegrationRequest{
		Name:         "Repo",
		ProviderType: "github",
		Config:       map[string]any{"access_token": "at"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return uuid.MustParse(created.ID)
}

// TestService_ListResources_GrantRevokedKeepsReconnect is the wiring half of
// #2149: the provider maps a dead grant to an actionable validation error, and
// Service.ListResources must pass that error through instead of re-wrapping it as
// "list resources failed: <raw provider text>". The prefix is what hid the
// remediation from the UI and leaked Google's invalid_grant string.
func TestService_ListResources_GrantRevokedKeepsReconnect(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourceslistgrant")
	t.Cleanup(tdb.Close)

	p := githubLister()
	p.listErr = apperror.NewValidation(driveReconnectRequired)
	svc := newListResourcesTestService(t, tdb, p)
	projectID := uuid.New()
	id := createGithubSource(t, svc, projectID)

	_, _, err := svc.ListResources(ctx, projectID, id, "")
	if err == nil {
		t.Fatal("ListResources(revoked grant) = nil, want the reconnect error")
	}
	if strings.Contains(err.Error(), "list resources failed") {
		t.Errorf("error was re-wrapped, losing the remediation: %q", err)
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want an *apperror.Error", err)
	}
	if appErr.Message != driveReconnectRequired {
		t.Errorf("message = %q, want %q", appErr.Message, driveReconnectRequired)
	}
}

// TestService_ListResources_TransientStaysWrapped pins the behaviour this issue
// deliberately did not change: a provider error that carries no remediation is
// still reported as "list resources failed: <detail>" for the operator.
func TestService_ListResources_TransientStaysWrapped(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourceslisttransient")
	t.Cleanup(tdb.Close)

	p := githubLister()
	p.listErr = errors.New("boom")
	svc := newListResourcesTestService(t, tdb, p)
	projectID := uuid.New()
	id := createGithubSource(t, svc, projectID)

	_, _, err := svc.ListResources(ctx, projectID, id, "")
	if err == nil {
		t.Fatal("ListResources(transient) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "list resources failed: boom") {
		t.Errorf("error = %q, want the prefixed provider detail", err)
	}
}
