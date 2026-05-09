#!/usr/bin/env bash
# LongMemEval end-to-end benchmark runner
# Usage: ./run.sh --project <id> [options]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Defaults
PROJECT=""
SPLIT="oracle"
LIMIT=""
QUESTION_TYPES=""
SCHEMA_POLICY="auto"
DRY_RUN=""
PARALLEL="1"
RESULTS_DIR="results"
LLM_JUDGE=""
SKIP_DOWNLOAD=false
SKIP_INGEST=false
SKIP_QUERY=false
SKIP_EVAL=false

usage() {
    cat <<EOF
Usage: $0 --project <id> [options]

  --project <id>              Memory project ID (or set MEMORY_PROJECT_ID)
  --split oracle|s|m          Dataset split (default: oracle)
  --limit <n>                 Max questions (default: all)
  --question-types <types>    Comma-separated, e.g. "single-session-user,knowledge-update"
  --schema-policy auto|reuse_only  (default: auto)
  --dry-run                   Branch+write but do not merge
  --parallel <n>              Question-level parallelism (default: 1)
  --results-dir <path>        (default: results/)
  --llm-judge                 Enable LLM-as-judge scoring in evaluate step
  --skip-download             Skip dataset download
  --skip-ingest               Skip ingest step
  --skip-query                Skip query step
  --skip-eval                 Skip eval step
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --project)         PROJECT="$2"; shift 2 ;;
        --split)           SPLIT="$2"; shift 2 ;;
        --limit)           LIMIT="$2"; shift 2 ;;
        --question-types)  QUESTION_TYPES="$2"; shift 2 ;;
        --schema-policy)   SCHEMA_POLICY="$2"; shift 2 ;;
        --dry-run)         DRY_RUN="--dry-run"; shift ;;
        --parallel)        PARALLEL="$2"; shift 2 ;;
        --results-dir)     RESULTS_DIR="$2"; shift 2 ;;
        --llm-judge)       LLM_JUDGE="--llm-judge"; shift ;;
        --skip-download)   SKIP_DOWNLOAD=true; shift ;;
        --skip-ingest)     SKIP_INGEST=true; shift ;;
        --skip-query)      SKIP_QUERY=true; shift ;;
        --skip-eval)       SKIP_EVAL=true; shift ;;
        -h|--help)         usage; exit 0 ;;
        *)                 echo "Unknown arg: $1"; usage; exit 1 ;;
    esac
done

if [[ -z "$PROJECT" && -z "${MEMORY_PROJECT_ID:-}" ]]; then
    echo "ERROR: --project required (or set MEMORY_PROJECT_ID)"
    exit 1
fi

PROJECT="${PROJECT:-$MEMORY_PROJECT_ID}"

DATA="data/longmemeval_${SPLIT}.json"
mkdir -p "$RESULTS_DIR" data

build_args() {
    local args="--project $PROJECT --split $SPLIT --results-dir $RESULTS_DIR"
    [[ -n "$LIMIT" ]]          && args="$args --limit $LIMIT"
    [[ -n "$QUESTION_TYPES" ]] && args="$args --question-types $QUESTION_TYPES"
    echo "$args"
}

if ! $SKIP_DOWNLOAD && [[ ! -f "$DATA" ]]; then
    echo "=== [0/3] Download ==="
    python3 download.py --split "$SPLIT" --output data
fi

if ! $SKIP_INGEST; then
    echo ""
    echo "=== [1/3] Ingest ==="
    IARGS="$(build_args) --schema-policy $SCHEMA_POLICY --parallel $PARALLEL"
    [[ -n "$DRY_RUN" ]] && IARGS="$IARGS $DRY_RUN"
    python3 ingest.py $IARGS
fi

if ! $SKIP_QUERY; then
    echo ""
    echo "=== [2/3] Query ==="
    python3 query.py $(build_args)
fi

if ! $SKIP_EVAL; then
    echo ""
    echo "=== [3/3] Evaluate ==="
    EARGS="--results-dir $RESULTS_DIR"
    [[ -n "$LLM_JUDGE" ]] && EARGS="$EARGS $LLM_JUDGE"
    python3 evaluate.py $EARGS
fi
