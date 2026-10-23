package sources

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/emergent-company/emergent.memory/internal/gdrive"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/xberg"
)

// driveReconnectRequired is the remediation for a source whose stored refresh
// token Google no longer accepts. The grant is gone (the user revoked access, or
// the token expired), so no retry can recover it: only a fresh consent restores
// access. It is worded as a validation error rather than an internal one so the
// UI shows the action instead of a generic backend-outage page.
const driveReconnectRequired = "the Google Drive connection for this source has expired or was revoked; reconnect the source to continue"

// driveUnsavedGrantRequired is the same state for credentials the user has just
// entered and is still testing: there is no source to reconnect yet, so naming
// one would send them looking for a control that does not exist.
const driveUnsavedGrantRequired = "the Google Drive authorization was rejected; sign in again to grant Drive access"

// errDriveGrantRejected is the sentinel form of driveReconnectRequired. The
// connector reports it verbatim (TestConnection output is shown as-is), and the
// service matches it with errors.Is to pick the wording for the caller's context
// instead of comparing message strings.
var errDriveGrantRejected = errors.New(driveReconnectRequired)

// isDriveGrantRejected reports whether err is Google rejecting the stored
// refresh token with invalid_grant. Transient token-endpoint failures (5xx,
// timeouts, network errors) are deliberately not matched: those deserve a
// retry, not a reconnect.
func isDriveGrantRejected(err error) bool {
	var retrieveErr *oauth2.RetrieveError
	if !errors.As(err, &retrieveErr) {
		return false
	}
	if retrieveErr.ErrorCode == "invalid_grant" {
		return true
	}
	return strings.Contains(string(retrieveErr.Body), "invalid_grant")
}

// driveGrantOrRaw swaps a rejected grant for the reconnect sentinel and leaves
// every other error untouched: a Google 5xx or a timeout deserves a retry, not a
// reconnect prompt. The sync path reports errors verbatim in the job history and
// the source's error row, so it gets the bare message — no "validation:" prefix.
func driveGrantOrRaw(err error) error {
	if isDriveGrantRejected(err) {
		return errDriveGrantRejected
	}
	return err
}

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
	ListFolders(ctx context.Context, parentID, nameQuery string) ([]gdrive.Item, bool, error)
	GetFolder(ctx context.Context, folderID string) (gdrive.Folder, error)
	AccessToken(ctx context.Context) (string, time.Time, error)
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

// driveMaxFolderTargets bounds the number of folders one Drive source may sync.
// It keeps a single sync job to a human-manageable scope and bounds the
// picker-save validation calls (one Drive files.get per folder).
const driveMaxFolderTargets = 50

// ConfigSchema is the non-secret config contract for Drive. The OAuth2
// credential keys (client_id/client_secret/refresh_token) are deliberately
// absent: Drive connect is OAuth-only and its client credentials are configured
// per project/org, never per integration. Legacy sources whose stored config
// still carries those keys keep syncing (see CredentialKeys).
//
// The canonical sync scope is `folders`: a bounded list of
// {folder_id, name, drive_id} targets chosen in the Google Picker. The legacy
// single-target keys (folder_id/shared_drive_id) are still accepted on write and
// normalized into `folders`; they are intentionally absent from the schema so
// the forms never render a raw id input.
func (p *googleDriveProvider) ConfigSchema() ProviderSchema {
	return ProviderSchema{
		Type: ProviderTypeGoogleDrive,
		Properties: map[string]any{
			"folders": map[string]any{
				"type":        "array",
				"description": "Folders to sync. Pick them in the folder picker; each entry is {folder_id, name, drive_id}.",
				"x-ui-widget": "folders",
				"maxItems":    driveMaxFolderTargets,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"folder_id": map[string]any{"type": "string"},
						"name":      map[string]any{"type": "string"},
						"drive_id":  map[string]any{"type": "string"},
					},
					"required": []string{"folder_id"},
				},
			},
			"mime_types": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional MIME-type allowlist; empty syncs all non-folder types. Applies to every selected folder.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Recurse into subfolders when true. Applies to every selected folder.",
			},
		},
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

// StrategyForScopes names the Drive strategy that produced these scopes, or ""
// for explicit scopes matching neither advertised strategy.
func (p *googleDriveProvider) StrategyForScopes(scopes []string) string {
	if len(scopes) != 1 {
		return ""
	}
	switch scopes[0] {
	case googleDriveFile:
		return string(ScopeStrategyDriveFile)
	case googleDriveReadonly:
		return string(ScopeStrategyDriveReadonly)
	}
	return ""
}

// CredentialKeys returns the write-only Drive credential keys (client id/secret
// + refresh token, plus access_token defensively).
func (p *googleDriveProvider) CredentialKeys() []string { return credentialKeyNames }

// ListResources lists the Drive folders a user may pick as a sync target.
// query (a non-empty name filter) is threaded into the Drive listing.
func (p *googleDriveProvider) ListResources(ctx context.Context, creds Credentials, cfg map[string]any, query string) ([]Resource, bool, error) {
	client, err := p.newClient(ctx, driveCredentials(creds))
	if err != nil {
		return nil, false, err
	}
	items, truncated, err := client.ListFolders(ctx, "", query)
	if err != nil {
		return nil, false, err
	}
	out := make([]Resource, len(items))
	for i, it := range items {
		out[i] = Resource{ID: it.ID, Name: it.Name, Type: "folder"}
	}
	return out, truncated, nil
}

func (p *googleDriveProvider) ValidateConfig(cfg map[string]any) error {
	return validateDriveConfig(cfg)
}

// NormalizeConfig rewrites the legacy single-target shape
// ({folder_id, shared_drive_id}) into the canonical `folders` list. It is
// idempotent and never invents a name: a legacy target keeps an empty name and
// the UI falls back to the folder id until the next picker save resolves it.
func (p *googleDriveProvider) NormalizeConfig(cfg map[string]any) {
	if cfg == nil {
		return
	}
	if folders, ok := parseFoldersValue(cfg["folders"]); ok && len(folders) > 0 {
		cfg["folders"] = foldersValue(folders)
		delete(cfg, "folder_id")
		delete(cfg, "shared_drive_id")
		return
	}
	folderID, _ := cfg["folder_id"].(string)
	if strings.TrimSpace(folderID) == "" {
		return
	}
	driveID, _ := cfg["shared_drive_id"].(string)
	cfg["folders"] = foldersValue([]driveFolderTarget{{
		FolderID: strings.TrimSpace(folderID),
		DriveID:  strings.TrimSpace(driveID),
	}})
	delete(cfg, "folder_id")
	delete(cfg, "shared_drive_id")
}

// ResolveResourceTargets validates picked folder ids against the source's own
// credentials and returns each folder's authoritative name and shared-drive id.
// The picker only sends ids, so names and drive ids cannot be spoofed by the
// browser, and a shared-drive folder is queryable by the sync afterwards.
func (p *googleDriveProvider) ResolveResourceTargets(ctx context.Context, creds Credentials, ids []string) ([]ResourceRef, error) {
	if len(ids) > driveMaxFolderTargets {
		return nil, apperror.NewValidation(fmt.Sprintf("select at most %d folders", driveMaxFolderTargets))
	}
	client, err := p.newClient(ctx, driveCredentials(creds))
	if err != nil {
		return nil, err
	}
	out := make([]ResourceRef, 0, len(ids))
	for _, id := range ids {
		folder, err := client.GetFolder(ctx, id)
		if err != nil {
			if isDriveGrantRejected(err) {
				return nil, apperror.NewValidation(driveReconnectRequired)
			}
			return nil, apperror.NewValidation("one of the selected folders is not accessible: folder " + id + ": " + err.Error())
		}
		out = append(out, ResourceRef{ID: folder.ID, Name: folder.Name, DriveID: folder.DriveID})
	}
	return out, nil
}

// PickerSession mints a short-lived Google Picker session bound to the source's
// own refresh token. The picker therefore browses exactly the account the source
// syncs, with the source's read scope.
func (p *googleDriveProvider) PickerSession(ctx context.Context, creds Credentials, _ map[string]any, picker PickerConfig) (*PickerSession, error) {
	if picker.Strategy == string(ScopeStrategyDriveFile) {
		return nil, apperror.NewValidation("this source was authorized with per-file access (drive.file), so the folder picker cannot browse its folders; reconnect the source with read-only Drive access")
	}
	client, err := p.newClient(ctx, driveCredentials(creds))
	if err != nil {
		return nil, err
	}
	token, expires, err := client.AccessToken(ctx)
	if err != nil {
		if isDriveGrantRejected(err) {
			return nil, apperror.NewValidation(driveReconnectRequired)
		}
		return nil, apperror.NewInternal("mint folder-picker access token", err)
	}
	return &PickerSession{
		AccessToken:  token,
		DeveloperKey: picker.DeveloperKey,
		AppID:        picker.AppID,
		Origin:       picker.Origin,
		ExpiresAt:    expires,
	}, nil
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
	folders, mimeTypes, recursive, err := parseDriveSyncConfig(cfg)
	if err != nil {
		return nil, err
	}
	// Union the targets, de-duplicating by Drive item id: overlapping picks
	// (a folder and one of its parents) would otherwise ingest the same file
	// twice in one run.
	seen := make(map[string]struct{})
	var out []SourceItem
	for _, folder := range folders {
		items, err := c.client.ListFolderItems(ctx, folder.FolderID, mimeTypes, recursive, folder.DriveID)
		if err != nil {
			return nil, driveGrantOrRaw(err)
		}
		for _, it := range items {
			if _, ok := seen[it.ID]; ok {
				continue
			}
			seen[it.ID] = struct{}{}
			out = append(out, SourceItem{
				ID:           it.ID,
				Name:         it.Name,
				MimeType:     it.MimeType,
				RevisionID:   it.RevisionID,
				Trashed:      it.Trashed,
				ModifiedTime: it.ModifiedTime,
			})
		}
	}
	return out, nil
}

func (c *googleDriveConnector) Fetch(ctx context.Context, item SourceItem) (content, mimeType string, err error) {
	if isGoogleNative(item.MimeType) {
		data, err := c.client.ExportText(ctx, item.ID, item.MimeType)
		if err != nil {
			return "", "", driveGrantOrRaw(err)
		}
		return string(data), googleNativeExportMime(item.MimeType), nil
	}
	data, err := c.client.DownloadFile(ctx, item.ID)
	if err != nil {
		return "", "", driveGrantOrRaw(err)
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
	err := c.client.TestConnection(ctx)
	if isDriveGrantRejected(err) {
		// TestConnection reports this string verbatim, so return the sentinel:
		// no "validation:" prefix, and the service can still rewrite the wording
		// for credentials that are not saved on a source yet.
		return errDriveGrantRejected
	}
	return err
}

// validateDriveConfig enforces the google_drive config contract: at least one
// folder target (canonical `folders`, or the legacy folder_id) and
// mime_types (when present) an array of non-empty strings.
func validateDriveConfig(cfg map[string]any) error {
	if _, _, _, err := parseDriveSyncConfig(cfg); err != nil {
		return err
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

// driveFolderTarget is one folder a Drive source syncs.
type driveFolderTarget struct {
	FolderID string
	Name     string
	DriveID  string
}

// foldersValue renders targets in the JSON shape stored in config
// (`[]map[string]any`, not the internal struct).
func foldersValue(folders []driveFolderTarget) []any {
	out := make([]any, 0, len(folders))
	for _, f := range folders {
		entry := map[string]any{"folder_id": f.FolderID}
		if name := strings.TrimSpace(f.Name); name != "" {
			entry["name"] = name
		}
		if driveID := strings.TrimSpace(f.DriveID); driveID != "" {
			entry["drive_id"] = driveID
		}
		out = append(out, entry)
	}
	return out
}

// parseFoldersValue decodes a config `folders` value in any of the shapes the
// API, the database, or a test may supply. Entries with an empty folder_id are
// rejected so a partially-written list cannot reach enumeration.
func parseFoldersValue(raw any) ([]driveFolderTarget, bool) {
	if raw == nil {
		return nil, false
	}
	appendEntry := func(target driveFolderTarget, out *[]driveFolderTarget) bool {
		target.FolderID = strings.TrimSpace(target.FolderID)
		if target.FolderID == "" {
			return false
		}
		target.Name = strings.TrimSpace(target.Name)
		target.DriveID = strings.TrimSpace(target.DriveID)
		*out = append(*out, target)
		return true
	}
	fromMap := func(m map[string]any) (driveFolderTarget, bool) {
		id, _ := m["folder_id"].(string)
		name, _ := m["name"].(string)
		drive, _ := m["drive_id"].(string)
		return driveFolderTarget{FolderID: id, Name: name, DriveID: drive}, true
	}

	var out []driveFolderTarget
	seen := make(map[string]struct{})
	appendUnique := func(t driveFolderTarget) bool {
		if !appendEntry(t, &out) {
			return false
		}
		id := out[len(out)-1].FolderID
		if _, dup := seen[id]; dup {
			out = out[:len(out)-1]
			return true
		}
		seen[id] = struct{}{}
		return true
	}
	switch v := raw.(type) {
	case []driveFolderTarget:
		for _, t := range v {
			if !appendUnique(t) {
				return nil, false
			}
		}
		return out, true
	case []map[string]any:
		for _, m := range v {
			t, _ := fromMap(m)
			if !appendUnique(t) {
				return nil, false
			}
		}
		return out, true
	case []any:
		for _, item := range v {
			switch m := item.(type) {
			case map[string]any:
				t, _ := fromMap(m)
				if !appendUnique(t) {
					return nil, false
				}
			case driveFolderTarget:
				if !appendUnique(m) {
					return nil, false
				}
			default:
				return nil, false
			}
		}
		return out, true
	default:
		return nil, false
	}
}

// parseDriveSyncConfig extracts the enumeration scope from a Drive source
// config: the folder targets (canonical `folders`, or the legacy single-target
// folder_id/shared_drive_id), the global MIME allowlist, and recursion.
func parseDriveSyncConfig(cfg map[string]any) (folders []driveFolderTarget, mimeTypes []string, recursive bool, err error) {
	if raw, ok := cfg["folders"]; ok && raw != nil {
		parsed, ok := parseFoldersValue(raw)
		if !ok {
			return nil, nil, false, apperror.NewValidation("config.folders must be an array of {folder_id, name, drive_id} objects")
		}
		folders = parsed
	}
	if len(folders) == 0 {
		// Legacy single-target shape.
		folderID, _ := cfg["folder_id"].(string)
		if strings.TrimSpace(folderID) == "" {
			return nil, nil, false, apperror.NewValidation("config.folders must name at least one folder")
		}
		sharedDriveID, _ := cfg["shared_drive_id"].(string)
		folders = []driveFolderTarget{{
			FolderID: strings.TrimSpace(folderID),
			DriveID:  strings.TrimSpace(sharedDriveID),
		}}
	}
	if len(folders) > driveMaxFolderTargets {
		return nil, nil, false, apperror.NewValidation(fmt.Sprintf("config.folders supports at most %d folders", driveMaxFolderTargets))
	}
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
	return folders, mimeTypes, recursive, nil
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
