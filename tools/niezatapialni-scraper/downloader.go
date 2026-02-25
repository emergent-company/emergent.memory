package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// DownloadSummary holds counters from a download run.
type DownloadSummary struct {
	Downloaded     int
	AlreadyExisted int
	SkippedNoURL   int
	Failed         int
}

// DownloadAll downloads MP3 files for all episodes with a non-nil MP3URL.
// Only runs when cfg.DownloadMP3 is true.
func DownloadAll(episodes []Episode, cfg Config) DownloadSummary {
	var summary DownloadSummary

	if err := os.MkdirAll(cfg.MP3Dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot create mp3 dir %s: %v\n", cfg.MP3Dir, err)
		return summary
	}

	for _, ep := range episodes {
		if ep.MP3URL == nil {
			fmt.Printf("no mp3_url for: %s\n", ep.PostURL)
			summary.SkippedNoURL++
			continue
		}

		filename := mp3Filename(*ep.MP3URL)
		destPath := filepath.Join(cfg.MP3Dir, filename)

		// Check if file already exists with non-zero size.
		if info, err := os.Stat(destPath); err == nil {
			if info.Size() > 0 {
				fmt.Printf("already exists: %s\n", filename)
				summary.AlreadyExisted++
				continue
			}
			// Zero-length file: remove and re-download.
			os.Remove(destPath)
		}

		fmt.Printf("downloading: %s\n", filename)
		if err := downloadFile(*ep.MP3URL, destPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			recordFailedDownload(cfg.FailedDownloadsPath, *ep.MP3URL)
			summary.Failed++
			continue
		}
		summary.Downloaded++
	}

	fmt.Printf("\nDownload summary: downloaded=%d, already_existed=%d, skipped_no_url=%d, failed=%d\n",
		summary.Downloaded, summary.AlreadyExisted, summary.SkippedNoURL, summary.Failed)
	return summary
}

// downloadFile streams an MP3 from srcURL to destPath.
func downloadFile(srcURL, destPath string, cfg Config) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(srcURL)
	if err != nil {
		return fmt.Errorf("GET %s: %w", srcURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, srcURL)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		// Remove partial file on write error.
		os.Remove(destPath)
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
}

// mp3Filename extracts the filename from an MP3 URL path.
func mp3Filename(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return "unknown.mp3"
	}
	return filepath.Base(u.Path)
}

// recordFailedDownload appends a URL to failed_downloads.txt.
func recordFailedDownload(path, rawURL string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: cannot open %s: %v\n", path, err)
		return
	}
	defer f.Close()
	fmt.Fprintln(f, rawURL)
}
