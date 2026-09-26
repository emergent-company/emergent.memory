## MODIFIED Requirements

### Requirement: Upload a document
The API SHALL accept a multipart file upload, ingest it as a new document, and return the resulting document id and chunk count. By default the upload SHALL attempt to detect whether the file is a new revision of an existing logical document (see `document-revision-auto-detect`); on a confident single match the API SHALL instead create a pending revision of that document. When the upload targets an existing document's revisions endpoint, the API SHALL create a new revision of that document without detection. Detection SHALL be disableable per request and an explicit revision target SHALL take precedence.

#### Scenario: Successful upload
- **WHEN** a client uploads a valid file that matches no existing document
- **THEN** the API ingests it as a new document and returns the new document id and its chunk count

#### Scenario: Successful revision upload
- **WHEN** a client uploads a valid file to a known document's revisions endpoint
- **THEN** the API creates a new pending revision of that document and returns the new revision id and version number

#### Scenario: Auto-detected revision upload
- **WHEN** a client uploads a valid file that confidently matches an existing document
- **THEN** the API creates a pending revision of that document and the response identifies the detected document

#### Scenario: Empty or invalid upload
- **WHEN** a client uploads an empty or unsupported file
- **THEN** the API rejects the request with a validation error

### Requirement: Authenticated access
The API SHALL require the same shared API-key trust boundary as the existing gateway endpoints and MUST NOT expose documents, chunks, revisions, revision diffs, or extraction/revision-apply actions to unauthenticated callers. Revision endpoints SHALL require the same document scopes as their non-revision equivalents (`documents:read` for reads, `documents:write` for revision creation and apply, `documents:delete` for discarding a revision).

#### Scenario: Unauthenticated request
- **WHEN** a client calls a document or revision endpoint without a valid API key
- **THEN** the API returns an unauthorized response and no document, revision, or diff data

#### Scenario: Insufficient scope on a revision write
- **WHEN** a client authenticated with only `documents:read` calls the revision create or apply endpoint
- **THEN** the API returns a forbidden response and the revision is not created or applied
