package sources

import (
	"context"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/gdrive"
)

// fakeDriveTransport satisfies DriveTransport for provider-level tests.
type fakeDriveTransport struct {
	folders        []gdrive.Item
	itemsByFolder  map[string][]gdrive.Item
	listCalls      []string
	getFolders     map[string]gdrive.Folder
	getErr         error
	getErrByID     map[string]error
	listItemsErr   error
	listFoldersErr error
	exportErr      error
	downloadErr    error
	token          string
	tokenErr       error
	testConnErr    error
}

func (f *fakeDriveTransport) ListFolderItems(_ context.Context, folderID string, _ []string, _ bool, _ string) ([]gdrive.Item, error) {
	f.listCalls = append(f.listCalls, folderID)
	if f.listItemsErr != nil {
		return nil, f.listItemsErr
	}
	return f.itemsByFolder[folderID], nil
}
func (f *fakeDriveTransport) ListFolders(context.Context, string, string) ([]gdrive.Item, bool, error) {
	if f.listFoldersErr != nil {
		return nil, false, f.listFoldersErr
	}
	return f.folders, false, nil
}
func (f *fakeDriveTransport) GetFolder(_ context.Context, id string) (gdrive.Folder, error) {
	if err, ok := f.getErrByID[id]; ok {
		return gdrive.Folder{}, err
	}
	if f.getErr != nil {
		return gdrive.Folder{}, f.getErr
	}
	if folder, ok := f.getFolders[id]; ok {
		return folder, nil
	}
	return gdrive.Folder{ID: id}, nil
}
func (f *fakeDriveTransport) AccessToken(context.Context) (string, time.Time, error) {
	if f.tokenErr != nil {
		return "", time.Time{}, f.tokenErr
	}
	token := f.token
	if token == "" {
		token = "at"
	}
	return token, time.Now().Add(time.Hour), nil
}
func (f *fakeDriveTransport) ExportText(context.Context, string, string) ([]byte, error) {
	return nil, f.exportErr
}
func (f *fakeDriveTransport) DownloadFile(context.Context, string) ([]byte, error) {
	return nil, f.downloadErr
}
func (f *fakeDriveTransport) TestConnection(context.Context) error { return f.testConnErr }

func TestGoogleDriveProvider_ListResources(t *testing.T) {
	p := NewGoogleDriveProvider(func(context.Context, gdrive.Credentials) (DriveTransport, error) {
		return &fakeDriveTransport{folders: []gdrive.Item{{ID: "f1", Name: "Docs"}, {ID: "f2", Name: "Archive"}}}, nil
	})
	resources, truncated, err := p.ListResources(context.Background(), Credentials{"refresh_token": "rt"}, nil, "", ConnectorDeps{})
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
