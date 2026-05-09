# LongMemEval Benchmark — Memory Knowledge Graph

Tests the full `memory remember` → `memory query` pipeline against
[LongMemEval](https://github.com/xiaowu0162/LongMemEval) (ICLR 2025).

## What it tests

500 questions over multi-session user–assistant conversations. Questions span 5 types:

| Type | Description |
|------|-------------|
| `single-session-user` | Fact stated once by user in one session |
| `single-session-assistant` | Fact stated once by assistant in one session |
| `knowledge-update` | Later session overrides earlier fact |
| `cross-session` | Answer requires combining facts from multiple sessions |
| `temporal-reasoning` | Answer requires reasoning about time |

## Dataset splits

| Split | Sessions per question | Questions | Notes |
|-------|-----------------------|-----------|-------|
| `oracle` | Evidence sessions only | 500 | Best for smoke test — no noise |
| `s` | ~40 sessions | 500 | Standard eval |
| `m` | ~500 sessions | 500 | Hard — full haystack |

## Setup

```bash
# 1. Install deps
cd tools/benchmarks
pip install -r requirements.txt

# 2. Set env vars
export MEMORY_API_URL=http://localhost:3012
export MEMORY_API_KEY=<your-api-key>
export MEMORY_PROJECT_ID=<your-project-id>
```

## Smoke test (recommended first run)

```bash
cd tools/benchmarks/longmemeval

# Download oracle split + run 3 questions end-to-end
./run.sh --project <id> --split oracle --limit 3
```

## Full benchmark

```bash
# All 500 questions, oracle split
./run.sh --project <id> --split oracle --parallel 5

# Standard split
./run.sh --project <id> --split s --parallel 5
```

## Options

```
--project <id>              Memory project ID
--split oracle|s|m          Dataset split (default: oracle)
--limit <n>                 Max questions (default: all)
--question-types <types>    e.g. "single-session-user,knowledge-update"
--schema-policy auto|reuse_only
--dry-run                   Write but do not merge
--parallel <n>              Question-level workers (default: 1)
--results-dir <path>        (default: results/)
--llm-judge                 Enable LLM-as-judge scoring (needs EVAL_LLM_* env vars)
--skip-download / --skip-ingest / --skip-query / --skip-eval
```

## Output

```
results/
  ingest_results.jsonl    # per-session ingest status
  query_results.jsonl     # per-question predictions
  eval_summary.json       # token F1 + exact match + optional LLM judge, by question type
```

## Metrics

- **Token F1** (primary) — SQuAD-style token overlap
- **Exact Match** — normalised string equality
- **LLM Judge** (optional) — 0/1 correctness scored by GPT-4o (or `EVAL_LLM_MODEL`)

## Environment variables

| Var | Default | Description |
|-----|---------|-------------|
| `MEMORY_API_URL` | `http://localhost:3012` | Memory server |
| `MEMORY_API_KEY` | — | API key |
| `MEMORY_PROJECT_ID` | — | Project ID |
| `EVAL_LLM_BASE_URL` | OpenAI | LLM judge base URL |
| `EVAL_LLM_API_KEY` | `$OPENAI_API_KEY` | LLM judge key |
| `EVAL_LLM_MODEL` | `gpt-4o` | LLM judge model |
