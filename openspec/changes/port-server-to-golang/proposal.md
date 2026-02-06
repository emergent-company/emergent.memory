# Change: Port Server from NestJS/TypeScript to Go

## Why

The current NestJS/TypeScript backend, while feature-complete, has performance limitations inherent to Node.js's single-threaded runtime. Go offers:

- **Lower latency**: Compiled language with no JIT warmup, faster cold starts
- **Better concurrency**: Native goroutines vs event loop for CPU-bound operations (embeddings, graph traversal)
- **Reduced memory**: Smaller memory footprint (~10-50x less than Node.js for similar workloads)
- **Single binary deployment**: No `node_modules`, simpler container images (~5-20MB vs ~500MB+)

The strangler fig approach allows gradual migration while maintaining production stability.

## What Changes

### Phase 0: Foundation (Research & Setup)

- Establish Go project structure within the monorepo
- Set up shared infrastructure (routing, middleware, database connections)
- Define API contract compatibility testing strategy
- Create migration playbook and rollback procedures

### Phase 1: Stateless APIs (Low Risk)

- Health/readiness endpoints
- Public configuration endpoints
- Simple CRUD modules (settings, user-profile)

### Phase 2: Auth & Core APIs

- JWT validation middleware (Zitadel integration)
- Scope-based authorization guards
- Organizations, Projects, Users modules
- API token management

### Phase 3: Data-Intensive Modules

- Graph module (objects, relationships, search)
- Documents module (upload, storage, metadata)
- Chunks and embeddings (pgvector integration)
- Unified search (hybrid semantic + lexical)

### Phase 4: AI/LLM Integration

- Chat module (conversations, streaming SSE)
- LangChain equivalent (LangChainGo or custom)
- Embedding generation (Google Gemini API)
- MCP integration

### Phase 5: Background Workers

- Extraction jobs (document processing pipeline)
- Email service (queue-based sending)
- Data source sync (ClickUp, external sources)
- Scheduled tasks (cron jobs)

### Phase 6: Cleanup & Cutover

- Remove NestJS server
- Update deployment configurations
- Archive TypeScript codebase

## Impact

### Affected Specs

- All server-side specifications (authentication, document-management, entity-extraction, chat-sdk-ui, etc.)
- No functional changes to specifications - API contracts remain identical
- New capability spec: `golang-migration` (migration patterns and compatibility)

### Affected Code

- **Primary**: `apps/server/` (complete rewrite to `apps/server-go/` or similar)
- **Secondary**: `tools/workspace-cli/` (process management updates)
- **Unchanged**: `apps/admin/` (frontend consumes same API contracts)

### **BREAKING** Changes

- Development workflow changes (Go toolchain required)
- Different debugging/profiling tools
- Migration period with dual servers
- Potential brief API inconsistencies during transition

## Risks

| Risk                             | Severity | Mitigation                                                           |
| -------------------------------- | -------- | -------------------------------------------------------------------- |
| Feature parity gaps              | High     | Comprehensive API contract tests; feature flags for gradual rollout  |
| Go ecosystem maturity for AI/LLM | Medium   | Evaluate LangChainGo, consider gRPC to Python sidecar for complex AI |
| Team learning curve              | Medium   | Establish Go coding standards early; pair programming                |
| Extended migration timeline      | Medium   | Strict phase gates; production-ready at each phase boundary          |
| OpenTelemetry instrumentation    | Low      | Go has mature OTEL libraries                                         |

## Success Metrics

- P99 API latency reduced by 40%+
- Memory usage reduced by 50%+
- Cold start time < 2 seconds (vs current ~15-20s)
- Container image size < 50MB
- Zero API contract regressions (validated by test suite)
