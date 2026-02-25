## Context

niezatapialni.com is a public WordPress blog hosting 600+ Polish gaming/pop-culture podcast episodes across 90 paginated listing pages. Each episode post contains a title, publication date, description, embedded MP3 player, post body, and a WordPress comment section. There is no API or RSS feed that provides full metadata + comments in bulk; scraping is the only path to a complete archive.

The tool is a **standalone Go binary** — it lives at `tools/niezatapialni-scraper/` with its own `go.mod`, completely independent of the monorepo server and admin apps. It compiles to a single executable with zero runtime dependencies.

## Goals / Non-Goals

**Goals:**

- Scrape all episode metadata (title, episode number, date, description, body, mp3_url, comments) into a single `episodes.jsonl` file
- Optionally download all MP3 files to a local directory (`--download-mp3`)
- Support `--continue` to safely resume after a crash or scripted interruption, retrying previously failed URLs
- Be polite to the server: configurable delay, bounded worker concurrency
- Distribute as a single compiled binary with no install ceremony

**Non-Goals:**

- Real-time or scheduled scraping (one-shot archival tool only)
- Storing data in a database (flat files are sufficient)
- Scraping non-podcast content (text articles, video posts)
- Authentication or handling paywalled content (site is fully public)
- Uploading or syncing to any remote storage

## Decisions

### 1. Go + colly for fetching and HTML parsing

**Decision:** Implement in Go using the `colly` scraping framework (`github.com/gocolly/colly/v2`) for HTTP fetching and HTML extraction via CSS selectors.

**Rationale:** The site is plain server-rendered WordPress HTML — no JavaScript gating, no dynamic content. A headless browser is unnecessary overhead. Go was chosen over Python for a self-contained single-binary distribution with no virtualenv or `pip install` required. `colly` is the most mature Go scraping library, providing built-in rate limiting, retry, and CSS selector extraction. The previously considered `crawl4ai` (Python-only) was dropped since Go was selected.

**Alternative considered:** `golang.org/x/net/html` directly — more control but significantly more boilerplate for selector-based extraction; rejected in favour of colly's ergonomic API.

---

### 2. Sequential pagination, concurrent episode scraping

**Decision:** Walk listing pages sequentially (page 1 → 90) to discover episode URLs, then scrape individual episode posts concurrently with a bounded worker pool (default 5 goroutines, configurable via `--workers`).

**Rationale:** Listing page order matters for the skip-list early-exit on `--continue`. Episode scraping order is irrelevant and benefits from goroutine concurrency. Keeping the two phases separate makes resume logic straightforward. colly's `Async` mode with `SetRequestTimeout` handles per-request timeouts cleanly.

**Alternative considered:** Fully parallel pagination — rejected because it complicates early-exit and adds unnecessary server load.

---

### 3. JSON Lines (`.jsonl`) as the output format

**Decision:** One JSON object per line in `episodes.jsonl`, appended incrementally as episodes complete.

**Rationale:** Incremental append means a crash loses at most one in-flight record. JSONL is trivially readable with `jq` or any streaming JSON parser. A single large JSON array would require loading the whole file into memory to append safely. Go's `encoding/json` handles this natively with `json.NewEncoder`.

**Alternative considered:** SQLite — overkill for a one-shot archive tool; adds CGO dependency, complicates cross-compilation.

---

### 4. `--continue` flag as explicit resume entry point

**Decision:** A single `--continue` flag triggers resume mode: load `episodes.jsonl` to build the skip-list, load `failed_urls.txt` to re-queue failures, then truncate `failed_urls.txt` before the run begins.

**Rationale:** Explicit opt-in avoids silently skipping episodes when the user intends a full fresh re-scrape (e.g. after fixing a parsing bug). Auto-detecting an existing file and always resuming would make intentional re-runs require manually deleting the output file.

---

### 5. MP3 download via `net/http` streaming

**Decision:** Use Go's standard `net/http` with streaming (`io.Copy`) for MP3 binary downloads.

**Rationale:** No additional dependency needed — `net/http` handles streaming downloads natively. Files are written to disk incrementally, so a download interrupted mid-way leaves a zero-or-partial file which the skip logic detects and re-downloads on `--continue`.

---

### 6. Package layout

```
tools/niezatapialni-scraper/
├── go.mod                  # module niezatapialni-scraper, Go 1.22
├── go.sum
├── main.go                 # CLI entry point (flag package)
├── crawler.go              # site-crawler: pagination + URL discovery
├── extractor.go            # episode-scraper: metadata + comments parsing
├── exporter.go             # data-exporter: JSONL write, dedup, failed_urls
└── downloader.go           # mp3-downloader: streaming download + skip logic
```

Single-file was considered but rejected; splitting by capability matches the spec structure and keeps each concern independently readable and testable.

---

### 7. HTML extraction strategy for WordPress structure

**Decision:** Use colly CSS selectors targeting stable WordPress semantic elements rather than fragile class names.

Key selectors:

- Title: `h1.entry-title` or `h2.entry-title > a`
- Date: `time.entry-date[datetime]` → `datetime` attribute (already ISO 8601)
- Body: `.entry-content` inner text
- MP3 URL: `a[href$=".mp3"]` (first match — covers both "Play in new window" and "Download" links)
- Comments: `#comments .comment-body`
- Pagination last page: `.page-numbers:not(.next)` last element text

**Rationale:** WordPress themes change class names but rarely change semantic HTML structure. The `datetime` attribute on `<time>` elements is the most reliable date source. Matching `href` ending in `.mp3` is more robust than looking for specific link text.

## Risks / Trade-offs

- **Site structure changes** → CSS selectors will break if the WordPress theme changes significantly. Mitigation: log extraction failures as warnings (partial records, not aborted runs); `failed_urls.txt` captures affected episodes for manual review.

- **MP3 URL domain inconsistency** → Observed that some older episodes use `niezatapialni.pl` and newer ones use `niezatapialni.com` as the MP3 host. Mitigation: store `mp3_url` verbatim without normalisation; downloader follows the URL as-is.

- **Rate limiting / IP block** → 90 listing pages + 600+ episode pages without delay could trigger server-side rate limiting. Mitigation: default 1 s inter-request delay; `--delay` flag for user control; colly's built-in `LimitRule` enforces this.

- **Large MP3 storage** → 600+ episodes × ~50–150 MB each ≈ up to 90 GB. Mitigation: `--download-mp3` is opt-in; `--mp3-dir` lets the user point to external storage.

- **Cross-compilation** → Go cross-compiles trivially (`GOOS=linux GOARCH=amd64 go build`). No CGO used, so all targets work out of the box.

## Open Questions

- Should episode body text include show-notes links as markdown, or plain text only? (Current spec says HTML-stripped plain text — revisit if downstream use needs structure.)
- Are nested/threaded WordPress comments present on this site? (Current spec: top-level only — verify during implementation.)
