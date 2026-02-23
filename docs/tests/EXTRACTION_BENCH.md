# Extraction Benchmark

A standalone CLI tool that tests the Emergent document extraction pipeline end-to-end and scores the results against a hardcoded ground truth.

**Location:** `apps/server-go/cmd/extraction-bench/main.go`  
**Run log:** `docs/tests/extraction_bench_log.jsonl`

---

## What It Does

1. Generates a synthetic ~5KB film-criticism document covering 10 films and 21 people
2. Creates an extraction job via the Emergent API (`source_type: "manual"`)
3. Polls until the job completes
4. Queries extracted graph objects and relationships, filtering by `_extraction_job_id`
5. Scores precision and recall against hardcoded ground truth
6. Prints a results table and appends a JSONL record to the run log
7. Exits 0 if all thresholds are met, 1 otherwise

---

## Usage

```bash
# Build
go build ./apps/server-go/cmd/extraction-bench/

# Run against dev server
./extraction-bench \
  --host http://mcj-emergent:3002 \
  --api-key e2e-test-user \
  --project-id d95a2a42-2c12-4d69-8242-8452019f2158 \
  --poll-timeout 600

# Score an already-completed job (skips extraction, useful for iterating on scoring logic)
./extraction-bench \
  --host http://mcj-emergent:3002 \
  --api-key e2e-test-user \
  --project-id d95a2a42-2c12-4d69-8242-8452019f2158 \
  --job-id 3afe9ff6-5cb9-4b11-b310-f8eaa1156258
```

### Flags

| Flag                     | Default                                 | Description                                      |
| ------------------------ | --------------------------------------- | ------------------------------------------------ |
| `--host`                 | (required)                              | Base URL of the target server                    |
| `--api-key`              | (required)                              | Auth token (`e2e-test-user` for dev)             |
| `--project-id`           | (required)                              | Project ID to run against                        |
| `--job-id`               | —                                       | Skip extraction, score an existing completed job |
| `--poll-timeout`         | `120`                                   | Seconds to wait for job completion               |
| `--min-entity-recall`    | `0.60`                                  | Minimum entity recall threshold                  |
| `--min-entity-precision` | `0.50`                                  | Minimum entity precision threshold               |
| `--min-rel-recall`       | `0.50`                                  | Minimum relationship recall threshold            |
| `--min-rel-precision`    | `0.40`                                  | Minimum relationship precision threshold         |
| `--log-file`             | `docs/tests/extraction_bench_log.jsonl` | Path to append JSONL run record                  |

---

## Ground Truth

The benchmark scores against 31 entities and 29 relationships hardcoded in `main.go`:

**10 movies:** The Shawshank Redemption, The Godfather, The Dark Knight, Schindler's List, Pulp Fiction, Forrest Gump, The Matrix, Goodfellas, Fight Club, Inception

**21 people:** Morgan Freeman, Tim Robbins, Frank Darabont, Marlon Brando, Al Pacino, Francis Ford Coppola, Christian Bale, Christopher Nolan, Liam Neeson, Steven Spielberg, John Travolta, Quentin Tarantino, Tom Hanks, Robert Zemeckis, Keanu Reeves, Lana Wachowski, Ray Liotta, Martin Scorsese, Brad Pitt, David Fincher, Leonardo DiCaprio

**29 relationships** of types: `ACTED_IN`, `DIRECTED`, `WROTE`

---

## Scoring

**Entity matching:** case-insensitive fuzzy substring match on the `name` property (or `title` as fallback). An extracted entity counts as matched if it fuzzy-matches any ground-truth entity.

**Relationship matching:** looks up `src_entity_id` / `dst_entity_id` from the matched entity map, then checks:

- Normalized relationship type matches (see type normalization below)
- Either direction is accepted (person→movie or movie→person)

**Type normalization** — the LLM may produce inverted or synonym types. These are all accepted:

| Extracted type                                                 | Normalized to |
| -------------------------------------------------------------- | ------------- |
| `ACTED_IN`, `STARS`, `STARRED_IN`, `APPEARS_IN`, `FEATURED_IN` | `ACTED_IN`    |
| `DIRECTED`, `DIRECTED_BY`                                      | `DIRECTED`    |
| `WROTE`, `WRITTEN_BY`, `AUTHORED`, `WROTE_SCREENPLAY`          | `WROTE`       |

**Precision** = matched / total extracted  
**Recall** = matched / total ground truth  
**Spurious** = extracted items with no ground-truth match (reduce precision)

---

## Extraction Pipeline Details

The benchmark uses `source_type: "manual"` which bypasses document parsing and chunking entirely. The full document text is sent directly to the LLM in a single prompt.

**API calls made by the benchmark:**

| #   | Call                                      | Purpose                                     |
| --- | ----------------------------------------- | ------------------------------------------- |
| 1   | `POST /api/admin/extraction-jobs`         | Create job with inline schemas              |
| 2…N | `GET /api/admin/extraction-jobs/:id`      | Poll every 2s until completed               |
| N+1 | `GET /api/graph/objects/search?limit=500` | Fetch all objects, filter client-side       |
| N+2 | `GET /api/graph/relationships?limit=500`  | Fetch all relationships, filter client-side |

Note: `/api/graph/objects/search?extraction_job_id=...` is not implemented server-side; both objects and relationships are filtered client-side via the `_extraction_job_id` property.

**Server-side pipeline (per job):**

| Step           | What happens                                                                            |
| -------------- | --------------------------------------------------------------------------------------- |
| Worker poll    | Background worker picks up job every 5s                                                 |
| Text loading   | `source_metadata.text` returned as-is — no chunking                                     |
| Schema loading | Inline `extraction_config` (no template pack for this project)                          |
| LLM call 1     | Entity extraction — `gemini-3-flash-preview`, temperature 0.0, max output 65,536 tokens |
| LLM call 2     | Relationship extraction — same model/settings                                           |
| Quality check  | If orphan rate > 30%, retry relationship call (max 3 retries)                           |
| Persist        | Objects saved as `status: "suggested"` with `_extraction_job_id` property               |

**Typical token usage for this benchmark document (~5KB):**

- Input: ~1,700–2,300 tokens per call (prompt + document)
- Output: ~700–1,750 tokens per call (JSON response)
- Total: ~3,500–4,000 input + ~2,400 output across both calls

**Typical job completion time:** 2–6 minutes (dominated by worker queue wait, not LLM latency)

---

## Latest Results

From run on 2026-02-23 (job `3afe9ff6-5cb9-4b11-b310-f8eaa1156258`):

| Metric           | Value      | Threshold |
| ---------------- | ---------- | --------- |
| Entity Recall    | **100.0%** | 60.0%     |
| Entity Precision | **88.6%**  | 50.0%     |
| Rel Recall       | **100.0%** | 50.0%     |
| Rel Precision    | **85.3%**  | 40.0%     |

All thresholds met. The 4 spurious entities (Chuck Palahniuk, Jonathan Nolan, Mario Puzo, Stephen King) and 4 spurious relationships (3× BASED_ON, 1× SIBLING_OF) are legitimate extractions not covered by the current ground truth — the document mentions these people explicitly.

Full run history: `docs/tests/extraction_bench_log.jsonl`

---

## Known Limitations

- **Queue latency:** Jobs can sit pending for several minutes if the worker is busy with other jobs. Use `--poll-timeout 600` (10 min) to be safe.
- **Server-side `extraction_job_id` filter unimplemented:** `GET /api/graph/objects/search?extraction_job_id=...` always returns empty. The benchmark works around this by fetching all objects and filtering client-side.
- **Objects persisted as `suggested`:** Extracted entities are not auto-confirmed. They appear in graph queries but require manual review in the UI.
- **Remote DB:** The benchmark targets `mcj-emergent:3002` which uses a separate database from the local dev postgres. Token usage data is only visible in the remote server's Langfuse traces or `logs/extractions/` trace log files.
