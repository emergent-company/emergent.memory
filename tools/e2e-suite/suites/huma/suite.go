// Package huma implements the HUMA document e2e suite.
// It uploads cached HUMA documents (assumed already downloaded) and polls
// extraction jobs to verify the pipeline processed them correctly.
package huma

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/tools/e2e-suite/suite"
)

// Suite implements the HUMA document upload+verify e2e suite.
type Suite struct{}

func (s *Suite) Name() string        { return "huma" }
func (s *Suite) Description() string { return "Upload HUMA docs from cache dir and verify extraction" }

// Run walks the cache directory, uploads all documents, then polls extraction.
func (s *Suite) Run(ctx context.Context, client *sdk.Client, cfg *suite.Config) (*suite.Result, error) {
	result := suite.NewResult(s.Name())

	cacheDir := getEnv("HUMA_CACHE_DIR", "/root/data")
	fmt.Printf("HUMA cache dir: %s\n", cacheDir)

	files, err := discoverFiles(cacheDir)
	if err != nil {
		return result, fmt.Errorf("discovering files in %s: %w", cacheDir, err)
	}
	if len(files) == 0 {
		return result, fmt.Errorf("no files found in %s — run the HUMA downloader first", cacheDir)
	}
	fmt.Printf("Found %d files to upload\n", len(files))

	// Upload phase
	fmt.Println("\nUploading HUMA documents...")
	records := suite.UploadFiles(ctx, client, files, suite.UploadOptions{
		Concurrency: cfg.Concurrency,
		AutoExtract: true,
		ServerURL:   cfg.ServerURL,
		ProjectID:   cfg.ProjectID,
	})

	// Build poll list from successful uploads
	var pollDocs []struct{ DocID, Filename string }
	for _, r := range records {
		if r.Status == suite.StatusFailed || r.DocumentID == "" {
			continue
		}
		pollDocs = append(pollDocs, struct{ DocID, Filename string }{r.DocumentID, r.Filename})
	}

	// Add upload results to the result set
	for _, r := range records {
		if r.Status == suite.StatusFailed {
			result.AddItem(suite.ItemResult{
				ID:       r.Filename,
				Name:     r.Filename,
				Status:   suite.StatusFailed,
				Duration: r.Duration,
				Error:    errStr(r.Error),
			})
		}
	}

	if len(pollDocs) == 0 {
		return result, fmt.Errorf("no documents were successfully uploaded")
	}

	// Poll extraction phase
	fmt.Printf("\nPolling extraction for %d documents (timeout: %s)...\n", len(pollDocs), cfg.Timeout)
	pollCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	extractionResults := suite.PollExtractionJobs(pollCtx, cfg.ServerURL, cfg.APIKey, cfg.ProjectID,
		pollDocs, suite.PollOptions{Concurrency: cfg.Concurrency})

	for _, er := range extractionResults {
		result.AddItem(suite.ItemResult{
			ID:       er.DocumentID,
			Name:     er.Filename,
			Status:   er.Status,
			Duration: er.Duration,
			Error:    er.Error,
		})
	}

	return result, nil
}

// discoverFiles returns all uploadable files from the cache directory.
func discoverFiles(cacheDir string) ([]suite.FileInput, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, err
	}

	var files []suite.FileInput
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(cacheDir, name)
		files = append(files, suite.FileInput{
			Path:        path,
			Filename:    name,
			ContentType: suite.DetectContentType(name),
		})
	}
	return files, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
