# Tasks for Go SDK Library

## 1. Project Setup

- [ ] 1.1 Create package structure at `apps/server-go/pkg/sdk/`
- [ ] 1.2 Add `go.mod` metadata (package path, description, license)
- [ ] 1.3 Create README.md with installation and quickstart
- [ ] 1.4 Set up CHANGELOG.md for version tracking
- [ ] 1.5 Create examples/ directory structure

## 2. Authentication Core

- [ ] 2.1 Implement `auth.Provider` interface
- [ ] 2.2 Implement API key provider (`auth/apikey.go`)
- [ ] 2.3 Implement OAuth provider with device flow (`auth/oauth.go`)
- [ ] 2.4 Implement OAuth token refresh logic
- [ ] 2.5 Implement credential storage/loading (`auth/credentials.go`)
- [ ] 2.6 Add unit tests for all auth providers
- [ ] 2.7 Add integration test for device flow (manual verification)

## 3. Main Client

- [ ] 3.1 Create `Client` struct with config (`sdk.go`)
- [ ] 3.2 Implement `New()` constructor with options pattern
- [ ] 3.3 Implement `NewWithDeviceFlow()` helper
- [ ] 3.4 Implement `SetContext()` for org/project defaults
- [ ] 3.5 Add request builder with auth injection
- [ ] 3.6 Add request option types (`WithOrg`, `WithProject`, etc.)
- [ ] 3.7 Add unit tests for client initialization
- [ ] 3.8 Add integration test against real server

## 4. Error Handling

- [ ] 4.1 Create `errors.Error` struct (`errors/errors.go`)
- [ ] 4.2 Implement error parsing from HTTP responses
- [ ] 4.3 Add error type predicates (`IsNotFound`, `IsForbidden`, etc.)
- [ ] 4.4 Add error wrapping for network errors
- [ ] 4.5 Add unit tests for all error scenarios
- [ ] 4.6 Document error handling in README

## 5. Documents Service

- [ ] 5.1 Create `documents.Client` struct (`documents/client.go`)
- [ ] 5.2 Define Document DTOs (`documents/types.go`)
- [ ] 5.3 Implement `List()` with pagination support
- [ ] 5.4 Implement `Get()` for single document
- [ ] 5.5 Implement `Create()` for new documents
- [ ] 5.6 Implement `Update()` for existing documents
- [ ] 5.7 Implement `Delete()` for document removal
- [ ] 5.8 Implement `Upload()` for multipart file upload
- [ ] 5.9 Implement `ListIter()` pagination iterator
- [ ] 5.10 Add unit tests for all document operations
- [ ] 5.11 Add integration tests against real server

## 6. Chunks Service

- [ ] 6.1 Create `chunks.Client` struct (`chunks/client.go`)
- [ ] 6.2 Define Chunk DTOs (`chunks/types.go`)
- [ ] 6.3 Implement `List()` with filters (documentID, search query)
- [ ] 6.4 Implement `Get()` for single chunk
- [ ] 6.5 Implement `ListIter()` pagination iterator
- [ ] 6.6 Add unit tests for all chunk operations
- [ ] 6.7 Add integration tests against real server

## 7. Search Service

- [ ] 7.1 Create `search.Client` struct (`search/client.go`)
- [ ] 7.2 Define Search request/response DTOs (`search/types.go`)
- [ ] 7.3 Implement `Search()` for unified search
- [ ] 7.4 Add support for fusion strategies (lexical, semantic, hybrid)
- [ ] 7.5 Add support for debug mode (`WithDebug()` option)
- [ ] 7.6 Add unit tests for all search operations
- [ ] 7.7 Add integration tests with debug mode validation

## 8. Graph Service

- [ ] 8.1 Create `graph.Objects` client (`graph/objects.go`)
- [ ] 8.2 Create `graph.Relationships` client (`graph/relationships.go`)
- [ ] 8.3 Create `graph.Search` client (`graph/search.go`)
- [ ] 8.4 Define Graph object DTOs (`graph/types.go`)
- [ ] 8.5 Implement object CRUD operations
- [ ] 8.6 Implement relationship CRUD operations
- [ ] 8.7 Implement graph search with filters
- [ ] 8.8 Implement object history/versioning support
- [ ] 8.9 Add unit tests for all graph operations
- [ ] 8.10 Add integration tests for full graph workflows

## 9. Chat Service with Streaming

- [ ] 9.1 Create `chat.Client` struct (`chat/client.go`)
- [ ] 9.2 Create `chat.Stream` struct for SSE (`chat/stream.go`)
- [ ] 9.3 Define Chat DTOs (`chat/types.go`)
- [ ] 9.4 Implement `ListConversations()` for conversation list
- [ ] 9.5 Implement `GetConversation()` for single conversation
- [ ] 9.6 Implement `CreateConversation()` for new conversation
- [ ] 9.7 Implement `SendMessage()` for non-streaming chat
- [ ] 9.8 Implement `SendMessageStream()` for streaming chat
- [ ] 9.9 Implement SSE event parsing and channel emission
- [ ] 9.10 Add stream cancellation on context cancel
- [ ] 9.11 Add unit tests for all chat operations
- [ ] 9.12 Add integration tests for streaming

## 10. Projects Service

- [ ] 10.1 Create `projects.Client` struct (`projects/client.go`)
- [ ] 10.2 Define Project DTOs (`projects/types.go`)
- [ ] 10.3 Implement `List()` with org filter
- [ ] 10.4 Implement `Get()` for single project
- [ ] 10.5 Implement `Create()` for new project
- [ ] 10.6 Implement `Update()` for existing project
- [ ] 10.7 Implement `Delete()` for project removal
- [ ] 10.8 Add unit tests for all project operations
- [ ] 10.9 Add integration tests against real server

## 11. Organizations Service

- [ ] 11.1 Create `orgs.Client` struct (`orgs/client.go`)
- [ ] 11.2 Define Organization DTOs (`orgs/types.go`)
- [ ] 11.3 Implement `List()` for all user's orgs
- [ ] 11.4 Implement `Get()` for single org
- [ ] 11.5 Implement `Create()` for new org
- [ ] 11.6 Implement `Update()` for existing org
- [ ] 11.7 Implement `Delete()` for org removal
- [ ] 11.8 Add unit tests for all org operations
- [ ] 11.9 Add integration tests against real server

## 12. Users Service

- [ ] 12.1 Create `users.Client` struct (`users/client.go`)
- [ ] 12.2 Define User DTOs (`users/types.go`)
- [ ] 12.3 Implement `Search()` for user search
- [ ] 12.4 Implement `GetProfile()` for current user profile
- [ ] 12.5 Implement `UpdateProfile()` for profile updates
- [ ] 12.6 Add unit tests for all user operations
- [ ] 12.7 Add integration tests against real server

## 13. API Tokens Service

- [ ] 13.1 Create `apitokens.Client` struct (`apitokens/client.go`)
- [ ] 13.2 Define API Token DTOs (`apitokens/types.go`)
- [ ] 13.3 Implement `List()` for project's API tokens
- [ ] 13.4 Implement `Create()` for new API token
- [ ] 13.5 Implement `Revoke()` for token revocation
- [ ] 13.6 Add unit tests for all token operations
- [ ] 13.7 Add integration tests against real server

## 14. Health Service

- [ ] 14.1 Create `health.Client` struct (`health/client.go`)
- [ ] 14.2 Define Health DTOs (`health/types.go`)
- [ ] 14.3 Implement `Health()` for health check
- [ ] 14.4 Implement `Ready()` for readiness check
- [ ] 14.5 Add unit tests for health operations
- [ ] 14.6 Add integration tests against real server

## 15. MCP Service

- [ ] 15.1 Create `mcp.Client` struct (`mcp/client.go`)
- [ ] 15.2 Define MCP DTOs (`mcp/types.go`)
- [ ] 15.3 Implement tool invocation
- [ ] 15.4 Implement resource access
- [ ] 15.5 Add unit tests for MCP operations
- [ ] 15.6 Add integration tests against real server

## 16. Testing Utilities

- [ ] 16.1 Create mock server (`testutil/mock.go`)
- [ ] 16.2 Implement configurable response handlers
- [ ] 16.3 Implement request tracking for assertions
- [ ] 16.4 Create fixture helpers (`testutil/fixtures.go`)
- [ ] 16.5 Add `ExampleDocument()`, `ExampleProject()`, etc. helpers
- [ ] 16.6 Add documentation for test utilities
- [ ] 16.7 Create example using mock server

## 17. Configuration Options

- [ ] 17.1 Create option types (`sdk.Option` interface)
- [ ] 17.2 Implement `WithHTTPClient()` option
- [ ] 17.3 Implement `WithRetry()` option with backoff
- [ ] 17.4 Implement `WithTimeout()` option
- [ ] 17.5 Implement `WithLogger()` option for request logging
- [ ] 17.6 Add unit tests for all options
- [ ] 17.7 Document all configuration options in README

## 18. Pagination Iterator

- [ ] 18.1 Create `Iterator` interface
- [ ] 18.2 Implement generic iterator pattern
- [ ] 18.3 Add auto-pagination logic (cursor-based)
- [ ] 18.4 Add early termination support
- [ ] 18.5 Add error accumulation (`Err()` method)
- [ ] 18.6 Add unit tests for iterator
- [ ] 18.7 Add integration tests with large result sets

## 19. Documentation

- [ ] 19.1 Write comprehensive README.md with:
  - [ ] Installation instructions
  - [ ] Quickstart guide
  - [ ] Authentication examples (both modes)
  - [ ] Common usage patterns
  - [ ] Error handling guide
- [ ] 19.2 Add godoc comments to all exported types
- [ ] 19.3 Add godoc comments to all exported functions
- [ ] 19.4 Add godoc examples for core operations
- [ ] 19.5 Create `examples/quickstart/main.go`
- [ ] 19.6 Create `examples/documents/main.go`
- [ ] 19.7 Create `examples/search/main.go`
- [ ] 19.8 Create `examples/chat/main.go`
- [ ] 19.9 Create `examples/graph/main.go`
- [ ] 19.10 Verify all examples compile and run

## 20. Migration and Integration

- [ ] 20.1 Update emergent-cli to import and use SDK
- [ ] 20.2 Refactor CLI client code to use `sdk.Client`
- [ ] 20.3 Update CLI auth logic to use SDK auth providers
- [ ] 20.4 Verify CLI functionality unchanged
- [ ] 20.5 Update test client (`tests/api/client`) to use SDK
- [ ] 20.6 Verify all E2E tests still pass
- [ ] 20.7 Add migration guide for internal code

## 21. Release Preparation

- [ ] 21.1 Run full test suite (unit + integration)
- [ ] 21.2 Run golangci-lint with strict mode
- [ ] 21.3 Run `go mod tidy` and verify dependencies
- [ ] 21.4 Create v1.0.0-rc1 release candidate
- [ ] 21.5 Test RC1 with external Go application
- [ ] 21.6 Fix any issues found in RC1
- [ ] 21.7 Create v1.0.0 final release
- [ ] 21.8 Publish release notes
- [ ] 21.9 Update main README with SDK documentation link
- [ ] 21.10 Announce SDK availability

## 22. Performance and Optimization

- [ ] 22.1 Add benchmarks for core operations
- [ ] 22.2 Profile memory allocation in hot paths
- [ ] 22.3 Optimize JSON marshaling/unmarshaling
- [ ] 22.4 Add connection pooling for HTTP client
- [ ] 22.5 Document performance characteristics
- [ ] 22.6 Add load testing example

## 23. Advanced Features (Post v1.0)

- [ ] 23.1 Add OpenTelemetry integration (opt-in)
- [ ] 23.2 Add structured logging (opt-in)
- [ ] 23.3 Add request/response middleware hooks
- [ ] 23.4 Add rate limit handling with backoff
- [ ] 23.5 Add circuit breaker pattern for resilience
- [ ] 23.6 Add batch operation helpers
- [ ] 23.7 Add code generation tool for custom types
