// Package niezatapialni implements the Niezatapialni podcast e2e suite.
// It uploads pre-downloaded MP3 files and polls transcription jobs, replacing
// the old bash-based upload_batch.sh with proper verification and reporting.
package niezatapialni

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/tools/e2e-suite/suite"
)

// Suite implements the Niezatapialni podcast upload+verify e2e suite.
type Suite struct{}

func (s *Suite) Name() string        { return "niezatapialni" }
func (s *Suite) Description() string { return "Upload Niezatapialni MP3s and verify transcription" }

// Run walks the MP3 directory, uploads all files, then polls transcription jobs.
func (s *Suite) Run(ctx context.Context, client *sdk.Client, cfg *suite.Config) (*suite.Result, error) {
	result := suite.NewResult(s.Name())

	mp3Dir := getEnv("NIEZATAPIALNI_MP3_DIR", "tools/niezatapialni-scraper/all_mp3s")
	fmt.Printf("MP3 directory: %s\n", mp3Dir)

	files, err := discoverMP3s(mp3Dir)
	if err != nil {
		return result, fmt.Errorf("discovering MP3s in %s: %w", mp3Dir, err)
	}
	if len(files) == 0 {
		return result, fmt.Errorf("no MP3 files found in %s — download them first", mp3Dir)
	}
	fmt.Printf("Found %d MP3 files\n", len(files))

	// Upload phase
	fmt.Println("\nUploading MP3 files...")
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

	// Record upload failures
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
		return result, fmt.Errorf("no MP3s were successfully uploaded")
	}

	// Poll transcription phase (MP3s go through the extraction/transcription pipeline)
	fmt.Printf("\nPolling transcription for %d episodes (timeout: %s)...\n", len(pollDocs), cfg.Timeout)
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

// discoverMP3s returns all .mp3 files from the given directory.
func discoverMP3s(dir string) ([]suite.FileInput, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []suite.FileInput
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(filepath.Ext(name), ".mp3") {
			continue
		}
		path := filepath.Join(dir, name)
		files = append(files, suite.FileInput{
			Path:        path,
			Filename:    name,
			ContentType: "audio/mpeg",
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
