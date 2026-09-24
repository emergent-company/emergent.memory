# Jev & Laya — System One decision models: research and Memory integration

Status: research note (no code changes).
Scope: what "Jev" and "Laya" are, what is actually usable today, and where such models
fit into Memory (extraction, agentic search, guardrails).
Related ruled-out path: [JEPA family](#appendix-a-jepa-family-ruled-out).

## TL;DR

- **Jev** = a real model, not an acronym and not JEPA. It is TypeSafe AI's "System One"
  model: you send a *state* plus *typed questions*, it returns **typed decisions with
  calibrated probabilities** and never generates text. Closed hosted API — **no weights,
  no published license, no paper**. $42 per billion input tokens.
- **Laya** = Convai Innovations' open-weight (Apache-2.0) analogue of the same idea,
  explicitly shipped as the open alternative to Jev, including a Jev-compatible HTTP
  server. This is the usable one.
- Relevance to Memory: these models target the tier Memory currently overpays for —
  **decisions that need no generation**. They do **not** replace LLM content extraction:
  division of labour is *Laya decides, the LLM generates*.
- Recommended first step: register Laya as a decision tool (MCP or HTTP) and use it for
  cheap gates — extraction triage, tool routing, relevance grading — after calibrating on
  our own data. Do **not** adopt zero-shot: base checkpoints are near chance on
  typed-decision tasks.

---

## 1. Jev (TypeSafe AI)

Source: <https://typesafe.ai> (homepage, fetched and verified). Docs referenced:
<https://docs.typesafe.ai>.

| Property | Value |
|---|---|
| What it is | "First System One model" — non-generative typed-decision model |
| Interface | Structured questions + state in → typed decisions + probabilities/confidence out |
| Training | RLCD (Reinforcement Learning for Calibrated Decisions); "new architecture, new sampler, new training algorithm" |
| Pricing | **$42 / billion input tokens** (= $0.042 / 1M); claimed 238× cheaper than the LLM they compare against |
| Vendor claims | 193.6× faster / 444.6× cheaper on System One workflows; "zero hallucinations" via confidence escalation |
| Weights | **Not released** (closed API) |
| License | **Not published** |
| Paper / benchmarks | **None published** — no standard accuracy numbers on the site |

Vendor's own example comparison: TypeSafe **$0.000081 / 0.114 s** vs an LLM
**$0.013880 / 8.566 s** for the same workflow. These are vendor figures — no independent
replication captured here.

**Caveats.** No weights, no license, no paper, no eval numbers. Pricing is vendor-only.
Claims like "zero hallucinations" and the intelligence-per-dollar chart are marketing;
treat as unvalidated until we benchmark on our own tasks.

---

## 2. Laya (Convai Innovations)

Source: <https://huggingface.co/convaiinnovations/laya> (model card, fetched and verified),
<https://github.com/NandhaKishorM/laya>, PyPI `laya`, demo
<https://huggingface.co/spaces/convaiinnovations/laya-demo>.

**Multilingual, non-autoregressive System 1 decision model.** Give it a *state* (text,
email, ticket, JSON) and *typed questions*; it returns typed answers with calibrated
probabilities in one forward pass. It never generates text — nothing to parse, nothing to
hallucinate.

| Checkpoint | Backbone | Params | Context | Best at |
|---|---|---|---|---|
| `convaiinnovations/laya` | ModernBERT-large | 421M | 512 | English, guardrails, email triage |
| `convaiinnovations/laya-multilingual` | mmBERT-base | 322M | 1024 (→8k) | 100+ languages, ~2.2× faster |
| `convaiinnovations/laya-typed-decisions` | ModernBERT-large | 421M | 1024 | typed-decision workflows (0.766 acc) |

- **License:** Apache-2.0 (code + weights), commercial use permitted.
- **Primitives:** `choice` (labelled options), `score` (ordinal), `noul` (boolean).
  The answer space is defined **at request time** — new question schemas need no retraining.
- **Architecture:** frozen-ish encoder + from-scratch decision head (2 transformer layers,
  option-marker scorer, act/escalate head). Each option is scored at its own `[MASK]`
  token, then softmaxed over that question's options. All questions in a call are answered
  in a **single forward pass**.
- **Training:** RLCD — policy reports a distribution, exploration adds zero-mean Gaussian
  noise to logits, reward is a strictly proper scoring rule (log + spherical, plus ranked
  probability score for ordinal). Updates are REINFORCE with a group-mean baseline.
- **Router:** auto-detects script/language in <0.5 ms and dispatches to the optimal
  checkpoint. Recommended production path.

### Latency / throughput (Tesla T4, from the card)

| questions per call | `laya` | `laya-multilingual` |
|---|---|---|
| 1 | 39.5 ms | 32.8 ms |
| 10 | 158.6 ms (15.9 ms/q) | 72.3 ms (7.2 ms/q) |
| 50 | 771 ms | 337 ms (6.8 ms/q) |

103–332 questions/sec batched on a single T4. Reported ~6–8× faster single-question than
independently measured Jev p50 (236–276 ms, third-party benchmark repos).

### Deployment surfaces (all confirmed on the model card)

- `pip install laya` → `Router`, batch and routed-batch scoring, prediction hooks.
- **`laya-serve`** — self-hosted **Jev-compatible HTTP server**, `POST /v1/systemone`,
  same request/response shape as TypeSafe Jev (swap base URL); optional bearer auth.
- **`laya[mcp]`** — optional **MCP server**.
- **`laya.onnx_agent.ONNXAgent`** — ONNX Runtime, plus `laya-ts` (Node + browser) giving
  identical answers; `laya[langchain]` integrations; `laya.load(..., fast=True)`
  (TileLang GPU path) and `torch.compile`.
- 40 finetunes / 26 quantizations published against the base, 32 HF Spaces using it.

### Honest limits (from the model card's own "Honest Limits")

1. **Base checkpoints are near chance zero-shot** on typed-decisions: 0.362 (`laya`) and
   0.342 (multilingual) vs a 0.461 majority-class baseline. The headline 0.766 belongs to
   the checkpoint fine-tuned on that benchmark's own training split. → Laya is a **fast
   base to specialise**, not a zero-shot decision engine.
2. **High-cardinality `choice` degrades badly.** Options share a fixed `head_max_len`
   budget (192 tokens English / 256 multilingual), so a 77-label question gives ~3–4 tokens
   per label: Banking77 0.425 vs Jev 0.870. Mitigations: raise
   `head_max_len`/`max_len`, or coarse-to-fine hierarchical choice. Jev supports up to 255
   options out of the box.
3. **`score` (ordinal) is the weakest primitive** — SST-5 0.372.
4. **`noul` can follow its own option labels instead of the state** (English checkpoint),
   returning confident "no" for clearly positive input (issue #156). Workaround: ask the
   same question as a 2-option `choice` with neutral keys.
5. **`action.act_probability` carries no usable signal yet** (issue #185, AUROC 0.30);
   gate on `confidence` instead (AUROC 0.77).
6. **Ships over-confident.** Per-(question type, option count) temperature fitting moves
   mean ECE **0.466 → 0.081** (`laya`) and **0.314 → 0.106** (multilingual). Must be
   calibrated on domain data before probabilities are trusted.
7. **English root only.** The English checkpoint fails non-Latin scripts while staying
   confident (Khmer: 0.000 accuracy at 0.952 confidence) — routing is mandatory, not optional.
8. Where Jev leads: >20 options at default settings, and soft distribution matching
   (0.580 vs 0.471 soft accuracy).

---

## 3. Where this fits Memory

Memory's current architecture pays LLM prices for a set of *decisions*, not generations.
Laya targets exactly that tier.

### 3.1 Extraction triage (highest leverage)

- **Existing precedent:** `apps/server/domain/extraction/document_classifier.go:57-164`
  already does vector-similarity first, LLM only when ambiguous. Laya replaces the LLM
  fallback with a ~7 ms, $0, calibratable classifier.
- **Candidate gates:** "does this chunk contain extractable entities?", "which schema /
  blueprint pack applies?", "is this document in scope for extraction at all?"
- **Seam:** extraction is currently purely LLM-agent driven
  (`apps/server/domain/extraction/agents/pipeline.go:112`). A non-LLM pre-filter is a new
  stage, not a `model.LLM` implementation.

### 3.2 Extraction decision points (low cardinality only)

- Entity dedupe / canonicalisation: "same node?" as a 2-option `choice` with neutral keys
  (avoid `noul` — see limit 4).
- Relationship-type routing among a small option set (<20 to stay clear of limit 2).

### 3.3 Agentic search

- **Tool selection** over the agent's whitelisted `ToolPool`
  (`apps/server/domain/agents/toolpool.go:98-120,351-400`).
- **Relevance grading** of retrieved context (the "Jev-as-a-Judge" pattern) — gate the
  expensive synthesis step.
- **Loop control**: stop/continue, query-rewrite decisions.
- **Memory-write gating**: should this turn be persisted?
- **Guardrails / moderation** — Laya's advertised strength.

### 3.4 Observability and grading

`laya-typed-decisions` ships an agent-trace-observability workflow (0.730 accuracy), which
maps onto grading agent runs and stiffening e2e assertions.

### 3.5 Integration paths (cheapest → deepest)

1. **MCP tool, no server changes.** Register `laya[mcp]` as an external tool via
   `apps/server/domain/mcpregistry/service.go:435-527` (`CallExternalTool`). Lowest risk,
   immediately usable by every agent through `ToolPool`.
2. **HTTP loopback.** Run `laya-serve` and dispatch from `ExecuteTool`
   (`apps/server/domain/mcp/service.go:2082`) — `search-knowledge`
   (`apps/server/domain/mcp/query_tools.go:66-199`) is the existing template for a
   loopback tool.
3. **Native model class.** `provider.ModelType` is `embedding | generative` only
   (`apps/server/domain/provider/entity.go:19-25`) and `model_type` in
   `provider_supported_models` (`apps/server/migrations/00038_create_provider_supported_models.sql:8-9`)
   has no slot for a router/classifier. Adding one, plus a backend mirroring the existing
   hosted-HTTP precedent `apps/server/pkg/adk/openai_model.go:14-52`, gives a first-class
   decision model usable by all domains.
4. **On-device.** ONNX / `laya-ts` ports are viable in `apps/connector.mac` and the iOS
   app for local routing at zero API cost.

### 3.6 Non-goals

- **Not** a replacement for `adk.ModelFactory.CreateModel`
  (`apps/server/pkg/adk/model.go:106,192`) — entity/relationship *content* still comes from
  an LLM. The only extraction model seam is that factory; Laya sits before or beside it.
- **Not** a text embedding model. Embeddings stay in
  `apps/server/pkg/embeddings/client.go:12` (`EmbeddingDimension = 768`), which is
  hardcoded and matched by `vector(768)` columns.

---

## 4. Recommendation

1. **Do not adopt Jev now.** Closed API, unpublished license, no eval numbers, and Laya
   self-hosts the same interface for $0. Revisit only if we need >20 options or soft
   distribution matching.
2. **Spike Laya as an MCP decision tool** (path 1). Cheap, reversible, no schema or
   `ModelType` changes.
3. **Calibrate before trusting.** Fit per-(question type, option count) temperature on our
   own chunk/tool/routing data and measure accuracy + ECE against current LLM calls.
   Uncalibrated ECE is 0.466 — this step is mandatory, not polish.
4. **Fine-tune per domain.** Zero-shot is near chance on typed-decision tasks; the value
   is in specialisation on our data.
5. **Respect the cardinality ceiling** (<20 options) until `head_max_len` is raised and
   re-measured.
6. **Keep routing in play** — English checkpoint silently fails non-Latin scripts at high
   confidence.

---

## Appendix A: JEPA family (ruled out)

Earlier investigation of the "JEPA" reading is recorded here so the path is not re-litigated.

- JEPA (I-JEPA, V-JEPA, V-JEPA 2, V-JEPA 2.1) is a **vision/video self-supervised**
  family. **No released JEPA model accepts text or emits retrieval-grade text
  embeddings.** Text-side "JEPA" work (LLM-JEPA, DLLM-JEPA, T-JEPA) is a fine-tuning
  objective, not a retrievable model.
- **Licensing:** I-JEPA, V-JEPA v1, LeJEPA are CC BY-NC 4.0 (non-commercial);
  V-JEPA 2 is the only member with commercial-use weights.
- **LeJEPA** (SIGReg objective) is notably **without official checkpoints** as of this
  note — treat wider claims cautiously.
- **"World model for agents"** claims are embodied visual planning only; there is no
  symbolic/tool/retrieval planning mechanism.
- Only legitimate Memory use, if ever needed: V-JEPA 2 as a **visual encoder** for
  video/image nodes in a multimodal graph, requiring non-768 vector columns and a
  text→visual alignment model. Not pursued.

## Appendix B: Verification status

**Verified directly against primary sources** (fetched during this research):

- <https://typesafe.ai> — Jev identity, typed decisions, calibration/confidence framing,
  RLCD training, $42/B pricing, vendor speed/cost claims.
- <https://huggingface.co/convaiinnovations/laya> — checkpoints, params, contexts,
  Apache-2.0 license, primitives, architecture, RLCD training, latency table, `laya-serve`
  / `laya[mcp]` / ONNX / `laya-ts` surfaces, and all eight "Honest Limits".

**Reported but NOT independently verified** (search-result level only — treat as leads):

- Derivative/adjacent projects: `Open-Jev` (Zefan-Cai), `this-that-model-1.0` (flock-io),
  `AnyJev` (nokia-applied-research), `kev` (jaredpalmer), `Visual Jev`, `simple-jev`,
  `open-jev-deberta-v3-large`, KaLM reranker/jev collection.
- Research using Jev: Jev-as-a-Judge, REFLEX, Jev-Mem, JEVQA, and others.
- Third-party Jev latency benchmarks (AbdelStark, nibzard) — the model card cites these
  for the 236–276 ms p50 figure.
- Any claim about Jev's founding team or Jev-vs-Laya superiority beyond the model card's
  own published tables.

**Note on dates:** the Jev launch and the Laya 0.3.11 release are recent; version numbers
and benchmark tables should be re-checked before they are cited in planning or specs.
