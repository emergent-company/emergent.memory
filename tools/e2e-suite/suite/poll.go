package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// PollOptions configures the extraction job poller.
type PollOptions struct {
	Interval    time.Duration // poll interval (default: 10s)
	Concurrency int           // max concurrent pollers (default: 4)
}

// ExtractionResult holds the polling outcome for one document.
type ExtractionResult struct {
	DocumentID string
	Filename   string
	Status     Status
	Duration   time.Duration
	Error      string
}

// PollExtractionJobs polls /api/admin/extraction-jobs for a list of document IDs.
// It runs concurrent polling bounded by opts.Concurrency.
// The context deadline controls the overall wall-clock timeout.
func PollExtractionJobs(
	ctx context.Context,
	serverURL, apiKey, projectID string,
	docs []struct{ DocID, Filename string },
	opts PollOptions,
) []ExtractionResult {
	if opts.Interval <= 0 {
		opts.Interval = 10 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}

	results := make([]ExtractionResult, 0, len(docs))
	var mu sync.Mutex

	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup

	for _, d := range docs {
		d := d
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			res := pollDocument(ctx, serverURL, apiKey, projectID, d.DocID, d.Filename, opts.Interval)

			icon := "✓"
			if res.Status == StatusFailed {
				icon = "✗"
			} else if res.Status == StatusTimeout {
				icon = "⏱"
			}
			fmt.Printf("  %s %-50s  %s  (%s)\n", icon, res.Filename, res.Status, res.Duration.Round(time.Second))

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}()
	}
	wg.Wait()

	return results
}

// pollDocument polls extraction jobs for a single document until a terminal state is reached.
func pollDocument(ctx context.Context, serverURL, apiKey, projectID, docID, filename string, interval time.Duration) ExtractionResult {
	start := time.Now()
	res := ExtractionResult{DocumentID: docID, Filename: filename}

	// Give the extraction pipeline a moment to create the job
	select {
	case <-ctx.Done():
		res.Status = StatusTimeout
		res.Error = "context cancelled before polling started"
		res.Duration = time.Since(start)
		return res
	case <-time.After(3 * time.Second):
	}

	jobsURL := fmt.Sprintf("%s/api/admin/extraction-jobs/projects/%s?source_id=%s&limit=10",
		serverURL, projectID, docID)

	noJobDeadline := time.Now().Add(90 * time.Second)

	for {
		select {
		case <-ctx.Done():
			res.Status = StatusTimeout
			res.Error = "global timeout reached"
			res.Duration = time.Since(start)
			return res
		default:
		}

		status, errMsg := fetchJobStatus(ctx, jobsURL, apiKey)
		switch status {
		case "completed":
			res.Status = StatusPassed
			res.Duration = time.Since(start)
			return res
		case "failed":
			res.Status = StatusFailed
			res.Error = errMsg
			res.Duration = time.Since(start)
			return res
		case "no_job":
			if time.Now().After(noJobDeadline) {
				// No extraction job was ever created — treat as passed (inline content)
				res.Status = StatusPassed
				res.Error = "no extraction job created"
				res.Duration = time.Since(start)
				return res
			}
		}

		select {
		case <-ctx.Done():
			res.Status = StatusTimeout
			res.Error = "global timeout reached"
			res.Duration = time.Since(start)
			return res
		case <-time.After(interval):
		}
	}
}

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
func fetchJobStatus(ctx context.Context, url, apiKey string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "error", err.Error()
	}
	req.Header.Set("X-API-Key", apiKey)

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

	job := result.Data.Jobs[0]
	errMsg := ""
	if job.ErrorMessage != nil {
		errMsg = *job.ErrorMessage
	}
	return job.Status, errMsg
}
