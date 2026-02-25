package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Comment represents a single reader comment on an episode post.
type Comment struct {
	Author string `json:"author"`
	Date   string `json:"date"`
	Body   string `json:"body"`
}

// Episode represents a single podcast episode with all scraped metadata.
type Episode struct {
	PostURL       string    `json:"post_url"`
	EpisodeNumber *int      `json:"episode_number"`
	Title         string    `json:"title"`
	Date          string    `json:"date"`
	Description   string    `json:"description"`
	Body          string    `json:"body"`
	MP3URL        *string   `json:"mp3_url"`
	Comments      []Comment `json:"comments"`
}

// Exporter handles writing episode records to a JSONL file and tracking failures.
type Exporter struct {
	mu          sync.Mutex
	outputPath  string
	failurePath string
	seen        map[string]bool
	file        *os.File
	enc         *json.Encoder
}

// NewExporter creates an Exporter. If continueMode is true it loads existing
// episodes for deduplication and loads+truncates failed_urls.txt for retry.
func NewExporter(outputPath, failurePath string, continueMode bool) (*Exporter, []string, error) {
	seen := make(map[string]bool)
	var retryURLs []string

	// Load existing seen URLs from output file for deduplication.
	if f, err := os.Open(outputPath); err == nil {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var ep Episode
			if err := json.Unmarshal([]byte(line), &ep); err == nil && ep.PostURL != "" {
				seen[ep.PostURL] = true
			}
		}
		f.Close()
		if len(seen) > 0 {
			fmt.Printf("Resuming: skipping %d already-scraped episodes\n", len(seen))
		}
	}

	// In continue mode: load failed_urls.txt for retry, then truncate it.
	if continueMode {
		if f, err := os.Open(failurePath); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				u := strings.TrimSpace(scanner.Text())
				if u != "" {
					retryURLs = append(retryURLs, u)
				}
			}
			f.Close()
			if len(retryURLs) > 0 {
				fmt.Printf("Retrying %d previously failed URLs\n", len(retryURLs))
				if err := os.Truncate(failurePath, 0); err != nil && !os.IsNotExist(err) {
					return nil, nil, fmt.Errorf("truncate failed_urls.txt: %w", err)
				}
			}
		} else if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("open failed_urls.txt: %w", err)
		} else {
			fmt.Println("No existing output found, starting fresh")
		}
	}

	// Open output file in append mode (create if needed).
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("open output file: %w", err)
	}

	enc := json.NewEncoder(file)
	enc.SetEscapeHTML(false)

	return &Exporter{
		outputPath:  outputPath,
		failurePath: failurePath,
		seen:        seen,
		file:        file,
		enc:         enc,
	}, retryURLs, nil
}

// Write appends an episode record to the JSONL file, skipping duplicates.
func (e *Exporter) Write(ep Episode) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.seen[ep.PostURL] {
		fmt.Printf("duplicate skipped: %s\n", ep.PostURL)
		return nil
	}

	// Ensure Comments is never null in JSON.
	if ep.Comments == nil {
		ep.Comments = []Comment{}
	}

	if err := e.enc.Encode(ep); err != nil {
		return fmt.Errorf("write episode: %w", err)
	}

	e.seen[ep.PostURL] = true
	return nil
}

// RecordFailure appends a URL to failed_urls.txt.
func (e *Exporter) RecordFailure(url string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	f, err := os.OpenFile(e.failurePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot open failed_urls.txt: %v\n", err)
		return
	}
	defer f.Close()
	fmt.Fprintln(f, url)
}

// SeenCount returns the number of already-seen post URLs.
func (e *Exporter) SeenCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.seen)
}

// SeenURLs returns a copy of the seen post-URL set for use as a skip-list.
func (e *Exporter) SeenURLs() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]bool, len(e.seen))
	for k, v := range e.seen {
		out[k] = v
	}
	return out
}

// Close flushes and closes the output file.
func (e *Exporter) Close() error {
	return e.file.Close()
}
