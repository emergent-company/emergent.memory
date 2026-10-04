## Why

`domain/provider` now routes chat (and embedding) through a data-driven vendor
registry: each `ProviderDefinition` declares a `Protocol`, and dispatch reads
that definition. Reranking has no such dimension — there is no way to declare
that a vendor speaks a rerank API, and no adapter to execute one. As a result
reranking cannot be routed through a configured vendor the way chat/embedding
are, even though several of the vendors the registry already lists (Aliyun
DashScope, Volcengine, Tencent LKEAP) expose rerank endpoints, and Cohere's
`/rerank` shape is the de-facto compatibility standard.

WeKnora-parity backends to model: **Cohere shape, DashScope, Volcengine
NIM/knowledgebase, Tencent LKEAP**.

## What Changes

- Add a `RerankAPI` protocol dimension to `ProviderDefinition` and a
  `RerankAPIFor` dispatch helper, mirroring the existing chat `Protocol` axis.
- Add `ModelTypeRerank` and register rerank support on the vendors that expose
  it: a new rerank-only **Cohere** vendor, plus **Aliyun** (DashScope),
  **Volcengine** (knowledgebase), and **Tencent LKEAP**.
- Add `pkg/rerank`: a provider-agnostic `Reranker` interface plus four adapters
  matching the registry `RerankAPI` values — Cohere-compatible flat `/rerank`,
  DashScope nested `input`/`parameters`, Volcengine knowledgebase `datas`/`scores`,
  and Tencent LKEAP `RunRerank` (TC3-HMAC-SHA256 signed).
- Add a provider-side factory (`NewReranker` / `Registry.NewRerankerFor`) that
  selects the adapter from the registry definition and reuses the existing
  credential plumbing (`ResolvedCredential.APIKey`/`BaseURL`/`Extra`/
  `ExtraHeaders` from #1451/#1452).
- Wire an **optional** post-fusion rerank stage into `domain/search`, gated by
  the opt-in `SEARCH_RERANK_*` config. The default in-house fused ranking is
  unchanged when unconfigured; on provider error/timeout the stage falls back to
  the fused order.

## Non-Goals

- Per-project rerank configuration and model resolution (owned by the held
  `add-retrieval-reranking` change).
- LLM-as-reranker / chat-model reranking.
- Any change to the default fusion strategies or the in-house ranking.

## Capabilities

### Modified Capabilities

- `multivendor-provider-registry`: the registry gains a rerank API dimension and
  per-vendor rerank declarations, and adapter selection reads the definition.

### Added Capabilities

- `search` (delta): an optional, default-off, vendor reranker stage keyed off the
  registry, with graceful fallback.

## Related / Overlap

- **`add-retrieval-reranking` (held, spec-only, PR #1410)** overlaps on the
  search stage. That change owns the *config-driven* stage: per-project
  `RerankModel`/`RerankTopN` via `domain/modelconfig`, widening all three legs to
  `max(limit, RerankTopN)`, trace `rerank_metadata`, and an LLM-as-reranker
  adapter. This change owns the *provider-registry* axis it depends on: the
  rerank protocol dimension, the vendor adapters, and registry-keyed selection.
  It is **not** archived or superseded; when `add-retrieval-reranking` is
  implemented it should build on `pkg/rerank` and the `RerankAPI` dimension, and
  its `search` requirement widens this change's simpler env-gated stage.
- **`multivendor-provider-registry` (in flight)** owns the base registry
  protocol dimension; this change extends it with the orthogonal rerank axis.
- No migration is required: rerank configuration reuses the existing provider
  credential row plus the opt-in env vars; nothing new is persisted.
