package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// Config holds all CLI-configurable settings passed to every module.
type Config struct {
	OutputPath          string
	FailurePath         string
	FailedDownloadsPath string
	MP3Dir              string
	Delay               time.Duration
	Workers             int
	DownloadMP3         bool
	Continue            bool
	MaxPages            int // debug: limit listing pages crawled (0 = all)
}

func main() {
	var (
		output       = flag.String("output", "episodes.jsonl", "Output JSONL file path")
		mp3Dir       = flag.String("mp3-dir", "mp3", "Directory to save downloaded MP3 files")
		delaySeconds = flag.Float64("delay", 1.0, "Delay in seconds between requests")
		workers      = flag.Int("workers", 5, "Number of concurrent episode-scraping workers")
		downloadMP3  = flag.Bool("download-mp3", false, "Download MP3 audio files")
		cont         = flag.Bool("continue", false, "Resume from existing output, retrying failed URLs")
		maxPages     = flag.Int("max-pages", 0, "Limit listing pages crawled (0 = all, for testing)")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `niezatapialni-scraper — archive episodes from niezatapialni.com

Usage:
  niezatapialni-scraper [flags]

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  # Full scrape, metadata only:
  niezatapialni-scraper

  # Resume after crash:
  niezatapialni-scraper --continue

  # Scrape + download audio:
  niezatapialni-scraper --download-mp3 --mp3-dir /data/mp3

  # Test with first 2 pages only:
  niezatapialni-scraper --max-pages 2
`)
	}

	flag.Parse()

	if *workers < 1 {
		fmt.Fprintln(os.Stderr, "ERROR: --workers must be >= 1")
		os.Exit(2)
	}
	if *delaySeconds < 0 {
		fmt.Fprintln(os.Stderr, "ERROR: --delay must be >= 0")
		os.Exit(2)
	}

	cfg := Config{
		OutputPath:          *output,
		FailurePath:         "failed_urls.txt",
		FailedDownloadsPath: "failed_downloads.txt",
		MP3Dir:              *mp3Dir,
		Delay:               time.Duration(float64(time.Second) * *delaySeconds),
		Workers:             *workers,
		DownloadMP3:         *downloadMP3,
		Continue:            *cont,
		MaxPages:            *maxPages,
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(2)
	}
}

func run(cfg Config) error {
	// Initialise exporter — loads seen URLs and (in continue mode) retry URLs.
	exp, retryURLs, err := NewExporter(cfg.OutputPath, cfg.FailurePath, cfg.Continue)
	if err != nil {
		return fmt.Errorf("init exporter: %w", err)
	}
	defer exp.Close()

	// Launch a single shared headless browser instance for Disqus comment scraping.
	fmt.Println("Launching headless browser for comment scraping...")
	browserURL := launcher.New().
		NoSandbox(true).
		Headless(true).
		Set("disable-gpu", "").
		Set("disable-dev-shm-usage", "").
		MustLaunch()
	browser := rod.New().ControlURL(browserURL).MustConnect()
	defer browser.MustClose()

	// Discover episode URLs.
	fmt.Println("Discovering episode URLs...")
	episodeURLs, err := DiscoverEpisodeURLs(cfg, exp.SeenURLs(), retryURLs)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}

	if len(episodeURLs) == 0 {
		fmt.Println("No episodes to scrape.")
	} else {
		fmt.Printf("Scraping %d episodes with %d workers...\n", len(episodeURLs), cfg.Workers)
		if err := scrapeWithWorkers(episodeURLs, exp, browser, cfg); err != nil {
			return err
		}
	}

	// Optionally download MP3s from the completed output file.
	if cfg.DownloadMP3 {
		fmt.Println("\nDownloading MP3 files...")
		episodes, err := loadEpisodes(cfg.OutputPath)
		if err != nil {
			return fmt.Errorf("load episodes for download: %w", err)
		}
		DownloadAll(episodes, cfg)
	}

	return nil
}

// scrapeWithWorkers runs a bounded goroutine pool to scrape episode URLs.
func scrapeWithWorkers(urls []string, exp *Exporter, browser *rod.Browser, cfg Config) error {
	jobs := make(chan string, len(urls))
	for _, u := range urls {
		jobs <- u
	}
	close(jobs)

	var (
		wg     sync.WaitGroup
		total  int64
		failed int64
	)

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for url := range jobs {
				ep, err := ScrapeEpisode(url, browser, cfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR scraping %s: %v\n", url, err)
					exp.RecordFailure(url)
					atomic.AddInt64(&failed, 1)
				} else {
					if werr := exp.Write(ep); werr != nil {
						fmt.Fprintf(os.Stderr, "ERROR writing %s: %v\n", url, werr)
					}
					atomic.AddInt64(&total, 1)
				}
				time.Sleep(cfg.Delay)
			}
		}()
	}

	wg.Wait()

	fmt.Printf("\nScrape summary: scraped=%d, failed=%d\n", total, failed)

	exitCode := 0
	if failed > 0 {
		exitCode = 1
	}
	_ = exitCode // caller may inspect; not fatal
	return nil
}

// loadEpisodes reads all Episode records from a JSONL file.
func loadEpisodes(path string) ([]Episode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var episodes []Episode
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ep Episode
		if err := json.Unmarshal([]byte(line), &ep); err != nil {
			continue // skip malformed lines
		}
		episodes = append(episodes, ep)
	}
	return episodes, scanner.Err()
}
