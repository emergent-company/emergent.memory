## Context

Testing extraction performance systematically is challenging without a consistent, real-world test set. We have a set of reference documents in Google Drive (Folder ID: `16qesqkUSHJTdKZCMoZtMe9GtesDxGE0A`). We need a tool that bridges this data source and the Emergent platform, acting as an end-to-end client. We are doing this as a standalone Go application to evaluate the system externally via the public Emergent SDK, simulating a real user's integration.

## Goals / Non-Goals

**Goals:**

- Automated syncing of files from the designated Google Drive folder.
- Programmatic upload of documents to the "huma" project via the Emergent Go SDK.
- Collection and reporting of extraction performance (time elapsed, success rate, error categorization).
- Configurable concurrency to test system load.

**Non-Goals:**

- A complex UI or dashboard (CLI output is sufficient).
- Testing systems other than document extraction.
- Complex state management (e.g., a database for the test suite itself). Local file caching is acceptable.

## Decisions

- **Architecture (Standalone CLI App):** We will build a standalone Go command-line tool. _Rationale:_ This keeps the core repository clean from testing-specific dependencies and acts as a true end-to-end client, validating the SDK experience simultaneously.
- **Google Drive Authentication:** Use a Service Account key (JSON) provided via environment variables. _Rationale:_ Allows for headless execution in scripts or CI without manual OAuth flows.
- **File Sync Mechanism:** Download files locally to a temporary/cache directory before uploading to Emergent. _Rationale:_ Simplifies the upload process, prevents timeouts on large streaming transfers directly from Google Drive, and allows inspecting files if an upload fails.
- **Concurrency Model:** Use a Go worker pool with a configurable number of workers (e.g., via CLI flag). _Rationale:_ Allows us to throttle the upload rate to prevent hitting rate limits, or intentionally stress-test the system.
- **Reporting Format:** Output results to standard output in structured JSON or a summary table. _Rationale:_ Easy to read manually or pipe to other tools for historical tracking.

## Risks / Trade-offs

- **Risk: Google Drive API Rate Limits.**
  _Mitigation:_ Implement exponential backoff and retries when listing or downloading files.
- **Risk: Emergent API Rate Limits/Throttling.**
  _Mitigation:_ The worker pool concurrency limit will act as a throttle. The script should also handle `429 Too Many Requests` gracefully.
- **Trade-off: Local Disk Space.**
  _Mitigation:_ Caching files locally requires disk space equivalent to the Drive folder size. Since this is a test suite, we assume the host environment has sufficient temporary storage, and we can add a cleanup routine after the run.
