package sources

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/gdrive"
)

func driveProviderWithTransport(f *fakeDriveTransport) SourceProvider {
	return NewGoogleDriveProvider(func(context.Context, gdrive.Credentials) (DriveTransport, error) {
		return f, nil
	})
}

// TestDriveNormalizeConfig_LegacyToFolders asserts the write path converts the
// pre-picker single-target shape into the canonical folder list exactly once.
func TestDriveNormalizeConfig_LegacyToFolders(t *testing.T) {
	p := newGoogleDriveProvider()
	cfg := map[string]any{
		"folder_id":       "f1",
		"shared_drive_id": "d1",
		"mime_types":      []any{"application/pdf"},
	}
	p.NormalizeConfig(cfg)

	folders, ok := cfg["folders"].([]any)
	if !ok || len(folders) != 1 {
		t.Fatalf("cfg[folders] = %#v, want one entry", cfg["folders"])
	}
	entry, ok := folders[0].(map[string]any)
	if !ok {
		t.Fatalf("folder entry = %#v, want a map", folders[0])
	}
	if entry["folder_id"] != "f1" || entry["drive_id"] != "d1" {
		t.Errorf("legacy folder = %#v, want folder_id f1 and drive_id d1", entry)
	}
	if _, ok := cfg["folder_id"]; ok {
		t.Error("legacy folder_id must be removed after normalization")
	}
	if _, ok := cfg["shared_drive_id"]; ok {
		t.Error("legacy shared_drive_id must be removed after normalization")
	}
	if _, ok := cfg["mime_types"]; !ok {
		t.Error("normalization must preserve unrelated config")
	}

	before := fmt.Sprintf("%#v", cfg)
	p.NormalizeConfig(cfg)
	if after := fmt.Sprintf("%#v", cfg); after != before {
		t.Errorf("NormalizeConfig must be idempotent:\nbefore %s\nafter  %s", before, after)
	}
}

func TestDriveNormalizeConfig_NoTargetLeavesConfigAlone(t *testing.T) {
	p := newGoogleDriveProvider()
	cfg := map[string]any{"mime_types": []any{"application/pdf"}}
	p.NormalizeConfig(cfg)
	if _, ok := cfg["folders"]; ok {
		t.Errorf("cfg = %#v, want no folders key without a target", cfg)
	}
}

// TestDriveValidateConfig_Folders covers the canonical list and the legacy
// single-target fallback the contract still accepts on write.
func TestDriveValidateConfig_Folders(t *testing.T) {
	p := newGoogleDriveProvider()
	valid := map[string]map[string]any{
		"canonical list": {"folders": []any{
			map[string]any{"folder_id": "f1", "name": "Reports"},
			map[string]any{"folder_id": "f2", "drive_id": "d1"},
		}},
		"legacy folder_id": {"folder_id": "f1"},
		"mime types":       {"folders": []any{map[string]any{"folder_id": "f1"}}, "mime_types": []any{"application/pdf"}},
	}
	for name, cfg := range valid {
		t.Run(name, func(t *testing.T) {
			if err := p.ValidateConfig(cfg); err != nil {
				t.Fatalf("ValidateConfig() error = %v", err)
			}
		})
	}

	tooMany := make([]any, driveMaxFolderTargets+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{"folder_id": fmt.Sprintf("f%d", i)}
	}
	invalid := map[string]map[string]any{
		"no target":        {},
		"empty list":       {"folders": []any{}},
		"entry without id": {"folders": []any{map[string]any{"name": "Reports"}}},
		"not a list":       {"folders": "f1"},
		"too many":         {"folders": tooMany},
		"blank mime type":  {"folders": []any{map[string]any{"folder_id": "f1"}}, "mime_types": []any{""}},
		"non-string mime":  {"folders": []any{map[string]any{"folder_id": "f1"}}, "mime_types": []any{42}},
		"legacy blank id":  {"folder_id": "  "},
	}
	for name, cfg := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := p.ValidateConfig(cfg); err == nil {
				t.Fatalf("ValidateConfig(%#v) = nil, want error", cfg)
			}
		})
	}
}

// TestDriveEnumerate_MultipleFoldersDedupe asserts one sync run unions every
// selected folder and ingests an overlapping file once.
func TestDriveEnumerate_MultipleFoldersDedupe(t *testing.T) {
	f := &fakeDriveTransport{itemsByFolder: map[string][]gdrive.Item{
		"f1": {{ID: "a", Name: "a"}, {ID: "b", Name: "b"}},
		"f2": {{ID: "b", Name: "b"}, {ID: "c", Name: "c"}},
	}}
	p := driveProviderWithTransport(f)
	conn, err := p.NewConnector(context.Background(), Credentials{"refresh_token": "rt"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"folders": []any{
			map[string]any{"folder_id": "f1"},
			map[string]any{"folder_id": "f2"},
		},
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("Enumerate() = %+v, want 3 deduped items", items)
	}
	if got, want := f.listCalls, []string{"f1", "f2"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ListFolderItems calls = %v, want %v", got, want)
	}
}

// TestDriveEnumerate_DuplicateTargetsCollapsed asserts a repeated folder id is
// walked once, so the save/config path cannot multiply Drive calls.
func TestDriveEnumerate_DuplicateTargetsCollapsed(t *testing.T) {
	f := &fakeDriveTransport{itemsByFolder: map[string][]gdrive.Item{
		"f1": {{ID: "a"}},
	}}
	p := driveProviderWithTransport(f)
	conn, err := p.NewConnector(context.Background(), Credentials{"refresh_token": "rt"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	items, err := conn.Enumerate(context.Background(), map[string]any{
		"folders": []any{
			map[string]any{"folder_id": "f1"},
			map[string]any{"folder_id": "f1"},
		},
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Enumerate() = %+v, want one item", items)
	}
	if len(f.listCalls) != 1 || f.listCalls[0] != "f1" {
		t.Errorf("ListFolderItems calls = %v, want [f1]", f.listCalls)
	}
}

// TestDriveEnumerate_LegacyConfig keeps pre-picker sources syncing after the
// config contract changed.
func TestDriveEnumerate_LegacyConfig(t *testing.T) {
	f := &fakeDriveTransport{itemsByFolder: map[string][]gdrive.Item{
		"f1": {{ID: "a"}},
	}}
	p := driveProviderWithTransport(f)
	conn, err := p.NewConnector(context.Background(), Credentials{"refresh_token": "rt"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	items, err := conn.Enumerate(context.Background(), map[string]any{"folder_id": "f1"})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "a" {
		t.Fatalf("Enumerate() = %+v, want the legacy folder's one item", items)
	}
}

// TestDriveEnumerate_NoTargetRejected asserts an OAuth-created source with no
// folder yet fails admission with an actionable error.
func TestDriveEnumerate_NoTargetRejected(t *testing.T) {
	p := driveProviderWithTransport(&fakeDriveTransport{})
	conn, err := p.NewConnector(context.Background(), Credentials{"refresh_token": "rt"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	_, err = conn.Enumerate(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "folders") {
		t.Fatalf("Enumerate() error = %v, want a folders validation error", err)
	}
}

func TestDriveResolveResourceTargets(t *testing.T) {
	f := &fakeDriveTransport{getFolders: map[string]gdrive.Folder{
		"f1": {ID: "f1", Name: "Reports", DriveID: "d1"},
		"f2": {ID: "f2", Name: "Contracts"},
	}}
	p := driveProviderWithTransport(f)
	resolver, ok := p.(ResourceTargetResolver)
	if !ok {
		t.Fatal("google_drive provider must implement ResourceTargetResolver")
	}
	refs, err := resolver.ResolveResourceTargets(context.Background(), Credentials{"refresh_token": "rt"}, []string{"f1", "f2"})
	if err != nil {
		t.Fatalf("ResolveResourceTargets() error = %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want 2", refs)
	}
	if refs[0].ID != "f1" || refs[0].Name != "Reports" || refs[0].DriveID != "d1" {
		t.Errorf("first ref = %+v", refs[0])
	}
	if refs[1].ID != "f2" || refs[1].Name != "Contracts" || refs[1].DriveID != "" {
		t.Errorf("second ref = %+v", refs[1])
	}
}

func TestDriveResolveResourceTargets_Errors(t *testing.T) {
	p := driveProviderWithTransport(&fakeDriveTransport{getErr: errors.New("not found")})
	resolver := p.(ResourceTargetResolver)
	if _, err := resolver.ResolveResourceTargets(context.Background(), Credentials{"refresh_token": "rt"}, []string{"missing"}); err == nil {
		t.Fatal("ResolveResourceTargets(missing) = nil, want an accessibility error")
	}

	tooMany := make([]string, driveMaxFolderTargets+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("f%d", i)
	}
	if _, err := resolver.ResolveResourceTargets(context.Background(), Credentials{"refresh_token": "rt"}, tooMany); err == nil {
		t.Fatal("ResolveResourceTargets(too many) = nil, want a bound error")
	}
}

// TestDrivePickerSession asserts the picker token is minted from the source's
// own credentials and carries the configured picker keys.
func TestDrivePickerSession(t *testing.T) {
	f := &fakeDriveTransport{token: "picker-token"}
	p := driveProviderWithTransport(f)
	picker, ok := p.(ResourcePicker)
	if !ok {
		t.Fatal("google_drive provider must implement ResourcePicker")
	}
	session, err := picker.PickerSession(context.Background(), Credentials{"refresh_token": "rt"}, nil, PickerConfig{
		DeveloperKey: "dev-key",
		AppID:        "12345",
		Origin:       "https://app.example",
		Strategy:     string(ScopeStrategyDriveReadonly),
	})
	if err != nil {
		t.Fatalf("PickerSession() error = %v", err)
	}
	if session.AccessToken != "picker-token" {
		t.Errorf("AccessToken = %q, want the minted token", session.AccessToken)
	}
	if session.DeveloperKey != "dev-key" || session.AppID != "12345" || session.Origin != "https://app.example" {
		t.Errorf("session = %+v", session)
	}
	if !session.ExpiresAt.After(time.Now()) {
		t.Errorf("ExpiresAt = %v, want a future expiry", session.ExpiresAt)
	}
}

// TestDrivePickerSession_DriveFileRejected: drive.file only grants access to the
// folder object itself, not its children, so browsing is impossible. The error
// names the scope the user must reconnect with.
func TestDrivePickerSession_DriveFileRejected(t *testing.T) {
	f := &fakeDriveTransport{token: "must-not-be-minted"}
	p := driveProviderWithTransport(f)
	picker := p.(ResourcePicker)
	_, err := picker.PickerSession(context.Background(), Credentials{"refresh_token": "rt"}, nil, PickerConfig{
		DeveloperKey: "dev-key",
		AppID:        "12345",
		Strategy:     string(ScopeStrategyDriveFile),
	})
	if err == nil {
		t.Fatal("PickerSession(drive.file) = nil, want a validation error")
	}
	if !strings.Contains(err.Error(), "drive.file") || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("PickerSession(drive.file) error = %q, want the drive.file remediation", err)
	}
}
