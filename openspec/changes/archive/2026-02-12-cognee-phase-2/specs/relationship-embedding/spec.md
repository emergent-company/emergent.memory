## ADDED Requirements

### Requirement: Automatic triplet text generation from graph relationships

The system SHALL generate natural language triplet text for each graph relationship using a template-based approach combining source object name, humanized relation type, and target object name.

#### Scenario: Successful triplet generation with valid names

- **WHEN** creating a relationship between two objects that have `name` properties (e.g., "Elon Musk" FOUNDED_BY "Tesla")
- **THEN** system generates triplet text in format "{source.name} {humanized_type} {target.name}" (e.g., "Elon Musk founded by Tesla")

#### Scenario: Triplet generation with missing name property

- **WHEN** creating a relationship where source or target lacks a `name` property
- **THEN** system falls back to using the object's `key` field instead of `name`

#### Scenario: Relation type humanization

- **WHEN** generating triplet text with relation type "WORKS_FOR"
- **THEN** system converts underscores to spaces and lowercases to produce "works for"

### Requirement: Embedding generation via Vertex AI during relationship creation

The system SHALL embed the generated triplet text using the existing Vertex AI text-embedding-004 service (768 dimensions) during relationship creation within the same database transaction.

#### Scenario: Successful embedding with synchronous generation

- **WHEN** creating a new graph relationship
- **THEN** system calls Vertex AI embedding service with triplet text and receives 768-dimensional vector

#### Scenario: Embedding service failure handling

- **WHEN** Vertex AI embedding service returns error (rate limit, network failure, etc.)
- **THEN** system rolls back the entire relationship creation transaction and returns error to caller

#### Scenario: Embedding latency within acceptable bounds

- **WHEN** embedding generation completes successfully
- **THEN** total relationship creation time (including embedding) SHALL be less than 300ms at p95

### Requirement: Storage of embedding in graph_relationships table

The system SHALL store the generated embedding vector in a new `embedding` column (type: vector(768)) in the `kb.graph_relationships` table.

#### Scenario: New relationships have non-null embeddings

- **WHEN** creating a new relationship via API
- **THEN** system stores embedding in `embedding` column and value is NOT NULL

#### Scenario: Existing relationships have null embeddings initially

- **WHEN** querying relationships created before this feature deployment
- **THEN** their `embedding` column value is NULL (until backfill runs)

#### Scenario: Storage overhead per relationship

- **WHEN** calculating storage requirements for embedding column
- **THEN** each relationship adds approximately 3KB of storage (768 floats × 4 bytes)

### Requirement: Backward compatibility with existing relationship creation

The system SHALL maintain backward compatibility for all existing relationship creation endpoints without breaking changes.

#### Scenario: Existing API behavior unchanged

- **WHEN** client calls existing relationship creation endpoint
- **THEN** endpoint signature, request format, and response format remain unchanged (embedding generation happens transparently)

#### Scenario: Relationship properties unchanged

- **WHEN** creating relationship with custom properties
- **THEN** all existing property handling (storage, retrieval, validation) works identically to pre-embedding behavior
