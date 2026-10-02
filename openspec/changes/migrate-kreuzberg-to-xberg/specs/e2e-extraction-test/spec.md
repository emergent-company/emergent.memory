## MODIFIED Requirements

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
- **THEN** the document JSON includes a non-empty `content` or `chunks` field confirming xberg extracted text
