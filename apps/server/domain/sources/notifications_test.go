package sources

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// recordingPublisher is the fake the sources tests drive: it records what the
// domain asked it to publish and resolve, and can be made to fail so the
// best-effort rule is testable.
type recordingPublisher struct {
	notices  []DataSourceReconnectNotice
	projects []string
	resolved []string
	err      error
}

func (p *recordingPublisher) PublishDataSourceNeedsReconnect(_ context.Context, projectID string, notice DataSourceReconnectNotice) error {
	p.notices = append(p.notices, notice)
	p.projects = append(p.projects, projectID)
	return p.err
}

func (p *recordingPublisher) ResolveDataSource(_ context.Context, projectID, sourceID string) error {
	p.resolved = append(p.resolved, projectID+"/"+sourceID)
	return p.err
}

// resolvedSet collapses duplicate resolves (the reconnect path legitimately
// resolves the same source twice) into the set of sources whose items were
// cleared.
func (p *recordingPublisher) resolvedSet() map[string]bool {
	out := make(map[string]bool, len(p.resolved))
	for _, r := range p.resolved {
		out[r] = true
	}
	return out
}

type notifyFixture struct {
	ctx       context.Context
	repo      *Repository
	svc       *Service
	worker    *SyncWorker
	publisher *recordingPublisher
	projectID uuid.UUID
	interval  int
}

func newNotifyFixture(t *testing.T, suffix string, publisher NotificationPublisher) notifyFixture {
	t.Helper()
	return newNotifyFixtureWithRegistry(t, suffix, publisher, NewDefaultRegistry())
}

func newNotifyFixtureWithRegistry(t *testing.T, suffix string, publisher NotificationPublisher, registry *Registry) notifyFixture {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, suffix)
	t.Cleanup(tdb.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	repo := NewRepository(tdb.DB, log)

	var svc *Service
	if publisher == nil {
		svc = NewService(repo, cfg, log, registry)
	} else {
		svc = NewService(repo, cfg, log, registry, WithNotificationPublisher(publisher))
	}
	fx := notifyFixture{
		ctx:       ctx,
		repo:      repo,
		svc:       svc,
		worker:    NewSyncWorker(svc, nil, nil, nil, nil, log),
		projectID: uuid.New(),
		interval:  60,
	}
	if p, ok := publisher.(*recordingPublisher); ok {
		fx.publisher = p
	}
	return fx
}

// TestFailJob_ParkPublishesOneReconnectNotice drives the publish seam: the park
// commits, and the member's item names the source, its provider, the deep link
// and the connector's own remediation — the same string the sync history keeps,
// so "what do I do" has one source of truth.
func TestFailJob_ParkPublishesOneReconnectNotice(t *testing.T) {
	pub := &recordingPublisher{}
	fx := newNotifyFixture(t, "sourcesnotifypark", pub)
	sourceID := createParkedTestSource(t, fx.ctx, fx.repo, fx.projectID, &fx.interval)
	job := createParkedTestJob(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	src := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)

	if err := fx.worker.failJob(fx.ctx, job, src, fmt.Errorf("enumerate source: %w", errDriveGrantRejected)); err == nil {
		t.Fatal("failJob() = nil, want the recorded failure returned")
	}

	parked := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if parked.Status != SourceStatusNeedsReconnect {
		t.Fatalf("Status = %q, want %q", parked.Status, SourceStatusNeedsReconnect)
	}
	if len(pub.notices) != 1 {
		t.Fatalf("notices = %d, want exactly one", len(pub.notices))
	}
	notice := pub.notices[0]
	if notice.SourceID != sourceID.String() {
		t.Errorf("SourceID = %q, want %q", notice.SourceID, sourceID)
	}
	if notice.GroupKey != "data_source:"+sourceID.String() {
		t.Errorf("GroupKey = %q, want one item per source", notice.GroupKey)
	}
	if notice.Provider != ProviderTypeGoogleDrive {
		t.Errorf("Provider = %q, want %q", notice.Provider, ProviderTypeGoogleDrive)
	}
	if notice.SourceName != "Drive" {
		t.Errorf("SourceName = %q, want %q", notice.SourceName, "Drive")
	}
	if !strings.Contains(notice.Title, "Drive") || !strings.Contains(notice.Title, "needs reconnecting") {
		t.Errorf("Title = %q, want the provider/source and the state", notice.Title)
	}
	if parked.LastError == nil {
		t.Fatal("LastError = nil, want the failure recorded on the source")
	}
	if notice.Message != *parked.LastError {
		t.Errorf("Message = %q, want the recorded remediation %q", notice.Message, *parked.LastError)
	}
	if notice.ActionURL != "/data-sources/"+sourceID.String() {
		t.Errorf("ActionURL = %q, want the source's page", notice.ActionURL)
	}
	if notice.ActionLabel != "Reconnect" {
		t.Errorf("ActionLabel = %q, want %q", notice.ActionLabel, "Reconnect")
	}
	if pub.projects[0] != fx.projectID.String() {
		t.Errorf("project = %q, want %q", pub.projects[0], fx.projectID)
	}
}

// TestFailJob_RefusedParkPublishesNothing is the other half of the invariant: a
// park refused by the credential guard leaves the source active, and the member
// must not be told to fix something that is already fixed.
func TestFailJob_RefusedParkPublishesNothing(t *testing.T) {
	pub := &recordingPublisher{}
	fx := newNotifyFixture(t, "sourcesnotifyrefused", pub)
	sourceID := createParkedTestSource(t, fx.ctx, fx.repo, fx.projectID, &fx.interval)
	inFlight := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	job := createParkedTestJob(t, fx.ctx, fx.repo, fx.projectID, sourceID)

	// The user reconnects mid-run, moving the grant epoch the running job holds.
	if err := fx.repo.ReplaceOAuthGrant(fx.ctx, sourceID, fx.projectID, []byte("sealed-new-grant"), inFlight.Config); err != nil {
		t.Fatalf("ReplaceOAuthGrant() error = %v", err)
	}
	if err := fx.worker.failJob(fx.ctx, job, inFlight, fmt.Errorf("enumerate source: %w", errDriveGrantRejected)); err == nil {
		t.Fatal("failJob() = nil, want the recorded failure returned")
	}

	got := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if got.Status != SourceStatusActive {
		t.Fatalf("Status = %q, want %q: the grant this run failed against was replaced", got.Status, SourceStatusActive)
	}
	if len(pub.notices) != 0 {
		t.Fatalf("notices = %d, want none for a refused park", len(pub.notices))
	}
}

// TestFailJob_PublisherErrorDoesNotChangeTheOutcome pins "notification delivery
// never changes the sync outcome": the park still commits, the failure is still
// on the source, and the returned error is the sync failure.
func TestFailJob_PublisherErrorDoesNotChangeTheOutcome(t *testing.T) {
	pub := &recordingPublisher{err: fmt.Errorf("notification backend down")}
	fx := newNotifyFixture(t, "sourcesnotifypubfail", pub)
	sourceID := createParkedTestSource(t, fx.ctx, fx.repo, fx.projectID, &fx.interval)
	job := createParkedTestJob(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	src := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)

	syncErr := fmt.Errorf("enumerate source: %w", errDriveGrantRejected)
	if err := fx.worker.failJob(fx.ctx, job, src, syncErr); err == nil {
		t.Fatal("failJob() = nil, want the sync failure returned")
	}

	parked := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if parked.Status != SourceStatusNeedsReconnect {
		t.Errorf("Status = %q, want the park to commit despite the publish error", parked.Status)
	}
	if parked.LastError == nil {
		t.Error("LastError = nil, want the failure recorded despite the publish error")
	}
	if len(pub.notices) != 1 {
		t.Errorf("publish attempts = %d, want 1 (the error is only logged)", len(pub.notices))
	}

	jobAfter, err := fx.repo.GetSyncJobByID(fx.ctx, job.ID)
	if err != nil || jobAfter == nil {
		t.Fatalf("GetSyncJob() = %v, %v", jobAfter, err)
	}
	if jobAfter.Status != SyncStatusFailed {
		t.Errorf("job status = %q, want %q", jobAfter.Status, SyncStatusFailed)
	}
}

// TestUpdate_ResolvesWhenTheSourceStopsBeingParked covers both explicit toggles:
// an enable retries the credential and a disable stops the source, and in either
// case the item has nothing left to ask for.
func TestUpdate_ResolvesWhenTheSourceStopsBeingParked(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		want    string
	}{
		{name: "enable", enabled: true, want: SourceStatusActive},
		{name: "disable", enabled: false, want: SourceStatusDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub := &recordingPublisher{}
			fx := newNotifyFixture(t, "sourcesnotifyupdate", pub)
			created, err := fx.svc.Create(fx.ctx, fx.projectID, &CreateIntegrationRequest{
				Name:         "repo",
				ProviderType: ProviderTypeGitHub,
				Config:       map[string]any{"repo": "acme/app", "access_token": "ghp_x"},
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			sourceID := uuid.MustParse(created.ID)
			if _, err := fx.repo.MarkSourceNeedsReconnect(fx.ctx, fx.projectID, sourceID, driveReconnectRequired, nil); err != nil {
				t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
			}
			pub.resolved = nil

			enabled := tc.enabled
			updated, err := fx.svc.Update(fx.ctx, fx.projectID, sourceID, &UpdateIntegrationRequest{Enabled: &enabled})
			if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			if got := updated.Status; got != string(tc.want) {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
			if !pub.resolvedSet()[fx.projectID.String()+"/"+sourceID.String()] {
				t.Fatalf("resolved = %v, want the source's item resolved", pub.resolved)
			}
		})
	}
}

// TestDelete_ResolvesTheItem: a deleted source leaves nothing actionable behind.
func TestDelete_ResolvesTheItem(t *testing.T) {
	pub := &recordingPublisher{}
	fx := newNotifyFixture(t, "sourcesnotifydelete", pub)
	created, err := fx.svc.Create(fx.ctx, fx.projectID, &CreateIntegrationRequest{
		Name:         "repo",
		ProviderType: ProviderTypeGitHub,
		Config:       map[string]any{"repo": "acme/app", "access_token": "ghp_x"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sourceID := uuid.MustParse(created.ID)
	if _, err := fx.repo.MarkSourceNeedsReconnect(fx.ctx, fx.projectID, sourceID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}
	pub.resolved = nil

	if err := fx.svc.Delete(fx.ctx, fx.projectID, sourceID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !pub.resolvedSet()[fx.projectID.String()+"/"+sourceID.String()] {
		t.Fatalf("resolved = %v, want the deleted source's item resolved", pub.resolved)
	}
}

// TestReconnect_ResolvesTheSourceItem covers a reconnect of the source itself.
func TestReconnect_ResolvesTheSourceItem(t *testing.T) {
	pub := &recordingPublisher{}
	fx := newNotifyFixture(t, "sourcesnotifyreconnect", pub)
	sourceID := createParkedTestSource(t, fx.ctx, fx.repo, fx.projectID, &fx.interval)
	src := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if _, err := fx.repo.MarkSourceNeedsReconnect(fx.ctx, fx.projectID, sourceID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}
	pub.resolved = nil

	if _, err := fx.svc.replaceCredentials(fx.ctx, src, Credentials{"access_token": "ya29.new"}, map[string]any{}); err != nil {
		t.Fatalf("replaceCredentials() error = %v", err)
	}
	if !pub.resolvedSet()[fx.projectID.String()+"/"+sourceID.String()] {
		t.Fatalf("resolved = %v, want the reconnected source's item resolved", pub.resolved)
	}
}

// TestReconnectOfSharedCredential_ResolvesOnlyReactivatedSources pins the
// RETURNING id rule: the set-based repair returns every parked sibling to
// active, and each of those is resolved — while a disabled sibling the statement
// leaves alone is neither reactivated nor reported as repaired.
func TestReconnectOfSharedCredential_ResolvesOnlyReactivatedSources(t *testing.T) {
	pub := &recordingPublisher{}
	fx := newNotifyFixture(t, "sourcesnotifyshared", pub)

	saved, err := fx.svc.CreateSavedCredential(fx.ctx, fx.projectID, ProviderTypeGitHub, "team PAT", map[string]any{"access_token": "ghp_old"})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	createOnCredential := func(name string, enabled bool) uuid.UUID {
		t.Helper()
		req := &CreateIntegrationRequest{
			Name:              name,
			ProviderType:      ProviderTypeGitHub,
			Config:            map[string]any{"repo": "acme/" + name},
			SavedCredentialID: &saved.ID,
		}
		created, err := fx.svc.Create(fx.ctx, fx.projectID, req)
		if err != nil {
			t.Fatalf("Create(%s) error = %v", name, err)
		}
		id := uuid.MustParse(created.ID)
		if !enabled {
			off := false
			if _, err := fx.svc.Update(fx.ctx, fx.projectID, id, &UpdateIntegrationRequest{Enabled: &off}); err != nil {
				t.Fatalf("Update(%s, enabled=false) error = %v", name, err)
			}
		}
		return id
	}

	parkedID := createOnCredential("parked", true)
	disabledID := createOnCredential("disabled", false)
	if _, err := fx.repo.MarkSourceNeedsReconnect(fx.ctx, fx.projectID, parkedID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}
	pub.resolved = nil

	src := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, parkedID)
	if _, err := fx.svc.replaceCredentials(fx.ctx, src, Credentials{"access_token": "ghp_new"}, map[string]any{}); err != nil {
		t.Fatalf("replaceCredentials() error = %v", err)
	}

	resolved := pub.resolvedSet()
	if !resolved[fx.projectID.String()+"/"+parkedID.String()] {
		t.Errorf("resolved = %v, want the reactivated source resolved", pub.resolved)
	}
	if resolved[fx.projectID.String()+"/"+disabledID.String()] {
		t.Errorf("resolved = %v, want the disabled sibling NOT resolved: it was never repaired", pub.resolved)
	}

	repaired := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, parkedID)
	if repaired.Status != SourceStatusActive {
		t.Errorf("parked source status = %q, want active", repaired.Status)
	}
	sibling := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, disabledID)
	if sibling.Status != SourceStatusDisabled {
		t.Errorf("disabled sibling status = %q, want it left alone", sibling.Status)
	}
}

// TestPark_WithNilPublisherStillParks is the optionality contract: the CLI, the
// dev MCP and every unit test build a Service without the notifications domain,
// and the park still has to work.
func TestPark_WithNilPublisherStillParks(t *testing.T) {
	fx := newNotifyFixture(t, "sourcesnotifynil", nil)
	sourceID := createParkedTestSource(t, fx.ctx, fx.repo, fx.projectID, &fx.interval)
	job := createParkedTestJob(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	src := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)

	if err := fx.worker.failJob(fx.ctx, job, src, fmt.Errorf("enumerate source: %w", errDriveGrantRejected)); err == nil {
		t.Fatal("failJob() = nil, want the recorded failure returned")
	}
	parked := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if parked.Status != SourceStatusNeedsReconnect {
		t.Fatalf("Status = %q, want %q with a nil publisher", parked.Status, SourceStatusNeedsReconnect)
	}
}

// emptyConnector enumerates nothing, so a sync over it succeeds with no items.
type emptyConnector struct{}

func (emptyConnector) Enumerate(context.Context, map[string]any) ([]SourceItem, error) {
	return nil, nil
}
func (emptyConnector) Fetch(context.Context, SourceItem) (string, string, error) {
	return "", "", nil
}
func (emptyConnector) TestConnection(context.Context, map[string]any) error { return nil }

// emptyProvider is a registered provider whose connector always succeeds.
type emptyProvider struct{ *stubProvider }

func (emptyProvider) NewConnector(context.Context, Credentials, ConnectorDeps) (Connector, error) {
	return emptyConnector{}, nil
}

// TestSuccessfulSync_ResolvesTheSourceItem covers the defensive resolve: a source
// that starts succeeding after an out-of-band credential fix must not keep an
// actionable item that no longer describes anything.
func TestSuccessfulSync_ResolvesTheSourceItem(t *testing.T) {
	pub := &recordingPublisher{}
	registry := NewRegistry()
	registry.Register(&emptyProvider{stubProvider: &stubProvider{catalog: Provider{
		Type: "github", Name: "GitHub", SourceType: "repo", Available: true,
	}}})
	fx := newNotifyFixtureWithRegistry(t, "sourcesnotifysyncok", pub, registry)

	interval := fx.interval
	blob, err := fx.svc.encryptCredentials(Credentials{"access_token": "ghp_x"})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	src, err := fx.repo.Create(fx.ctx, &Source{
		ID:                   uuid.New(),
		ProjectID:            fx.projectID,
		Provider:             "github",
		DisplayName:          "repo",
		Config:               map[string]any{"repo": "acme/app"},
		EncryptedCredentials: blob,
		SyncPolicy:           SyncPolicyScheduled,
		SyncIntervalMinutes:  &interval,
		Cursor:               map[string]any{},
		Status:               SourceStatusActive,
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Create(source) error = %v", err)
	}
	if _, err := fx.repo.MarkSourceNeedsReconnect(fx.ctx, fx.projectID, src.ID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}
	pub.resolved = nil

	job := createParkedTestJob(t, fx.ctx, fx.repo, fx.projectID, src.ID)
	if err := fx.worker.ProcessJob(fx.ctx, job); err != nil {
		t.Fatalf("ProcessJob() error = %v", err)
	}

	if !pub.resolvedSet()[fx.projectID.String()+"/"+src.ID.String()] {
		t.Fatalf("resolved = %v, want the syncing source's item resolved", pub.resolved)
	}
	completed, err := fx.repo.GetSyncJobByID(fx.ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSyncJobByID() error = %v", err)
	}
	if completed.Status != SyncStatusCompleted {
		t.Fatalf("job status = %q, want %q", completed.Status, SyncStatusCompleted)
	}
}
