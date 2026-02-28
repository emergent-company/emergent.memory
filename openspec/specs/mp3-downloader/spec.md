# mp3-downloader Specification

## Purpose
TBD - created by archiving change niezatapialni-mp3-scraper. Update Purpose after archive.
## Requirements
### Requirement: Download MP3 files for episodes with a valid mp3_url

When the `--download-mp3` flag is passed, the downloader SHALL fetch the audio file from `mp3_url` and save it to the configured output directory (`mp3/` by default, overridable via `--mp3-dir`).

#### Scenario: Successful download

- **WHEN** `mp3_url` is non-null and the server returns HTTP 200
- **THEN** the downloader SHALL save the file as `<mp3-dir>/<filename>` where `<filename>` is the last path segment of the URL (e.g. `Niezatapialni_615.mp3`)

#### Scenario: Episode has no mp3_url

- **WHEN** `mp3_url` is `null` in the episode record
- **THEN** the downloader SHALL skip that episode, log "no mp3_url for: <post_url>", and continue

### Requirement: Skip already-downloaded files

The downloader SHALL not re-download a file that already exists on disk with a non-zero size.

#### Scenario: File already present

- **WHEN** the target file path already exists and its size is greater than 0 bytes
- **THEN** the downloader SHALL skip the download and log "already exists: <filename>"

#### Scenario: File present but zero-length (incomplete previous download)

- **WHEN** the target file exists but has size 0
- **THEN** the downloader SHALL delete it and re-download

### Requirement: Preserve original filenames from the URL

The downloader SHALL derive the local filename directly from the MP3 URL path without modification.

#### Scenario: URL ends with a .mp3 filename

- **WHEN** `mp3_url` is `https://niezatapialni.com/podcast/Niezatapialni_615.mp3`
- **THEN** the saved file SHALL be named `Niezatapialni_615.mp3`

### Requirement: Report download progress and errors

The downloader SHALL log progress and handle HTTP errors without crashing the full run.

#### Scenario: HTTP error response

- **WHEN** the server returns a non-200 status for an MP3 URL
- **THEN** the downloader SHALL log the URL, status code, and error, append the URL to `failed_downloads.txt`, and continue with the next episode

#### Scenario: Completed run summary

- **WHEN** all episodes have been processed
- **THEN** the downloader SHALL print a summary: total downloaded, already existed, skipped (no URL), failed

