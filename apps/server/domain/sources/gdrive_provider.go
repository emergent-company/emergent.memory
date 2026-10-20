package sources

import (
	"context"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/gdrive"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/xberg"
)

// credentialKeyNames are the config keys that carry provider credential
// material. They are write-only: stripped from the persisted config and never
// returned. access_token is included defensively — the OAuth callback only
// copies CredentialKeys() into the config, and Drive's connect schema has no
// access-token field, so it is unreachable today, but the strip-on-read /
// strip-on-write defence must still treat it as credential material if a legacy
// row or API caller ever stored one.
var credentialKeyNames = []string{"client_id", "client_secret", "refresh_token", "access_token"}

// DriveTransport is the Drive transport surface the connector needs. It is the
// subset of *gdrive.Client used for enumeration, content fetch, connection
// testing, and resource listing; a fake satisfies it in tests.
type DriveTransport interface {
	ListFolderItems(ctx context.Context, folderID string, mimeTypes []string, recursive bool, sharedDriveID string) ([]gdrive.Item, error)
	ListFolders(ctx context.Context, parentID string) ([]gdrive.Item, error)
	ExportText(ctx context.Context, fileID, mimeType string) ([]byte, error)
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
	TestConnection(ctx context.Context) error
}

// googleDriveProvider implements SourceProvider for the google_drive provider.
type googleDriveProvider struct {
	newClient func(ctx context.Context, creds gdrive.Credentials) (DriveTransport, error)
}

func newGoogleDriveProvider() *googleDriveProvider {
	return &googleDriveProvider{
		newClient: func(ctx context.Context, creds gdrive.Credentials) (DriveTransport, error) {
			return gdrive.New(ctx, creds)
		},
	}
}

// NewGoogleDriveProvider builds the Google Drive provider with an injectable
// transport constructor. It exists so tests can substitute a fake transport and
// exercise the real connector logic. Production wiring uses the default
// (gdrive.New) via the registry.
func NewGoogleDriveProvider(newClient func(ctx context.Context, creds gdrive.Credentials) (DriveTransport, error)) SourceProvider {
	return &googleDriveProvider{newClient: newClient}
}

func (p *googleDriveProvider) Catalog() Provider {
	return Provider{
		Type:        ProviderTypeGoogleDrive,
		Name:        "Google Drive",
		Description: "Sync documents from a Google Drive folder into your knowledge base.",
		SourceType:  SourceTypeDrive,
		Vendor:      "google",
		VendorName:  "Google",
		Available:   true,
	}
}

// ConfigSchema is the non-secret config contract for Drive. The OAuth2
// credential keys (client_id/client_secret/refresh_token) are deliberately
// absent: Drive connect is OAuth-only and its client credentials are configured
// per project/org, never per integration. Legacy sources whose stored config
// still carries those keys keep syncing (see CredentialKeys).
func (p *googleDriveProvider) ConfigSchema() ProviderSchema {
	return ProviderSchema{
		Type: ProviderTypeGoogleDrive,
		Properties: map[string]any{
			"folder_id": map[string]any{
				"type":        "string",
				"description": "The Google Drive folder id to sync from.",
			},
			"shared_drive_id": map[string]any{
				"type":        "string",
				"description": "Optional shared-drive id when the folder lives in a shared drive.",
			},
			"mime_types": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional MIME-type allowlist; empty syncs all non-folder types.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Recurse into subfolders when true.",
			},
		},
		Required: []string{"folder_id"},
	}
}

// CredentialFields is intentionally empty: Drive connect is OAuth-only
// (add-datasource-oauth-app-setup). The connect form must never render a
// pasted-credential surface for Drive. Legacy sources that were connected by
// pasting credentials before this change still sync: their already-encrypted
// refresh_token blobs are read untouched by the connector/refresh path.
func (p *googleDriveProvider) CredentialFields() []ProviderField {
	return nil
}

func (p *googleDriveProvider) AuthMethods() []AuthMethod {
	return []AuthMethod{AuthMethodOAuth}
}

func (p *googleDriveProvider) OAuth() (OAuthStrategy, bool) {
	return newGoogleOAuthStrategy(), true
}

// Strategies advertises the org-selectable Drive scope strategies (design D6).
func (p *googleDriveProvider) Strategies() []ScopeStrategy { return driveScopeStrategies }

// ScopesForStrategy maps an org-selected Drive strategy to its scope set.
func (p *googleDriveProvider) ScopesForStrategy(strategy string) ([]string, bool) {
	return newGoogleOAuthStrategy().ScopesForStrategy(strategy)
}

// CredentialKeys returns the write-only Drive credential keys (client id/secret
// + refresh token, plus access_token defensively).
func (p *googleDriveProvider) CredentialKeys() []string { return credentialKeyNames }

// ListResources lists the Drive folders a user may pick as a sync target.
func (p *googleDriveProvider) ListResources(ctx context.Context, creds Credentials, cfg map[string]any) ([]Resource, error) {
	client, err := p.newClient(ctx, driveCredentials(creds))
	if err != nil {
		return nil, err
	}
	items, err := client.ListFolders(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]Resource, len(items))
	for i, it := range items {
		out[i] = Resource{ID: it.ID, Name: it.Name, Type: "folder"}
	}
	return out, nil
}

func (p *googleDriveProvider) ValidateConfig(cfg map[string]any) error {
	return validateDriveConfig(cfg)
}

func (p *googleDriveProvider) ExtractCredentials(cfg map[string]any) (Credentials, error) {
	creds := Credentials{}
	for _, k := range credentialKeyNames {
		if v, ok := cfg[k]; ok {
			creds[k] = v
		}
	}
	if token, _ := creds["refresh_token"].(string); strings.TrimSpace(token) == "" {
		return nil, apperror.NewValidation("config.refresh_token is required")
	}
	return creds, nil
}

func (p *googleDriveProvider) NewConnector(ctx context.Context, creds Credentials, deps ConnectorDeps) (Connector, error) {
	client, err := p.newClient(ctx, driveCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &googleDriveConnector{client: client, deps: deps}, nil
}

// driveCredentials projects the opaque credential map onto the Drive client's
// typed credential struct. The stored scope (from the org-selected strategy at
// connect time) is carried through so the client refreshes with the same scope
// it was authorized for.
func driveCredentials(creds Credentials) gdrive.Credentials {
	c, _ := creds["client_id"].(string)
	s, _ := creds["client_secret"].(string)
	r, _ := creds["refresh_token"].(string)
	scope, _ := creds["scope"].(string)
	return gdrive.Credentials{ClientID: c, ClientSecret: s, RefreshToken: r, Scope: scope}
}

// googleDriveConnector implements Connector over a Drive transport.
type googleDriveConnector struct {
	client DriveTransport
	deps   ConnectorDeps
}

func (c *googleDriveConnector) Enumerate(ctx context.Context, cfg map[string]any) ([]SourceItem, error) {
	folderID, mimeTypes, recursive, sharedDriveID, err := parseDriveSyncConfig(cfg)
	if err != nil {
		return nil, err
	}
	items, err := c.client.ListFolderItems(ctx, folderID, mimeTypes, recursive, sharedDriveID)
	if err != nil {
		return nil, err
	}
	out := make([]SourceItem, len(items))
	for i, it := range items {
		out[i] = SourceItem{
			ID:           it.ID,
			Name:         it.Name,
			MimeType:     it.MimeType,
			RevisionID:   it.RevisionID,
			Trashed:      it.Trashed,
			ModifiedTime: it.ModifiedTime,
		}
	}
	return out, nil
}

func (c *googleDriveConnector) Fetch(ctx context.Context, item SourceItem) (content, mimeType string, err error) {
	if isGoogleNative(item.MimeType) {
		data, err := c.client.ExportText(ctx, item.ID, item.MimeType)
		if err != nil {
			return "", "", err
		}
		return string(data), googleNativeExportMime(item.MimeType), nil
	}
	data, err := c.client.DownloadFile(ctx, item.ID)
	if err != nil {
		return "", "", err
	}
	if c.deps.Extract != nil && xberg.ShouldUseXberg(item.MimeType, item.Name) {
		text, err := c.deps.Extract(ctx, data, item.Name, item.MimeType)
		if err != nil {
			return "", "", err
		}
		return text, item.MimeType, nil
	}
	return string(data), item.MimeType, nil
}

func (c *googleDriveConnector) TestConnection(ctx context.Context, _ map[string]any) error {
	return c.client.TestConnection(ctx)
}

// validateDriveConfig enforces the google_drive config contract: folder_id
// required, mime_types (when present) an array of non-empty strings.
func validateDriveConfig(cfg map[string]any) error {
	if folderID, _ := cfg["folder_id"].(string); strings.TrimSpace(folderID) == "" {
		return apperror.NewValidation("config.folder_id is required")
	}
	if mimeTypes, ok := cfg["mime_types"].([]any); ok {
		for _, m := range mimeTypes {
			if s, ok := m.(string); !ok || strings.TrimSpace(s) == "" {
				return apperror.NewValidation("config.mime_types must be an array of non-empty strings")
			}
		}
	} else if mimeTypes, ok := cfg["mime_types"].([]string); ok {
		for _, s := range mimeTypes {
			if strings.TrimSpace(s) == "" {
				return apperror.NewValidation("config.mime_types must be an array of non-empty strings")
			}
		}
	}
	return nil
}

// parseDriveSyncConfig extracts the enumeration scope from a Drive source config.
func parseDriveSyncConfig(cfg map[string]any) (folderID string, mimeTypes []string, recursive bool, sharedDriveID string, err error) {
	folderID, _ = cfg["folder_id"].(string)
	if strings.TrimSpace(folderID) == "" {
		return "", nil, false, "", apperror.NewValidation("config.folder_id is required")
	}
	sharedDriveID, _ = cfg["shared_drive_id"].(string)
	recursive, _ = cfg["recursive"].(bool)
	switch mt := cfg["mime_types"].(type) {
	case []string:
		mimeTypes = mt
	case []any:
		for _, v := range mt {
			if s, ok := v.(string); ok {
				mimeTypes = append(mimeTypes, s)
			}
		}
	}
	return folderID, mimeTypes, recursive, sharedDriveID, nil
}

// isGoogleNative reports whether a MIME type is a Google-native editable doc.
func isGoogleNative(mimeType string) bool {
	return strings.HasPrefix(mimeType, "application/vnd.google-apps.")
}

// googleNativeExportMime maps a Google-native MIME type to the text export
// target's MIME type (mirrors gdrive.exportMime).
func googleNativeExportMime(mimeType string) string {
	if mimeType == "application/vnd.google-apps.spreadsheet" {
		return "text/csv"
	}
	return "text/plain"
}
