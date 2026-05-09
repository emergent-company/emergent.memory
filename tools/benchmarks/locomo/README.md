# LoCoMo Benchmark — Memory Knowledge Graph

Tests the full `memory remember` → `memory query` pipeline against the
[LoCoMo](https://github.com/snap-research/locomo) benchmark (ACL 2024).

## What it tests

10 long multi-session conversations (two personas, ~19 sessions each, months of
simulated chat). The agent must extract entities and facts during ingest, then
answer QA pairs across 5 categories:

| Cat | Type | Cross-session? |
|-----|------|---------------|
| 1 | Single-hop | No |
| 2 | Temporal | Mostly yes |
| 3 | Open-domain | No |
| 4 | Single-session detail | No |
| 5 | Adversarial | No (unanswerable) |

## Setup

```bash
# 1. Get the data
# Download locomo10.json from https://github.com/snap-research/locomo
# Place it at: tools/benchmarks/locomo/data/locomo10.json

# 2. Install deps
cd tools/benchmarks
pip install -r requirements.txt

# 3. Set env vars
export MEMORY_API_URL=http://localhost:3012
export MEMORY_API_KEY=<your-api-key>
export MEMORY_PROJECT_ID=<your-project-id>
```

## Smoke test (recommended first run)

```bash
cd tools/benchmarks/locomo

# 1 conversation, sessions 1-3 only, single-hop QA only (~3 ingest calls, ~15-20 QA pairs)
./run.sh --project <id> --limit 1 --sessions 1-3 --categories 1
```

## Full benchmark

```bash
# All 10 conversations, all sessions, all categories
./run.sh --project <id> --parallel 5
```

## Options

```
--project <id>              Memory project ID
--limit <n>                 Max conversations (default: all)
--conversations <i,j>       0-based indices e.g. "0" or "0,1,2"
--sessions <range>          Session range to ingest e.g. "1-3" (default: all)
--ingest-mode raw|obs|both  raw=full dialogue, obs=extracted facts (default: raw)
--schema-policy auto|reuse_only
--dry-run                   Write but do not merge
--parallel <n>              Conversation-level workers
--categories <n,n>          QA category filter e.g. "1,2"
--results-dir <path>        (default: results/)
--skip-ingest / --skip-query / --skip-eval
```

## Output

```
results/
  ingest_results.jsonl    # per-session ingest status
  query_results.jsonl     # per-question predictions
  eval_summary.json       # token F1 + exact match, overall + per-category
```

## Metrics

- **Token F1** (primary) — SQuAD-style, token overlap between predicted and gold answer
- **Exact Match** — normalised string equality

## Ingest modes

| Mode | What is sent to `memory remember` |
|------|-----------------------------------|
| `raw` | Full dialogue turns with timestamps (tests full extraction pipeline) |
| `observations` | GPT-3.5 pre-extracted factual bullets (tests storage+retrieval only) |
| `both` | Both — richer context |

Start with `raw` for the most meaningful test.
