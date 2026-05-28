# Knowledge Compilation Concepts for Emergent Memory

Research synthesis: Karpathy's LLM Wiki gist + Pinecone Nexus announcement (May 2026).
Evaluated against our extraction pipeline, domain-test bench, and unified search.

---

## The Central Idea: Compile Once, Retrieve Smart

Both Karpathy and Pinecone independently converge on the same insight:

> **RAG re-derives knowledge from scratch on every query. The better pattern compiles knowledge once at ingest time, then serves structured artifacts at query time.**

Karpathy: "The knowledge is compiled once and then kept current, not re-derived on every query."
Pinecone: "Nexus moves reasoning from retrieval to compilation."

Our extraction pipeline already does half of this — entities and relationships are compiled at ingest. But we stop there. The compiled graph is then queried by the same RAG-style chunk retrieval that Karpathy and Pinecone are arguing against. The synthesis layer — what sits between raw graph nodes and an agent's question — is missing.

---

## Concept 1: The Synthesis Layer (Highest Value)

### What it is

After entity/relationship extraction, generate **compiled summary artifacts** per entity cluster, domain, or topic. These are LLM-generated markdown or structured documents that synthesize across multiple source documents — concept pages, entity overviews, cross-document summaries.

Karpathy calls these wiki pages. Pinecone calls them artifacts. Both agree they should be:
- Persistent (written to storage, not ephemeral)
- Versioned (updated when new sources arrive)
- Pre-synthesized (reasoning done at compile time)
- Cross-referenced (linked to source entities and documents)

### Why it matters for us

Right now our stack is:
```
[documents] → extraction → [graph: entities + relationships]
                                  ↓
                           hybrid search (BM25 + vector)
                                  ↓
                           chat/ask (burns tokens re-synthesizing)
```

Every chat session re-reads raw chunks and re-derives relationships the extraction pipeline already found. The extracted graph exists but agents don't consume it as compiled synthesis — they re-discover it from chunks at query time.

A synthesis layer would look like:
```
[documents] → extraction → [graph: entities + relationships]
                                  ↓
                           synthesis agent → [compiled artifacts]
                                  ↓
                           KnowQL-style query → agent receives answer, not chunks
```

### What to build

A post-extraction synthesis step triggered after domain schema is finalized:

1. For each entity in the extracted graph, generate a summary page (name, type, properties, all source documents it appears in, all relationships, contradictions if any)
2. For each domain, generate a domain overview (what types exist, how many instances, key entities, cross-entity patterns)
3. Store synthesis documents as first-class objects in the graph, linked via `synthesized_from` relationships to source entities
4. Update synthesis documents incrementally when new documents are extracted into the same domain

This is directly enabled by our existing architecture: extraction pipeline produces the raw material, schema packs define the domain, the graph stores the result. Synthesis is a new agent pass over existing graph data.

---

## Concept 2: Lint Pass — Contradiction and Orphan Detection

### What it is

Karpathy's lint operation: periodically health-check the knowledge base. Find:
- **Contradictions**: two source documents assert conflicting values for the same entity property
- **Orphan entities**: extracted entities with no relationships (likely extraction noise or missed connections)
- **Stale claims**: entity properties from old documents superseded by newer sources
- **Missing concepts**: entities referenced many times but lacking a summary artifact
- **Suggested investigations**: gaps in coverage the user should fill

### Why it matters for us

Our extraction pipeline has no post-extraction quality signal. Once entities land in the graph, there is no mechanism to flag that:
- Document A says Company X was founded in 2010, Document B says 2012
- Person Y appears in 8 documents but has no relationships (orphan)
- A re-extracted document updated an entity but left old property values alongside new ones

The `relationship_builder.go` handles orphan retry at extract time, but only within a single document pass. Cross-document contradiction detection does not exist.

### What to build

A `LintAgent` or scheduled monitoring job that runs per-project:

1. **Contradiction detection**: for each entity, compare property values across all source documents. Flag where values differ. Confidence scoring can deprioritize low-confidence sources.
2. **Orphan report**: entities with zero relationships after retry threshold exceeded. Surface these to users for manual review or re-extraction.
3. **Stale property detection**: when a document is re-extracted, compare old vs new property values. Flag properties not updated by the newer extraction.
4. **Coverage gaps**: entity types mentioned in document text but not matching any extracted entity — suggests schema is missing a type.
5. **Output**: lint report stored as a graph object, surfaced in the admin UI, triggerable manually or on a schedule.

This aligns with our existing `monitoring` and `discoveryjobs` domains. The lint job is a new job type alongside `reextraction`.

---

## Concept 3: Extraction Diff Log

### What it is

Karpathy's `log.md`: append-only, per-ingest record of exactly what changed — pages created, pages updated, contradictions flagged, cross-references added.

Pinecone Nexus: every artifact is versioned — every answer traces back to its source data and transformations.

### Why it matters for us

Our extraction monitoring shows job status (queued/running/completed/failed) and object counts. It does not show *what changed* semantically — which entities were created vs enriched, which relationships were new vs already existed, which properties were updated and from what to what.

Users uploading document 15 into a project cannot see how it changed the knowledge graph. This is a transparency and trust problem.

### What to build

Per-extraction-job semantic diff:
- Entities created (new `temp_id` → new graph node)
- Entities enriched (matched existing entity, properties updated — old vs new value)
- Entities referenced (matched existing, no changes)
- Relationships created (new edge)
- Relationships already existed (deduped)
- Contradictions detected (property value conflict with existing)

Store diff as structured JSON on the extraction job record. Surface in the document detail UI as "What this document added to your knowledge base."

Implementation: the extraction pipeline already tracks `action: create | enrich | reference` per entity. Capturing the before/after state during the `enrich` write path gives us the diff with low additional cost.

---

## Concept 4: Entity Index Document

### What it is

Karpathy's `index.md`: a catalog of every wiki page with one-line summaries, organized by type. The LLM reads the index first when answering a query, then drills into specific pages. At moderate scale this replaces embedding search entirely.

Pinecone's composable retriever serves a similar function: structured, typed answer shaped for the agent's task — not raw chunks.

### Why it matters for us

Our `ask` agent currently queries hybrid search (BM25+vector over chunks) to find context. It does not have a structured catalog of what entities exist in the project. An agent answering "who are the key people in this project?" has to discover entities by searching chunks rather than reading a pre-built entity catalog.

For projects with 50+ documents and hundreds of entities, a per-project entity index would:
- Reduce the number of search rounds needed to answer a question
- Let the agent navigate the graph intentionally rather than discovering it through retrieval
- Lower token cost per query (read index → read targeted entity pages vs. read 20 chunks)

### What to build

A generated, auto-maintained entity index document per project:

```markdown
# Entity Index: [Project Name]

## People (14)
- **John Smith** — Patient, KCH medical records. Key relationships: treated_by Dr. Patel, resident_of Cambridge.
- **Sarah Chen** — Contact, personal notes + AI chat sessions. Key: colleague, meeting participant.

## Organizations (6)
- **Acme Corp** — Party in supplier agreement. Key: contracted_with TechSupply Ltd.

## Events (3)
...
```

Generated after each extraction job completes. Stored as a special document in the project. The ask/chat agent reads the index at the start of each session before searching chunks.

---

## Concept 5: Query Answer Filing (Compounding Loop)

### What it is

Karpathy's most important compounding mechanism: when a query generates a valuable insight, file it back into the wiki as a new page. The wiki grows not just from external source documents but from the user's own exploration.

"This way your explorations compound in the knowledge base just like ingested sources do."

Pinecone Nexus: "Persistent, durable knowledge representations that maintain context across sessions, users, and workflows. Not ephemeral retrieval results but rather curated artifacts that compound over time."

### Why it matters for us

Every chat session with the `ask` agent produces synthesized answers. These answers may synthesize across 5 documents, resolve a question that took 10 turns to arrive at, or surface a connection no single document makes explicit. Currently all of that is lost when the session ends.

A user who discovers "the medical lab results confirm the goal mentioned in personal notes to reduce cholesterol" has produced a cross-domain insight that is not in any source document. That insight should become a graph object.

### What to build

A "Save to knowledge base" action in the chat UI:

1. User selects a chat answer (or the agent offers to file it)
2. System creates a `Synthesis` graph object with:
   - `content`: the answer text
   - `query`: the question that generated it
   - `source_entities`: the entity refs used to produce it
   - `source_documents`: the document refs cited
   - `created_by`: user or agent
3. Synthesis objects are indexed and searchable alongside extracted entities
4. Future queries can find and cite synthesis objects as sources

This closes the compounding loop: documents → extracted entities → query → synthesis → future queries can cite synthesis.

---

## Concept 6: KnowQL-Style Query Interface

### What it is

Pinecone's KnowQL: a declarative query language for agents. Six primitives:

| Primitive | Meaning |
|---|---|
| `intent` | What task the agent is completing (not just what to find) |
| `filter` | Scope constraints (domain, date range, source type, entity type) |
| `provenance` | Field-level source citation required |
| `output_shape` | Exact structure the agent needs back — no re-parsing |
| `confidence` | Minimum confidence threshold per field |
| `budget` | Latency and depth cap (e.g., "answer in <500ms, max 3 hops") |

### How it compares to our unified search

Our unified search returns ranked results (chunks + objects). Agents re-parse these into answers.
KnowQL returns structured answers shaped for the task. Agents act directly on them.

The gap is `output_shape`, `provenance`, and `budget`. These are the primitives that:
- Eliminate re-parsing token cost
- Enable per-field auditability (compliance-critical domains: medical, legal)
- Give agents predictable latency SLOs

### What to build (minimal version)

A structured query endpoint alongside the existing search API:

```json
{
  "intent": "summarize patient health status",
  "filter": { "entity_type": "Person", "entity_name": "John Smith" },
  "output_shape": {
    "summary": "string",
    "key_metrics": [{ "name": "string", "value": "string", "unit": "string", "date": "string" }],
    "risk_flags": ["string"]
  },
  "provenance": true,
  "confidence": 0.7
}
```

Returns a structured object with each field populated from the graph and cited back to source documents. The LLM fills the output_shape from graph context rather than returning raw chunks.

This is closer to our ask agent than to raw search — but as a structured API rather than a chat interface. The agent gets a typed answer, not a conversation.

---

## Priority Assessment

| Concept | Effort | Impact | Aligns with existing arch | Recommend |
|---|---|---|---|---|
| Synthesis layer | High | Very High | Yes — post-extraction agent pass | Yes — core gap |
| Lint pass | Medium | High | Yes — new monitoring job type | Yes — quality signal missing |
| Extraction diff log | Low | Medium | Yes — extend existing job record | Yes — quick win |
| Entity index document | Medium | High | Yes — generated artifact, maintained by extraction worker | Yes — improves ask agent |
| Query answer filing | Medium | High | Yes — new Synthesis object type in graph | Yes — closes compounding loop |
| KnowQL query interface | High | Medium-High | Partial — extends search + ask | Later — after synthesis layer exists |

### Recommended sequence

1. **Extraction diff log** — lowest effort, immediate transparency value, enables lint
2. **Lint pass** — builds on diff log, high quality signal, directly tests with domain-test bench
3. **Entity index document** — enables better ask agent behavior with low model cost
4. **Synthesis layer** — the core architectural shift; enables everything else to compound
5. **Query answer filing** — closes the compounding loop once synthesis layer exists
6. **KnowQL interface** — formalizes the query contract once the artifact layer is solid

---

## Relationship to Our Current Architecture

```
TODAY:
[raw docs] ──upload──▶ extraction worker ──▶ [graph: entities + rels]
                                                      │
                                               hybrid search
                                                      │
                                              ask/chat (ephemeral)

WITH THESE CONCEPTS:
[raw docs] ──upload──▶ extraction worker ──▶ [graph: entities + rels]
                              │                       │
                         diff log ◀──────────────────┘
                              │
                        lint agent (periodic)
                              │
                        synthesis agent ──▶ [artifacts: entity pages, domain overview, index]
                              │                       │
                              │                structured query (KnowQL-style)
                              │                       │
                              └──────────▶ ask/chat ──┶──▶ [synthesis objects: filed answers]
                                                              │
                                                       future queries cite synthesis
```

The graph becomes a compounding knowledge asset rather than a static extraction output.

---

## Sources

- Karpathy, A. (2026-04-04). *LLM Wiki* [GitHub Gist]. https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f
- Karpathy tweet thread (2026-04-03 / 2026-04-04). https://twitter.com/karpathy/status/2039805659525644595
- Agentpedia Codes (2026-04-04). *Karpathy's LLM Wiki: The Complete Guide to His Idea File*. https://agentpedia.codes/blog/karpathy-llm-wiki-idea-file
- Liberty, E. & Ashutosh, A. (2026-05-04). *Pinecone Nexus: The Knowledge Engine for Agents*. https://www.pinecone.io/blog/knowledge-infrastructure-for-agents/
- Internal: `bench/domain-test/run_domain_test.py` — domain schema discovery bench
- Internal: `apps/server/domain/extraction/agents/varr_bench_test.go` — extraction F1 bench
