# go-sdk Specification

## Purpose
TBD - created by archiving change add-go-sdk-library. Update Purpose after archive.
## Requirements
### Requirement: SDK Client Initialization

The SDK SHALL provide a main `Client` struct that serves as the entry point for all API operations.

#### Scenario: Initialize with API key (standalone mode)

- **WHEN** user creates client with API key configuration
- **THEN** client authenticates all requests with `X-API-Key` header
- **AND** no OAuth credential files are created or required

#### Scenario: Initialize with OAuth (full deployment)

- **WHEN** user creates client with OAuth configuration
- **THEN** client loads credentials from specified path
- **AND** client auto-refreshes expired tokens
- **AND** client uses `Authorization: Bearer` header

#### Scenario: First-time OAuth via device flow

- **WHEN** user creates client with device flow option
- **THEN** SDK initiates OAuth device flow
- **AND** prints device code and verification URL
- **AND** polls token endpoint until user completes authentication
- **AND** stores credentials at configured path

### Requirement: Service Clients

The SDK SHALL provide dedicated service clients for each API domain, accessible as fields on the main `Client`.

#### Scenario: Access documents service

- **WHEN** user calls `client.Documents.List(ctx)`
- **THEN** SDK makes authenticated GET request to `/api/documents`
- **AND** returns typed `[]Document` response
- **AND** handles pagination automatically

#### Scenario: Access all service clients

- **WHEN** user initializes client
- **THEN** SDK provides `Documents`, `Chunks`, `Graph`, `Search`, `Chat`, `Projects`, `Orgs`, `Users`, `APITokens`, `Health`, `MCP` service clients
- **AND** all share the same authentication and context

### Requirement: Multi-Tenant Context

The SDK SHALL support organization and project context for multi-tenant API operations.

#### Scenario: Set default context on client

- **WHEN** user calls `client.SetContext(orgID, projectID)`
- **THEN** all subsequent API calls include `X-Org-ID` and `X-Project-ID` headers
- **AND** context persists across all service clients

#### Scenario: Override context per request

- **WHEN** user calls API method with `WithOrg()` and `WithProject()` options
- **THEN** SDK uses provided context for that request only
- **AND** default context remains unchanged

### Requirement: Authentication Providers

The SDK SHALL implement authentication via pluggable providers.

#### Scenario: API key provider

- **WHEN** client configured with API key
- **THEN** provider adds `X-API-Key: {key}` header to all requests
- **AND** no refresh logic is needed

#### Scenario: OAuth provider with token refresh

- **WHEN** OAuth access token is expired
- **THEN** provider automatically refreshes using refresh token
- **AND** updates stored credentials
- **AND** retries original request with new token

#### Scenario: OAuth provider without refresh token

- **WHEN** OAuth access token is expired and no refresh token available
- **THEN** provider returns error prompting re-authentication
- **AND** user must run device flow again

### Requirement: Type-Safe DTOs

The SDK SHALL provide strongly-typed Go structs for all API request and response types.

#### Scenario: Document DTO with validation

- **WHEN** user creates `CreateDocumentRequest` struct
- **THEN** SDK validates required fields before sending request
- **AND** returns validation error if fields missing
- **AND** provides helpful error messages

#### Scenario: Response unmarshaling

- **WHEN** SDK receives API response
- **THEN** SDK unmarshals JSON into typed struct
- **AND** returns structured error if unmarshal fails
- **AND** exposes response fields via Go struct fields

### Requirement: Error Handling

The SDK SHALL provide structured error types matching server API error responses.

#### Scenario: HTTP error response

- **WHEN** server returns 404 with JSON error body
- **THEN** SDK returns `*Error` type with `StatusCode`, `Code`, `Message`
- **AND** error implements Go `error` interface
- **AND** caller can type-assert to `*Error` for structured access

#### Scenario: Network error

- **WHEN** HTTP request fails due to network issue
- **THEN** SDK wraps network error in `*Error` type
- **AND** sets `Code` to `"network_error"`
- **AND** preserves underlying error as `Cause`

### Requirement: Streaming Support

The SDK SHALL support Server-Sent Events (SSE) streaming for real-time operations.

#### Scenario: Chat message streaming

- **WHEN** user calls `client.Chat.SendMessageStream(ctx, conversationID, message)`
- **THEN** SDK returns stream object with `Events()` channel
- **AND** channel emits token events as server sends them
- **AND** stream closes when server sends done event
- **AND** stream propagates errors via error events

#### Scenario: Stream cancellation

- **WHEN** user cancels context during streaming
- **THEN** SDK closes HTTP connection immediately
- **AND** stream channel closes
- **AND** no more events are emitted

### Requirement: Pagination Support

The SDK SHALL provide iterator pattern for paginated list operations.

#### Scenario: Iterate through all documents

- **WHEN** user creates `client.Documents.ListIter(ctx, options)`
- **THEN** SDK returns iterator with `Next()`, `Document()`, `Err()` methods
- **AND** `Next()` fetches next page automatically when needed
- **AND** iterator stops when no more pages exist

#### Scenario: Early iteration termination

- **WHEN** user breaks from iterator loop
- **THEN** SDK stops fetching additional pages
- **AND** network resources are released

### Requirement: Graph Operations

The SDK SHALL provide comprehensive graph object and relationship management.

#### Scenario: Create graph object

- **WHEN** user calls `client.Graph.Objects.Create(ctx, object)`
- **THEN** SDK creates object via POST to `/api/graph/objects`
- **AND** returns created object with generated ID
- **AND** handles type schema validation

#### Scenario: Create relationship

- **WHEN** user calls `client.Graph.Relationships.Create(ctx, fromID, toID, relationshipType)`
- **THEN** SDK creates relationship via POST
- **AND** validates that objects exist
- **AND** returns relationship with metadata

#### Scenario: Graph search with debug mode

- **WHEN** user calls `client.Graph.Search.Search(ctx, query, WithDebug(true))`
- **THEN** SDK includes `?debug=true` query parameter
- **AND** response includes debug timing information
- **AND** requires `search:debug` scope

### Requirement: File Upload

The SDK SHALL support multipart file upload for documents.

#### Scenario: Upload document file

- **WHEN** user calls `client.Documents.Upload(ctx, file, metadata)`
- **THEN** SDK creates multipart form request
- **AND** sets proper `Content-Type: multipart/form-data` header
- **AND** uploads file with metadata in single request
- **AND** returns document ID

### Requirement: Testing Utilities

The SDK SHALL provide test utilities for SDK users to test their code.

#### Scenario: Mock server for offline testing

- **WHEN** user imports `sdk/testutil` package
- **THEN** SDK provides `NewMockServer()` function
- **AND** mock server implements all API endpoints with configurable responses
- **AND** mock tracks requests for assertions

#### Scenario: Fixture data helpers

- **WHEN** user imports test fixtures
- **THEN** SDK provides `ExampleDocument()`, `ExampleProject()`, etc. helpers
- **AND** fixtures generate valid test data
- **AND** fixtures are customizable via options

### Requirement: Context Propagation

The SDK SHALL accept `context.Context` as first parameter for all API operations to support cancellation, timeouts, and tracing.

#### Scenario: Request timeout

- **WHEN** user creates context with timeout `ctx, cancel := context.WithTimeout(ctx, 5*time.Second)`
- **THEN** SDK cancels request if timeout exceeded
- **AND** returns `context.DeadlineExceeded` error

#### Scenario: Request cancellation

- **WHEN** user cancels context during API call
- **THEN** SDK aborts in-flight request
- **AND** returns `context.Canceled` error

### Requirement: Configuration Options

The SDK SHALL support flexible configuration via option pattern.

#### Scenario: Custom HTTP client

- **WHEN** user calls `sdk.New(config, sdk.WithHTTPClient(customClient))`
- **THEN** SDK uses provided HTTP client for all requests
- **AND** user can configure custom transport, timeouts, proxies

#### Scenario: Retry configuration

- **WHEN** user calls `sdk.New(config, sdk.WithRetry(3, backoff.Exponential()))`
- **THEN** SDK retries failed requests up to 3 times
- **AND** uses exponential backoff between retries
- **AND** only retries idempotent operations (GET, PUT, DELETE)

### Requirement: Documentation and Examples

The SDK SHALL include comprehensive godoc documentation and runnable examples.

#### Scenario: Godoc for all public APIs

- **WHEN** user runs `go doc sdk.Client`
- **THEN** godoc shows detailed usage documentation
- **AND** all exported functions, types, fields have comments
- **AND** examples are included in godoc

#### Scenario: Quickstart example

- **WHEN** user reads `examples/quickstart/main.go`
- **THEN** example demonstrates client initialization, authentication, basic CRUD operations
- **AND** example compiles and runs successfully
- **AND** example includes error handling

### Requirement: Backward Compatibility

The SDK SHALL maintain semantic versioning and backward compatibility guarantees.

#### Scenario: Non-breaking changes in minor versions

- **WHEN** SDK releases minor version (e.g., 1.1.0 → 1.2.0)
- **THEN** existing code continues to compile without changes
- **AND** no exported types, functions, or fields are removed
- **AND** new features are additive only

#### Scenario: Deprecation warnings

- **WHEN** SDK deprecates functionality
- **THEN** deprecation is clearly marked in godoc
- **AND** alternative approach is documented
- **AND** deprecated functionality remains for at least 2 minor versions

