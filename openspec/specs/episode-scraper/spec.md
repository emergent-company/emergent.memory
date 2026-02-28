# episode-scraper Specification

## Purpose
TBD - created by archiving change niezatapialni-mp3-scraper. Update Purpose after archive.
## Requirements
### Requirement: Extract episode metadata from a post page

Given an episode post URL, the scraper SHALL extract the following fields using `colly` CSS selector callbacks:

- `episode_number` (integer, parsed from title e.g. "NZ615")
- `title` (full post heading string)
- `date` (publication date as ISO 8601 string, e.g. `2026-02-16`)
- `description` (teaser/excerpt text visible on the listing page, if present, or first paragraph of post body)
- `body` (full post body text, stripped of HTML)
- `mp3_url` (direct `.mp3` URL from the audio player embed or the "Play in new window" / "Download" link)
- `post_url` (the canonical URL of the episode post)

#### Scenario: Standard episode post

- **WHEN** the scraper fetches a standard episode post page containing a podcast player and episode number in the heading
- **THEN** it SHALL return a record with all seven fields populated and non-empty

#### Scenario: Post without an episode number

- **WHEN** the post heading does not begin with a recognised episode prefix ("NZ", "NZ ")
- **THEN** `episode_number` SHALL be `null` and all other available fields SHALL still be extracted

#### Scenario: Post without an MP3 link

- **WHEN** no `.mp3` URL is present anywhere on the post page
- **THEN** `mp3_url` SHALL be `null` and the record SHALL still be written with remaining fields

### Requirement: Extract all reader comments from a post page

The scraper SHALL extract every top-level reader comment from the post's comment section.

#### Scenario: Post has comments

- **WHEN** a post page contains one or more WordPress comments
- **THEN** the scraper SHALL return a list of comment objects, each containing:
  - `author` (display name)
  - `date` (ISO 8601 comment timestamp)
  - `body` (comment text, HTML-stripped)

#### Scenario: Post has no comments

- **WHEN** the comment section is absent or empty
- **THEN** the scraper SHALL return an empty list for the `comments` field

### Requirement: Tolerate scraping errors gracefully

The scraper SHALL not abort the entire run when a single episode post fails to load or parse.

#### Scenario: Network error on individual post

- **WHEN** colly fails to fetch a post URL (timeout, 5xx, DNS failure)
- **THEN** the scraper SHALL log the URL and error, record it in a `failed_urls.txt` file, and continue with the next URL

#### Scenario: Parsing error on individual post

- **WHEN** the HTML structure of a post does not match expectations and required fields cannot be extracted
- **THEN** the scraper SHALL log a warning, write a partial record with populated fields and `null` for missing ones, and continue

