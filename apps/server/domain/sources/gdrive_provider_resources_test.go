package sources

import (
	"context"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/gdrive"
)

// fakeDriveTransport satisfies DriveTransport for provider-level tests.
type fakeDriveTransport struct {
	folders []gdrive.Item
}

func (f *fakeDriveTransport) ListFolderItems(context.Context, string, []string, bool, string) ([]gdrive.Item, error) {
	return nil, nil
}
func (f *fakeDriveTransport) ListFolders(context.Context, string, string) ([]gdrive.Item, bool, error) {
	return f.folders, false, nil
}
func (f *fakeDriveTransport) ExportText(context.Context, string, string) ([]byte, error) {
	return nil, nil
}
func (f *fakeDriveTransport) DownloadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakeDriveTransport) TestConnection(context.Context) error                 { return nil }

func TestGoogleDriveProvider_ListResources(t *testing.T) {
	p := NewGoogleDriveProvider(func(context.Context, gdrive.Credentials) (DriveTransport, error) {
		return &fakeDriveTransport{folders: []gdrive.Item{{ID: "f1", Name: "Docs"}, {ID: "f2", Name: "Archive"}}}, nil
	})
	resources, truncated, err := p.ListResources(context.Background(), Credentials{"refresh_token": "rt"}, nil, "")
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if truncated {
		t.Fatalf("ListResources() truncated = true, want false")
	}
	if len(resources) != 2 || resources[0].ID != "f1" || resources[0].Name != "Docs" || resources[0].Type != "folder" {
		t.Fatalf("ListResources() = %+v, want two folders", resources)
	}
}

func TestDriveCredentials_CarriesScope(t *testing.T) {
	creds := driveCredentials(Credentials{
		"client_id": "cid", "client_secret": "cs", "refresh_token": "rt", "scope": googleDriveReadonly,
	})
	if creds.Scope != googleDriveReadonly {
		t.Fatalf("driveCredentials().Scope = %q, want %q", creds.Scope, googleDriveReadonly)
	}
	if creds.ClientID != "cid" || creds.ClientSecret != "cs" || creds.RefreshToken != "rt" {
		t.Fatalf("driveCredentials() = %+v", creds)
	}
}
