# Changelog

All notable changes to the Emergent Go SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added (Phase 4 - In Progress)

**Test Infrastructure:**

- Test utilities - Mock HTTP server infrastructure (`testutil/mock.go`, 106 lines)
- Test fixtures - Reusable test data for all services (`testutil/fixtures.go`, 170 lines)

**Unit Tests (10 services tested, 43 test cases):**

- Projects service (11 tests) - List, Get, Create, Update, Delete, Members + error cases → 70.5% coverage
- Organizations service (4 tests) - CRUD operations → 66.2% coverage
- Users service (2 tests) - Profile management → 64.9% coverage
- API Tokens service (4 tests) - Token lifecycle → 66.2% coverage
- Health service (4 tests) - Health probes → 72.2% coverage
- MCP service (4 tests) - JSON-RPC operations → 61.5% coverage
- Documents service (7 tests) - List, Get + error/edge cases → 83.3% coverage
- Chunks service (2 tests) - List with filtering → 71.0% coverage
- Search service (3 tests) - Hybrid/semantic search + errors → 65.4% coverage
- Graph service (3 tests) - Objects, relationships → 61.7% coverage

**Examples (4 working programs):**

- `examples/basic/` - Basic SDK setup and health check
- `examples/documents/` - Document and chunk management
- `examples/search/` - Search with different strategies
- `examples/projects/` - Project CRUD workflow
- `examples/README.md` - Complete documentation with usage instructions

**Current Metrics:**

- **Total coverage**: 37.6% (up from 29.1%)
- **Tested services**: 10 of 11 (91%) - missing: Chat (SSE streaming)
- **Test files**: 11 test suites
- **Example files**: 4 programs + README
- **All tests passing**: ✅ 43/43

### Added (Phase 3 - Complete)

- **Projects service** - Complete CRUD operations (List, Get, Create, Update, Delete)
- **Projects service** - Member management (ListMembers, RemoveMember)
- **Organizations service** - CRUD operations (List, Get, Create, Delete)
- **Users service** - Profile management (GetProfile, UpdateProfile)
- **API Tokens service** - Token lifecycle (Create, List, Get, Revoke)
- **Health service** - Health probes (Health, Ready, Healthz for k8s)
- **MCP service** - Model Context Protocol JSON-RPC client
- **MCP service** - Tools, Resources, and Prompts support
- Updated main Client to integrate all 6 new services
- SetContext now updates all 11 service clients

### Added (Phase 2 - Complete)

- OAuth 2.0 device flow authentication with automatic token refresh
- OIDC discovery and credential storage
- Graph service client (objects, relationships, search)
- Chat service client with SSE streaming support
- Complete `NewWithDeviceFlow()` implementation
- Stream event parsing for real-time chat responses

### Added (Phase 1 - Complete)

- Initial SDK implementation
- Core client infrastructure with pluggable authentication
- API key authentication provider
- Documents service client (List, Get)
- Chunks service client (List with filtering)
- Search service client (unified search with fusion strategies)
- Structured error handling with type predicates
- Multi-tenancy context management (SetContext)
- Comprehensive README with quickstart examples

### Pending (Phase 4 - Remaining)

- Unit tests for Chat service (SSE streaming)
- Unit tests for Auth service (OAuth device flow)
- Coverage target: 80%+ (currently 36.9%)
- Example programs in `examples/` directory
- golangci-lint compliance

### Pending (Phase 5)

- Pagination iterators
- CLI migration (`tools/emergent-cli` → use SDK)
- Test client migration (`tests/api/client` → use SDK)
- v1.0.0 release preparation

## History

- **2026-02-11**: Phase 4 started - Test infrastructure created, 7 services tested (29.1% coverage)
- **2026-02-11**: Phase 3 implementation complete - Projects, Orgs, Users, API Tokens, Health, MCP services
- **2026-02-11**: Phase 2 implementation complete - OAuth, Graph, Chat with streaming
- **2026-02-11**: Phase 1 implementation complete - core SDK with Documents, Chunks, Search
