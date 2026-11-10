package sources

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// probeProvider is a stub provider with a programmable cheap-auth answer. It
// implements GrantProber so the probe path can be driven without a network.
type probeProvider struct {
	*stubProvider

	mu      sync.Mutex
	calls   int
	err     error
	onProbe func()
}

func (p *probeProvider) ProbeGrant(context.Context, Credentials, ConnectorDeps) error {
	p.mu.Lock()
	p.calls++
	err := p.err
	onProbe := p.onProbe
	p.mu.Unlock()
	if onProbe != nil {
		onProbe()
	}
	return err
}

func (p *probeProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// plainProvider has no GrantProber: a provider that cannot answer cheaply must
// be skipped rather than made to look probed.
type plainProvider struct{ *stubProvider }

func newProbeRegistry(p SourceProvider) *Registry {
	reg := NewRegistry()
	reg.Register(p)
	return reg
}

func probeStub(err error) *probeProvider {
	return &probeProvider{
		stubProvider: &stubProvider{catalog: Provider{
			Type: ProviderTypeGoogleDrive, Name: "Google Drive", SourceType: SourceTypeDrive, Available: true,
		}},
		err: err,
	}
}

// createProbeSource inserts a source with a real encrypted credential blob, so
// the probe goes through the same decrypt path production uses.
func createProbeSource(t *testing.T, fx notifyFixture, policy string, interval *int, status string, lastProbed *time.Time) uuid.UUID {
	t.Helper()
	blob, err := fx.svc.encryptCredentials(Credentials{"access_token": "ya29.probe"})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	now := time.Now().UTC()
	src, err := fx.repo.Create(fx.ctx, &Source{
		ID:                   uuid.New(),
		ProjectID:            fx.projectID,
		Provider:             ProviderTypeGoogleDrive,
		DisplayName:          "Drive",
		Config:               map[string]any{"folders": []any{"folder-1"}},
		EncryptedCredentials: blob,
		SyncPolicy:           policy,
		SyncIntervalMinutes:  interval,
		Cursor:               map[string]any{},
		Status:               status,
		LastProbedAt:         lastProbed,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		t.Fatalf("Create(source) error = %v", err)
	}
	return src.ID
}

// TestShouldProbe_CadenceDecision pins the policy that decides when a source is
// eligible and due: only an active, scheduled, long-interval source whose last
// probe is at least a cadence old.
func TestShouldProbe_CadenceDecision(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	long := 24 * 60
	short := 60
	manual := 0
	probedRecently := now.Add(-30 * time.Minute)
	probedLongAgo := now.Add(-2 * time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name string
		src  *Source
		want bool
	}{
		{
			name: "never probed, long interval",
			src:  &Source{Status: SourceStatusActive, SyncPolicy: SyncPolicyScheduled, SyncIntervalMinutes: &long},
			want: true,
		},
		{
			name: "probed a cadence ago",
			src:  &Source{Status: SourceStatusActive, SyncPolicy: SyncPolicyScheduled, SyncIntervalMinutes: &long, LastProbedAt: &probedLongAgo},
			want: true,
		},
		{
			name: "probed recently",
			src:  &Source{Status: SourceStatusActive, SyncPolicy: SyncPolicyScheduled, SyncIntervalMinutes: &long, LastProbedAt: &probedRecently},
			want: false,
		},
		{
			name: "clock skew: probe stamped in the future",
			src:  &Source{Status: SourceStatusActive, SyncPolicy: SyncPolicyScheduled, SyncIntervalMinutes: &long, LastProbedAt: &future},
			want: false,
		},
		{
			name: "short interval: the sync is its own probe",
			src:  &Source{Status: SourceStatusActive, SyncPolicy: SyncPolicyScheduled, SyncIntervalMinutes: &short},
			want: false,
		},
		{
			name: "manual source is never scheduled",
			src:  &Source{Status: SourceStatusActive, SyncPolicy: SyncPolicyManual, SyncIntervalMinutes: &manual},
			want: false,
		},
		{
			name: "no interval",
			src:  &Source{Status: SourceStatusActive, SyncPolicy: SyncPolicyScheduled},
			want: false,
		},
		{
			name: "parked source is not re-probed",
			src:  &Source{Status: SourceStatusNeedsReconnect, SyncPolicy: SyncPolicyScheduled, SyncIntervalMinutes: &long},
			want: false,
		},
		{
			name: "disabled source is not probed",
			src:  &Source{Status: SourceStatusDisabled, SyncPolicy: SyncPolicyScheduled, SyncIntervalMinutes: &long},
			want: false,
		},
		{name: "nil source", src: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldProbe(now, tc.src, probeCadence, probeIntervalThreshold); got != tc.want {
				t.Errorf("shouldProbe() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestProbeDueSources_ParksADeadGrantAndPublishes is the end-to-end probe: a
// long-interval source whose provider rejects the grant is parked and leaves one
// grouped actionable item naming the source and the remediation. A second sweep
// finds nothing to do.
func TestProbeDueSources_ParksADeadGrantAndPublishes(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	pub := &recordingPublisher{}
	prober := probeStub(fmt.Errorf("google drive: %w", errDriveGrantRejected))
	fx := newNotifyFixtureWithRegistry(t, "sourcesprobepark", pub, newProbeRegistry(prober))

	interval := 24 * 60
	sourceID := createProbeSource(t, fx, SyncPolicyScheduled, &interval, SourceStatusActive, nil)

	if err := fx.svc.ProbeDueSources(fx.ctx); err != nil {
		t.Fatalf("ProbeDueSources() error = %v", err)
	}

	if got := prober.callCount(); got != 1 {
		t.Fatalf("ProbeGrant calls = %d, want 1", got)
	}
	parked := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if parked.Status != SourceStatusNeedsReconnect {
		t.Fatalf("Status = %q, want %q", parked.Status, SourceStatusNeedsReconnect)
	}
	if parked.LastProbedAt == nil {
		t.Fatal("LastProbedAt = nil, want the probe recorded")
	}
	if parked.LastError == nil || !IsReconnectRequired(errors.New(*parked.LastError)) {
		t.Fatalf("LastError = %v, want the reconnect remediation", parked.LastError)
	}
	if len(pub.notices) != 1 {
		t.Fatalf("notices = %d, want exactly 1", len(pub.notices))
	}
	notice := pub.notices[0]
	if notice.SourceID != sourceID.String() {
		t.Errorf("notice.SourceID = %q, want %q", notice.SourceID, sourceID)
	}
	if notice.GroupKey != NotificationGroupKey(sourceID.String()) {
		t.Errorf("notice.GroupKey = %q, want one item per source", notice.GroupKey)
	}
	if notice.ActionURL != "/data-sources/"+sourceID.String() {
		t.Errorf("notice.ActionURL = %q, want the source's page", notice.ActionURL)
	}
	if !IsReconnectRequired(errors.New(notice.Message)) {
		t.Errorf("notice.Message = %q, want the reconnect remediation", notice.Message)
	}

	// A parked source is no longer a candidate, and the probe clock suppresses a
	// repeat even for an active source.
	if err := fx.svc.ProbeDueSources(fx.ctx); err != nil {
		t.Fatalf("second ProbeDueSources() error = %v", err)
	}
	if got := prober.callCount(); got != 1 {
		t.Errorf("ProbeGrant calls after the second sweep = %d, want 1", got)
	}
	if len(pub.notices) != 1 {
		t.Errorf("notices after the second sweep = %d, want 1", len(pub.notices))
	}
}

// TestProbeDueSources_TransientFailureKeepsTheSourceActive pins the line between
// "the grant is dead" and "the provider is having a bad day": a transient probe
// failure is logged, does not park, does not create an error row or a
// notification, and the probe clock still advances so the sweep does not hammer
// the provider.
func TestProbeDueSources_TransientFailureKeepsTheSourceActive(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	pub := &recordingPublisher{}
	prober := probeStub(errors.New("google drive: 503 Service Unavailable"))
	fx := newNotifyFixtureWithRegistry(t, "sourcesprobetransient", pub, newProbeRegistry(prober))

	interval := 24 * 60
	sourceID := createProbeSource(t, fx, SyncPolicyScheduled, &interval, SourceStatusActive, nil)

	if err := fx.svc.ProbeDueSources(fx.ctx); err != nil {
		t.Fatalf("ProbeDueSources() error = %v", err)
	}

	src := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if src.Status != SourceStatusActive {
		t.Errorf("Status = %q, want active", src.Status)
	}
	if src.LastError != nil {
		t.Errorf("LastError = %v, want nil: a probe failure is not a sync failure", *src.LastError)
	}
	if src.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0", src.ErrorCount)
	}
	if src.LastProbedAt == nil {
		t.Error("LastProbedAt = nil, want the probe attempt recorded")
	}
	if len(pub.notices) != 0 {
		t.Errorf("notices = %d, want none", len(pub.notices))
	}

	if err := fx.svc.ProbeDueSources(fx.ctx); err != nil {
		t.Fatalf("second ProbeDueSources() error = %v", err)
	}
	if got := prober.callCount(); got != 1 {
		t.Errorf("ProbeGrant calls = %d, want 1: the cadence suppresses a repeat", got)
	}
}

// TestProbeDueSources_SkipsIneligibleSources covers the query/decision seam: a
// short interval, a manual policy, a parked source and a recently probed source
// are all left alone, while the one eligible source is probed.
func TestProbeDueSources_SkipsIneligibleSources(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	pub := &recordingPublisher{}
	prober := probeStub(nil)
	fx := newNotifyFixtureWithRegistry(t, "sourcesprobeeligible", pub, newProbeRegistry(prober))

	long := 24 * 60
	short := 60
	manual := 0
	recent := time.Now().UTC().Add(-30 * time.Minute)

	createProbeSource(t, fx, SyncPolicyScheduled, &short, SourceStatusActive, nil)
	createProbeSource(t, fx, SyncPolicyManual, &manual, SourceStatusActive, nil)
	createProbeSource(t, fx, SyncPolicyScheduled, &long, SourceStatusNeedsReconnect, nil)
	createProbeSource(t, fx, SyncPolicyScheduled, &long, SourceStatusActive, &recent)
	eligibleID := createProbeSource(t, fx, SyncPolicyScheduled, &long, SourceStatusActive, nil)

	if err := fx.svc.ProbeDueSources(fx.ctx); err != nil {
		t.Fatalf("ProbeDueSources() error = %v", err)
	}
	if got := prober.callCount(); got != 1 {
		t.Fatalf("ProbeGrant calls = %d, want 1 (only the eligible source)", got)
	}
	probed := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, eligibleID)
	if probed.LastProbedAt == nil {
		t.Error("the eligible source was not marked probed")
	}
	if len(pub.notices) != 0 {
		t.Errorf("notices = %d, want none", len(pub.notices))
	}
}

// TestProbeDueSources_SkipsAProviderWithoutCheapProbe pins the optional
// capability: a provider that does not implement GrantProber is never probed and
// never parked, and the source keeps its status.
func TestProbeDueSources_SkipsAProviderWithoutCheapProbe(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	pub := &recordingPublisher{}
	plain := &plainProvider{stubProvider: &stubProvider{catalog: Provider{
		Type: ProviderTypeGoogleDrive, Name: "Google Drive", SourceType: SourceTypeDrive, Available: true,
	}}}
	fx := newNotifyFixtureWithRegistry(t, "sourcesprobeplain", pub, newProbeRegistry(plain))

	interval := 24 * 60
	sourceID := createProbeSource(t, fx, SyncPolicyScheduled, &interval, SourceStatusActive, nil)

	if err := fx.svc.ProbeDueSources(fx.ctx); err != nil {
		t.Fatalf("ProbeDueSources() error = %v", err)
	}
	src := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if src.Status != SourceStatusActive {
		t.Errorf("Status = %q, want active", src.Status)
	}
	if len(pub.notices) != 0 {
		t.Errorf("notices = %d, want none", len(pub.notices))
	}
}

// TestProbeDueSources_LeavesASourceWithAnActiveJobAlone covers the race rule: a
// source whose sync is in flight is about to answer the same question, so the
// probe does not spend a provider call on it.
func TestProbeDueSources_LeavesASourceWithAnActiveJobAlone(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	pub := &recordingPublisher{}
	prober := probeStub(nil)
	fx := newNotifyFixtureWithRegistry(t, "sourcesprobeactivejob", pub, newProbeRegistry(prober))

	interval := 24 * 60
	sourceID := createProbeSource(t, fx, SyncPolicyScheduled, &interval, SourceStatusActive, nil)
	createParkedTestJob(t, fx.ctx, fx.repo, fx.projectID, sourceID)

	if err := fx.svc.ProbeDueSources(fx.ctx); err != nil {
		t.Fatalf("ProbeDueSources() error = %v", err)
	}
	if got := prober.callCount(); got != 0 {
		t.Errorf("ProbeGrant calls = %d, want 0 while a sync job is active", got)
	}
}

// TestProbeDueSources_RefusedParkWhenTheGrantWasReplaced covers the epoch guard
// on the probe path (#2425): a reconnect that lands while the provider call is
// in flight wins, so the probe's stale verdict must not park the source nor
// raise a notification for a grant nobody uses any more.
func TestProbeDueSources_RefusedParkWhenTheGrantWasReplaced(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	pub := &recordingPublisher{}
	prober := probeStub(fmt.Errorf("google drive: %w", errDriveGrantRejected))
	fx := newNotifyFixtureWithRegistry(t, "sourcesprobepoch", pub, newProbeRegistry(prober))

	interval := 24 * 60
	sourceID := createProbeSource(t, fx, SyncPolicyScheduled, &interval, SourceStatusActive, nil)
	prober.onProbe = func() {
		if err := fx.repo.ReplaceOAuthGrant(fx.ctx, sourceID, fx.projectID, []byte("sealed-new-grant"), map[string]any{}); err != nil {
			t.Errorf("ReplaceOAuthGrant() error = %v", err)
		}
	}

	if err := fx.svc.ProbeDueSources(fx.ctx); err != nil {
		t.Fatalf("ProbeDueSources() error = %v", err)
	}

	src := mustGetSource(t, fx.ctx, fx.repo, fx.projectID, sourceID)
	if src.Status != SourceStatusActive {
		t.Errorf("Status = %q, want active: the grant was replaced mid-probe", src.Status)
	}
	if len(pub.notices) != 0 {
		t.Errorf("notices = %d, want none for a stale verdict", len(pub.notices))
	}
}
