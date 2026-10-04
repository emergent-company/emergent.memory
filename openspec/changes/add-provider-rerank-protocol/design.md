## Context

Chat and embedding dispatch already flow through `domain/provider`: a
`ProviderDefinition` declares a wire `Protocol`, and `ProtocolFor` /
`ProtocolForBaseURL` select the client. Rerank has no equivalent. This change
adds the rerank axis orthogonally, keeping the registry the single source of
truth for "which wire shape does this vendor speak for rerank".

## Goals / Non-Goals

**Goals**

- A `RerankAPI` dimension on `ProviderDefinition` with a dispatch helper.
- A provider-agnostic `pkg/rerank` interface plus one adapter per verified
  backend shape.
- Registry-keyed selection reusing credential/extra-header plumbing.
- An opt-in, default-off search rerank stage with graceful fallback.

**Non-Goals**

- Per-project rerank config/model resolution (held `add-retrieval-reranking`).
- LLM-as-reranker, streaming, or chunk-level reranking.

## Decisions

### D1 — A separate `RerankAPI` type, not an extension of chat `Protocol`

A vendor's chat protocol and its rerank shape are independent axes: Aliyun is
OpenAI-compatible for chat and DashScope-native for rerank; Tencent LKEAP is
OpenAI-compatible for chat and TC3-signed `RunRerank` for rerank. Overloading
the chat `Protocol` would make one field carry two meanings. `ProviderDefinition`
therefore gains `RerankAPI RerankAPI`, and `Registry.RerankAPIFor(pt)` mirrors
`ProtocolFor(pt)`. Empty means "no rerank support".

### D2 — Distinct wire shapes, verified only

Four `RerankAPI` values, each backed by publicly documented (or first-party-SDK
verified) shapes:

| `RerankAPI` | Vendor | Shape | Verified via |
|---|---|---|---|
| `cohere` | Cohere + Cohere-compatible | `POST /rerank` `{model,query,documents[],top_n}` → `results[{index,relevance_score}]` | Cohere docs + OpenAPI spec |
| `dashscope` | Aliyun | `POST .../text-rerank/text-rerank` `{model,input:{query,documents},parameters:{top_n}}` → `output.results[]` | Aliyun Model Studio docs |
| `volcengine` | Volcengine knowledgebase | `POST /api/knowledge/service/rerank` `{rerank_model,datas:[{query,content}],top_k}` → `data.scores[]` (also `data:[...]`) | WeKnora `volcengineknowledge` client |
| `tencent-lkeap` | Tencent LKEAP | TC3-HMAC-SHA256 `RunRerank` `{Query,Docs,Model}` → `Response.ScoreList[]` | Tencent SDK models + API Explorer |

Deliberately **not** implemented (shape unverified / not publicly documented):
Volcengine Ark `POST /api/v3/rerank` and VikingDB direct `/api/vikingdb/rerank`
(SigV4). These are marked not-yet-supported rather than guessed; adding them is a
new `RerankAPI` value + adapter with no changes to callers.

### D3 — Adapter interface normalizes to `(Index, Score)`

```go
type Result struct { Index int; Score float64 }
type Reranker interface {
    Rerank(ctx context.Context, query string, documents []string) ([]Result, error)
}
```

Every backend ultimately produces "score per input document". Cohere/DashScope
return `index` + score; Volcengine/Tencent return positional score lists. The
adapters normalize to best-first `[]Result` so callers never branch on vendor.
`Index` refers to the position in the input `documents` slice.

### D4 — Selection reuses the registry + credential plumbing

`provider.NewReranker(def, cred, model)` maps `RerankAPI` → `pkg/rerank` API,
takes the base URL from `cred.BaseURL` or `DefaultBaseURLs[ModelTypeRerank]`,
the bearer key from `cred.APIKey`, per-vendor extras from `cred.Extra`
(Tencent `secret_key`/`region`), and header-declared extras from
`cred.ExtraHeaders`. `Registry.NewRerankerFor(pt, cred, model)` resolves the
definition first. This is the same plumbing #1451/#1452 established for chat.

### D5 — Search stage is opt-in and default-off

`domain/search.Service` holds an optional `rerank.Reranker` (nil by default).
The stage runs after `filterBelowMinScore` on the top-N fused candidates; the
untouched tail keeps its fused order. It is wired from opt-in env config
(`SEARCH_RERANK_PROVIDER`, `SEARCH_RERANK_MODEL`, `SEARCH_RERANK_API_KEY`,
`SEARCH_RERANK_BASE_URL`, `SEARCH_RERANK_TOP_N`, plus `SEARCH_RERANK_SECRET_KEY`/
`SEARCH_RERANK_REGION`) so behaviour is byte-for-byte unchanged when unset. On
any provider error or timeout the stage returns the pre-rerank fused order, so a
rerank failure can never fail a search. Per-project configuration is explicitly
out of scope here and remains with the held `add-retrieval-reranking` change.

## Risks / Trade-offs

- **Latency/cost on the search path.** Mitigated by the top-N bound (default 50,
  capped 100), a hard timeout, fallback, and default-off wiring.
- **Shape drift at vendors.** Mitigated by normalizing in one adapter per shape
  and testing request/response shapes with `httptest`.
- **Duplicate search-stage requirement with the held change.** Mitigated by
  scoping this delta to the registry-keyed, env-gated stage and recording the
  overlap in the proposal; the held change widens it later.
