## MODIFIED Requirements

### Requirement: Upload a document

The API SHALL accept a multipart file upload, ingest it as a new document, and return the resulting document id and chunk count. The API SHALL also accept ingestion by URL and, for existing documents that record a source, a refresh operation that re-reads the source and reports whether the document changed.

#### Scenario: Successful upload

- **WHEN** a client uploads a valid file
- **THEN** the API ingests it and returns the new document id and its chunk count

#### Scenario: Empty or invalid upload

- **WHEN** a client uploads an empty or unsupported file
- **THEN** the API rejects the request with a validation error

#### Scenario: Ingest by URL

- **WHEN** a client submits a valid public URL for ingestion
- **THEN** the API ingests the page and returns the new document id and its `source_url`

#### Scenario: Refresh a sourced document

- **WHEN** a client refreshes a document that records a source
- **THEN** the API re-reads the source and reports whether the document content changed

#### Scenario: Refresh a document without a source

- **WHEN** a client refreshes a document that has no source
- **THEN** the API returns a validation error and does not modify the document
