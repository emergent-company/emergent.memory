# document-api Specification

## Purpose
Provides the gateway HTTP API for browsing, uploading, and extracting documents stored in the Emergent Memory service, under the shared API-key trust boundary.

## Requirements

### Requirement: List documents

The API SHALL list ingested documents, each with its identifier, name, chunk count, and extraction status.

#### Scenario: Documents present

- **WHEN** a client lists documents and ingested documents exist
- **THEN** the API returns those documents, each with its id, name, chunk count, and extraction status

#### Scenario: No documents

- **WHEN** a client lists documents and none exist
- **THEN** the API returns an empty list, not an error

### Requirement: Get a document

The API SHALL return a single document's detail, including its name, mime type, chunk count, extraction status, and extracted object count when available.

#### Scenario: Known document

- **WHEN** a client requests a known document's detail
- **THEN** the API returns that document's detail fields

#### Scenario: Unknown document

- **WHEN** a client requests a document id that does not exist
- **THEN** the API returns a not-found error response, not a crash

### Requirement: List a document's chunks

The API SHALL list the chunks of a document in order, each with its index, size, embedding status, and text.

#### Scenario: Document has chunks

- **WHEN** a client lists chunks for a document that has chunks
- **THEN** the API returns the chunks in index order, each with its text and embedding status

#### Scenario: Document has no chunks

- **WHEN** a client lists chunks for a document with no chunks
- **THEN** the API returns an empty list, not an error

### Requirement: Upload a document

The API SHALL accept a multipart file upload, ingest it as a new document, and return the resulting document id and chunk count.

#### Scenario: Successful upload

- **WHEN** a client uploads a valid file
- **THEN** the API ingests it and returns the new document id and its chunk count

#### Scenario: Empty or invalid upload

- **WHEN** a client uploads an empty or unsupported file
- **THEN** the API rejects the request with a validation error

### Requirement: Trigger extraction

The API SHALL let a client trigger an extraction job for a document and return a reference to the resulting job.

#### Scenario: Extraction triggered

- **WHEN** a client triggers extraction for a known document
- **THEN** the API returns a reference to the created extraction job

#### Scenario: Unknown document

- **WHEN** a client triggers extraction for a document id that does not exist
- **THEN** the API returns a not-found error response

### Requirement: Authenticated access

The API SHALL require the same shared API-key trust boundary as the existing gateway endpoints and MUST NOT expose documents, chunks, or extraction actions to unauthenticated callers.

#### Scenario: Unauthenticated request

- **WHEN** a client calls a document endpoint without a valid API key
- **THEN** the API returns an unauthorized response and no document data
