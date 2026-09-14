## ADDED Requirements

### Requirement: Document extraction e2e test uses CLI for job management
The `TestCLIInstalled_DocumentExtractionWithSchema` test SHALL use `memory extraction jobs create` and `memory extraction jobs get` CLI commands instead of raw HTTP helper functions. All job operations SHALL be logged via `rl.CLI()`.

#### Scenario: Trigger extraction via CLI
- **WHEN** the test triggers an extraction job
- **THEN** it calls `memory extraction jobs create --project <id> --document <id> --output json` and logs the output via `rl.CLI()`

#### Scenario: Poll job status via CLI
- **WHEN** the test polls for extraction job completion
- **THEN** it calls `memory extraction jobs get <jobID> --output json` in a loop and logs each poll via `rl.Printf()`, breaking when status is `completed` or `failed`

#### Scenario: Run log has no empty sections
- **WHEN** the test completes
- **THEN** every section in the run log has at least one child event (no silent waiting phases)

---

### Requirement: Document conversion e2e test exercises Kreuzberg
A new test `TestCLIInstalled_DocumentConversion` SHALL upload a PDF fixture, poll until `conversionStatus` is `"completed"`, and assert the document has non-empty content.

#### Scenario: PDF upload returns conversion pending or completed status
- **WHEN** a PDF file is uploaded via `memory documents upload`
- **THEN** the upload response contains `conversionStatus` of `"pending"`, `"processing"`, or `"completed"`

#### Scenario: Document reaches completed conversion status
- **WHEN** the test polls `memory documents get <id> --output json` for up to 3 minutes
- **THEN** `conversionStatus` reaches `"completed"` before timeout

#### Scenario: Completed document has non-empty content
- **WHEN** `conversionStatus` is `"completed"`
- **THEN** the document JSON includes a non-empty `content` or `chunks` field confirming Kreuzberg extracted text
