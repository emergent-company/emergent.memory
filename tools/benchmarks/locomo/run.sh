#!/usr/bin/env bash
# LoCoMo end-to-end benchmark runner
# Usage: ./run.sh --project <id> [options]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Defaults
PROJECT=""
LIMIT=""
CONVERSATIONS=""
SESSIONS=""
INGEST_MODE="raw"
SCHEMA_POLICY="auto"
DRY_RUN=""
PARALLEL="1"
CATEGORIES=""
RESULTS_DIR="results"
SKIP_INGEST=false
SKIP_QUERY=false
SKIP_EVAL=false

usage() {
    cat <<EOF
Usage: $0 --project <id> [options]

  --project <id>              Memory project ID (or set MEMORY_PROJECT_ID)
  --limit <n>                 Max conversations (default: all)
  --conversations <i,j,...>   0-based conversation indices (e.g. "0")
  --sessions <range>          Session range to ingest, e.g. "1-3" (default: all)
  --ingest-mode raw|obs|both  (default: raw)
  --schema-policy auto|reuse_only  (default: auto)
  --dry-run                   Branch+write but do not merge
  --parallel <n>              Conversation-level parallelism (default: 1)
  --categories <n,n,...>      QA category numbers to evaluate (default: all)
  --results-dir <path>        (default: results/)
  --skip-ingest               Skip ingest, run query+eval only
  --skip-query                Skip query, run ingest+eval only
  --skip-eval                 Skip eval
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --project)        PROJECT="$2"; shift 2 ;;
        --limit)          LIMIT="$2"; shift 2 ;;
        --conversations)  CONVERSATIONS="$2"; shift 2 ;;
        --sessions)       SESSIONS="$2"; shift 2 ;;
        --ingest-mode)    INGEST_MODE="$2"; shift 2 ;;
        --schema-policy)  SCHEMA_POLICY="$2"; shift 2 ;;
        --dry-run)        DRY_RUN="--dry-run"; shift ;;
        --parallel)       PARALLEL="$2"; shift 2 ;;
        --categories)     CATEGORIES="$2"; shift 2 ;;
        --results-dir)    RESULTS_DIR="$2"; shift 2 ;;
        --skip-ingest)    SKIP_INGEST=true; shift ;;
        --skip-query)     SKIP_QUERY=true; shift ;;
        --skip-eval)      SKIP_EVAL=true; shift ;;
        -h|--help)        usage; exit 0 ;;
        *)                echo "Unknown arg: $1"; usage; exit 1 ;;
    esac
done

if [[ -z "$PROJECT" && -z "${MEMORY_PROJECT_ID:-}" ]]; then
    echo "ERROR: --project required (or set MEMORY_PROJECT_ID)"
    exit 1
fi

PROJECT="${PROJECT:-$MEMORY_PROJECT_ID}"

DATA="data/locomo10.json"
if [[ ! -f "$DATA" ]]; then
    echo "ERROR: $DATA not found."
    echo "Download locomo10.json from https://github.com/snap-research/locomo and place it in locomo/data/"
    exit 1
fi

mkdir -p "$RESULTS_DIR"

build_args() {
    local args="--project $PROJECT --results-dir $RESULTS_DIR"
    [[ -n "$LIMIT" ]]         && args="$args --limit $LIMIT"
    [[ -n "$CONVERSATIONS" ]] && args="$args --conversations $CONVERSATIONS"
    [[ -n "$SESSIONS" ]]      && args="$args --sessions $SESSIONS"
    echo "$args"
}

if ! $SKIP_INGEST; then
    echo "=== [1/3] Ingest ==="
    IARGS="$(build_args) --ingest-mode $INGEST_MODE --schema-policy $SCHEMA_POLICY --parallel $PARALLEL"
    [[ -n "$DRY_RUN" ]] && IARGS="$IARGS $DRY_RUN"
    python3 ingest.py $IARGS
fi

if ! $SKIP_QUERY; then
    echo ""
    echo "=== [2/3] Query ==="
    QARGS="$(build_args)"
    [[ -n "$SESSIONS" ]]   && QARGS="$QARGS --sessions $SESSIONS"
    [[ -n "$CATEGORIES" ]] && QARGS="$QARGS --categories $CATEGORIES"
    python3 query.py $QARGS
fi

if ! $SKIP_EVAL; then
    echo ""
    echo "=== [3/3] Evaluate ==="
    python3 evaluate.py --results-dir "$RESULTS_DIR"
fi
