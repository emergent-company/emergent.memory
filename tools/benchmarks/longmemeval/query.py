#!/usr/bin/env python3
"""
LongMemEval query — runs questions through Memory's query agent and saves predictions.

Usage:
    python query.py --project <id> [options]

Options:
    --project        Memory project ID
    --data           Path to JSON file (default: data/longmemeval_oracle.json)
    --split          oracle | s | m  (determines default data file)
    --limit          Max questions (default: all)
    --question-types Comma-separated types to include
    --results-dir    Output dir (default: results/)
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))
from shared.memory_client import query


QUESTION_TYPES = [
    "single-session-user",
    "single-session-assistant",
    "knowledge-update",
    "cross-session",
    "temporal-reasoning",
]


def main() -> None:
    ap = argparse.ArgumentParser(description="Run LongMemEval QA through Memory query agent")
    ap.add_argument("--project", default=os.environ.get("MEMORY_PROJECT_ID", ""))
    ap.add_argument("--data", default="", help="Path to JSON (default: data/longmemeval_<split>.json)")
    ap.add_argument("--split", choices=["oracle", "s", "m"], default="oracle")
    ap.add_argument("--limit", type=int, default=None)
    ap.add_argument("--question-types", default="", help="e.g. 'single-session-user,knowledge-update'")
    ap.add_argument("--results-dir", default="results")
    args = ap.parse_args()

    data_path = Path(args.data) if args.data else Path(f"data/longmemeval_{args.split}.json")
    if not data_path.exists():
        print(f"ERROR: {data_path} not found. Run: python download.py --split {args.split}", file=sys.stderr)
        sys.exit(1)

    results_dir = Path(args.results_dir)
    results_dir.mkdir(parents=True, exist_ok=True)

    with open(data_path) as f:
        data = json.load(f)

    items: list[dict] = data if isinstance(data, list) else list(data.values())

    type_filter: set[str] = set()
    if args.question_types:
        type_filter = {t.strip() for t in args.question_types.split(",")}
    if type_filter:
        items = [it for it in items if it.get("question_type", "") in type_filter]

    if args.limit:
        items = items[: args.limit]

    print(f"Querying {len(items)} question(s), split={args.split}")

    all_results: list[dict] = []

    for idx, item in enumerate(items):
        qid = item.get("question_id", str(idx))
        qtype = item.get("question_type", "unknown")
        question = item.get("question", "")
        gold = item.get("answer", "")
        ns = f"lme-{qid}"

        t0 = time.time()
        try:
            result = query(question, project_id=args.project, namespace=ns)
            predicted = result["answer"]
            error = result.get("error")
        except Exception as exc:
            predicted = ""
            error = str(exc)

        elapsed = int((time.time() - t0) * 1000)

        row = {
            "question_id": qid,
            "question_idx": idx,
            "namespace": ns,
            "question_type": qtype,
            "question": question,
            "gold": gold,
            "predicted": predicted,
            "error": error,
            "elapsed_ms": elapsed,
        }
        all_results.append(row)

        status = "✓" if not error else "✗"
        print(f"  {status} [{qtype}] {question[:60]}...")
        if not error:
            print(f"      Gold: {gold}")
            print(f"      Pred: {predicted[:80]}")

    out_path = results_dir / "query_results.jsonl"
    with open(out_path, "w") as f:
        for row in all_results:
            f.write(json.dumps(row) + "\n")

    print(f"\nDone: {len(all_results)} questions answered → {out_path}")


if __name__ == "__main__":
    main()
