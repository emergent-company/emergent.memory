// Package drive provides Google Drive synchronization for the huma-test-suite.
// It authenticates using Application Default Credentials (ADC) by default,
// or a service account key file if GOOGLE_SERVICE_ACCOUNT_JSON is set.
// It lists files from a configured folder and downloads them to a local cache
// directory — skipping files that already exist and haven't changed.
package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/emergent-company/emergent/tools/huma-test-suite/internal/config"
)

// driveFields are the file fields fetched from the Drive API.
const driveFields = "id, name, mimeType, modifiedTime, size, parents"

// Google Workspace MIME types that require export instead of direct download.
var exportMIME = map[string]string{
	"application/vnd.google-apps.document":     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"application/vnd.google-apps.spreadsheet":  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.google-apps.presentation": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// exportExtension maps a Google Workspace export MIME type to a file extension.
var exportExtension = map[string]string{
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   ".docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
}

// SyncStats reports the results of a sync operation.
type SyncStats struct {
	Total       int
	Downloaded  int
	Skipped     int
	Failed      int
	FailedFiles []FailedFile
}

// FailedFile records a file that could not be downloaded.
type FailedFile struct {
	Name string
	Err  string
}

// Syncer manages Drive file synchronisation to a local cache.
type Syncer struct {
	svc      *drive.Service
	cfg      *config.Config
	cacheDir string
}

// NewSyncer creates a new Drive Syncer.
// Authentication priority:
//  1. If cfg.GoogleServiceAccountJSON is set, use that service account key file.
//  2. Otherwise, fall back to Application Default Credentials (ADC) —
//     works with `gcloud auth application-default login` or Workload Identity.
func NewSyncer(ctx context.Context, cfg *config.Config) (*Syncer, error) {
	var opts []option.ClientOption

	if cfg.GoogleServiceAccountJSON != "" {
		data, err := os.ReadFile(cfg.GoogleServiceAccountJSON)
		if err != nil {
			return nil, fmt.Errorf("reading service account file %q: %w", cfg.GoogleServiceAccountJSON, err)
		}
		creds, err := google.CredentialsFromJSON(ctx, data, drive.DriveReadonlyScope)
		if err != nil {
			return nil, fmt.Errorf("parsing service account credentials: %w", err)
		}
		opts = append(opts, option.WithCredentials(creds))
		fmt.Println("Auth: service account key file")
	} else {
		// Use ADC — picks up gcloud user credentials or Workload Identity automatically
		opts = append(opts, option.WithScopes(drive.DriveReadonlyScope))
		fmt.Println("Auth: Application Default Credentials (ADC)")
	}

	svc, err := drive.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating Drive service: %w", err)
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir %q: %w", cfg.CacheDir, err)
	}

	// Store metadata about synced files alongside downloads
	metaDir := filepath.Join(cfg.CacheDir, ".meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating meta dir: %w", err)
	}

	return &Syncer{
		svc:      svc,
		cfg:      cfg,
		cacheDir: cfg.CacheDir,
	}, nil
}

// Sync lists all files in the configured Drive folder and downloads any that
// are new or have been modified since the last sync.
// Task 2.2: List files recursively from Google Drive folder.
// Task 2.3: Download with caching (skip unchanged files).
func (s *Syncer) Sync(ctx context.Context) (*SyncStats, error) {
	fmt.Printf("Listing files in folder %s...\n", s.cfg.FolderID)

	files, err := s.listFiles(ctx, s.cfg.FolderID)
	if err != nil {
		return nil, fmt.Errorf("listing files: %w", err)
	}

	stats := &SyncStats{Total: len(files)}
	fmt.Printf("Found %d file(s). Starting download...\n\n", len(files))

	for i, f := range files {
		fmt.Printf("[%d/%d] %s", i+1, len(files), f.Name)

		localPath, skip, err := s.downloadFile(ctx, f)
		if err != nil {
			fmt.Printf(" — FAILED: %v\n", err)
			stats.Failed++
			stats.FailedFiles = append(stats.FailedFiles, FailedFile{Name: f.Name, Err: err.Error()})
			continue
		}
		if skip {
			fmt.Printf(" — skipped (cached)\n")
			stats.Skipped++
			continue
		}

		fmt.Printf(" — downloaded to %s\n", localPath)
		stats.Downloaded++
	}

	return stats, nil
}

// listFiles recursively lists all non-folder files under the given folder ID.
// Task 2.2 implementation.
func (s *Syncer) listFiles(ctx context.Context, folderID string) ([]*drive.File, error) {
	var result []*drive.File
	var pageToken string

	for {
		query := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
		req := s.svc.Files.List().
			Context(ctx).
			Q(query).
			Fields("nextPageToken, files(" + driveFields + ")").
			PageSize(200).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true)

		if pageToken != "" {
			req = req.PageToken(pageToken)
		}

		var resp *drive.FileList
		err := withRetry(ctx, func() error {
			var e error
			resp, e = req.Do()
			return e
		})
		if err != nil {
			return nil, fmt.Errorf("listing page: %w", err)
		}

		for _, f := range resp.Files {
			if f.MimeType == "application/vnd.google-apps.folder" {
				// Recurse into subfolders
				sub, err := s.listFiles(ctx, f.Id)
				if err != nil {
					return nil, fmt.Errorf("listing subfolder %q: %w", f.Name, err)
				}
				result = append(result, sub...)
			} else {
				result = append(result, f)
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return result, nil
}

// fileMeta records metadata for cached files to detect changes.
type fileMeta struct {
	DriveID      string    `json:"driveId"`
	ModifiedTime time.Time `json:"modifiedTime"`
	LocalPath    string    `json:"localPath"`
}

// downloadFile downloads a single Drive file to the local cache.
// Returns (localPath, skipped, error). skipped is true if the cached copy is current.
// Task 2.3 implementation.
func (s *Syncer) downloadFile(ctx context.Context, f *drive.File) (string, bool, error) {
	// Determine local file name (handle Google Workspace exports)
	localName := sanitizeName(f.Name)
	exportMime := ""

	if mime, ok := exportMIME[f.MimeType]; ok {
		exportMime = mime
		if ext, ok := exportExtension[mime]; ok {
			if !strings.HasSuffix(localName, ext) {
				localName += ext
			}
		}
	}

	localPath := filepath.Join(s.cacheDir, localName)
	metaPath := filepath.Join(s.cacheDir, ".meta", localName+".json")

	// Parse modifiedTime from Drive
	modTime, _ := time.Parse(time.RFC3339, f.ModifiedTime)

	// Check if we already have a current cached copy
	if existing, err := loadMeta(metaPath); err == nil {
		if existing.DriveID == f.Id && !existing.ModifiedTime.Before(modTime) {
			if _, statErr := os.Stat(localPath); statErr == nil {
				return localPath, true, nil // skip — already cached and up to date
			}
		}
	}

	// Download the file content with retry
	var body io.ReadCloser
	err := withRetry(ctx, func() error {
		var e error
		if exportMime != "" {
			// Google Workspace file — export to Office format
			resp, e2 := s.svc.Files.Export(f.Id, exportMime).Context(ctx).Download()
			if e2 != nil {
				return e2
			}
			body = resp.Body
			return nil
		}
		// Binary file — direct download
		resp, e2 := s.svc.Files.Get(f.Id).SupportsAllDrives(true).Context(ctx).Download()
		if e2 != nil {
			return e2
		}
		body = resp.Body
		e = nil
		return e
	})
	if err != nil {
		return "", false, fmt.Errorf("downloading: %w", err)
	}
	defer body.Close()

	// Write to local cache
	out, err := os.Create(localPath)
	if err != nil {
		return "", false, fmt.Errorf("creating local file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, body); err != nil {
		return "", false, fmt.Errorf("writing file content: %w", err)
	}

	// Save metadata for future sync runs
	meta := fileMeta{
		DriveID:      f.Id,
		ModifiedTime: modTime,
		LocalPath:    localPath,
	}
	if err := saveMeta(metaPath, meta); err != nil {
		// Non-fatal: just means we'll re-download next time
		fmt.Printf("  (warning: could not save metadata: %v)\n", err)
	}

	return localPath, false, nil
}

// withRetry executes fn with exponential backoff on transient errors.
// Task 2.4: Add retry and exponential backoff for Google Drive API calls.
func withRetry(ctx context.Context, fn func() error) error {
	const maxAttempts = 5
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		// Check if the context was cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Only retry on rate limit (429) or server errors (5xx)
		if !isRetryable(err) {
			return err
		}

		if attempt == maxAttempts {
			return fmt.Errorf("after %d attempts: %w", maxAttempts, err)
		}

		wait := time.Duration(float64(backoff) * math.Pow(2, float64(attempt-1)))
		fmt.Printf("  (rate limited, retrying in %s...)\n", wait.Round(time.Millisecond))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil
}

// isRetryable returns true if the Drive API error warrants a retry.
func isRetryable(err error) bool {
	if apiErr, ok := err.(*googleapi.Error); ok {
		switch apiErr.Code {
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}

// sanitizeName converts a Drive file name into a safe local filename.
func sanitizeName(name string) string {
	// Replace path separators that could break local paths
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}

// loadMeta reads cached file metadata from disk.
func loadMeta(path string) (*fileMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m fileMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// saveMeta writes file metadata to disk.
func saveMeta(path string, m fileMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
