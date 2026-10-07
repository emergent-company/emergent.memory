## 1. Embedding-job gating (`embedding-job-gating`)

- [ ] 1.1 Add the typed embedding error kinds to `pkg/embeddings` — `ErrNotConfigured`, a model-denied/status kind, `ErrDimensionMismatch`, and a transient kind — composed via `errors.Is`/`As`.
- [ ] 1.2 Unit test: `pkg/embeddings` errors classify correctly under `errors.Is`/`As` for not-configured, model-denied/status, dimension-mismatch, and transient, and the worker's terminal/transient decision consumes no error substring.
- [ ] 1.3 Make the noop fallback terminal: when no usable credential resolves, `pkg/embeddings` returns a terminal not-configured error instead of an empty vector via `NoopClient`.
- [ ] 1.4 Unit test: an unresolvable project yields a terminal not-configured error and never a nil/empty vector from `NoopClient`.
- [ ] 1.5 Resolve the effective embedding model at every enqueue chokepoint — graph-object write (incl. bulk), document chunking, relationship enqueue, and the sweep — using `modelconfig.Service.ResolveEmbeddingModel`; refuse/skip with no job row when empty, fail open on resolver error.
- [ ] 1.6 Unit test: each chokepoint (graph-object write, bulk write, chunking, relationship enqueue, sweep) inserts 0 job rows for an unresolvable project and enqueues normally for a resolvable one; a resolver error fails open.
- [ ] 1.7 Surface the refusal per caller: admin enqueue API `409 no_embedding_model`, MCP/tool enqueue a clear error (no row), content write/ingest logs and skips without failing the write; membership enforced before the guard.
- [ ] 1.8 Unit test: admin API returns `409 no_embedding_model` + 0 rows; MCP/tool returns a clear error + 0 rows; content write still succeeds with 0 rows; an unauthorized admin request returns `403` without consulting model resolution.
- [ ] 1.9 Introduce the project-level blocked state (`kb.projects.embedding_blocked` + `embedding_blocked_reason`, or a small dedicated table); workers and the sweep skip blocked projects.
- [ ] 1.10 Unit test: a blocked project's jobs are skipped by the chunk, graph-object, and graph-relationship workers and by the sweep; the blocked fact is stored at project scope, not per job.
- [ ] 1.11 Self-heal on config change: clear the block and set a backfill marker when a project's embedding model is set/updated; the sweep backfills under its throttle; no mass blocked→pending flip.
- [ ] 1.12 Unit test: setting/updating a blocked project's model clears the block and sets the backfill marker; no existing blocked job rows are bulk-flipped to `pending`.
- [ ] 1.13 Unify retry caps / dead-letter across the chunk, graph-object, and graph-relationship queues.
- [ ] 1.14 Unit test: a job exhausting retries in any of the three queues dead-letters under the same max-attempts threshold and the same `dead_letter` semantics.
- [ ] 1.15 Verification: from `apps/server` run `go build ./...`, `go vet ./...`, `go test ./...`; focused `go test ./domain/extraction/... ./pkg/embeddings/... ./domain/modelconfig/...`.

## 2. Provider model-name routing (`provider-model-name-routing`)

- [ ] 2.1 Make `modelref.StripRoutingPrefix` project-context aware: strip the first segment only when it names a recognized dialect / registered routing provider AND is not a project-registered instance/gateway slug.
- [ ] 2.2 Unit test: `gemini/gemini-embedding-001` with a project-registered `gemini` slug preserves the full reference; a bare dialect prefix is stripped; an unknown first segment is left unchanged.

## 3. Verification (provider-model-name-routing)

- [ ] 3.1 From `apps/server` run `go build ./...`, `go vet ./...`, `go test ./...`; focused `go test ./domain/provider/... ./pkg/modelref/...`.

## 4. Embedding worker throughput alignment (`embedding-worker-throughput`)

- [ ] 4.1 Re-express the worker's terminal-vs-transient classification in typed embedding error kinds (`errors.Is`/`As`), removing substring matching; make the noop fallback terminal.
- [ ] 4.2 Unit test: a typed terminal error (not-configured, model-denied/status, dimension-mismatch) marks the job failed with no retry; a typed transient error requeues with backoff; noop fallback is terminal, never requeued.
- [ ] 4.3 Bring the graph-relationship queue to the same max-attempts / dead-letter semantics as the graph-object and chunk queues.
- [ ] 4.4 Unit test: a relationship job exhausting retries or hitting a terminal error retires under the same rules as a graph-object job.
- [ ] 4.5 Verification: from `apps/server` run `go build ./...`, `go vet ./...`, `go test ./...`; focused `go test ./domain/extraction/... ./pkg/embeddings/...`.

## 5. Sweep admission ceilings (`embedding-sweep-ceilings`)

- [ ] 5.1 Change sweep admission to refuse projects that are blocked or whose effective model is unresolvable (resolvability, not `embedding_model != ''`), and skip blocked projects so a terminal job's object is not re-admitted every sweep.
- [ ] 5.2 Unit test: the sweep admits nothing for a blocked or unresolvable-model project; a terminal job's object is not re-enqueued on each sweep while its project is blocked; a resolvable project still admits under the depth formula.
- [ ] 5.3 Verification: from `apps/server` run `go build ./...`, `go vet ./...`, `go test ./...`; focused `go test ./domain/extraction/... ./pkg/embeddings/...`.

## 6. Embedding status surface (`embedding-status`)

- [ ] 6.1 Surface the project blocked state and reason on the embeddings status page; include blocked in the counts; broaden the "no embedding model" warning to cover misconfigured (unusable) models, not only unset.
- [ ] 6.2 Unit test: the page shows the blocked state and reason for a blocked project; blocked is included in counts; a misconfigured (unusable) model triggers the warning; a resolvable model shows no warning.
- [ ] 6.3 Verification: from `apps/server` run `go build ./...`, `go vet ./...`, `go test ./...`; focused `go test ./domain/extraction/... ./domain/modelconfig/...`.

## 7. Graph embedding refresh (`graph-embedding-refresh`)

- [ ] 7.1 Stamp `embedding_space` (resolved model id + dimension) at graph-object/relationship writes; re-embed on embedding-model change rather than compare across spaces; keep the 768 dimension.
- [ ] 7.2 Unit test: a model change re-embeds the object instead of comparing against the old model's vector; the stored `embedding_space` records the model id and 768 dimension.
- [ ] 7.3 Verification: from `apps/server` run `go build ./...`, `go vet ./...`, `go test ./...`; focused `go test ./domain/extraction/... ./pkg/embeddings/... ./domain/graph/...`.
