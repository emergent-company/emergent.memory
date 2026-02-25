## 1. Project Scaffold

- [x] 1.1 Create `tools/niezatapialni-scraper/` directory with `go.mod` (module `niezatapialni-scraper`, Go 1.22)
- [x] 1.2 Add `github.com/gocolly/colly/v2` dependency and run `go mod tidy`
- [x] 1.3 Create empty stub files: `main.go`, `crawler.go`, `extractor.go`, `exporter.go`, `downloader.go`

## 2. CLI Entry Point (`main.go`)

- [x] 2.1 Define CLI flags with the `flag` package: `--output`, `--mp3-dir`, `--delay`, `--workers`, `--download-mp3`, `--continue`
- [x] 2.2 Wire flags into a `Config` struct passed to all modules
- [x] 2.3 Print usage/help and validate required flag combinations on startup
- [x] 2.4 Add top-level error handling and exit codes (0 = success, 1 = partial failure, 2 = fatal)

## 3. Exporter (`exporter.go`)

- [x] 3.1 Implement `Exporter` struct that opens/creates `episodes.jsonl` in append mode on init
- [x] 3.2 On init, read all existing `post_url` values from `episodes.jsonl` into an in-memory `seen` set for deduplication
- [x] 3.3 Implement `Write(episode Episode) error` — skips duplicates, appends JSON line, flushes immediately
- [x] 3.4 Implement `failed_urls.txt` append on `RecordFailure(url string)`
- [x] 3.5 Implement `--continue` startup logic: load `failed_urls.txt` URLs into a return slice for re-queuing, then truncate the file
- [x] 3.6 Define the `Episode` struct with fields: `PostURL`, `EpisodeNumber *int`, `Title`, `Date`, `Description`, `Body`, `MPURL *string`, `Comments []Comment`
- [x] 3.7 Define the `Comment` struct with fields: `Author`, `Date`, `Body`
- [x] 3.8 Ensure JSON field names are snake_case (`json:"post_url"` etc.) and key order matches spec

## 4. Site Crawler (`crawler.go`)

- [x] 4.1 Implement `DiscoverEpisodeURLs(cfg Config, skipSet map[string]bool, retryURLs []string) ([]string, error)` using colly
- [x] 4.2 Detect total page count from `.page-numbers:not(.next)` last element on page 1
- [x] 4.3 Iterate pages 1→N sequentially, extracting all `h1.entry-title a[href]` and `h2.entry-title a[href]` post URLs per page
- [x] 4.4 Skip any discovered URL present in `skipSet`; log skipped count at end of discovery
- [x] 4.5 Prepend `retryURLs` (from `failed_urls.txt`) to the final URL list, overriding skip for those entries
- [x] 4.6 Apply inter-request delay via colly `LimitRule` using `cfg.Delay`
- [x] 4.7 Handle `--continue` not passed: ignore existing output, start fresh from page 1

## 5. Episode Extractor (`extractor.go`)

- [x] 5.1 Implement `ScrapeEpisode(url string, cfg Config) (Episode, error)` using a colly collector
- [x] 5.2 Extract `title` from `h1.entry-title` or `h2.entry-title`
- [x] 5.3 Parse `episode_number` (integer) from title prefix matching `NZ` followed by digits; set `null` if not matched
- [x] 5.4 Extract `date` from `time.entry-date[datetime]` attribute; parse to ISO 8601 date string (`2006-01-02`)
- [x] 5.5 Extract `mp3_url` from first `a[href$=".mp3"]` on the page; set `null` if not found
- [x] 5.6 Extract `description` from `.entry-summary` or first `<p>` inside `.entry-content` as fallback
- [x] 5.7 Extract `body` as full inner text of `.entry-content`, HTML tags stripped
- [x] 5.8 Extract `comments` from `#comments .comment-body`: each comment's author (`.comment-author .fn`), date (`time[datetime]` attr), body (`.comment-text p` inner text joined)
- [x] 5.9 On network error or HTTP non-200: return error (caller writes to `failed_urls.txt`)
- [x] 5.10 On partial parse (some fields missing): return partial `Episode` with `null` fields and `nil` error; log warning

## 6. Worker Pool (in `main.go` or `crawler.go`)

- [x] 6.1 Implement a goroutine worker pool of size `cfg.Workers` (default 5) that consumes episode URLs from a channel
- [x] 6.2 Each worker calls `ScrapeEpisode`, then calls `exporter.Write` or `exporter.RecordFailure` depending on result
- [x] 6.3 Apply per-request delay between episode fetches within each worker
- [x] 6.4 Collect and log a run summary on completion: total scraped, skipped (resume), failed

## 7. MP3 Downloader (`downloader.go`)

- [x] 7.1 Implement `DownloadAll(episodes []Episode, cfg Config) Summary` — only runs when `cfg.DownloadMP3` is true
- [x] 7.2 Skip episodes where `MPURL` is `nil`; log "no mp3_url for: <post_url>"
- [x] 7.3 Skip files that already exist with size > 0; log "already exists: <filename>"
- [x] 7.4 Delete and re-download files that exist with size == 0 (incomplete prior download)
- [x] 7.5 Derive local filename from last path segment of `MPURL` (e.g. `Niezatapialni_615.mp3`)
- [x] 7.6 Stream download via `net/http` GET → `io.Copy` to file in `cfg.MP3Dir`; create `cfg.MP3Dir` if it doesn't exist
- [x] 7.7 On non-200 HTTP response: log error, append URL to `failed_downloads.txt`, continue
- [x] 7.8 Print final summary: downloaded, already existed, skipped (no URL), failed

## 8. Integration & Manual Testing

- [x] 8.1 Run scraper against a single known episode URL and verify all fields in output JSON
- [x] 8.2 Run full listing crawl (or first 2 pages with `--max-pages` debug flag) and verify JSONL output
- [x] 8.3 Simulate crash mid-run (Ctrl-C), then run with `--continue` and verify no duplicates and failed URLs are retried
- [ ] 8.4 Run with `--download-mp3` on a small set and verify MP3 files saved correctly
- [ ] 8.5 Verify re-run with `--download-mp3` skips already-downloaded files
- [x] 8.6 Build binary with `go build -o niezatapialni-scraper .` and confirm zero external runtime deps
