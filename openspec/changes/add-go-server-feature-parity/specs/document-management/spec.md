## ADDED Requirements

### Requirement: Batch Document Upload

The system SHALL support uploading multiple documents in a single request via `POST /api/v2/documents/upload/batch`.

#### Scenario: Successful batch upload

- **WHEN** user sends multipart form with multiple files and valid auth
- **THEN** system creates documents for each file
- **AND** returns array of results with document IDs and status for each file

#### Scenario: Partial batch failure

- **WHEN** some files in batch fail validation (e.g., too large)
- **THEN** system creates documents for valid files
- **AND** returns error details for failed files
- **AND** HTTP status is 207 Multi-Status

#### Scenario: Batch size limits enforced

- **WHEN** batch exceeds maximum file count (default: 10)
- **THEN** system returns 400 Bad Request with clear error message

#### Scenario: Total size limits enforced

- **WHEN** combined file size exceeds limit (default: 50MB)
- **THEN** system returns 400 Bad Request with clear error message
