# Improvement Suggestion: RAG & Search Optimizations from Open-Source Research

**Status:** Proposed  
**Priority:** High  
**Category:** Performance / Architecture  
**Proposed:** 2026-02-14  
**Proposed by:** AI Agent (comparative analysis of 5 open-source projects)  
**Assigned to:** Unassigned

---

## Summary

A collection of 14 actionable improvements to Emergent's RAG pipeline, search system, extraction pipeline, and observability — identified by analyzing techniques from Haystack, Genkit, LangGraph, AutoRAG, and Casibase against Emergent's current implementation.

---

## Research Methodology

Five open-source projects were selected for their overlap with Emergent's architecture and capabilities:

| Project                                                                 | Stars | Relevance                                                                             | Source       |
| ----------------------------------------------------------------------- | ----- | ------------------------------------------------------------------------------------- | ------------ |
| [deepset-ai/haystack](https://github.com/deepset-ai/haystack)           | 24.2k | Production RAG framework with advanced retrieval, fusion, ranking, and evaluation     | Python       |
| [firebase/genkit](https://github.com/firebase/genkit)                   | 5.5k  | Google's AI framework with first-class Go SDK — streaming, tracing, structured output | Go/JS/Python |
| [langchain-ai/langgraph](https://github.com/langchain-ai/langgraph)     | 24.7k | Stateful multi-agent orchestration as graphs — checkpointing, retries, fan-out        | Python       |
| [Marker-Inc-Korea/AutoRAG](https://github.com/Marker-Inc-Korea/AutoRAG) | 4.6k  | RAG evaluation & optimization — metrics, chunking tuning, fusion benchmarking         | Python       |
| [casibase/casibase](https://github.com/casibase/casibase)               | 4.4k  | Go-based AI knowledge platform — multi-model LLM, MCP, markdown chunking              | Go           |

Each project's source code was read directly via GitHub to extract specific implementation patterns. The findings below reference the exact source files where each technique is implemented.

---

## Proposed Improvements

### Category A: Search & Retrieval

---

#### A1. Embedding Cache (LRU)

**Priority:** P0 — High impact, low effort  
**Source:** Gap analysis (no open-source project skips this)

**Current State:**  
Emergent has no caching layer for query embeddings. Every search call hits the Vertex AI embedding API (~100-200ms). The embedding deduplication in `search/service.go` (computing once per unified search) helps, but repeated identical queries still re-embed.

> Note: Improvement #015 already proposed deduplicating the `EmbedQuery()` call across the 3 parallel search goroutines. This proposal addresses the _cross-request_ cache that #015 does not cover.

**Proposed Change:**  
Add an in-memory LRU cache keyed by `SHA256(query_text + model_name)` with a configurable TTL (default 5 minutes) and max size (default 1000 entries). Embeddings are deterministic for the same input, so caching is safe.

```
Cache miss:  query → Vertex AI API (100-200ms) → cache store → return
Cache hit:   query → cache lookup (< 1ms) → return
```

**Implementation:**

1. Add a `sync.Map` or LRU cache (e.g., `hashicorp/golang-lru/v2`) in `pkg/embeddings/module.go`
2. Wrap `EmbedQuery()` with cache check/store logic
3. Add cache hit/miss metrics via existing OTel instrumentation

**Expected Impact:**

- Eliminates redundant Vertex AI calls for repeated queries (common in chat-style interactions)
- ~100-200ms latency reduction on cache hits
- Reduced Vertex AI API costs

**Reference:** Haystack's `CacheChecker` component (`haystack/components/caching/cache_checker.py`) implements a similar pattern at the document level — checking a document store for already-processed items before running expensive operations.

**Affected Components:**

- `apps/server-go/pkg/embeddings/module.go`
- `apps/server-go/pkg/embeddings/client.go`

**Estimated Effort:** Small (half day)

---

#### A2. Lost-in-the-Middle Reordering

**Priority:** P0 — High impact, very low effort  
**Source:** Haystack — `haystack/components/rankers/lost_in_the_middle.py`

**Current State:**  
Emergent returns search results sorted by fusion score (highest first). When these results are passed as context to an LLM for RAG, the model attends most strongly to the beginning and end of the context window, and tends to "lose" information in the middle.

**Proposed Change:**  
After fusion scoring and before passing results to the LLM, reorder results so the most relevant items are at the **beginning and end**, with least relevant items in the **middle**:

```
Input (by score):   [1st, 2nd, 3rd, 4th, 5th, 6th, 7th, 8th]
Output (reordered): [1st, 3rd, 5th, 7th, 8th, 6th, 4th, 2nd]
```

Algorithm: Alternate placing items at the front and back of the reordered list.

**Implementation:**

1. Add a `reorderLostInMiddle(results []SearchResult) []SearchResult` function
2. Call it in `search/service.go` after fusion, before returning results
3. Make it configurable (opt-in via search request parameter)

**Expected Impact:**

- Improved RAG answer quality with zero additional latency or API cost
- Particularly valuable when returning 10+ context chunks

**Reference:** Based on the paper ["Lost in the Middle: How Language Models Use Long Contexts"](https://arxiv.org/abs/2307.03172) (Liu et al., 2023). Haystack's implementation supports `word_count_threshold` to cap total context size and `top_k` to limit results before reordering.

**Affected Components:**

- `apps/server-go/domain/search/service.go`

**Estimated Effort:** Small (2-3 hours)

---

#### A3. Distribution-Based Score Fusion (DBSF)

**Priority:** P1  
**Source:** Haystack — `haystack/components/joiners/document_joiner.py:186-222`

**Current State:**  
Emergent's `weighted` fusion in `search/repository.go` uses z-score normalization + sigmoid to normalize scores from different retrievers before weighted combination. This works well when both retrievers return enough results for meaningful statistics, but can produce degenerate scores when one retriever returns very few results (e.g., 1-2 hits).

**Proposed Change:**  
Add DBSF as an alternative fusion strategy. DBSF normalizes each retriever's scores to [0, 1] using `(score - (mean - 3σ)) / (6σ)`, clamped to [0, 1]. This handles different score distributions more robustly than z-score + sigmoid.

```
For each retriever:
  μ = mean(scores), σ = stddev(scores)
  normalized_score = clamp((score - (μ - 3σ)) / (6σ), 0, 1)
Then: final_score = Σ(weight_i × normalized_score_i) / Σ(weight_i)
```

**Implementation:**

1. Add `dbsf` to the `FusionStrategy` enum in `search/dto.go`
2. Implement `dbsfFusion()` in `search/repository.go` alongside existing `weightedFusion()`
3. Use DBSF as the new default (backward-compatible — existing `weighted` strategy remains available)

**Expected Impact:**

- More stable hybrid search scores across varying result set sizes
- Better handling of edge cases (few FTS matches, many vector matches, or vice versa)

**Reference:** Haystack implements four fusion strategies: `concatenation`, `merge` (weighted sum), `reciprocal_rank_fusion` (k=61), and `distribution_based_rank_fusion`. Emergent already has `weighted` and `rrf` — adding DBSF completes the set.

**Affected Components:**

- `apps/server-go/domain/search/repository.go`
- `apps/server-go/domain/search/dto.go`

**Estimated Effort:** Small (half day)

---

#### A4. Multi-Query Retrieval

**Priority:** P1  
**Source:** Haystack — `haystack/components/retrievers/multi_query_embedding_retriever.py`

**Current State:**  
Emergent searches with a single query string. If the user's phrasing doesn't match the vocabulary in the indexed documents, relevant results are missed.

**Proposed Change:**  
Before search, use the LLM to generate 2-3 reformulations of the user's query. Run retrieval for each query variant in parallel (fan-out via goroutines), then deduplicate and merge scores.

```
User query: "Who started Tesla?"
Reformulations:
  → "Tesla founder"
  → "Tesla Motors founding history"
  → "Elon Musk Tesla origin"

Fan-out: 3 parallel retrievals → deduplicate by doc ID → keep highest score
```

**Implementation:**

1. Add a `QueryExpander` that calls Gemini with a short prompt to generate N reformulations
2. In `search/service.go`, optionally expand the query before the parallel search goroutines
3. Each variant gets its own embedding + retrieval pass
4. Merge results by document ID, keeping the highest score per document
5. Make configurable: `expandQuery: bool`, `expansionCount: int` (default 3)

**Expected Impact:**

- 15-30% improvement in recall (well-documented in RAG literature)
- Trades latency (1 extra LLM call for expansion, ~200ms) for significantly better results
- Fan-out retrieval runs in parallel, so wall-clock time increase is minimal

**Reference:** Haystack's `MultiQueryEmbeddingRetriever` uses `ThreadPoolExecutor` for parallel embedding of multiple queries. Emergent can use goroutines for the same pattern.

**Affected Components:**

- `apps/server-go/domain/search/service.go` (new query expansion step)
- `apps/server-go/pkg/embeddings/module.go` (batch embed multiple queries)

**Estimated Effort:** Medium (1-2 days)

---

#### A5. Sentence Window Retrieval

**Priority:** P1  
**Source:** Haystack — `haystack/components/retrievers/sentence_window_retriever.py`

**Current State:**  
Emergent retrieves fixed-size chunks. Small chunks are good for retrieval precision, but the LLM benefits from larger context windows that include surrounding text.

**Proposed Change:**  
After initial retrieval, expand each result by fetching neighboring chunks from the same document. Use `startOffset`/`endOffset` metadata (already stored in `kb.chunks`) to merge overlapping regions.

```
Retrieved chunk: [chunk 5 of document X]
Expanded window: [chunk 4 | chunk 5 | chunk 6] of document X (merged, overlaps removed)
```

**Implementation:**

1. Add a `expandChunkWindow(chunkID, windowSize int)` function in `chunks/repository.go`
2. Query chunks from the same document with adjacent offsets
3. Merge text using `startOffset`/`endOffset` to handle overlapping splits
4. Return expanded text as the context, but keep the original chunk's score
5. Make configurable: `windowSize: int` (default 1 — expand by 1 chunk in each direction)

**Expected Impact:**

- Better LLM context without changing indexing granularity
- Decouples retrieval precision (small chunks) from generation context (large windows)

**Reference:** Haystack's implementation tracks `split_id`, `split_idx_start`, and `_split_overlap` metadata per chunk. It uses overlap-aware concatenation to avoid duplicating text at chunk boundaries.

**Affected Components:**

- `apps/server-go/domain/chunks/repository.go`
- `apps/server-go/domain/search/service.go`

**Estimated Effort:** Medium (1-2 days)

---

#### A6. Diversity Ranker (Maximum Marginal Relevance)

**Priority:** P2  
**Source:** Haystack — `haystack/components/rankers/sentence_transformers_diversity.py`

**Current State:**  
Emergent returns results sorted purely by relevance score. When multiple chunks from the same document section score highly, the results contain near-duplicate content, wasting context window space.

**Proposed Change:**  
After scoring, apply Maximum Marginal Relevance (MMR) to balance relevance and diversity:

```
MMR(d) = λ × sim(d, query) - (1 - λ) × max(sim(d, d_selected))
```

Where `λ` controls the relevance/diversity tradeoff (default 0.7 = favor relevance).

**Implementation:**

1. Add a `rerankMMR(results []SearchResult, queryEmbedding []float32, lambda float64)` function
2. Iteratively select the result that maximizes MMR
3. Embeddings for chunks are already stored in `kb.chunks.embedding` — load them during search
4. Make configurable: `diversityRanking: bool`, `diversityLambda: float64`

**Expected Impact:**

- More diverse context for the LLM — less redundant information
- Particularly valuable for broad queries that match many chunks from the same document

**Reference:** Haystack implements two diversity strategies: _Greedy Diversity Order_ (iteratively select least-similar-to-selected) and _MMR_ (balance query relevance vs. inter-document diversity). MMR is the more practical choice for RAG.

**Affected Components:**

- `apps/server-go/domain/search/service.go`

**Estimated Effort:** Small (half day)

---

### Category B: Chunking Improvements

---

#### B1. Markdown-Aware Chunking with Heading Path Prefixes

**Priority:** P1  
**Source:** Casibase — `split/markdown.go`

**Current State:**  
Emergent's chunking strategies (character/sentence/paragraph in `domain/chunking/service.go`) treat all text uniformly. When documents have clear markdown structure (headers, sections), chunks lose their structural context.

**Proposed Change:**  
For markdown-formatted documents, build a heading tree and prefix each chunk with its hierarchical heading path:

```
Original section:
  # Architecture
  ## Backend
  ### Database
  PostgreSQL is used as the primary data store...

Chunk output:
  "Architecture > Backend > Database: PostgreSQL is used as the primary data store..."
```

Additionally, extract tables separately and preserve their column headers as context.

**Implementation:**

1. Add a `markdown` chunking strategy to `domain/chunking/service.go`
2. Parse heading hierarchy (`#`, `##`, `###`, etc.) into a tree
3. For each text block, determine its heading ancestry and prepend as a path prefix
4. Extract markdown/HTML tables separately with their nearest heading context
5. Apply standard sentence/paragraph chunking to the remaining text blocks

**Expected Impact:**

- LLM receives structural context ("this text is from the Database section under Backend Architecture")
- Tables are preserved as coherent units rather than split across chunks
- Low cost — heading paths are short prefixes (~10-20 tokens)

**Reference:** Casibase's `split/markdown.go` implements `ExtractMarkdownTree()` that builds a heading tree, then `SplitMarkdownText()` that splits by heading with path prefixes. Tables are extracted via regex and processed separately.

**Affected Components:**

- `apps/server-go/domain/chunking/service.go`

**Estimated Effort:** Medium (1-2 days)

---

#### B2. Semantic (Embedding-Based) Chunking

**Priority:** P3  
**Source:** Haystack — `haystack/components/preprocessors/embedding_based_document_splitter.py`

**Current State:**  
Emergent splits by fixed character/sentence/paragraph boundaries. These boundaries don't necessarily align with topic transitions in the text.

**Proposed Change:**  
Split documents at semantic boundaries by:

1. Split text into sentences
2. Group sentences into overlapping windows (e.g., 3 sentences per group)
3. Embed each group
4. Compute cosine distance between consecutive groups
5. Split where the distance exceeds the Nth percentile threshold (e.g., 95th percentile = split at the top 5% of topic shifts)

**Implementation:**

1. Add a `semantic` chunking strategy
2. Use the existing embedding client to embed sentence groups
3. Compute cosine distances between adjacent groups
4. Split at peaks above the percentile threshold
5. Apply min/max length constraints with recursive re-splitting for oversized chunks

**Expected Impact:**

- Chunks that respect topic boundaries — more coherent retrieval units
- Better retrieval precision (less "topic pollution" per chunk)

**Trade-off:** Adds embedding cost during ingestion (embedding N sentence groups per document). Best reserved for high-value documents where retrieval quality matters most.

**Reference:** Haystack's implementation uses a `threshold` parameter (percentile of cosine distances) and handles edge cases with `min_length`/`max_length` constraints. Groups that exceed `max_length` are recursively re-split.

**Affected Components:**

- `apps/server-go/domain/chunking/service.go`
- `apps/server-go/pkg/embeddings/module.go` (batch embed sentence groups)

**Estimated Effort:** Medium (2-3 days)

---

### Category C: Extraction Pipeline

---

#### C1. Parallel Fan-Out Entity Extraction

**Priority:** P2  
**Source:** LangGraph — `langgraph/pregel/__init__.py` (Send-based fan-out)

**Current State:**  
Emergent's ADK extraction pipeline (`domain/extraction/agents/pipeline.go`) processes document text through a sequential pipeline: EntityExtractor → RelationshipBuilder → QualityChecker. For large documents with multiple chunks, entity extraction processes chunks sequentially.

**Proposed Change:**  
Fan-out entity extraction across document chunks in parallel, then fan-in (merge + deduplicate) before relationship building:

```
Current:   chunk1 → extract → chunk2 → extract → chunk3 → extract → relationships → quality
Proposed:  chunk1 → extract ─┐
           chunk2 → extract ─┼─→ merge/dedup → relationships → quality
           chunk3 → extract ─┘
```

**Implementation:**

1. Split the extraction input into chunks (already chunked at document level)
2. Launch goroutines per chunk for entity extraction
3. Fan-in results via a channel, deduplicate entities by name similarity
4. Proceed with relationship building on the merged entity set
5. Use `errgroup.Group` with a concurrency limit to avoid overwhelming the Gemini API

**Expected Impact:**

- Extraction time reduced proportionally to chunk count (e.g., 5 chunks → ~5x faster)
- Gemini API supports concurrent requests — this utilizes available throughput

**Reference:** LangGraph's `Send()` primitive creates parallel tasks with isolated state. The `PregelRunner` collects results via `concurrent.futures` with `FIRST_COMPLETED` wait strategy. Genkit's tool execution uses the same pattern: goroutines + buffered channel with indexed results.

**Affected Components:**

- `apps/server-go/domain/extraction/agents/pipeline.go`
- `apps/server-go/domain/extraction/object_extraction_worker.go`

**Estimated Effort:** Medium (2-3 days)

---

#### C2. Adaptive Quality Retry Loops

**Priority:** P2  
**Source:** LangGraph — `RemainingSteps` managed value, conditional routing

**Current State:**  
Emergent's quality checker (`domain/extraction/agents/quality_checker.go`) retries up to 3 times when the orphan rate exceeds 30%. Each retry uses the same extraction strategy — it highlights orphan entity IDs in the prompt and asks the RelationshipBuilder to try again.

**Proposed Change:**  
Adapt the retry strategy based on iteration count:

| Iteration | Strategy                                                                                              |
| --------- | ----------------------------------------------------------------------------------------------------- |
| 1         | Standard: highlight orphan IDs, request relationships                                                 |
| 2         | Relaxed: lower the relationship type constraints, allow broader relationship types                    |
| 3         | Fallback: accept the current state, flag low-confidence entities for human review instead of retrying |

**Implementation:**

1. Pass `iterationCount` into the RelationshipBuilder prompt template
2. On iteration 2: widen the allowed relationship types or relax schema constraints
3. On iteration 3: accept results and mark orphan entities with a `confidence: low` flag
4. Log iteration strategy changes in `kb.object_extraction_logs` for observability

**Expected Impact:**

- Avoids wasting 3 identical retry attempts when the extraction genuinely can't find relationships
- Faster completion for difficult documents (iteration 3 terminates instead of retrying)
- Better data quality signals (explicit low-confidence flags vs. silent orphans)

**Reference:** LangGraph's `RemainingSteps` managed value lets nodes detect when they're approaching the recursion limit and adapt behavior accordingly. The `CachePolicy` with TTL avoids re-running expensive computations for identical inputs.

**Affected Components:**

- `apps/server-go/domain/extraction/agents/quality_checker.go`
- `apps/server-go/domain/extraction/agents/relationship_builder.go`
- `apps/server-go/domain/extraction/agents/prompts.go`

**Estimated Effort:** Small (half day)

---

#### C3. Extraction Cost Estimation (DryRun)

**Priority:** P2  
**Source:** Casibase — `model/provider.go` (DryRun pattern)

**Current State:**  
Emergent has no way to estimate extraction cost before running it. Users trigger extraction without knowing how many tokens will be consumed or what the approximate cost will be.

**Proposed Change:**  
Add a `/api/extraction/estimate` endpoint that:

1. Takes the same parameters as the extraction endpoint
2. Counts input tokens using `tiktoken-go` (or Google's tokenizer)
3. Estimates output tokens based on entity/relationship count heuristics from the schema
4. Returns estimated token counts and approximate cost

**Implementation:**

1. Add `tiktoken-go` dependency for token counting
2. Create an `EstimateExtraction()` function that counts document tokens and estimates output
3. Apply pricing per model (Gemini Flash vs Pro have different rates)
4. Return estimate in the API response before the user confirms extraction

**Expected Impact:**

- User trust — no surprise costs from large document extraction
- Better resource planning for batch extraction jobs

**Reference:** Casibase uses a `$CasibaseDryRun$` prefix on questions to trigger estimation mode. Their `calculatePrice()` methods use hardcoded per-model pricing tables. A simpler approach for Emergent would be a dedicated estimate endpoint.

**Affected Components:**

- `apps/server-go/domain/extraction/` (new estimate handler)
- `apps/admin/src/` (UI to display estimates before confirming)

**Estimated Effort:** Medium (1-2 days)

---

### Category D: Observability & Evaluation

---

#### D1. RAG Quality Evaluation Metrics

**Priority:** P2  
**Source:** Haystack — `haystack/components/evaluators/`, AutoRAG — `autorag/evaluate/`

**Current State:**  
Emergent has extraction evaluation (entity/relationship F1 scores via LangFuse experiments, see improvement #014) but **no evaluation for RAG/search quality**. There is no way to measure whether search changes actually improve answer quality.

**Proposed Change:**  
Implement two evaluation layers:

**Layer 1 — Retrieval metrics (no LLM required):**

- **MRR (Mean Reciprocal Rank):** How high does the first relevant result rank?
- **Recall@K:** What fraction of relevant documents appear in the top K results?
- **NDCG:** Normalized Discounted Cumulative Gain — accounts for ranking position

**Layer 2 — Generation metrics (LLM-as-judge):**

- **Faithfulness:** Break the answer into atomic statements, check each against context. Score = grounded statements / total statements.
- **Context Relevance:** What fraction of retrieved contexts are actually relevant to the question?

**Implementation:**

1. Create a `golden-rag` dataset in LangFuse with (question, expected_answer, relevant_doc_ids) triples
2. Implement retrieval metrics as Go functions in a new `evaluation/` package
3. Implement faithfulness scoring using Gemini as the judge (few-shot prompt from Haystack's approach)
4. Add an `/api/evaluation/rag` endpoint that runs a test suite and returns aggregate metrics
5. Integrate with CI to run on search/chunking changes

**Expected Impact:**

- Data-driven search optimization — every change can be measured
- Regression detection — catch search quality degradation before production
- Enables informed decisions about chunking strategies, fusion parameters, and ranking changes

**Reference:** Haystack's `FaithfulnessEvaluator` breaks answers into statements using few-shot prompting, then verifies each against context with binary scoring. Their `EvaluationRunResult` aggregates results with `comparative_detailed_report()` for side-by-side comparison of two configurations. AutoRAG automates this at scale by testing multiple configurations (chunk sizes, retrieval strategies, fusion methods) and selecting the best combination per metric.

**Affected Components:**

- New package: `apps/server-go/domain/evaluation/` (or `pkg/evaluation/`)
- LangFuse dataset: `golden-rag` (new)
- CI pipeline (optional — run evaluations on search changes)

**Estimated Effort:** Large (3-5 days for full suite, 1 day for MRR + Recall@K only)

---

#### D2. Action-Based Observability Wrapper

**Priority:** P3  
**Source:** Genkit — `go/core/action.go`, `go/core/tracing/tracing.go`

**Current State:**  
Emergent traces LLM calls via LangFuse (`domain/extraction/agents/`) and has some OTel integration. However, tracing is applied manually per call site — there's no generic wrapper that automatically traces all AI operations.

**Proposed Change:**  
Define a generic `Action[In, Out]` type that wraps any AI operation with automatic tracing, input/output recording, latency metrics, and error handling:

```go
type Action[In, Out any] struct {
    name       string
    actionType string // "embed", "generate", "extract", "search"
    fn         func(ctx context.Context, in In) (Out, error)
}

func (a *Action[In, Out]) Run(ctx context.Context, in In) (Out, error) {
    ctx, span := tracer.Start(ctx, a.name)
    defer span.End()
    span.SetAttributes(attribute.String("action.type", a.actionType))
    start := time.Now()
    out, err := a.fn(ctx, in)
    metrics.ActionLatency.Record(ctx, time.Since(start).Milliseconds())
    metrics.ActionRequests.Add(ctx, 1)
    if err != nil {
        span.RecordError(err)
    }
    return out, err
}
```

**Implementation:**

1. Define the `Action[In, Out]` generic type in a `pkg/action/` package
2. Wrap existing embedding, generation, and extraction calls as Actions
3. Register Actions in a registry for runtime discovery
4. Use OTel for spans and metrics (already available in the Go ecosystem)

**Expected Impact:**

- Every AI operation automatically traced without per-call instrumentation
- Consistent latency and error metrics across all AI operations
- Foundation for middleware (caching, retries, rate limiting) applied uniformly

**Reference:** Genkit's `ActionDef[In, Out, Stream]` wraps every AI operation in an OTel span via `RunInNewSpan[I, O]()`. Metrics are lazy-initialized with `sync.OnceValue` to defer setup until the `MeterProvider` is configured. The `markedError` pattern prevents duplicate error reporting up the span tree.

**Affected Components:**

- New package: `apps/server-go/pkg/action/`
- `apps/server-go/pkg/embeddings/` (wrap as Actions)
- `apps/server-go/domain/extraction/agents/` (wrap as Actions)

**Estimated Effort:** Medium (2-3 days)

---

### Category E: Go-Specific Patterns

---

#### E1. `iter.Seq2` for LLM Streaming

**Priority:** P3  
**Source:** Genkit — `go/ai/generate.go` (GenerateStream returns `iter.Seq2`)

**Current State:**  
Emergent uses SSE streaming for chat responses, with callback-based patterns internally.

**Proposed Change:**  
For internal Go interfaces, expose LLM streaming via Go 1.23's `iter.Seq2[*Chunk, error]`. This enables idiomatic consumption:

```go
// Provider returns an iterator
stream := model.GenerateStream(ctx, prompt)

// Consumer uses range loop
for chunk, err := range stream {
    if err != nil {
        return err
    }
    sseWriter.WriteChunk(chunk)
}
```

**Implementation:**

1. Define streaming interfaces using `iter.Seq2` for model providers
2. Internally, use a callback-to-channel bridge (callback writes to channel, iterator reads from channel)
3. Support early termination via sentinel error (`errStreamStop`)

**Expected Impact:**

- More idiomatic Go API for internal streaming
- Callers can use standard Go patterns (early return, defer, error handling) with streams
- Foundation for composable stream transformations

**Reference:** Genkit's `GenerateStream()` returns `iter.Seq2[*ModelResponseChunk, error]`. Internally, the model's `StreamingCallback` feeds a channel that the iterator reads from. A wrapper callback adds `Index` to each chunk for ordered reassembly.

**Affected Components:**

- `apps/server-go/domain/chat/` (streaming interfaces)
- `apps/server-go/pkg/` (streaming utilities)

**Estimated Effort:** Medium (1-2 days)

---

## Priority Summary

| Priority | ID  | Improvement                    | Impact                    | Effort |
| -------- | --- | ------------------------------ | ------------------------- | ------ |
| **P0**   | A1  | Embedding cache (LRU)          | Lower latency, lower cost | Small  |
| **P0**   | A2  | Lost-in-the-middle reordering  | Better RAG answers        | Small  |
| **P1**   | A3  | DBSF score fusion              | Robust hybrid search      | Small  |
| **P1**   | A4  | Multi-query retrieval          | +15-30% recall            | Medium |
| **P1**   | A5  | Sentence window retrieval      | Better LLM context        | Medium |
| **P1**   | B1  | Markdown heading path prefixes | Structural context        | Medium |
| **P2**   | A6  | Diversity ranker (MMR)         | Less redundant results    | Small  |
| **P2**   | C1  | Parallel fan-out extraction    | Faster extraction         | Medium |
| **P2**   | C2  | Adaptive quality retries       | Smarter retry loops       | Small  |
| **P2**   | C3  | DryRun cost estimation         | User trust                | Medium |
| **P2**   | D1  | RAG evaluation metrics         | Data-driven optimization  | Large  |
| **P3**   | B2  | Semantic chunking              | Better chunk boundaries   | Medium |
| **P3**   | D2  | Action-based observability     | Unified tracing           | Medium |
| **P3**   | E1  | iter.Seq2 streaming            | Idiomatic Go              | Medium |

**Recommended implementation order:** A1 → A2 → A3 → A6 → C2 → B1 → A4 → A5 → D1 → C1 → C3 → D2 → B2 → E1

---

## Benefits

- **User Benefits:** Better search results, faster responses, more relevant RAG answers, cost transparency
- **Developer Benefits:** Data-driven optimization via evaluation metrics, unified observability, idiomatic Go patterns
- **System Benefits:** Lower Vertex AI costs (~66% embedding reduction from cache), faster extraction, more robust fusion scoring
- **Business Benefits:** Core product quality improvements — better knowledge retrieval directly impacts user retention

---

## Risks & Considerations

- **Breaking Changes:** None — all improvements are additive with backward-compatible defaults
- **Performance Impact:** Positive across the board — the only trade-off is multi-query retrieval adding ~200ms for query expansion (offset by cache hits)
- **Security Impact:** Neutral — no auth or access control changes
- **Dependencies:** `hashicorp/golang-lru/v2` for LRU cache (widely used, well-maintained). `tiktoken-go` for token counting (optional, for DryRun). All other improvements use existing dependencies.
- **Migration Required:** No — no schema changes needed

---

## Testing Strategy

- [ ] Unit tests for each new algorithm (DBSF fusion, MMR ranking, lost-in-the-middle reordering)
- [ ] Integration tests for embedding cache (hit/miss/eviction behavior)
- [ ] E2E tests for multi-query retrieval and sentence window expansion
- [ ] Performance benchmarks before/after (search latency, extraction time)
- [ ] RAG evaluation test suite (golden-rag dataset)
- [ ] Regression tests ensuring existing search behavior unchanged when new features are disabled

---

## Related Items

- Related to improvement #014 (extraction evaluation enhancements)
- Related to improvement #015 (relationship triplet search enhancement — embedding deduplication overlap with A1)
- Builds on existing search infrastructure in `domain/search/`
- Builds on existing extraction pipeline in `domain/extraction/agents/`

---

## References

### Projects Analyzed

- [deepset-ai/haystack](https://github.com/deepset-ai/haystack) — RAG framework (pipeline architecture, fusion strategies, retrievers, rankers, evaluators)
- [firebase/genkit](https://github.com/firebase/genkit) — Google's AI framework (Go SDK, Action pattern, streaming, tracing)
- [langchain-ai/langgraph](https://github.com/langchain-ai/langgraph) — Multi-agent orchestration (graph execution, checkpointing, fan-out, retries)
- [Marker-Inc-Korea/AutoRAG](https://github.com/Marker-Inc-Korea/AutoRAG) — RAG evaluation & optimization (metrics, chunking tuning, benchmarking)
- [casibase/casibase](https://github.com/casibase/casibase) — Go AI knowledge platform (multi-model LLM, markdown chunking, DryRun, MCP)

### Papers

- [Lost in the Middle: How Language Models Use Long Contexts](https://arxiv.org/abs/2307.03172) — Liu et al., 2023
- [Reciprocal Rank Fusion](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf) — Cormack et al., 2009
- [The Maximal Marginal Relevance (MMR)](https://www.cs.cmu.edu/~jgc/publication/The_Use_MMR_Diversity_Based_LTMIR_1998.pdf) — Carbonell & Goldstein, 1998

### Key Source Files Referenced

- Haystack: `components/joiners/document_joiner.py`, `components/rankers/lost_in_the_middle.py`, `components/rankers/sentence_transformers_diversity.py`, `components/retrievers/sentence_window_retriever.py`, `components/retrievers/multi_query_embedding_retriever.py`, `components/evaluators/faithfulness.py`, `components/caching/cache_checker.py`, `components/preprocessors/embedding_based_document_splitter.py`
- Genkit: `go/core/action.go`, `go/core/tracing/tracing.go`, `go/ai/generate.go`, `go/ai/tools.go`, `go/ai/formatter.go`
- LangGraph: `langgraph/pregel/__init__.py`, `langgraph/checkpoint/base/__init__.py`, `langgraph/types.py`
- Casibase: `split/markdown.go`, `model/provider.go`, `agent/mcp.go`

---

## Notes

### What Emergent Already Does Well

This analysis also highlighted areas where Emergent is ahead of the open-source comparisons:

- **Template pack system** is more mature than any comparable schema/ontology system (versioning, migration, per-project customization, extraction guidelines)
- **Unified 3-way search** (graph objects + text chunks + relationships) is more comprehensive than most RAG systems which only search document chunks
- **PostgreSQL as single data store** (documents, vectors, graphs, queues, auth) avoids the operational complexity of multi-database architectures
- **MCP server with 29 tools** is one of the most complete MCP implementations for knowledge graph access
- **Quality checker with orphan detection** in the extraction pipeline is a pattern not found in comparable systems

### Interaction Between Improvements

Several improvements compound when combined:

- **A1 (cache) + A4 (multi-query):** Multi-query generates repeated similar queries across sessions — caching amortizes the extra embedding cost
- **A2 (reordering) + A5 (window expansion):** Window expansion provides more context per result, reordering ensures the best expanded contexts are at attention-favorable positions
- **A3 (DBSF) + A6 (MMR):** Better fusion scoring feeds into diversity ranking — both improve the quality of the final result set
- **D1 (evaluation) + everything else:** Evaluation metrics are the foundation for measuring whether any other improvement actually helps

---

**Last Updated:** 2026-02-14 by AI Agent
