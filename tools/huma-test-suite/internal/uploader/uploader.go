// Package uploader implements the upload phase of the huma-test-suite.
// It reads cached files from the local cache directory and uploads them
// to an Emergent project using the SDK, with a bounded worker pool and
// exponential backoff retry on 429 responses.
package uploader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/documents"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
	"github.com/emergent-company/emergent/tools/huma-test-suite/internal/config"
)

// UploadRecord captures the result of a single document upload.
type UploadRecord struct {
	LocalPath   string
	Filename    string
	DocumentID  string
	UploadedAt  time.Time
	IsDuplicate bool
	Err         error
}

// Stats summarises the outcome of the upload phase.
type Stats struct {
	Total      int
	Successful int
	Duplicates int
	Failed     int
	Records    []UploadRecord
}

// Uploader orchestrates parallel file uploads to an Emergent project.
type Uploader struct {
	client *sdk.Client
	cfg    *config.Config
}

// New creates a new Uploader initialised with the Emergent SDK client.
func New(cfg *config.Config) (*Uploader, error) {
	if cfg.EmergentAPIKey == "" {
		return nil, fmt.Errorf("EMERGENT_API_KEY is required for upload phase")
	}
	if cfg.EmergentProjectID == "" {
		return nil, fmt.Errorf("EMERGENT_PROJECT_ID is required for upload phase")
	}

	client, err := sdk.New(sdk.Config{
		ServerURL: cfg.EmergentServerURL,
		Auth: sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: cfg.EmergentAPIKey,
		},
		OrgID:     cfg.EmergentOrgID,
		ProjectID: cfg.EmergentProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialise Emergent SDK: %w", err)
	}

	return &Uploader{client: client, cfg: cfg}, nil
}

// Upload runs the upload phase: discovers cached files, uploads them with
// a worker pool, and returns aggregated stats.
func (u *Uploader) Upload(ctx context.Context) (*Stats, error) {
	files, err := discoverFiles(u.cfg.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list cache directory: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files found in cache directory %s — run --phase download first", u.cfg.CacheDir)
	}

	jobs := make(chan string, len(files))
	for _, f := range files {
		jobs <- f
	}
	close(jobs)

	var (
		mu      sync.Mutex
		records []UploadRecord
	)

	workers := u.cfg.Concurrency
	if workers <= 0 {
		workers = 3
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				rec := u.uploadFile(ctx, path)
				mu.Lock()
				records = append(records, rec)
				mu.Unlock()

				if rec.Err != nil {
					fmt.Printf("  ✗ %-40s  ERROR: %v\n", rec.Filename, rec.Err)
				} else if rec.IsDuplicate {
					fmt.Printf("  ~ %-40s  duplicate (id=%s)\n", rec.Filename, rec.DocumentID)
				} else {
					fmt.Printf("  ✓ %-40s  id=%s\n", rec.Filename, rec.DocumentID)
				}
			}
		}()
	}

	wg.Wait()

	stats := &Stats{Total: len(records), Records: records}
	for _, r := range records {
		switch {
		case r.Err != nil:
			stats.Failed++
		case r.IsDuplicate:
			stats.Duplicates++
		default:
			stats.Successful++
		}
	}

	return stats, nil
}

// uploadFile uploads a single file with exponential backoff on rate-limit errors.
// Text files (.md, .txt) are uploaded as inline content to avoid server-side
// deduplication by empty content hash (which happens when text extraction has
// not yet run at upload time). Binary files (.pdf, .docx, etc.) use multipart upload.
func (u *Uploader) uploadFile(ctx context.Context, path string) UploadRecord {
	filename := filepath.Base(path)
	rec := UploadRecord{LocalPath: path, Filename: filename}

	ext := strings.ToLower(filepath.Ext(filename))
	isTextFile := ext == ".md" || ext == ".txt"

	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				rec.Err = ctx.Err()
				return rec
			case <-time.After(backoff):
			}
		}

		var (
			docID       string
			isDuplicate bool
			err         error
		)

		if isTextFile {
			docID, isDuplicate, err = u.uploadTextFile(ctx, path, filename)
		} else {
			docID, isDuplicate, err = u.uploadBinaryFile(ctx, path, filename)
		}

		if err != nil {
			if isRateLimit(err) && attempt < maxAttempts-1 {
				continue
			}
			rec.Err = err
			return rec
		}

		rec.DocumentID = docID
		rec.IsDuplicate = isDuplicate
		rec.UploadedAt = time.Now()
		return rec
	}

	rec.Err = fmt.Errorf("exceeded max retry attempts")
	return rec
}

// uploadTextFile uploads a text file using the inline-content Create endpoint,
// which computes a proper content hash and avoids empty-hash deduplication.
// After creation, it triggers an extraction job via the admin endpoint.
func (u *Uploader) uploadTextFile(ctx context.Context, path, filename string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read: %w", err)
	}

	doc, err := u.client.Documents.Create(ctx, &documents.CreateRequest{
		Filename: filename,
		Content:  string(data),
	})
	if err != nil {
		return "", false, err
	}

	// Trigger extraction job via admin API
	_ = u.triggerExtraction(ctx, doc.ID)

	return doc.ID, false, nil
}

// triggerExtraction fires an extraction job for a document via the admin endpoint.
func (u *Uploader) triggerExtraction(ctx context.Context, docID string) error {
	body := fmt.Sprintf(
		`{"project_id":%q,"source_type":"document","source_id":%q,"extraction_config":{}}`,
		u.cfg.EmergentProjectID, docID,
	)
	req, err := newJSONRequest(ctx, "POST",
		u.cfg.EmergentServerURL+"/api/admin/extraction-jobs",
		u.cfg.EmergentAPIKey,
		u.cfg.EmergentProjectID,
		body,
	)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// uploadBinaryFile uploads a binary file using multipart upload with autoExtract=true.
func (u *Uploader) uploadBinaryFile(ctx context.Context, path, filename string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	result, err := u.client.Documents.UploadWithOptions(ctx, &documents.UploadFileInput{
		Filename: filename,
		Reader:   f,
	}, true /* autoExtract */)
	if err != nil {
		return "", false, err
	}

	docID := ""
	if result.Document != nil {
		docID = result.Document.ID
	} else if result.ExistingDocumentID != nil {
		docID = *result.ExistingDocumentID
	}
	return docID, result.IsDuplicate, nil
}

// discoverFiles returns all uploadable files from the cache directory,
// skipping the .meta subdirectory and hidden files.
func discoverFiles(cacheDir string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		files = append(files, filepath.Join(cacheDir, name))
	}
	return files, nil
}

// isRateLimit returns true if the error indicates an HTTP 429 response.
func isRateLimit(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *sdkerrors.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429
	}
	return strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate")
}

// newJSONRequest constructs an authenticated JSON POST request.
func newJSONRequest(ctx context.Context, method, url, apiKey, projectID, body string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	return req, nil
}
