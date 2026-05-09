#!/usr/bin/env python3
"""
LoCoMo query — runs QA pairs through Memory's query agent and saves predictions.

Filters QA pairs to only those answerable from ingested sessions (using evidence field).
If --sessions was used during ingest, pass the same value here.

Usage:
    python query.py --project <id> [options]

Options:
    --project       Memory project ID
    --data          Path to locomo10.json (default: data/locomo10.json)
    --ingest-results Path to ingest_results.jsonl to determine which sessions are loaded
    --limit         Max conversations (default: all)
    --conversations Comma-separated 0-based indices
    --sessions      Session range that was ingested, e.g. "1-3" (filters QA evidence)
    --categories    Comma-separated category numbers to run, e.g. "1,2" (default: all)
    --results-dir   Output dir (default: results/)
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


CATEGORY_NAMES = {
    1: "single-hop",
    2: "temporal",
    3: "open-domain",
    4: "single-session",
    5: "adversarial",
}


def parse_session_range(s: str) -> tuple[int, int] | None:
    if not s or s.lower() == "all":
        return None
    parts = s.split("-")
    if len(parts) == 2:
        return int(parts[0]), int(parts[1])
    v = int(parts[0])
    return v, v


def evidence_in_range(evidence: list[str], session_range: tuple[int, int] | None) -> bool:
    """Return True if all evidence turns belong to sessions within the range."""
    if not session_range:
        return True
    lo, hi = session_range
    for eid in evidence:
        # dia_id format: "D{session}:{turn}"
        try:
            sess_num = int(eid.split(":")[0].lstrip("D"))
            if not (lo <= sess_num <= hi):
                return False
        except (ValueError, IndexError):
            return False
    return True


def main() -> None:
    ap = argparse.ArgumentParser(description="Run LoCoMo QA through Memory query agent")
    ap.add_argument("--project", default=os.environ.get("MEMORY_PROJECT_ID", ""))
    ap.add_argument("--data", default="data/locomo10.json")
    ap.add_argument("--limit", type=int, default=None)
    ap.add_argument("--conversations", default="")
    ap.add_argument("--sessions", default="", help="Same session range used during ingest")
    ap.add_argument("--categories", default="", help="e.g. '1,2,3' (default: all)")
    ap.add_argument("--results-dir", default="results")
    args = ap.parse_args()

    data_path = Path(args.data)
    if not data_path.exists():
        print(f"ERROR: {data_path} not found", file=sys.stderr)
        sys.exit(1)

    results_dir = Path(args.results_dir)
    results_dir.mkdir(parents=True, exist_ok=True)

    with open(data_path) as f:
        data = json.load(f)

    conversations = data if isinstance(data, list) else data.get("data", list(data.values()))

    if args.conversations:
        indices = [int(x) for x in args.conversations.split(",")]
        conversations = [conversations[i] for i in indices if i < len(conversations)]
    if args.limit:
        conversations = conversations[: args.limit]

    session_range = parse_session_range(args.sessions)
    category_filter: set[int] = set()
    if args.categories:
        category_filter = {int(c) for c in args.categories.split(",")}

    all_results: list[dict] = []

    for conv_idx, conv in enumerate(conversations):
        sample_id = conv.get("sample_id", str(conv_idx))
        ns = f"conv{conv_idx:02d}"
        qa_pairs = conv.get("qa", [])

        # Filter to QA pairs answerable from ingested sessions
        filtered = []
        for qa in qa_pairs:
            evidence = qa.get("evidence", [])
            cat = qa.get("category", 0)
            if category_filter and cat not in category_filter:
                continue
            if not evidence_in_range(evidence, session_range):
                continue
            filtered.append(qa)

        print(f"\n[conv {conv_idx}] {sample_id} — {len(filtered)} answerable QA pairs (of {len(qa_pairs)} total)")

        for qa in filtered:
            question = qa["question"]
            gold = qa["answer"]
            cat = qa.get("category", 0)
            cat_name = CATEGORY_NAMES.get(cat, str(cat))

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
                "sample_id": sample_id,
                "conv_idx": conv_idx,
                "namespace": ns,
                "question": question,
                "gold": gold,
                "predicted": predicted,
                "category": cat_name,
                "category_num": cat,
                "evidence": qa.get("evidence", []),
                "error": error,
                "elapsed_ms": elapsed,
            }
            all_results.append(row)

            status = "✓" if not error else "✗"
            print(f"  {status} [{cat_name}] Q: {question[:60]}...")
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
