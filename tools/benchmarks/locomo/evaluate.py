#!/usr/bin/env python3
"""
LoCoMo evaluate — computes token F1 and exact match from query_results.jsonl.

Usage:
    python evaluate.py [--results-dir results/] [--output results/eval_summary.json]
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))
from shared.metrics import token_f1, exact_match, aggregate


def main() -> None:
    ap = argparse.ArgumentParser(description="Evaluate LoCoMo QA results")
    ap.add_argument("--results-dir", default="results")
    ap.add_argument("--output", default="", help="Path for summary JSON (default: results/eval_summary.json)")
    args = ap.parse_args()

    results_dir = Path(args.results_dir)
    query_results_path = results_dir / "query_results.jsonl"
    if not query_results_path.exists():
        print(f"ERROR: {query_results_path} not found. Run query.py first.", file=sys.stderr)
        sys.exit(1)

    rows: list[dict] = []
    with open(query_results_path) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))

    # Compute per-question metrics
    scored: list[dict] = []
    for row in rows:
        if row.get("error"):
            continue
        pred = row.get("predicted", "")
        gold = row.get("gold", "")
        f1 = token_f1(pred, gold)
        em = exact_match(pred, gold)
        scored.append({**row, "f1": f1, "em": em})

    if not scored:
        print("No scored results found.")
        return

    f1_agg = aggregate(scored, "f1")
    em_agg = aggregate(scored, "em")

    summary = {
        "total_questions": len(rows),
        "scored_questions": len(scored),
        "skipped_errors": len(rows) - len(scored),
        "token_f1": f1_agg,
        "exact_match": em_agg,
    }

    out_path = Path(args.output) if args.output else results_dir / "eval_summary.json"
    with open(out_path, "w") as f:
        json.dump(summary, f, indent=2)

    # Print report
    print("\n=== LoCoMo Evaluation Results ===\n")
    print(f"Questions: {len(scored)} scored / {len(rows)} total")
    print(f"\nToken F1 (overall):   {f1_agg['mean']:.4f}")
    print(f"Exact Match (overall): {em_agg['mean']:.4f}")

    print("\nToken F1 by category:")
    for cat, stats in sorted(f1_agg["by_category"].items()):
        print(f"  {cat:<20} {stats['mean']:.4f}  (n={stats['count']})")

    print(f"\nSummary written to {out_path}")


if __name__ == "__main__":
    main()
