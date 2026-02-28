# site-crawler Specification

## Purpose
TBD - created by archiving change niezatapialni-mp3-scraper. Update Purpose after archive.
## Requirements
### Requirement: Crawl all paginated listing pages

The crawler SHALL iterate through all paginated listing pages on niezatapialni.com (starting at `https://niezatapialni.com/` and continuing through `/?paged=2`, `/?paged=3`, … up to the last detected page) using the `colly` Go scraping framework.

#### Scenario: Discovers all pages automatically

- **WHEN** the crawler starts with no page count configured
- **THEN** it SHALL detect the last page number from pagination links on the first listing page and enqueue all pages from 1 to N

#### Scenario: Stops at the last page

- **WHEN** the crawler reaches a page with no "next page" link and no further numbered pagination
- **THEN** it SHALL stop pagination and not attempt to fetch beyond the last page

### Requirement: Extract episode post URLs from listing pages

For each listing page the crawler SHALL extract the URL of every individual episode post found on that page.

#### Scenario: Multiple posts on one page

- **WHEN** a listing page contains multiple episode post headings, each linking to a `/?p=<id>` URL
- **THEN** the crawler SHALL return all such URLs as a list, preserving order (top to bottom)

#### Scenario: Listing page contains no episode links

- **WHEN** a listing page contains no recognisable episode post links (e.g. a voting/announcement post)
- **THEN** the crawler SHALL skip that page without error and continue to the next

### Requirement: Configurable rate limiting

The crawler SHALL insert a configurable delay between HTTP requests to avoid overloading the server.

#### Scenario: Default delay applied

- **WHEN** no delay is configured by the user
- **THEN** the crawler SHALL wait at least 1 second between successive page fetches

#### Scenario: Custom delay applied

- **WHEN** the user sets `--delay <seconds>` on the CLI
- **THEN** the crawler SHALL wait exactly that many seconds between requests

### Requirement: Resumable crawl via URL skip-list

The crawler SHALL accept a set of already-processed episode URLs and skip them during discovery.

#### Scenario: Previously scraped URL encountered

- **WHEN** a discovered episode URL is present in the skip-list
- **THEN** the crawler SHALL not enqueue that URL for scraping and SHALL log it as "skipped"

#### Scenario: Fresh run with empty skip-list

- **WHEN** no skip-list is provided
- **THEN** the crawler SHALL enqueue every discovered URL

### Requirement: Continue mode resumes from existing output

The crawler SHALL support a `--continue` flag that automatically loads the existing `episodes.jsonl` and `failed_urls.txt` output files to build the skip-list, so the run picks up exactly where it left off after a crash, error, or manual interruption.

#### Scenario: --continue with existing output file

- **WHEN** `--continue` is passed and `episodes.jsonl` exists
- **THEN** the crawler SHALL read all `post_url` values from `episodes.jsonl` and skip those URLs during discovery, logging the count: "Resuming: skipping N already-scraped episodes"

#### Scenario: --continue also retries previously failed URLs

- **WHEN** `--continue` is passed and `failed_urls.txt` exists and is non-empty
- **THEN** the crawler SHALL enqueue every URL in `failed_urls.txt` for re-scraping (overriding the skip-list for those URLs), and clear `failed_urls.txt` before the run begins

#### Scenario: --continue with no existing output

- **WHEN** `--continue` is passed but neither `episodes.jsonl` nor `failed_urls.txt` exist
- **THEN** the crawler SHALL log "No existing output found, starting fresh" and proceed as a normal run

#### Scenario: --continue not passed (default behaviour)

- **WHEN** `--continue` is not passed
- **THEN** the crawler SHALL ignore any existing output files and start a full crawl from page 1

