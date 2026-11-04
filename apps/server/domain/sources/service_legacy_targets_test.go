package sources

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// targetResolverStub is a source provider whose name resolution is scripted, so
// the legacy single-target enrichment can be tested without reaching Drive.
type targetResolverStub struct {
	stubProvider
	refs   []ResourceRef
	err    error
	calls  int
	lastID string
}

func (p *targetResolverStub) ExtractCredentials(map[string]any) (Credentials, error) {
	return Credentials{"refresh_token": "rt"}, nil
}

func (p *targetResolverStub) ResolveResourceTargets(_ context.Context, _ Credentials, ids []string) ([]ResourceRef, error) {
	p.calls++
	if len(ids) > 0 {
		p.lastID = ids[0]
	}
	return p.refs, p.err
}

func newLegacyTargetService(t *testing.T, tdb *testdb.TestDB, stub *targetResolverStub) *Service {
	t.Helper()
	reg := NewRegistry()
	reg.Register(stub)
	svc := newCarriedConfigTestService(t, tdb)
	svc.providers = reg
	return svc
}

// TestGet_ResolvesLegacyFolderName asserts a source that predates canonical
// `folders` targets still reports its folder's name, so the UI never renders a
// bare opaque id where a name is available.
func TestGet_ResolvesLegacyFolderName(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourceslegacytargets")
	t.Cleanup(tdb.Close)
	stub := &targetResolverStub{
		stubProvider: stubProvider{catalog: Provider{Type: ProviderTypeGoogleDrive, Name: "Google Drive", SourceType: SourceTypeDrive}},
		refs:         []ResourceRef{{ID: "folder-1", Name: "Reports", DriveID: "drive-9"}},
	}
	svc := newLegacyTargetService(t, tdb, stub)

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config:       map[string]any{"folder_id": "folder-1"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := svc.Get(ctx, projectID, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stub.calls != 1 || stub.lastID != "folder-1" {
		t.Fatalf("resolver calls = %d (last id %q), want a single lookup of folder-1", stub.calls, stub.lastID)
	}
	folders, ok := got.Config["folders"].([]any)
	if !ok || len(folders) != 1 {
		t.Fatalf("Config[folders] = %#v, want one resolved target", got.Config["folders"])
	}
	entry, ok := folders[0].(map[string]any)
	if !ok {
		t.Fatalf("folders[0] = %#v, want a map", folders[0])
	}
	if entry["folder_id"] != "folder-1" || entry["name"] != "Reports" || entry["drive_id"] != "drive-9" {
		t.Fatalf("folders[0] = %#v, want folder-1/Reports/drive-9", entry)
	}
	// The legacy key stays readable; the annotation is additive.
	if got.Config["folder_id"] != "folder-1" {
		t.Fatalf("Config[folder_id] = %#v, want the legacy id preserved", got.Config["folder_id"])
	}
	// The lookup only decorates the response: the stored source is unchanged.
	stored, err := svc.repo.GetByID(ctx, uuid.MustParse(created.ID), projectID)
	if err != nil {
		t.Fatalf("repo.GetByID() error = %v", err)
	}
	if _, ok := stored.Config["folders"]; ok {
		t.Fatalf("stored Config[folders] = %#v, want the read to stay annotation-only", stored.Config["folders"])
	}
}

// TestGet_KeepsLegacyConfigWhenResolutionFails asserts a Drive failure cannot
// break the detail read: the source comes back with its stored config.
func TestGet_KeepsLegacyConfigWhenResolutionFails(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourceslegacytargetserr")
	t.Cleanup(tdb.Close)
	stub := &targetResolverStub{
		stubProvider: stubProvider{catalog: Provider{Type: ProviderTypeGoogleDrive, Name: "Google Drive", SourceType: SourceTypeDrive}},
		err:          errors.New("drive unavailable"),
	}
	svc := newLegacyTargetService(t, tdb, stub)

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config:       map[string]any{"folder_id": "folder-1"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := svc.Get(ctx, projectID, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, ok := got.Config["folders"]; ok {
		t.Fatalf("Config[folders] = %#v, want the config untouched when resolution fails", got.Config["folders"])
	}
	if got.Config["folder_id"] != "folder-1" {
		t.Fatalf("Config[folder_id] = %#v, want folder-1", got.Config["folder_id"])
	}
}

// TestGet_SkipsResolutionForCanonicalTargets asserts the lookup is only for
// legacy single-target sources; a canonical list is already named.
func TestGet_SkipsResolutionForCanonicalTargets(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourceslegacytargetscanon")
	t.Cleanup(tdb.Close)
	stub := &targetResolverStub{
		stubProvider: stubProvider{catalog: Provider{Type: ProviderTypeGoogleDrive, Name: "Google Drive", SourceType: SourceTypeDrive}},
		refs:         []ResourceRef{{ID: "other", Name: "Other"}},
	}
	svc := newLegacyTargetService(t, tdb, stub)

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folders": []any{map[string]any{"folder_id": "folder-1", "name": "Reports"}},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := svc.Get(ctx, projectID, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stub.calls != 0 {
		t.Fatalf("resolver calls = %d, want none for canonical targets", stub.calls)
	}
	folders, _ := got.Config["folders"].([]any)
	if len(folders) != 1 {
		t.Fatalf("Config[folders] = %#v, want the canonical list", got.Config["folders"])
	}
}
