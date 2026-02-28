# data-exporter Specification

## Purpose
TBD - created by archiving change niezatapialni-mp3-scraper. Update Purpose after archive.
## Requirements
### Requirement: Write episode records to a JSON Lines file

The exporter SHALL append each scraped episode record as a single JSON object on its own line to an output `.jsonl` file (`episodes.jsonl` by default, overridable via `--output`).

#### Scenario: Successful write of one record

- **WHEN** a scraped episode record is passed to the exporter
- **THEN** the exporter SHALL write a valid JSON object on a new line in the output file, with all fields serialised (nulls included)

#### Scenario: Output file does not exist yet

- **WHEN** the output `.jsonl` file does not exist at startup
- **THEN** the exporter SHALL create it automatically before writing the first record

### Requirement: Deduplicate records by post URL

The exporter SHALL not write a record whose `post_url` already exists in the output file.

#### Scenario: Duplicate URL encountered mid-run

- **WHEN** the exporter is asked to write a record whose `post_url` is already present in `episodes.jsonl`
- **THEN** it SHALL skip the write, log "duplicate skipped: <url>", and not corrupt the file

#### Scenario: Resume from existing file

- **WHEN** the scraper is restarted and the output file already contains N records
- **THEN** the exporter SHALL load those N `post_url` values at startup and use them as the skip-list for the site-crawler

### Requirement: Produce a consistent JSON schema per record

Every record written to the output file SHALL contain exactly these top-level keys in a consistent order:
`post_url`, `episode_number`, `title`, `date`, `description`, `body`, `mp3_url`, `comments`

#### Scenario: All fields present

- **WHEN** all fields were successfully extracted
- **THEN** the JSON object SHALL contain all eight keys with their values

#### Scenario: Optional fields are null

- **WHEN** `episode_number` or `mp3_url` could not be extracted
- **THEN** the JSON object SHALL still contain those keys with value `null`

### Requirement: Write failed URLs to a separate log

The exporter SHALL maintain a `failed_urls.txt` file listing every URL that could not be scraped, one per line.

#### Scenario: A URL fails to scrape

- **WHEN** the episode-scraper reports a failure for a URL
- **THEN** the exporter SHALL append that URL to `failed_urls.txt`

#### Scenario: No failures occur

- **WHEN** the run completes without any errors
- **THEN** `failed_urls.txt` SHALL either not exist or be empty

### Requirement: Clear failed_urls.txt at the start of a --continue run

When resuming with `--continue`, previously failed URLs are re-queued for scraping. The exporter SHALL clear (truncate) `failed_urls.txt` before the run begins so that only failures from the current run are recorded.

#### Scenario: --continue clears stale failures

- **WHEN** `--continue` is passed and `failed_urls.txt` contains URLs from a prior run
- **THEN** the exporter SHALL truncate `failed_urls.txt` to empty before scraping starts, so that the file only reflects failures from the current run

#### Scenario: Normal run does not clear failed_urls.txt

- **WHEN** `--continue` is not passed
- **THEN** the exporter SHALL append new failures to `failed_urls.txt` without clearing it first

