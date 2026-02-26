package suite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/documents"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// FileInput describes a file to upload.
type FileInput struct {
	Path        string // filesystem path (used if Content is nil)
	Filename    string // display name / filename in the API
	ContentType string // empty = auto-detect from extension
	Content     []byte // optional in-memory content; if nil, Path is read
}

// UploadOptions controls the upload worker pool.
type UploadOptions struct {
	Concurrency int // worker pool size (default: 4)
	MaxRetries  int // max retry attempts on 429/503 (default: 5)
	AutoExtract bool // trigger extraction after upload
	ServerURL   string
	ProjectID   string
}

// UploadRecord captures the outcome of a single file upload.
type UploadRecord struct {
	Filename    string
	DocumentID  string
	Status      Status
	IsDuplicate bool
	Error       error
	Duration    time.Duration
}

// UploadFiles uploads a slice of files concurrently and returns per-file records.
// Uses exponential backoff on 429/503 errors.
func UploadFiles(ctx context.Context, client *sdk.Client, files []FileInput, opts UploadOptions) []UploadRecord {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 5
	}

	jobs := make(chan FileInput, len(files))
	for _, f := range files {
		jobs <- f
	}
	close(jobs)

	var (
		mu      sync.Mutex
		records []UploadRecord
	)

	var wg sync.WaitGroup
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				rec := uploadFile(ctx, client, f, opts)
				mu.Lock()
				records = append(records, rec)
				mu.Unlock()

				if rec.Error != nil {
					fmt.Printf("  ✗ %-50s  ERROR: %v\n", rec.Filename, rec.Error)
				} else if rec.IsDuplicate {
					fmt.Printf("  ~ %-50s  duplicate (id=%s)\n", rec.Filename, rec.DocumentID)
				} else {
					fmt.Printf("  ✓ %-50s  id=%s\n", rec.Filename, rec.DocumentID)
				}
			}
		}()
	}
	wg.Wait()

	return records
}

// uploadFile uploads a single file with exponential backoff retry.
func uploadFile(ctx context.Context, client *sdk.Client, f FileInput, opts UploadOptions) UploadRecord {
	start := time.Now()
	rec := UploadRecord{Filename: f.Filename}

	ext := strings.ToLower(filepath.Ext(f.Filename))
	isText := ext == ".md" || ext == ".txt"

	for attempt := 0; attempt < opts.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				rec.Status = StatusTimeout
				rec.Error = ctx.Err()
				rec.Duration = time.Since(start)
				return rec
			case <-time.After(backoff):
			}
		}

		var (
			docID       string
			isDuplicate bool
			err         error
		)

		if isText {
			docID, err = uploadTextFile(ctx, client, f, opts)
		} else {
			docID, isDuplicate, err = uploadBinaryFile(ctx, client, f)
		}

		if err != nil {
			if isRetryable(err) && attempt < opts.MaxRetries-1 {
				continue
			}
			rec.Status = StatusFailed
			rec.Error = err
			rec.Duration = time.Since(start)
			return rec
		}

		rec.DocumentID = docID
		rec.IsDuplicate = isDuplicate
		rec.Status = StatusPassed
		rec.Duration = time.Since(start)
		return rec
	}

	rec.Status = StatusFailed
	rec.Error = fmt.Errorf("exceeded max retry attempts")
	rec.Duration = time.Since(start)
	return rec
}

// uploadTextFile uploads a text file using the inline-content Create endpoint.
// After creation it triggers an extraction job via the admin endpoint.
func uploadTextFile(ctx context.Context, client *sdk.Client, f FileInput, opts UploadOptions) (string, error) {
	content, err := fileContent(f)
	if err != nil {
		return "", err
	}

	doc, err := client.Documents.Create(ctx, &documents.CreateRequest{
		Filename: f.Filename,
		Content:  string(content),
	})
	if err != nil {
		return "", err
	}

	_ = triggerExtraction(ctx, opts.ServerURL, client, opts.ProjectID, doc.ID)
	return doc.ID, nil
}

// uploadBinaryFile uploads a binary file using multipart upload.
func uploadBinaryFile(ctx context.Context, client *sdk.Client, f FileInput) (string, bool, error) {
	content, err := fileContent(f)
	if err != nil {
		return "", false, err
	}

	result, err := client.Documents.UploadWithOptions(ctx, &documents.UploadFileInput{
		Filename: f.Filename,
		Reader:   bytes.NewReader(content),
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

// fileContent returns the content of f, reading from disk if f.Content is nil.
func fileContent(f FileInput) ([]byte, error) {
	if f.Content != nil {
		return f.Content, nil
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Path, err)
	}
	return data, nil
}

// triggerExtraction fires an extraction job for a document via the admin endpoint.
func triggerExtraction(ctx context.Context, serverURL string, client *sdk.Client, projectID, docID string) error {
	body := fmt.Sprintf(
		`{"project_id":%q,"source_type":"document","source_id":%q,"extraction_config":{}}`,
		projectID, docID,
	)
	req, err := http.NewRequestWithContext(ctx, "POST",
		serverURL+"/api/admin/extraction-jobs",
		bytes.NewBufferString(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// Authenticate the request via the SDK client
	resp, err := client.Do(ctx, req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// DetectContentType returns the MIME type for a file, using extension as a hint.
func DetectContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".md", ".txt":
		return "text/plain"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// isRetryable returns true for transient server errors (429, 503).
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *sdkerrors.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429 || apiErr.StatusCode == 503
	}
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "503") || strings.Contains(s, "rate")
}
