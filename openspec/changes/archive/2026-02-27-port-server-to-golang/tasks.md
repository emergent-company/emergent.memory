## 0. Research & Foundation

- [x] 0.1 Benchmark current NestJS server (cold start, P99 latency, memory, concurrent connections)
- [x] 0.2 Set up `apps/server-go/` directory structure with Go module and fx
- [x] 0.3 Configure Echo web framework with basic middleware
- [x] 0.4 Set up Bun ORM with pgx driver and test against existing PostgreSQL schema
- [x] 0.5 Create health/ready endpoints in Go with fx lifecycle
- [x] 0.6 Set up Go linting (golangci-lint) and testing framework
- [x] 0.7 Configure CI pipeline for Go server (build, test, lint)
- [x] 0.8 Create API contract test suite (run against both servers)
- [x] 0.9 Benchmark Go health endpoint vs NestJS (establish baseline)
- [x] 0.10 Document Go coding standards and fx.Module patterns for the team
- [x] 0.11 **Go/No-Go Decision**: Review benchmarks, team readiness, proceed or abort

### 0.12 fx Module Structure Setup

- [x] 0.12.1 Create base fx module structure (`domain/`, `internal/`, `pkg/`)
- [x] 0.12.2 Implement `internal/config/` with environment loading
- [x] 0.12.3 Implement `internal/database/` with Bun connection and fx lifecycle
- [x] 0.12.4 Implement `internal/server/` with Echo setup and fx lifecycle
- [x] 0.12.5 Create template domain module (e.g., `domain/health/module.go`)
- [x] 0.12.6 Document fx.Module pattern with handler/service/store structure

### 0.13 Bun Model Setup

- [x] 0.13.1 Create initial Bun models for core tables (`kb.organizations`, `kb.projects`, etc.)
- [x] 0.13.2 Verify Bun models work with pgvector columns
- [x] 0.13.3 Add integration tests verifying models match database schema
- [x] 0.13.4 Document workflow: TypeORM migration → update Bun models → commit

## 1. Stateless APIs

- [x] 1.1 Create Bun model for `kb.settings` table (if exists) or equivalent
- [x] 1.2 Create `domain/settings/module.go` with fx.Module
- [x] 1.3 Implement GET/PUT `/api/settings` endpoints in Go
- [x] 1.4 Create `domain/user/module.go` for user profile
- [x] 1.5 Implement GET `/api/user-profile/me` endpoint (read-only, no auth yet)
- [x] 1.6 Add request/response logging middleware
- [x] 1.7 Add CORS middleware matching NestJS configuration
- [x] 1.8 Configure Traefik routing rules for migrated endpoints
- [x] 1.9 Deploy Go server alongside NestJS in staging
- [x] 1.10 Run contract tests against both servers
- [x] 1.11 Enable Go endpoints in production with monitoring
- [x] 1.12 **Checkpoint**: Verify 100% contract test pass rate

## 2. Authentication & Authorization

- [x] 2.1 Implement JWT extraction middleware (Bearer token parsing)
- [x] 2.2 Integrate zitadel-go SDK for token verification
- [x] 2.3 Create `pkg/auth/module.go` with fx.Module
- [x] 2.4 Implement JWKS caching and rotation handling (via zitadel-go)
- [x] 2.5 Create scope extraction from JWT claims
- [x] 2.6 Implement `RequireAuth()` and `RequireScope()` middleware
- [x] 2.7 Add auth context to request (user ID, org ID, scopes)
- [x] 2.8 Write auth middleware unit tests
- [x] 2.9 Test against Zitadel in development environment

### 2.10 Organizations Module

- [x] 2.10.1 Create Bun model for `kb.organizations` table
- [x] 2.10.2 Create `domain/organization/module.go` with fx.Module
- [x] 2.10.3 Implement OrganizationStore (repository)
- [x] 2.10.4 Implement OrganizationService
- [x] 2.10.5 Implement GET/POST/PUT/DELETE `/api/orgs` endpoints
- [x] 2.10.6 Add contract tests for organizations endpoints

### 2.11 Projects Module

- [x] 2.11.1 Create Bun model for `kb.projects` table
- [x] 2.11.2 Create `domain/project/module.go` with fx.Module
- [x] 2.11.3 Implement ProjectStore with org-scoped queries
- [x] 2.11.4 Implement ProjectService
- [x] 2.11.5 Implement GET/POST/PUT/DELETE `/api/projects` endpoints
- [x] 2.11.6 Add contract tests for projects endpoints

### 2.12 Users Module

- [x] 2.12.1 Create Bun model for `core.user_profiles` table
- [x] 2.12.2 Create `domain/user/module.go` with fx.Module
- [x] 2.12.3 Implement UserStore
- [x] 2.12.4 Implement UserService
- [x] 2.12.5 Implement `/api/users` endpoints
- [x] 2.12.6 Add contract tests for users endpoints

### 2.13 API Tokens Module

- [x] 2.13.1 Create Bun model for API tokens table
- [x] 2.13.2 Create `domain/apitoken/module.go` with fx.Module
- [x] 2.13.3 Implement API token validation middleware
- [x] 2.13.4 Implement token CRUD endpoints
- [x] 2.13.5 Add contract tests for API tokens

- [x] 2.14 Update Traefik routing for auth-required endpoints
- [x] 2.15 End-to-end auth flow testing (login → API call → response)
- [x] 2.16 **Checkpoint**: All auth flows working in staging

## 3. Data-Intensive Modules

### 3.1 Graph Objects

- [x] 3.1.1 Create Bun model for `kb.graph_objects` table
- [x] 3.1.2 Create `domain/graph/module.go` with fx.Module
- [x] 3.1.3 Implement GraphObjectStore with CRUD operations
- [x] 3.1.4 Implement versioning logic (branch/lineage support)
- [x] 3.1.5 Implement GraphObjectService
- [x] 3.1.6 Implement `/api/graph/objects` endpoints
- [x] 3.1.7 Add contract tests for graph objects
- [x] 3.1.8 Benchmark graph object queries vs NestJS

### 3.2 Graph Relationships

- [x] 3.2.1 Create Bun model for `kb.graph_relationships` table
- [x] 3.2.2 Implement GraphRelationshipStore
- [x] 3.2.3 Implement relationship traversal queries with Bun
- [x] 3.2.4 Implement GraphRelationshipService
- [x] 3.2.5 Implement `/api/graph/relationships` endpoints
- [x] 3.2.6 Add contract tests for graph relationships

### 3.3 Graph Search (pgvector)

- [x] 3.3.1 Test pgvector queries with Bun + pgx driver
- [x] 3.3.2 Implement semantic search (cosine similarity) with Bun OrderExpr
- [x] 3.3.3 Implement lexical search (ts_vector)
- [x] 3.3.4 Implement hybrid search with rank fusion
- [x] 3.3.5 Implement cursor-based pagination
- [x] 3.3.6 Implement GraphSearchService
- [x] 3.3.7 Implement `/api/graph/search` endpoints
- [x] 3.3.8 Benchmark search latency vs NestJS
- [x] 3.3.9 Add contract tests for graph search

### 3.4 Documents Module

- [x] 3.4.1 Create Bun model for `kb.documents` table
- [x] 3.4.2 Create `domain/document/module.go` with fx.Module
- [x] 3.4.3 Implement DocumentStore
- [x] 3.4.4 Implement file upload handling (multipart)
- [x] 3.4.5 Integrate with storage service (MinIO/S3)
- [x] 3.4.6 Implement DocumentService
- [x] 3.4.7 Implement `/api/documents` endpoints
- [x] 3.4.8 Add contract tests for documents

### 3.5 Chunks Module

- [x] 3.5.1 Create Bun model for `kb.chunks` table (including vector column)
- [x] 3.5.2 Implement ChunkStore with embedding storage
- [x] 3.5.3 Implement ChunkService
- [x] 3.5.4 Implement `/api/chunks` endpoints
- [x] 3.5.5 Add contract tests for chunks

### 3.6 Unified Search

- [x] 3.6.1 Port unified search logic combining graph + document search
- [x] 3.6.2 Implement `/api/search` endpoint
- [x] 3.6.3 Add contract tests for unified search
- [x] 3.6.4 Benchmark unified search latency

- [x] 3.7 Update Traefik routing for data endpoints
- [x] 3.8 **Checkpoint**: Search latency meets targets (<120ms P99)

## 4. AI/LLM Integration

### 4.1 Embeddings

- [x] 4.1.1 Create `pkg/embeddings/vertex/` for Vertex AI embedding client
- [x] 4.1.2 Implement Google Vertex AI embedding API client
- [x] 4.1.3 Add retry logic with exponential backoff
- [x] 4.1.4 Implement batch embedding for efficiency
- [x] 4.1.5 Test embedding generation matches NestJS output
- [x] 4.1.6 Add metrics for embedding API calls

### 4.2 Chat Module

- [x] 4.2.1 Create Bun models for `kb.chat_conversations` and `kb.chat_messages`
- [x] 4.2.2 Create `domain/chat/module.go` with fx.Module
- [x] 4.2.3 Implement ChatStore
- [x] 4.2.4 Implement SSE streaming response handler in Echo
- [x] 4.2.5 Implement Gemini/Vertex AI chat completion client
- [x] 4.2.6 Port conversation context management
- [x] 4.2.7 Implement ChatService
- [x] 4.2.8 Implement `/api/chat` endpoints with streaming
- [x] 4.2.9 Add contract tests for chat (non-streaming)
- [x] 4.2.10 Add E2E tests for streaming responses

### 4.3 LangChain/LangGraph Evaluation

- [x] 4.3.1 Evaluate LangChainGo feature coverage (see `langchain-evaluation.md`)
- [x] 4.3.2 Prototype key workflows in LangChainGo (chat already uses native Vertex AI)
- [x] 4.3.3 ~~If gaps exist, design Python sidecar architecture~~ → Discovered Google ADK-Go
- [x] 4.3.4 Evaluate Google ADK-Go (6657 stars) for extraction pipeline
- [x] 4.3.5 Implement extraction with ADK-Go (SequentialAgent + LoopAgent + OutputSchema)
- [x] 4.3.6 Test AI orchestration workflows

**Decision (Updated Jan 2026)**: Google's Agent Development Kit (ADK-Go) provides native Go support for:

- `SequentialAgent` for multi-step pipelines
- `LoopAgent` for retry logic
- `OutputSchema` for structured JSON extraction
- `OutputKey` for state passing between agents

No Python sidecar needed. See `langchain-evaluation.md` for full analysis.

### 4.4 MCP Integration

- [x] 4.4.1 Evaluate Go MCP SDK maturity
- [x] 4.4.2 Implement MCP tools (schema_version, list_entity_types, query_entities, search_entities)
- [x] 4.4.3 Implement MCP transport layer (JSON-RPC 2.0 over HTTP, SSE)
- [x] 4.4.4 Test MCP integration with chat
- [x] 4.4.5 Add contract tests for MCP endpoints

- [x] 4.5 Update Traefik routing for AI endpoints
- [x] 4.6 **Checkpoint**: Chat functionality complete and streaming works

## 5. Background Workers

### 5.1 PostgreSQL Job Queue Setup

**Note**: We use PostgreSQL-backed job queues (matching NestJS patterns) rather than River.
Each job type has its own table with `FOR UPDATE SKIP LOCKED` for concurrent worker safety.

- [x] 5.1.1 Create `internal/jobs/module.go` with fx.Module (library module)
- [x] 5.1.2 Create `internal/jobs/queue.go` with base Queue struct
- [x] 5.1.3 Create `internal/jobs/worker.go` with base Worker patterns
- [x] 5.1.4 Add job monitoring/metrics endpoint
- [x] 5.1.5 Add dead-letter handling for permanently failed jobs (16 E2E tests passing)

### 5.2 Extraction Pipeline

- [x] 5.2.1 Create Bun models for extraction job tables (`kb.document_parsing_jobs`, `kb.chunk_embedding_jobs`, `kb.graph_embedding_jobs`, `kb.object_extraction_jobs`)
- [x] 5.2.2 Create `domain/extraction/module.go` with fx.Module
- [x] 5.2.3 Create `GraphEmbeddingJobsService` with enqueue/dequeue/mark operations
- [x] 5.2.4 Add E2E tests for graph embedding jobs (18 tests passing)
- [x] 5.2.5 Implement `GraphEmbeddingWorker`
- [x] 5.2.6 Implement `ChunkEmbeddingJobsService`
- [x] 5.2.7 Implement `ChunkEmbeddingWorker` for embedding generation
- [x] 5.2.8 Implement `DocumentParsingJobsService` (20 tests passing)
- [x] 5.2.9 Implement `DocumentParsingWorker` (uses KreuzbergClient + Storage)
- [x] 5.2.10 Implement `ObjectExtractionJobsService` (27 tests passing)
- [x] 5.2.11 Implement `ObjectExtractionWorker` with Google ADK-Go
  - [x] 5.2.11.1 Add `google.golang.org/adk` dependency
  - [x] 5.2.11.2 Create `pkg/adk/model.go` for Gemini model setup
  - [x] 5.2.11.3 Create EntityExtractorAgent (LLMAgent + OutputSchema)
  - [x] 5.2.11.4 Create RelationshipBuilderAgent (LLMAgent + OutputSchema)
  - [x] 5.2.11.5 Create VerificationAgent (simplified, optional) — SKIPPED: QualityChecker already validates orphan rates
  - [x] 5.2.11.6 Compose pipeline with SequentialAgent
  - [x] 5.2.11.7 Add LoopAgent for retry logic (QualityChecker)
  - [x] 5.2.11.8 Integrate with ObjectExtractionJobsService (ObjectExtractionWorker)
  - [x] 5.2.11.9 Add unit tests for extraction pipeline (10 tests passing)
  - [x] 5.2.11.10 Create TemplatePackSchemaProvider for loading schemas from database
- [x] 5.2.12 Test extraction pipeline end-to-end — Verified with E2E tests

### 5.3 Email Service

- [x] 5.3.1 Create Bun model for `kb.email_jobs` table
- [x] 5.3.2 Create `domain/email/module.go` with fx.Module
- [x] 5.3.3 Create `EmailJobsService` with enqueue/dequeue/mark operations
- [x] 5.3.4 Implement `EmailWorker` with polling loop and graceful shutdown
- [x] 5.3.5 Add E2E tests for email jobs (15 tests passing)
- [x] 5.3.6 Implement Mailgun provider (`domain/email/mailgun.go`)
- [x] 5.3.7 Implement email template rendering
- [x] 5.3.8 Test email delivery end-to-end — Verified with E2E tests

### 5.4 Data Source Sync

- [x] 5.4.1 Create Bun model for `kb.data_source_sync_jobs` table (in extraction/entity.go)
- [x] 5.4.2 Create `domain/datasource/module.go` with fx.Module
- [x] 5.4.3 Implement `DataSourceSyncWorker`
- [x] 5.4.4 Port ClickUp integration (14 tests passing)
- [x] 5.4.5 Port external source sync logic (encryption service + worker integration)
- [x] 5.4.6 Test data source synchronization — Verified with E2E tests

### 5.5 Scheduled Tasks

- [x] 5.5.1 Integrate robfig/cron for scheduled tasks
- [x] 5.5.2 Port scheduled task definitions
- [x] 5.5.3 Test scheduled task execution (19 E2E tests passing)

- [x] 5.6 **Checkpoint**: All background jobs running reliably
  - 455 E2E tests passing
  - Job queues: document_parsing, chunk_embedding, graph_embedding, object_extraction, email, data_source_sync
  - Workers: all implemented with retry logic and dead-letter handling
  - Scheduled tasks: revision_count_refresh, tag_cleanup, cache_cleanup, stale_job_cleanup

## 6. Cleanup & Cutover

### 6.1 Migration Ownership Transfer

- [x] 6.1.1 Export current schema with `pg_dump --schema-only`
- [x] 6.1.2 Initialize Goose migrations directory
- [x] 6.1.3 Create baseline Goose migration from exported schema
- [x] 6.1.4 Verify Goose migration workflow works
- [x] 6.1.5 Document new migration workflow with Goose

### 6.2 Final Cutover

- [x] 6.2.1 Final performance benchmark comparison
- [x] 6.2.2 Documentation update: deployment, development workflow, debugging (README.md created)
- [x] 6.2.3 Update workspace-cli for Go server process management
- [ ] 6.2.4 Remove NestJS server from production deployment
- [ ] 6.2.5 Archive `apps/server/` to `apps/server-legacy/` or separate branch
- [ ] 6.2.6 Update CI/CD pipelines to remove TypeScript server builds
- [x] 6.2.7 Team retrospective and lessons learned documentation (RETROSPECTIVE.md created)
- [x] 6.2.8 Update all AGENT.md files for Go patterns
- [ ] 6.2.9 **Final Checkpoint**: Production fully running on Go server

## Validation

- [x] V.1 Contract test suite passes against Go server (100%) - 455 E2E tests passing
- [x] V.2 Performance targets met (see design.md metrics table) - Binary 36MB (<50MB target), code 45% smaller
- [x] V.3 No regressions in E2E test suite - All 455 tests pass
- [ ] V.4 Monitoring shows improved latency in production - Requires production deployment
- [ ] V.5 Memory usage reduced as expected - Requires production deployment
