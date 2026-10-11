// Package gdrive implements a Google Drive v3 transport client used by the
// Drive data-source connector. It wraps google.golang.org/api/drive/v3 and
// golang.org/x/oauth2 and is constructed so tests can inject an HTTP client
// (option.WithHTTPClient) and a test endpoint (option.WithEndpoint) — no live
// Google calls are made in tests.
package gdrive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	// DefaultMaxBytes is the default per-item size cap (25 MiB).
	DefaultMaxBytes int64 = 25 << 20
	// DefaultRequestTimeout is the default per-call request timeout.
	DefaultRequestTimeout = 60 * time.Second

	// folderMimeType identifies a Drive folder item.
	folderMimeType = "application/vnd.google-apps.folder"

	// driveReadonlyScope is the OAuth2 scope requested for the client when the
	// stored credential carries no scope (legacy pasted-credential sources).
	driveReadonlyScope = "https://www.googleapis.com/auth/drive.readonly"
)

// requestedScope resolves the OAuth2 scope for a credential: the stored scope
// (the org-selected strategy at connect time) when present, else the legacy
// drive.readonly default.
func requestedScope(scope string) string {
	if strings.TrimSpace(scope) != "" {
		return scope
	}
	return driveReadonlyScope
}

// Credentials holds the OAuth2 client credentials and refresh token for a
// Drive source. It is never stored in plaintext (see EncryptCredentials).
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	// Scope is the OAuth2 scope the refresh token was authorized for (the
	// org-selected strategy at connect time). Empty falls back to
	// driveReadonlyScope for legacy pasted-credential sources.
	Scope string `json:"scope,omitempty"`
}

// Item is a single in-scope Drive item after enumeration.
type Item struct {
	ID           string
	Name         string
	MimeType     string
	RevisionID   string
	Trashed      bool
	ModifiedTime time.Time
}

// Config controls client behaviour. Zero values fall back to DefaultConfig.
type Config struct {
	// MaxBytes is the per-item size cap for export/download. Default 25 MiB.
	MaxBytes int64
	// RequestTimeout is the per-call timeout. Default 60s.
	RequestTimeout time.Duration
}

// DefaultConfig returns the sane defaults used when Config fields are unset.
func DefaultConfig() Config {
	return Config{
		MaxBytes:       DefaultMaxBytes,
		RequestTimeout: DefaultRequestTimeout,
	}
}

// Client is a Google Drive v3 transport client.
type Client struct {
	svc *drive.Service
	cfg Config
}

// SizeError indicates a Drive response exceeded the configured MaxBytes cap.
type SizeError struct {
	Limit int64
}

func (e *SizeError) Error() string {
	return fmt.Sprintf("google drive: content exceeds %d-byte size limit", e.Limit)
}

// New builds a Client from OAuth2 refresh-token credentials. It constructs an
// oauth2 token source and a *drive.Service. Additional opts (e.g.
// option.WithEndpoint) are appended after the credential-backed HTTP client.
func New(ctx context.Context, creds Credentials, opts ...option.ClientOption) (*Client, error) {
	if creds.RefreshToken == "" {
		return nil, errors.New("google drive: refresh token is required")
	}

	oauthCfg := oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{requestedScope(creds.Scope)},
	}
	tokenSource := oauthCfg.TokenSource(ctx, &oauth2.Token{RefreshToken: creds.RefreshToken})
	hc := oauth2.NewClient(ctx, tokenSource)

	return newClient(ctx, hc, opts...)
}

// NewWithHTTPClient builds a Client from an already-configured *http.Client
// (no credentials). Intended for tests: pass option.WithEndpoint(server.URL)
// to point the client at a mock Drive endpoint.
func NewWithHTTPClient(hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	if hc == nil {
		return nil, errors.New("google drive: http client is required")
	}
	return newClient(context.Background(), hc, opts...)
}

func newClient(ctx context.Context, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	opts = append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)
	svc, err := drive.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("google drive: create service: %w", err)
	}
	return &Client{svc: svc, cfg: DefaultConfig()}, nil
}

// WithConfig sets the client configuration, applying defaults for zero-valued
// fields. It returns the client for chaining.
func (c *Client) WithConfig(cfg Config) *Client {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	c.cfg = cfg
	return c
}

// ListFolderItems enumerates the items directly or (when recursive) anywhere
// beneath folderID, filtered to the given MIME-type allowlist (empty = all
// non-folder types) and excluding trashed items. Results are paginated
// breadth-first.
func (c *Client) ListFolderItems(ctx context.Context, folderID string, mimeTypes []string, recursive bool, sharedDriveID string) ([]Item, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	var out []Item
	queue := []string{folderID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		files, err := c.listChildren(ctx, current, mimeTypes, recursive, sharedDriveID)
		if err != nil {
			return nil, fmt.Errorf("google drive: list folder %q: %w", current, err)
		}

		for _, f := range files {
			if f.MimeType == folderMimeType {
				if recursive {
					queue = append(queue, f.Id)
				}
				continue
			}
			out = append(out, toItem(f))
		}
	}

	return out, nil
}

// ListFolders lists Drive folders (optionally under parentID — empty lists all
// accessible non-trashed folders) for the resource-selection step.
func (c *Client) ListFolders(ctx context.Context, parentID string) ([]Item, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	q := "mimeType = '" + folderMimeType + "' and trashed = false"
	if strings.TrimSpace(parentID) != "" {
		q = "'" + escapeDriveQueryValue(parentID) + "' in parents and " + q
	}

	var out []Item
	pageToken := ""
	for {
		call := c.svc.Files.List().
			Q(q).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Fields("nextPageToken,files(id,name,mimeType,headRevisionId,trashed,modifiedTime)")
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		fileList, err := call.Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("google drive: list folders: %w", err)
		}
		for _, f := range fileList.Files {
			out = append(out, toItem(f))
		}
		if fileList.NextPageToken == "" {
			break
		}
		pageToken = fileList.NextPageToken
	}
	return out, nil
}

// listChildren lists one page (all pages) of children of a single folder,
// returning both matching files and — when recursive — subfolders.
func (c *Client) listChildren(ctx context.Context, folderID string, mimeTypes []string, recursive bool, sharedDriveID string) ([]*drive.File, error) {
	q := fmt.Sprintf("'%s' in parents and trashed = false", escapeDriveQueryValue(folderID))

	var clauses []string
	for _, t := range mimeTypes {
		clauses = append(clauses, fmt.Sprintf("mimeType = '%s'", escapeDriveQueryValue(t)))
	}
	// When a MIME filter is applied AND recursion is enabled, also include
	// folders so subdirectories can be discovered. With no MIME filter, the
	// unfiltered query already returns folders.
	if recursive && len(mimeTypes) > 0 {
		clauses = append(clauses, "mimeType = '"+folderMimeType+"'")
	}
	if len(clauses) > 0 {
		q += " and (" + strings.Join(clauses, " or ") + ")"
	}

	var files []*drive.File
	pageToken := ""
	for {
		call := c.svc.Files.List().
			Q(q).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Fields("nextPageToken,files(id,name,mimeType,headRevisionId,trashed,modifiedTime)")
		if sharedDriveID != "" {
			call = call.DriveId(sharedDriveID)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		fileList, err := call.Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		if fileList.Files != nil {
			files = append(files, fileList.Files...)
		}
		if fileList.NextPageToken == "" {
			break
		}
		pageToken = fileList.NextPageToken
	}

	return files, nil
}

// GetRevision returns the item's head revision id (a cheap change-detection
// pre-filter) by selecting only headRevisionId/version/modifiedTime.
func (c *Client) GetRevision(ctx context.Context, fileID string) (string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	f, err := c.svc.Files.Get(fileID).
		SupportsAllDrives(true).
		Fields("headRevisionId,version,modifiedTime").
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("google drive: get file revision %q: %w", fileID, err)
	}
	return f.HeadRevisionId, nil
}

// exportMime maps Google-native MIME types to a text export target the
// ingestion pipeline can parse.
var exportMime = map[string]string{
	"application/vnd.google-apps.document":     "text/plain",
	"application/vnd.google-apps.spreadsheet":  "text/csv",
	"application/vnd.google-apps.presentation": "text/plain",
}

// ExportText exports a Google-native document to text, bounded by MaxBytes.
func (c *Client) ExportText(ctx context.Context, fileID, mimeType string) ([]byte, error) {
	target, ok := exportMime[mimeType]
	if !ok {
		return nil, fmt.Errorf("google drive: unsupported export MIME type %q", mimeType)
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	resp, err := c.svc.Files.Export(fileID, target).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("google drive: export %q: %w", fileID, err)
	}
	defer resp.Body.Close()

	return readBounded(resp.Body, c.cfg.MaxBytes)
}

// DownloadFile downloads a file's media bytes, bounded by MaxBytes.
func (c *Client) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	resp, err := c.svc.Files.Get(fileID).SupportsAllDrives(true).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("google drive: download %q: %w", fileID, err)
	}
	defer resp.Body.Close()

	return readBounded(resp.Body, c.cfg.MaxBytes)
}

// TestConnection performs a cheap, bounded call (about.get) and returns a
// descriptive error when the connection or credentials are invalid.
func (c *Client) TestConnection(ctx context.Context) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if _, err := c.svc.About.Get().Fields("user").Context(ctx).Do(); err != nil {
		return fmt.Errorf("google drive: connection test failed: %w", err)
	}
	return nil
}

// withTimeout derives a per-call context bounded by the configured timeout.
func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.cfg.RequestTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.cfg.RequestTimeout)
}

// readBounded reads at most maxBytes bytes from r, returning a SizeError when
// the content exceeds the cap.
func readBounded(r io.Reader, maxBytes int64) ([]byte, error) {
	lr := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, &SizeError{Limit: maxBytes}
	}
	return data, nil
}

// escapeDriveQueryValue escapes a value for a Drive files.list `q` clause,
// which uses single quotes and backslash escaping.
func escapeDriveQueryValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return v
}

// toItem converts a *drive.File into a package-level Item, parsing the
// RFC3339 ModifiedTime string.
func toItem(f *drive.File) Item {
	var modTime time.Time
	if f.ModifiedTime != "" {
		if t, err := time.Parse(time.RFC3339, f.ModifiedTime); err == nil {
			modTime = t
		}
	}
	return Item{
		ID:           f.Id,
		Name:         f.Name,
		MimeType:     f.MimeType,
		RevisionID:   f.HeadRevisionId,
		Trashed:      f.Trashed,
		ModifiedTime: modTime,
	}
}
