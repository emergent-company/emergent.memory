## 1. Project Setup

- [x] 1.1 Initialize a new Go module (`go mod init`) for the standalone test suite
- [x] 1.2 Add the required dependencies (Google Drive API client, Emergent Go SDK, `godotenv` or similar for env vars)
- [x] 1.3 Setup the CLI entrypoint structure (e.g., using `flag` or `cobra`) to accept commands/flags to run specific phases independently (e.g., `--phase download`, `--phase upload`, `--phase all`) and accept configuration (folder ID, concurrency level, and cache directory path)

## 2. Google Drive Sync Implementation (Download Phase)

- [x] 2.1 Implement Google Drive service account authentication using credentials from environment variables
- [x] 2.2 Create a function to list all files recursively within a given Google Drive Folder ID (`16qesqkUSHJTdKZCMoZtMe9GtesDxGE0A`)
- [x] 2.3 Implement download logic to fetch files to the local cache directory, skipping files that already exist and haven't been modified
- [x] 2.4 Add retry and exponential backoff logic for Google Drive API calls to handle rate limits

## 3. Emergent SDK Upload Implementation (Upload Phase)

- [x] 3.1 Initialize the Emergent SDK client using API keys and project settings from environment variables
- [x] 3.2 Create a worker pool implementation based on the configured concurrency level
- [x] 3.3 Implement the upload task: text files via inline-content Create API (unique content hash), binary files via multipart upload with autoExtract=true
- [x] 3.4 Handle `429 Too Many Requests` responses from the Emergent API with exponential backoff and retries during upload
- [x] 3.5 Record the resulting Document IDs and timestamps for each successfully uploaded file in a thread-safe data structure

## 4. Extraction Verification & Polling

- [x] 4.1 Implement a polling mechanism using the admin extraction-jobs API to check status per document
- [x] 4.2 Track documents that transition to a successful state and record their completion timestamp to calculate total elapsed time
- [x] 4.3 Track documents that transition to a failed state and record the error reason/code provided by the API
- [x] 4.4 Set a global timeout (30 min) to stop polling if documents remain in a pending state indefinitely

## 5. Reporting & Summary

- [x] 5.1 Aggregate the collected data into a structured report (Total Documents, Successes, Failures, Average Extraction Time, Error Breakdown)
- [x] 5.2 Implement logic to print the report to standard output in a readable summary table format
- [x] 5.3 (Optional) Implement an automated cleanup routine to clear the local file cache after the report is generated if requested via CLI flag
