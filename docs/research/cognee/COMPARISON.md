# Cognee vs Emergent: Comprehensive Comparison

**Analysis Date:** February 11, 2026  
**Cognee Version:** Latest from `topoteretes/cognee` (Apache 2.0)  
**Emergent Version:** Server-Go implementation (in production)

---

## Executive Summary

Both Cognee and Emergent are knowledge management systems that transform documents into searchable knowledge graphs. However, they differ significantly in architecture, tech stack, and core workflows:

| Aspect                | Cognee                                  | Emergent                                         |
| --------------------- | --------------------------------------- | ------------------------------------------------ |
| **Primary Language**  | Python 3.10-3.13                        | Go 1.24+ (previously TypeScript/NestJS)          |
| **Web Framework**     | FastAPI                                 | Echo (Go HTTP framework)                         |
| **Core Workflow**     | `add() → cognify() → search()`          | Ingest → Extract → Search                        |
| **Graph Databases**   | Kuzu (default), Neo4j, Neptune          | PostgreSQL + pgvector (graph emulated in schema) |
| **Vector Databases**  | LanceDB (default), ChromaDB, PGVector   | PostgreSQL + pgvector (single database)          |
| **Relational DB**     | SQLite (default), PostgreSQL            | PostgreSQL 17 (primary database)                 |
| **LLM Integration**   | Instructor (structured outputs), OpenAI | Google ADK-Go, Vertex AI                         |
| **Architecture**      | Pipeline-based task composition         | Domain-driven fx modules                         |
| **Multi-tenancy**     | User → Dataset → Data hierarchy         | Org → Project → Document hierarchy               |
| **Search Strategies** | 15+ retrieval types (pluggable)         | Hybrid (FTS + vector) unified search             |
| **License**           | Apache 2.0 (open source)                | Proprietary                                      |

---

## 1. Architecture Comparison

### 1.1 System Architecture

**Cognee:**

- **Pattern**: Pipeline-based task orchestration
- **Structure**: Modular tasks composed into pipelines (ECL: Extract → Cognify → Load)
- **Database Strategy**: Multi-backend adapters (interface-based)
- **Concurrency**: Async/await (Python asyncio)
- **Dependency Injection**: Manual composition in modules
- **Data Flow**: Raw Data → DocumentChunks → Graph Nodes/Edges → Search

**Emergent:**

- **Pattern**: Domain-driven design with fx dependency injection
- **Structure**: 19 domain modules (agents, chat, chunks, documents, extraction, graph, search, etc.)
- **Database Strategy**: Single PostgreSQL with schema separation (`kb.*`, `core.*`)
- **Concurrency**: Go routines with context cancellation
- **Dependency Injection**: Uber fx (explicit wiring)
- **Data Flow**: Document Upload → Parsing (Kreuzberg) → Chunking → Embedding → Graph Extraction → Search

### 1.2 Module Organization

**Cognee Structure:**

```
cognee/
├── api/              # FastAPI versioned routers (v1, v2)
├── cli/              # CLI entry points
├── infrastructure/   # Databases, LLM providers, embeddings
│   ├── databases/
│   │   ├── graph/    # Kuzu, Neo4j, Neptune adapters
│   │   ├── vector/   # LanceDB, ChromaDB, PGVector adapters
│   │   └── relational/ # SQLite, PostgreSQL adapters
│   └── llm/
│       ├── extraction/  # Instructor-based graph extraction
│       └── prompts/     # Jinja templates for prompts
├── modules/          # Domain logic (graph, retrieval, ontology)
├── tasks/            # Reusable pipeline tasks
└── shared/           # Cross-cutting helpers
```

**Emergent Structure:**

```
server-go/
├── cmd/
│   ├── server/       # Main entry point (fx composition)
│   └── migrate/      # Migration CLI
├── domain/           # Business logic (19 domains)
│   ├── documents/    # Document CRUD + upload
│   ├── chunks/       # Chunks with embeddings
│   ├── extraction/   # ADK-Go pipeline + job queue
│   ├── graph/        # Graph objects + relationships
│   ├── search/       # Unified search (FTS + vector)
│   └── ...
├── internal/         # Private packages
│   ├── auth/         # Zitadel middleware
│   ├── database/     # Bun ORM + pgx driver
│   └── server/       # Echo HTTP setup
└── pkg/              # Public packages
    ├── adk/          # Google ADK-Go agents
    ├── embeddings/   # Vertex AI embeddings
    └── kreuzberg/    # Document parsing client
```

---

## 2. Technology Stack Comparison

### 2.1 Core Technologies

| Layer                | Cognee                                  | Emergent                                |
| -------------------- | --------------------------------------- | --------------------------------------- |
| **Backend Language** | Python 3.10-3.13                        | Go 1.24+                                |
| **Web Framework**    | FastAPI                                 | Echo (Go)                               |
| **ORM**              | SQLAlchemy                              | Bun (Uptrace)                           |
| **Database Driver**  | psycopg2 / aiooqlite                    | pgx (Go)                                |
| **Primary Database** | SQLite (dev), PostgreSQL (prod)         | PostgreSQL 17                           |
| **Graph Database**   | Kuzu, Neo4j, Neptune (pluggable)        | PostgreSQL (emulated with schema)       |
| **Vector Database**  | LanceDB, ChromaDB, PGVector (pluggable) | PostgreSQL + pgvector                   |
| **LLM Integration**  | Instructor + OpenAI                     | Google ADK-Go + Vertex AI               |
| **Embedding Model**  | Configurable (OpenAI, Vertex, etc.)     | Vertex AI `text-embedding-004`          |
| **Auth**             | Custom (JWT in examples)                | Zitadel (OAuth2/OIDC)                   |
| **Storage**          | Local filesystem                        | MinIO/S3                                |
| **Job Queue**        | Built-in async tasks                    | Built-in job queues (polling-based)     |
| **Testing**          | pytest (unit, integration, E2E)         | testify (E2E suites, 609 tests passing) |

### 2.2 Database Adapters

**Cognee: Multi-Backend Flexibility**

Cognee uses **interface-based adapters** to support multiple backends:

**Graph Databases:**

- **Kuzu** (default) - Embedded OLAP graph database (~600 LOC adapter)
- **Neo4j** - Industry-standard graph database (~400 LOC adapter)
- **AWS Neptune** - Managed graph service (~300 LOC adapter)

**Vector Databases:**

- **LanceDB** (default) - Embedded vector search (~200 LOC adapter)
- **ChromaDB** - Embedded vector database (~300 LOC adapter)
- **PGVector** - PostgreSQL extension (~400 LOC adapter)

**Interface:** `GraphDBInterface` (416 LOC) defines 20+ abstract methods:

- `add_node()`, `add_nodes()`, `add_edge()`, `add_edges()`
- `get_node()`, `get_nodes()`, `get_neighbors()`
- `query()`, `has_edge()`, `delete_graph()`, `get_graph_metrics()`
- Decorator: `@record_graph_changes` tracks all graph mutations in relational ledger

**Emergent: Single-Database Simplicity**

Emergent uses **PostgreSQL for everything**:

- **Graph**: Emulated with `kb.graph_objects` and `kb.graph_relationships` tables
- **Vector**: pgvector extension for embeddings (`kb.chunks`, `kb.graph_objects` with `embedding` columns)
- **Relational**: Core data in `core.*` schema (`user_profiles`, `organizations`, `projects`)
- **Full-Text Search**: Built-in PostgreSQL FTS with GIN indexes

**Trade-offs:**

- **Cognee**: Flexibility at the cost of adapter maintenance (3 graph + 3 vector adapters)
- **Emergent**: Simplicity at the cost of vendor lock-in (PostgreSQL-only)

---

## 3. Graph Extraction Pipeline

### 3.1 Cognee: Instructor-Based Extraction

**Workflow:**

```python
# cognee/tasks/graph/extract_graph_from_data.py
async def extract_graph_from_data(
    data_chunks: List[DocumentChunk],
    graph_model: Type[BaseModel],
    config: Config = None,
    custom_prompt: Optional[str] = None,
):
    # 1. Extract graphs from chunks concurrently
    chunk_graphs = await asyncio.gather(
        *[extract_content_graph(chunk.text, graph_model, custom_prompt)
          for chunk in data_chunks]
    )

    # 2. Validate edges (filter missing source/target nodes)
    for graph in chunk_graphs:
        valid_node_ids = {node.id for node in graph.nodes}
        graph.edges = [e for e in graph.edges
                       if e.source_node_id in valid_node_ids
                       and e.target_node_id in valid_node_ids]

    # 3. Integrate with ontology and store
    return await integrate_chunk_graphs(
        data_chunks, chunk_graphs, graph_model, ontology_resolver
    )
```

**Key Features:**

- **Instructor Library**: Uses `instructor` for structured LLM outputs (Pydantic models)
- **Ontology Validation**: Optional ontology resolver validates entities against schema
- **Concurrent Extraction**: `asyncio.gather()` for parallel LLM calls
- **Edge Validation**: Filters edges with missing nodes before storage
- **Triplet Embedding**: Optional embeddings for graph edges (for semantic search)

**LLM Integration:**

```python
# cognee/infrastructure/llm/extraction/knowledge_graph/extract_content_graph.py
async def extract_content_graph(
    content: str,
    response_model: Type[BaseModel],
    custom_prompt: Optional[str] = None
):
    system_prompt = custom_prompt or render_prompt('graph_prompt.txt')

    # Instructor wraps OpenAI to return structured Pydantic model
    content_graph = await LLMGateway.acreate_structured_output(
        content, system_prompt, response_model
    )

    return content_graph
```

### 3.2 Emergent: ADK-Go Agent Pipeline

**Workflow:**

```go
// apps/server-go/domain/extraction/object_extraction_jobs.go
func (s *ObjectExtractionJobsService) CreateJob(ctx context.Context, opts CreateObjectExtractionJobOptions) (*ObjectExtractionJob, error) {
    // 1. Create job in database (pending status)
    job := &ObjectExtractionJob{
        ProjectID:        opts.ProjectID,
        DocumentID:       opts.DocumentID,
        ChunkID:          opts.ChunkID,
        JobType:          JobTypeFullExtraction,
        Status:           JobStatusPending,
        EnabledTypes:     opts.EnabledTypes, // Entity types to extract
        ExtractionConfig: opts.ExtractionConfig, // LLM settings
    }

    // 2. Insert job into kb.object_extraction_jobs
    _, err := s.db.NewInsert().Model(job).Exec(ctx)

    return job, err
}

// Worker dequeues and processes jobs
func (w *ObjectExtractionWorker) ProcessJob(ctx context.Context, job *ObjectExtractionJob) error {
    // 1. Load project schemas (from template pack)
    schemas, err := w.schemaProvider.GetProjectSchemas(ctx, job.ProjectID)

    // 2. Call ADK-Go agent to extract entities/relationships
    result, err := w.adkClient.Extract(ctx, &adk.ExtractionRequest{
        Content:      chunkContent,
        Schemas:      schemas,
        EnabledTypes: job.EnabledTypes,
    })

    // 3. Validate and insert graph objects
    for _, obj := range result.Objects {
        validated, err := validateProperties(obj.Properties, schema)
        graphSvc.Create(ctx, projectID, obj)
    }

    // 4. Insert relationships
    for _, rel := range result.Relationships {
        graphSvc.CreateRelationship(ctx, projectID, rel)
    }

    return nil
}
```

**Key Features:**

- **Job Queue**: Polling-based job queue with `FOR UPDATE SKIP LOCKED` (no external broker)
- **ADK-Go Agents**: Google Agent Development Kit for LLM orchestration
- **Schema Validation**: Template pack schemas (JSON Schema) validate properties
- **Concurrent Workers**: Multiple workers poll for jobs (configurable batch size)
- **Retry Logic**: Built-in retry with exponential backoff (default: 3 retries)
- **Stale Job Recovery**: Automatically recovers jobs stuck in "processing" (30 min threshold)

**LLM Integration:**

- Google Vertex AI `gemini-2.0-flash-exp` for extraction
- Structured outputs via schema definitions
- Embeddings via `text-embedding-004`

### 3.3 Comparison: Extraction Pipeline

| Aspect                  | Cognee                              | Emergent                                  |
| ----------------------- | ----------------------------------- | ----------------------------------------- |
| **LLM Library**         | Instructor (Pydantic)               | Google ADK-Go (agents)                    |
| **Concurrency**         | `asyncio.gather()`                  | Go routines + job queue                   |
| **Validation**          | Ontology resolver (optional)        | JSON Schema validation (required)         |
| **Edge Filtering**      | Pre-storage validation              | Post-extraction validation                |
| **Embedding**           | Optional triplet embedding          | Required for objects (graph search)       |
| **Retry Logic**         | Not visible (may be in LLM gateway) | Built-in job queue retry (3 attempts)     |
| **Stale Job Handling**  | Not visible                         | Automatic recovery (30 min threshold)     |
| **Parallel Extraction** | Per-chunk concurrency               | Project-level concurrency (1 job/project) |

---

## 4. Retrieval Strategies

### 4.1 Cognee: Pluggable Retrieval System

Cognee has **15+ retrieval strategies** implementing `BaseRetriever`:

**Retriever Workflow (3-step pipeline):**

```python
# cognee/modules/retrieval/base_retriever.py
class BaseRetriever(ABC):
    @abstractmethod
    async def get_retrieved_objects(self, query: str) -> Any:
        """Fetch raw data (e.g., Graph Edges, Vector chunks)"""
        pass

    @abstractmethod
    async def get_context_from_objects(self, query: str, retrieved_objects: Any) -> str:
        """Process raw data into LLM-ready context (e.g., text string)"""
        pass

    @abstractmethod
    async def get_completion_from_context(self, query: str, retrieved_objects: Any, context: Any) -> Union[List[str], List[dict]]:
        """Generate final response using LLM + context"""
        pass

    async def get_completion(self, query: str):
        """Full pipeline: retrieve → contextualize → complete"""
        objects = await self.get_retrieved_objects(query)
        context = await self.get_context_from_objects(query, objects)
        return await self.get_completion_from_context(query, objects, context)
```

**Available Retrievers:**

1. **GraphCompletionRetriever**: Triplet search → LLM completion
2. **GraphCompletionCoTRetriever**: Chain-of-thought reasoning on graph
3. **GraphSummaryCompletionRetriever**: Hierarchical summarization
4. **EntityCompletionRetriever**: Entity-centric search
5. **TripletRetriever**: Raw triplet retrieval (no LLM)
6. **TemporalRetriever**: Time-aware graph traversal
7. **NaturalLanguageRetriever**: Vector search only
8. **LexicalRetriever**: Keyword-based search
9. **CompletionRetriever**: RAG completion (vector + LLM)
10. **ChunksRetriever**: Raw chunk retrieval
11. **CypherSearchRetriever**: Direct Cypher query execution
12. **SummariesRetriever**: Summarized content retrieval
13. **CodingRulesRetriever**: Code-specific retrieval
14. **JaccardRetrieval**: Similarity-based retrieval
15. **Base community retrievers** (pluggable)

**Example: GraphCompletionRetriever**

```python
# cognee/modules/retrieval/graph_completion_retriever.py
class GraphCompletionRetriever(BaseRetriever):
    async def get_retrieved_objects(self, query: str) -> List[Edge]:
        # Brute-force triplet search (vector search on nodes + edges)
        triplets = await brute_force_triplet_search(
            query, top_k=5, triplet_distance_penalty=3.5
        )

        # Update access timestamps for nodes
        entity_nodes = get_entity_nodes_from_triplets(triplets)
        await update_node_access_timestamps(entity_nodes)

        return triplets

    async def get_context_from_objects(self, query, retrieved_objects) -> str:
        # Convert triplets to text: "Node1 -[RELATIONSHIP]-> Node2"
        return await resolve_edges_to_text(retrieved_objects)

    async def get_completion_from_context(self, query, retrieved_objects, context):
        # Generate LLM completion with context + conversation history
        completion = await generate_completion(
            query=query,
            context=context,
            conversation_history=conversation_history,
            response_model=self.response_model,
        )

        # Optionally save interaction to graph (for feedback loop)
        if self.save_interaction:
            await self.save_qa(question=query, answer=completion, context=context, triplets=retrieved_objects)

        return [completion]
```

### 4.2 Emergent: Unified Hybrid Search

Emergent has **unified search service** combining FTS + vector:

```go
// apps/server-go/domain/search/service.go
type Service struct {
    repo *Repository
    log  *slog.Logger
}

func (s *Service) Search(ctx context.Context, params SearchParams) (*SearchResponse, error) {
    // 1. Execute hybrid search (FTS + vector + graph metadata)
    results, err := s.repo.Search(ctx, params)

    // 2. Rank results (RRF fusion or vector similarity)
    ranked := rankResults(results, params.RankingStrategy)

    // 3. Return paginated results
    return &SearchResponse{
        Items:      ranked,
        Total:      len(ranked),
        NextCursor: nextCursor,
    }, nil
}

// Repository executes SQL with FTS + vector CTE
func (r *Repository) Search(ctx context.Context, params SearchParams) ([]*SearchResult, error) {
    // Hybrid search SQL:
    // 1. FTS query (ts_rank_cd for BM25-like scoring)
    // 2. Vector similarity (<=> operator for cosine distance)
    // 3. Join graph_objects metadata (type, properties, etc.)
    // 4. Reciprocal Rank Fusion (RRF) to combine scores
    query := `
        WITH fts AS (
            SELECT id, ts_rank_cd(fts_vector, query) AS rank
            FROM kb.chunks, to_tsquery($1) query
            WHERE fts_vector @@ query
        ),
        vector AS (
            SELECT id, 1 - (embedding <=> $2) AS similarity
            FROM kb.chunks
            ORDER BY embedding <=> $2
            LIMIT 100
        )
        SELECT * FROM fts
        FULL OUTER JOIN vector USING (id)
        ORDER BY (fts.rank + vector.similarity) DESC
    `

    return results, nil
}
```

**Search Types:**

- **Hybrid (default)**: FTS + vector with RRF fusion
- **FTS only**: Full-text search with BM25 scoring
- **Vector only**: Cosine similarity on embeddings
- **Graph metadata**: Filters by object type, properties, relationships

**No Retriever Abstraction:**

- Emergent does **not separate retrieval strategies** into pluggable classes
- All search logic lives in `SearchService` and `SearchRepository`
- LLM integration happens in `ChatService` (separate from search)

### 4.3 Comparison: Retrieval

| Aspect                   | Cognee                                                | Emergent                           |
| ------------------------ | ----------------------------------------------------- | ---------------------------------- |
| **Retrieval Strategies** | 15+ pluggable retrievers                              | 1 unified hybrid search            |
| **Abstraction Level**    | 3-step pipeline (retrieve → contextualize → complete) | Single search method               |
| **LLM Integration**      | Built into retrievers                                 | Separate `ChatService`             |
| **Graph Traversal**      | Multiple strategies (temporal, CoT, entity-centric)   | Basic relationship joins           |
| **Conversation History** | Built-in session cache                                | Not visible                        |
| **Feedback Loop**        | Optional QA pair storage in graph                     | Not visible                        |
| **Access Tracking**      | Automatic timestamp updates                           | Not visible                        |
| **Extensibility**        | Easy (implement `BaseRetriever`)                      | Requires modifying `SearchService` |
| **Complexity**           | High (15+ classes)                                    | Low (1 service)                    |

---

## 5. Multi-Tenancy & Access Control

### 5.1 Cognee: User → Dataset → Data

**Hierarchy:**

```
User
 └── Dataset (isolated database per dataset)
      └── Data (documents, chunks, graph nodes)
```

**Access Control:**

- **Backend access control** (optional feature)
- Each dataset gets **dedicated databases** (vector + graph + relational)
- User-dataset mapping stored in relational DB
- Adapters implement `create_dataset()` and `delete_dataset()` for provisioning

**Vector DB Example:**

```python
# cognee/infrastructure/databases/vector/vector_db_interface.py
class VectorDBInterface(Protocol):
    @classmethod
    async def create_dataset(cls, dataset_id: Optional[UUID], user: Optional[User]) -> dict:
        """
        Return connection info for a vector database for the given dataset.
        Function can auto handle deploying of the actual database if needed.
        Each dataset needs to map to a unique vector database when backend
        access control is enabled to facilitate a separation of concern for data.
        """
        pass

    async def delete_dataset(self, dataset_id: UUID, user: User) -> None:
        """Delete the vector database for the given dataset."""
        pass
```

**Graph Relationship Ledger:**

- All graph changes tracked in `GraphRelationshipLedger` (relational table)
- Decorator `@record_graph_changes` logs node/edge operations with creator function

### 5.2 Emergent: Org → Project → Document

**Hierarchy:**

```
Organization (tenant)
 └── Project (workspace)
      └── Document
           └── Chunk
                └── Graph Objects/Relationships
```

**Access Control:**

- **Zitadel OAuth2/OIDC** for authentication
- **Row-Level Security (RLS)** in PostgreSQL (middleware enforces `project_id`)
- **Schema separation**: `core.*` (users, orgs, projects) vs `kb.*` (documents, chunks, graph)
- **API token support**: Machine-to-machine via `core.api_tokens` table

**RLS Enforcement:**

```go
// internal/middleware/rls.go
func RLSMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        projectID := c.Param("projectId")
        userID := getUserFromContext(c)

        // Verify user has access to project
        if !hasAccess(userID, projectID) {
            return echo.ErrForbidden
        }

        // Set RLS context for database queries
        db.SetContext(projectID, userID)
        return next(c)
    }
}
```

### 5.3 Comparison: Multi-Tenancy

| Aspect           | Cognee                                 | Emergent                                 |
| ---------------- | -------------------------------------- | ---------------------------------------- |
| **Hierarchy**    | User → Dataset → Data                  | Org → Project → Document                 |
| **Isolation**    | Database-per-dataset                   | Schema-based (single DB)                 |
| **Provisioning** | Adapter-specific (LanceDB, Kuzu, etc.) | PostgreSQL schemas only                  |
| **RLS**          | Not visible (adapter-level)            | Middleware + PostgreSQL RLS              |
| **Auth**         | Custom (JWT examples)                  | Zitadel (OAuth2/OIDC)                    |
| **API Tokens**   | Not visible                            | Built-in (`core.api_tokens`)             |
| **Audit Trail**  | GraphRelationshipLedger                | Not visible (could use PostgreSQL audit) |

---

## 6. Testing Approach

### 6.1 Cognee: pytest (Unit, Integration, E2E)

**Test Structure:**

```
cognee/tests/
├── unit/               # Unit tests (mocked dependencies)
├── integration/        # Integration tests (real databases)
└── e2e/                # End-to-end tests (FastAPI TestClient)
```

**Example Test:**

```python
# tests/unit/test_extraction.py
@pytest.mark.asyncio
async def test_extract_graph_from_chunks():
    chunks = [
        DocumentChunk(id=uuid4(), text="Alice works at Google."),
        DocumentChunk(id=uuid4(), text="Bob is a software engineer."),
    ]

    graph = await extract_graph_from_data(chunks, KnowledgeGraph)

    assert len(graph.nodes) == 3  # Alice, Google, Bob
    assert len(graph.edges) == 2  # works_at, is_a
```

**Fixtures:**

- `pytest` fixtures for database setup/teardown
- `async` test support via `pytest-asyncio`
- `unittest.mock` for LLM mocking

### 6.2 Emergent: testify (E2E Focus)

**Test Structure:**

```
server-go/tests/
├── e2e/                # End-to-end HTTP API tests (23 suites)
│   ├── documents_test.go
│   ├── chunks_test.go
│   ├── graph_test.go
│   ├── extraction_test.go
│   └── search_test.go
└── integration/        # Service + DB integration (8 suites)
    └── graph_service_test.go
```

**Example Test (testify suite):**

```go
// tests/e2e/graph_test.go
type GraphSuite struct {
    suite.Suite
    ctx *testutil.E2EContext
}

func (s *GraphSuite) SetupSuite() {
    s.ctx = testutil.NewE2EContext(s.T())
}

func (s *GraphSuite) TestCreateGraphObject() {
    // 1. Create project
    project := s.ctx.CreateProject("test-project")

    // 2. Create graph object
    obj := &CreateGraphObjectRequest{
        Type: "person",
        Key:  "alice",
        Properties: map[string]interface{}{
            "name": "Alice",
            "age":  30,
        },
    }
    resp := s.ctx.POST("/api/projects/%s/graph/objects", project.ID).
        WithJSON(obj).
        Expect().Status(201).JSON().Object()

    // 3. Verify object exists
    s.Equal("person", resp.Value("type").String().Raw())
    s.Equal("alice", resp.Value("key").String().Raw())
}

func TestGraphSuite(t *testing.T) {
    suite.Run(t, new(GraphSuite))
}
```

**Test Utilities:**

- `testutil.E2EContext` provides database + HTTP client + auth helpers
- **609 E2E tests** passing (full feature parity with NestJS)
- **No unit tests** (focus on E2E coverage)

### 6.3 Comparison: Testing

| Aspect            | Cognee                                            | Emergent                         |
| ----------------- | ------------------------------------------------- | -------------------------------- |
| **Framework**     | pytest                                            | testify (Go)                     |
| **Test Types**    | Unit, Integration, E2E                            | E2E, Integration                 |
| **Coverage**      | Not documented                                    | 609 E2E tests passing            |
| **Mocking**       | unittest.mock                                     | Manual test doubles              |
| **Database**      | In-memory SQLite (unit), PostgreSQL (integration) | PostgreSQL with schema isolation |
| **HTTP Client**   | FastAPI TestClient                                | httpexpect (Go)                  |
| **Async Support** | pytest-asyncio                                    | Native (Go routines)             |

---

## 7. Deployment & Operations

### 7.1 Cognee: Docker + Python

**Deployment:**

- **Docker**: Official Dockerfile (Python 3.10+)
- **Dependencies**: FastAPI, SQLAlchemy, asyncpg, Kuzu, LanceDB
- **Configuration**: Environment variables (`POSTGRES_URL`, `OPENAI_API_KEY`, etc.)
- **CLI**: `cognee` command-line tool (installed via pip)

**Observability:**

- Not documented (likely custom logging)

### 7.2 Emergent: Docker + Go Binary

**Deployment:**

- **Docker**: Multi-stage build (Go 1.24 → Alpine)
- **Binary Size**: ~36MB (optimized with `-ldflags="-s -w"`)
- **Dependencies**: PostgreSQL, MinIO, Zitadel, Kreuzberg
- **Configuration**: `.env` file + environment variables
- **Orchestration**: `workspace:*` npm scripts (PID-based process manager)

**Observability:**

- **Logs**: JSON structured logging (zap) to `logs/server.log`
- **LangFuse**: Self-hosted tracing for LLM jobs (Docker Compose)
- **Health Checks**: `/health` and `/ready` endpoints

### 7.3 Comparison: Deployment

| Aspect                 | Cognee                               | Emergent                        |
| ---------------------- | ------------------------------------ | ------------------------------- |
| **Language Runtime**   | Python (CPython)                     | Go (native binary)              |
| **Binary Size**        | N/A (interpreted)                    | ~36MB                           |
| **Startup Time**       | Slow (Python imports)                | Fast (compiled binary)          |
| **Memory Usage**       | High (Python + LLM models)           | Low (Go + external AI services) |
| **Hot Reload**         | Manual restart or `uvicorn --reload` | Built-in (`air` or inotify)     |
| **Observability**      | Not documented                       | LangFuse, structured logs       |
| **Process Management** | Manual (systemd, Docker, etc.)       | workspace-cli (PID-based)       |

---

## 8. Key Architectural Differences

### 8.1 Pipeline-Based vs Domain-Driven

**Cognee (Pipeline-Based):**

- Tasks are **composable functions** that return data for next task
- Example: `add() → chunk() → extract_graph() → store()`
- Easy to **extend** by adding new tasks
- **Async/await** for concurrency (Python asyncio)

**Emergent (Domain-Driven):**

- Domains are **self-contained modules** with entity/store/service/handler
- Example: `DocumentService → ExtractionService → GraphService → SearchService`
- Each domain has **isolated tests**
- **fx dependency injection** for explicit wiring

### 8.2 Multi-Backend vs Single-Database

**Cognee (Multi-Backend):**

- **Interface-based adapters** for graph/vector/relational databases
- Supports Kuzu, Neo4j, Neptune, LanceDB, ChromaDB, PGVector, SQLite
- **Flexibility**: Choose best tool for the job
- **Complexity**: Maintaining 6+ adapters (3 graph + 3 vector)

**Emergent (Single-Database):**

- **PostgreSQL for everything**: relational + vector + graph (emulated)
- **Simplicity**: One database, one ORM (Bun), one driver (pgx)
- **Performance**: pgvector + FTS in same query (no joins across databases)
- **Vendor lock-in**: PostgreSQL-only (no Neo4j, no Kuzu)

### 8.3 Pluggable Retrievers vs Unified Search

**Cognee (Pluggable):**

- **15+ retrievers** implementing `BaseRetriever`
- Easy to add new retrieval strategies (implement 3 methods)
- LLM integration **built into** each retriever
- **Conversation history** and **feedback loop** per retriever

**Emergent (Unified):**

- **1 search service** with hybrid (FTS + vector) logic
- LLM integration **separate** (`ChatService`)
- **Simpler** codebase (no abstraction overhead)
- **Less flexible** (requires modifying `SearchService` for new strategies)

---

## 9. Notable Strengths & Weaknesses

### 9.1 Cognee Strengths

1. **Multi-Backend Flexibility**: Swap Kuzu for Neo4j, LanceDB for PGVector (adapters handle it)
2. **Pluggable Retrievers**: 15+ search strategies out-of-the-box
3. **Instructor Integration**: Structured LLM outputs with Pydantic validation
4. **Ontology Support**: Optional ontology resolver for domain-specific validation
5. **Access Tracking**: Built-in node access timestamps (for usage analytics)
6. **Open Source**: Apache 2.0 license (community contributions welcome)

### 9.2 Cognee Weaknesses

1. **Python Performance**: Slower than Go for CPU-bound tasks
2. **Adapter Maintenance**: 6+ adapters to maintain (3 graph + 3 vector)
3. **Complex Dependencies**: Kuzu, LanceDB, OpenAI, Instructor, etc.
4. **Testing Coverage**: Not documented (hard to assess quality)
5. **Deployment Complexity**: Multiple database options increase ops burden

### 9.3 Emergent Strengths

1. **Go Performance**: Fast startup, low memory, native concurrency
2. **Single Database**: PostgreSQL for everything (simpler ops)
3. **Production Ready**: 609 E2E tests passing, deployed in production
4. **OAuth2/OIDC**: Industry-standard auth via Zitadel
5. **Job Queue**: Built-in polling-based queue (no external broker)
6. **Observability**: LangFuse tracing, structured logs

### 9.4 Emergent Weaknesses

1. **Vendor Lock-In**: PostgreSQL-only (no Neo4j, no Kuzu)
2. **No Pluggable Retrievers**: Hard to extend search strategies
3. **No Ontology Support**: Schema validation is JSON Schema-based (no ontology resolver)
4. **Proprietary**: Closed-source (no community contributions)
5. **LLM Vendor Lock-In**: Google Vertex AI only (no OpenAI, no Anthropic)

---

## 10. Recommendations

### 10.1 Patterns Worth Adopting from Cognee

1. **Interface-Based Database Adapters**

   - **Why**: Enables swapping PostgreSQL for Neo4j without rewriting domain logic
   - **How**: Create `GraphRepository` interface with multiple implementations (Postgres, Neo4j)
   - **Impact**: Medium effort, high flexibility gain

2. **Pluggable Retrieval Strategies**

   - **Why**: Easier to experiment with different search algorithms (temporal, CoT, entity-centric)
   - **How**: Create `Retriever` interface with 3-step pipeline (retrieve → contextualize → complete)
   - **Impact**: High effort, high extensibility gain

3. **Ontology Resolver**

   - **Why**: Enables domain-specific validation (e.g., medical entities, legal entities)
   - **How**: Extend JSON Schema validation with ontology file support (OWL, RDF, custom)
   - **Impact**: Medium effort, high domain-specificity gain

4. **Access Tracking**

   - **Why**: Enables usage analytics (which nodes are most accessed?)
   - **How**: Add `last_accessed_at` column to `kb.graph_objects`, update on search
   - **Impact**: Low effort, medium analytics gain

5. **Conversation History Cache**

   - **Why**: Enables multi-turn conversations with context retention
   - **How**: Store chat sessions in `core.chat_sessions` with message history
   - **Impact**: Low effort (already have chat), high UX gain

6. **Triplet Embedding**
   - **Why**: Enables semantic search on relationships (not just entities)
   - **How**: Add `embedding` column to `kb.graph_relationships`, embed "Entity1 -[REL]-> Entity2"
   - **Impact**: Medium effort, high search quality gain

### 10.2 Patterns to Avoid from Cognee

1. **Python Performance**: Stick with Go for CPU-bound tasks (extraction, embedding)
2. **Adapter Proliferation**: Avoid supporting 6+ databases (ops complexity)
3. **Brute-Force Triplet Search**: Inefficient for large graphs (use indexed queries)

### 10.3 Emergent Patterns to Keep

1. **Single Database**: PostgreSQL simplicity is a huge ops win
2. **Job Queue**: Polling-based queue is simple and works well
3. **E2E Test Focus**: 609 tests give high confidence in deployments
4. **fx Dependency Injection**: Explicit wiring is easier to debug than "magic"

---

## Conclusion

Cognee and Emergent are both strong knowledge management systems with different trade-offs:

- **Cognee** excels in **flexibility** (multi-backend, pluggable retrievers) and **open-source community**.
- **Emergent** excels in **simplicity** (single database, single language) and **production readiness** (609 tests, deployed).

**Best adoption candidates from Cognee:**

1. **Pluggable Retrieval Strategies** (high impact, high effort)
2. **Ontology Resolver** (high domain-specificity, medium effort)
3. **Triplet Embedding** (high search quality, medium effort)
4. **Access Tracking** (medium analytics, low effort)

**Next steps:**

- Review `SUGGESTIONS.md` for detailed implementation approaches
- Decide if OpenSpec proposals are needed for each adoption candidate
