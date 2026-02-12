# GraphRAG & Knowledge Graph Implementation Plan

**Status**: Draft
**Owner**: AI Agent Team
**Date**: February 11, 2026
**Based on**: [Market Analysis](../research/market/MARKET_ANALYSIS.md) and [Cognee Recommendations](../research/cognee/SUGGESTIONS.md)

---

## Executive Summary

This plan outlines the roadmap to evolve Emergent's Knowledge Graph capabilities from a static document store to a **dynamic, agent-ready memory system**.

Key drivers:

1.  **GraphRAG Performance**: Adopt Microsoft's hierarchical summary pattern for 3x better global query answering.
2.  **Temporal Correctness**: Adopt Graphiti's edge invalidation to handle changing facts without contradictions.
3.  **Usage Intelligence**: Implement access tracking to prioritize high-value content.

## Phase 1: Quick Wins (Week 1)

_Focus: Low effort, high immediate value, zero risk._

### 1.1 Access Tracking

**Goal**: Know what users are actually searching for to optimize content.

- **Schema**: Add `last_accessed_at` (TIMESTAMPTZ) to `kb.graph_objects`.
- **Logic**: Async update in `SearchService` whenever a node appears in search results.
- **Value**: Enables "Most Viewed Entities" analytics and cache warming.
- **Effort**: ~1 hour.

### 1.2 Conversation History Cache

**Goal**: Enable "What about in Q3?" follow-up questions.

- **Schema**: Add `context_summary` and `retrieval_context` to `core.chat_messages`.
- **Logic**: Retrieve last 5 turns in `ChatService`, inject into LLM prompt.
- **Value**: Coherent multi-turn conversations.
- **Effort**: ~1.5 hours.

---

## Phase 2: Temporal & Semantic Foundation (Month 1)

_Focus: Solving the "Stale Facts" problem and improving relationship search._

### 2.1 Temporal Edge Invalidation (The "Graphiti Pattern")

**Goal**: Handle conflicting facts over time (e.g., User moved from NY to SF).

- **Schema**: Add `valid_at` and `invalid_at` (TIMESTAMPTZ) to `kb.graph_relationships`.
- **Logic**:
  - On ingestion: Check if new edge conflicts with existing active edge.
  - If conflict: Update old edge `invalid_at = now()`, insert new edge.
- **Value**: "Time Travel" queries + no hallucinations of old states.
- **Effort**: ~1 day (Logic port from Graphiti).

### 2.2 Triplet Embeddings

**Goal**: Search by relationship meaning ("founded by", "located in").

- **Schema**: Add `embedding` (vector) to `kb.graph_relationships`.
- **Logic**:
  - Generate text: "Elon Musk [source] founded [rel] Tesla [target]".
  - Embed and store.
  - Update `SearchService` to query relationships alongside nodes.
- **Value**: 10-20% recall improvement for relationship-heavy queries.
- **Effort**: ~1.5 hours.

---

## Phase 3: Advanced GraphRAG (Month 2)

_Focus: Answering "Global" questions like "What are the main themes?"_

### 3.1 Community Detection & Summarization

**Goal**: Hierarchical understanding of the corpus.

- **Algorithm**: Implement Leiden clustering (or similar) on the graph structure.
- **Summarization**: Generate LLM summaries for each cluster.
- **Storage**: Store summaries as `CommunityNode` entities in Postgres.
- **Retrieval**: "Global Search" mode queries summaries first, then drill down.
- **Value**: Parity with Microsoft GraphRAG's "Global Search" capability.
- **Effort**: ~1 week.

### 3.2 Pluggable Retrieval Strategies

**Goal**: Allow domain-specific tuning (e.g., Temporal Search vs. Semantic Search).

- **Architecture**: Refactor `SearchService` to use a `Retriever` interface.
- **Implementations**:
  - `HybridRetriever` (Current: Vector + Text)
  - `GraphTraversalRetriever` (BFS from seed nodes)
  - `TemporalRetriever` (Time-aware traversal)
- **Value**: Flexibility to A/B test strategies.
- **Effort**: ~2-3 days.

---

## Phase 4: Ecosystem & Compliance (Month 3+)

### 4.1 LangChain & LlamaIndex Connectors

- **Goal**: Drop-in compatibility with the broader AI ecosystem.
- **Deliverable**: Python packages (`emergent-langchain`) that wrap our API.

### 4.2 Ontology Resolver

- **Goal**: Strict validation for Medical/Legal/Financial domains.
- **Logic**: Validate extracted entities against a YAML schema before insertion.

---

## Implementation Checklist

- [ ] **Step 1**: Execute [Access Tracking Plan](../research/cognee/SUGGESTIONS.md#1-access-tracking-★★★-priority-1---quick-win).
- [ ] **Step 2**: Execute [Conversation History Plan](../research/cognee/SUGGESTIONS.md#2-conversation-history-cache-★★★-priority-2---quick-win).
- [ ] **Step 3**: Design Schema Migration for Temporal Edges (`valid_at`, `invalid_at`).
- [ ] **Step 4**: Prototype Triplet Embeddings.
