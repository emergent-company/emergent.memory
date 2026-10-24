package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/googleapi"

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

// The picker runs in the browser's Google session, so a save can carry folder
// ids that the source's own account cannot see at all. When every picked id is
// in that state the cause is the account, not the folders, and the message must
// say so instead of naming the first id Google refused (#2162).
func TestDriveResolveResourceTargets_AllUnavailableIsAnAccountMismatch(t *testing.T) {
	f := &fakeDriveTransport{getErr: &googleapi.Error{Code: http.StatusNotFound, Message: "File not found"}}
	resolver := driveProviderWithTransport(f).(ResourceTargetResolver)

	_, err := resolver.ResolveResourceTargets(context.Background(), Credentials{"refresh_token": "rt"}, []string{"f1", "f2"})
	if err == nil {
		t.Fatal("ResolveResourceTargets(unavailable) = nil, want an error")
	}
	if !strings.Contains(err.Error(), driveFoldersWrongAccount) {
		t.Errorf("error = %q, want the account-mismatch explanation", err)
	}
	if strings.Contains(err.Error(), "folder f1") {
		t.Errorf("error = %q, must not name a picked folder when the account is the cause", err)
	}
}

// One bad id among resolvable ones is a bad pick, not a wrong account: keep the
// specific message so the user knows which folder to remove.
func TestDriveResolveResourceTargets_OneUnavailableKeepsTheSpecificMessage(t *testing.T) {
	f := &fakeDriveTransport{
		getFolders: map[string]gdrive.Folder{"f2": {ID: "f2", Name: "Contracts"}},
		getErrByID: map[string]error{"f1": &googleapi.Error{Code: http.StatusNotFound}},
	}
	resolver := driveProviderWithTransport(f).(ResourceTargetResolver)

	_, err := resolver.ResolveResourceTargets(context.Background(), Credentials{"refresh_token": "rt"}, []string{"f1", "f2"})
	if err == nil {
		t.Fatal("ResolveResourceTargets(one unavailable) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "folder f1") {
		t.Errorf("error = %q, want it to name the inaccessible folder", err)
	}
	if strings.Contains(err.Error(), driveFoldersWrongAccount) {
		t.Errorf("error = %q, must keep the specific message when some ids resolve", err)
	}
}

// A Google outage is not evidence of a wrong account: the copy must leave the
// user with a retry, not send them to re-pick folders that were fine.
func TestDriveResolveResourceTargets_TransientFailureKeepsTheSpecificMessage(t *testing.T) {
	f := &fakeDriveTransport{getErr: &googleapi.Error{Code: http.StatusServiceUnavailable}}
	resolver := driveProviderWithTransport(f).(ResourceTargetResolver)

	_, err := resolver.ResolveResourceTargets(context.Background(), Credentials{"refresh_token": "rt"}, []string{"f1"})
	if err == nil {
		t.Fatal("ResolveResourceTargets(503) = nil, want an error")
	}
	if strings.Contains(err.Error(), driveFoldersWrongAccount) {
		t.Errorf("error = %q, must not blame the account for a 5xx", err)
	}
}

// A 403 is not one thing: rate limits and scope problems must not be read as
// "you used the wrong account", while a permission reason on the file is exactly
// that shape (#2173).
func TestDriveFolderUnavailableClassifies403Reasons(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "404 is not found", err: &googleapi.Error{Code: http.StatusNotFound}, want: true},
		{name: "403 insufficientFilePermissions", err: &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "insufficientFilePermissions"}}}, want: true},
		{name: "403 forbidden", err: &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "forbidden"}}}, want: true},
		{name: "403 userRateLimitExceeded is transient", err: &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}}}, want: false},
		{name: "403 rateLimitExceeded is transient", err: &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "rateLimitExceeded"}}}, want: false},
		{name: "403 dailyLimitExceeded is transient", err: &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "dailyLimitExceeded"}}}, want: false},
		{name: "403 insufficientPermissions is a scope problem", err: &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "insufficientPermissions"}}}, want: false},
		{name: "403 without a reason is not a guess", err: &googleapi.Error{Code: http.StatusForbidden}, want: false},
		{name: "a mixed list takes the permission reason", err: &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}, {Reason: "insufficientFilePermissions"}}}, want: true},
		{name: "500 is not a permission", err: &googleapi.Error{Code: http.StatusInternalServerError}, want: false},
		{name: "a non-Google error is not matched", err: errors.New("dial tcp: timeout"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := driveFolderUnavailable(tc.err); got != tc.want {
				t.Errorf("driveFolderUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// The end-to-end consequence: every picked id failing on a rate limit keeps the
// retryable message instead of the account-mismatch copy.
func TestDriveResolveResourceTargets_RateLimitKeepsTheRetryableMessage(t *testing.T) {
	f := &fakeDriveTransport{getErr: &googleapi.Error{
		Code:    http.StatusForbidden,
		Errors:  []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}},
		Message: "User rate limit exceeded.",
	}}
	resolver := driveProviderWithTransport(f).(ResourceTargetResolver)

	_, err := resolver.ResolveResourceTargets(context.Background(), Credentials{"refresh_token": "rt"}, []string{"f1", "f2"})
	if err == nil {
		t.Fatal("ResolveResourceTargets(rate limited) = nil, want an error")
	}
	if strings.Contains(err.Error(), driveFoldersWrongAccount) {
		t.Errorf("error = %q, must not blame the account for a rate limit", err)
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error = %q, want Google's own message so the user knows to retry", err)
	}
}
