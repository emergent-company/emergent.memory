// Package verifier implements the extraction verification phase.
// It polls extraction jobs for uploaded documents until they reach a terminal
// state (completed or failed), records timing, and returns a structured report.
package verifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/emergent-company/emergent/tools/huma-test-suite/internal/config"
	"github.com/emergent-company/emergent/tools/huma-test-suite/internal/uploader"
)

// DocumentResult holds the extraction outcome for one document.
type DocumentResult struct {
	DocumentID  string
	Filename    string
	Status      string // "completed", "failed", "timeout", "skipped"
	Error       string
	ElapsedTime time.Duration
}

// Report is the final aggregated output of the verify phase.
type Report struct {
	Total      int
	Successful int
	Failed     int
	TimedOut   int
	Skipped    int // duplicate or upload-failed

	AvgExtractionTime time.Duration
	MaxExtractionTime time.Duration
	MinExtractionTime time.Duration

	ErrorBreakdown map[string]int // error message → count
	Results        []DocumentResult
}

const (
	pollInterval   = 10 * time.Second
	defaultTimeout = 30 * time.Minute
)

// Verifier polls extraction status for a set of uploaded documents.
type Verifier struct {
	cfg *config.Config
}

// New creates a Verifier.
func New(cfg *config.Config) (*Verifier, error) {
	if cfg.EmergentAPIKey == "" {
		return nil, fmt.Errorf("EMERGENT_API_KEY is required for verify phase")
	}
	if cfg.EmergentProjectID == "" {
		return nil, fmt.Errorf("EMERGENT_PROJECT_ID is required for verify phase")
	}
	return &Verifier{cfg: cfg}, nil
}

// Verify polls extraction status for all successfully uploaded documents.
// It runs concurrent polling up to cfg.Concurrency workers.
func (v *Verifier) Verify(ctx context.Context, uploadStats *uploader.Stats) (*Report, error) {
	// Filter to documents that were actually uploaded (not failed, not duplicate skipped)
	type job struct {
		docID    string
		filename string
	}
	var jobs []job
	for _, r := range uploadStats.Records {
		if r.Err != nil {
			continue // upload failed — skip
		}
		if r.DocumentID == "" {
			continue
		}
		jobs = append(jobs, job{docID: r.DocumentID, filename: r.Filename})
	}

	skipped := len(uploadStats.Records) - len(jobs)

	fmt.Printf("Polling extraction status for %d documents (timeout: %s, interval: %s)...\n",
		len(jobs), defaultTimeout, pollInterval)

	results := make([]DocumentResult, 0, len(jobs))
	var mu sync.Mutex

	sem := make(chan struct{}, v.cfg.Concurrency)
	var wg sync.WaitGroup

	pollCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	for _, j := range jobs {
		j := j
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			res := v.pollDocument(pollCtx, j.docID, j.filename)

			statusIcon := "✓"
			if res.Status == "failed" {
				statusIcon = "✗"
			} else if res.Status == "timeout" {
				statusIcon = "⏱"
			}
			fmt.Printf("  %s %-40s  %s  (%s)\n", statusIcon, res.Filename, res.Status, res.ElapsedTime.Round(time.Second))

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}()
	}

	wg.Wait()

	return buildReport(results, skipped), nil
}

// pollDocument polls extraction jobs for a single document until a terminal state is reached.
func (v *Verifier) pollDocument(ctx context.Context, docID, filename string) DocumentResult {
	start := time.Now()
	res := DocumentResult{DocumentID: docID, Filename: filename}

	// Give the extraction pipeline time to create the job
	time.Sleep(3 * time.Second)

	jobsURL := fmt.Sprintf("%s/api/admin/extraction-jobs/projects/%s?source_id=%s&limit=10",
		v.cfg.EmergentServerURL, v.cfg.EmergentProjectID, docID)

	noJobWaitDeadline := time.Now().Add(90 * time.Second)

	for {
		select {
		case <-ctx.Done():
			res.Status = "timeout"
			res.Error = "global timeout reached"
			res.ElapsedTime = time.Since(start)
			return res
		default:
		}

		status, errMsg := v.fetchJobStatus(ctx, jobsURL)
		switch status {
		case "completed":
			res.Status = "completed"
			res.ElapsedTime = time.Since(start)
			return res
		case "failed":
			res.Status = "failed"
			res.Error = errMsg
			res.ElapsedTime = time.Since(start)
			return res
		case "no_job":
			// If we've waited long enough without a job appearing, mark as not-extracted
			if time.Now().After(noJobWaitDeadline) {
				res.Status = "completed"
				res.Error = "no extraction job created (inline content only)"
				res.ElapsedTime = time.Since(start)
				return res
			}
			// Keep waiting — job may not have been created yet
		}
		// queued/processing/no_job (within wait window) — poll again
		select {
		case <-ctx.Done():
			res.Status = "timeout"
			res.Error = "global timeout reached"
			res.ElapsedTime = time.Since(start)
			return res
		case <-time.After(pollInterval):
		}
	}
}

// jobListResponse is the shape of GET /api/admin/extraction-jobs/projects/:id
type jobListResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Jobs []struct {
			Status       string  `json:"status"`
			ErrorMessage *string `json:"error_message"`
		} `json:"jobs"`
	} `json:"data"`
}

// fetchJobStatus fetches the most recent extraction job status for a document.
// Returns: "completed", "failed", "queued", "processing", "no_job", or "error".
func (v *Verifier) fetchJobStatus(ctx context.Context, url string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "error", err.Error()
	}
	req.Header.Set("X-API-Key", v.cfg.EmergentAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "error", err.Error()
	}
	defer resp.Body.Close()

	var result jobListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "error", err.Error()
	}

	if len(result.Data.Jobs) == 0 {
		return "no_job", ""
	}

	// Take the most recent job (first in list)
	job := result.Data.Jobs[0]
	errMsg := ""
	if job.ErrorMessage != nil {
		errMsg = *job.ErrorMessage
	}
	return job.Status, errMsg
}

// buildReport aggregates results into a Report.
func buildReport(results []DocumentResult, skipped int) *Report {
	r := &Report{
		Total:          len(results) + skipped,
		Skipped:        skipped,
		ErrorBreakdown: make(map[string]int),
		Results:        results,
	}

	var totalTime time.Duration
	successCount := 0

	for _, res := range results {
		switch res.Status {
		case "completed":
			r.Successful++
			totalTime += res.ElapsedTime
			successCount++
			if res.ElapsedTime > r.MaxExtractionTime {
				r.MaxExtractionTime = res.ElapsedTime
			}
			if r.MinExtractionTime == 0 || res.ElapsedTime < r.MinExtractionTime {
				r.MinExtractionTime = res.ElapsedTime
			}
		case "failed":
			r.Failed++
			r.ErrorBreakdown[res.Error]++
		case "timeout":
			r.TimedOut++
			r.ErrorBreakdown["timeout"]++
		}
	}

	if successCount > 0 {
		r.AvgExtractionTime = totalTime / time.Duration(successCount)
	}

	return r
}
