## Why

The niezatapialni.com podcast archive contains 600+ episodes of Polish gaming and pop-culture content spread across 90 paginated pages of a WordPress blog. There is no bulk export of this data, so scraping is required to build a structured archive of all MP3 files with their metadata (titles, dates, descriptions, comments) for offline preservation, analysis, or indexing.

## What Changes

- Introduce a standalone Go binary using `colly` to crawl all paginated listing pages on niezatapialni.com
- Extract per-episode data: episode number, title, publication date, teaser description, MP3 URL, and reader comments
- Persist collected data to a structured JSON output file (one record per episode)
- Optionally download the MP3 files alongside the metadata
- Support resumable crawling (skip already-scraped episodes) and configurable rate limiting to be polite to the server

## Capabilities

### New Capabilities

- `site-crawler`: Pagination-aware crawler that walks all listing pages (`/?paged=N`) on niezatapialni.com and discovers individual episode post URLs
- `episode-scraper`: Per-page scraper that extracts episode metadata (title, episode number, date, description excerpt, full body text, MP3 URL) and all reader comments from each episode post
- `data-exporter`: Writer that serialises scraped episode records to a JSON Lines (`.jsonl`) file for easy downstream consumption, with deduplication and resume support
- `mp3-downloader`: Optional module that downloads the MP3 file for each episode to a local directory, using the URL extracted by the episode scraper

### Modified Capabilities

_(none — this is a new standalone tool, no existing specs are affected)_

## Impact

- **New code**: standalone Go binary (`tools/niezatapialni-scraper/`) with its own `go.mod` — no changes to the existing monorepo server or admin apps
- **Dependencies**: `github.com/gocolly/colly/v2` for scraping; standard library `net/http` for MP3 downloads — Go 1.22+
- **Distribution**: single compiled binary, no runtime install required
- **External systems**: niezatapialni.com (read-only HTTP requests); no auth required, site is public
- **Output artefacts**: `episodes.jsonl` (metadata), optional `mp3/` directory with downloaded audio files
- **Rate limiting**: configurable delay between requests; respect `Retry-After` headers to avoid burdening the server
