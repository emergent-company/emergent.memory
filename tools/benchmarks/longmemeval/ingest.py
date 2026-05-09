#!/usr/bin/env python3
"""
LongMemEval ingest — feeds haystack sessions into Memory via `remember`.

Each session (user+assistant turns) is formatted as a timestamped dialogue
and sent as one remember() call. Uses per-question namespacing so sessions
from different questions don't cross-contaminate the graph.

Usage:
    python ingest.py --project <id> [options]

Options:
    --project       Memory project ID
    --data          Path to JSON file (default: data/longmemeval_oracle.json)
    --split         oracle | s | m  (determines default data file)
    --limit         Max questions to ingest (default: all)
    --question-types Comma-separated types to include, e.g. "single-session-user,knowledge-update"
    --schema-policy auto | reuse_only (default: auto)
    --dry-run       Branch+write but do not merge
    --parallel      Question-level parallelism (default: 1)
    --results-dir   (default: results/)
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))
from shared.memory_client import remember


def format_session(turns: list[dict], date_str: str, session_idx: int) -> str:
    """Format user+assistant turns as a readable block for the insert agent."""
    lines = [f"Chat session {session_idx + 1} on {date_str}:"]
    lines.append("")
    for turn in turns:
        role = turn.get("role", "unknown").capitalize()
        content = turn.get("content", "").strip()
        if content:
            lines.append(f"{role}: {content}")
    return "\n".join(lines)


def ingest_question(
    item: dict,
    question_idx: int,
    schema_policy: str,
    dry_run: bool,
    project_id: str,
) -> list[dict]:
    """Ingest all haystack sessions for one question. Returns per-session result dicts."""
    qid = item.get("question_id", str(question_idx))
    ns = f"lme-{qid}"

    sessions = item.get("haystack_sessions", [])
    dates = item.get("haystack_dates", [])

    results = []
    for i, session in enumerate(sessions):
        date_str = dates[i] if i < len(dates) else f"session-{i + 1}"
        text = format_session(session, date_str, i)

        t0 = time.time()
        try:
            result = remember(
                text,
                project_id=project_id,
                schema_policy=schema_policy,
                dry_run=dry_run,
                namespace=ns,
            )
            row = {
                "question_id": qid,
                "question_idx": question_idx,
                "session_idx": i,
                "namespace": ns,
                "status": "error" if result.get("error") else "ok",
                "error": result.get("error"),
                "elapsed_ms": result.get("elapsed_ms", int((time.time() - t0) * 1000)),
                "tools": result.get("tools", []),
            }
        except Exception as exc:
            row = {
                "question_id": qid,
                "question_idx": question_idx,
                "session_idx": i,
                "namespace": ns,
                "status": "error",
                "error": str(exc),
                "elapsed_ms": int((time.time() - t0) * 1000),
                "tools": [],
            }
        results.append(row)
        print(f"  q={qid} session={i} → {row['status']}  ({row['elapsed_ms']}ms)")

    return results


def main() -> None:
    ap = argparse.ArgumentParser(description="Ingest LongMemEval sessions into Memory")
    ap.add_argument("--project", default=os.environ.get("MEMORY_PROJECT_ID", ""))
    ap.add_argument("--data", default="", help="Path to JSON (default: data/longmemeval_<split>.json)")
    ap.add_argument("--split", choices=["oracle", "s", "m"], default="oracle")
    ap.add_argument("--limit", type=int, default=None)
    ap.add_argument("--question-types", default="", help="e.g. 'single-session-user,knowledge-update'")
    ap.add_argument("--schema-policy", choices=["auto", "reuse_only"], default="auto")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--parallel", type=int, default=1)
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

    print(f"Ingesting {len(items)} question(s), split={args.split}, schema_policy={args.schema_policy}, dry_run={args.dry_run}")

    all_results: list[dict] = []

    if args.parallel > 1:
        with ThreadPoolExecutor(max_workers=args.parallel) as pool:
            futures = {
                pool.submit(
                    ingest_question,
                    item, idx, args.schema_policy, args.dry_run, args.project,
                ): idx
                for idx, item in enumerate(items)
            }
            for fut in as_completed(futures):
                all_results.extend(fut.result())
    else:
        for idx, item in enumerate(items):
            qid = item.get("question_id", str(idx))
            qtype = item.get("question_type", "unknown")
            n_sessions = len(item.get("haystack_sessions", []))
            print(f"\n[q {idx}] {qid}  type={qtype}  sessions={n_sessions}")
            rows = ingest_question(item, idx, args.schema_policy, args.dry_run, args.project)
            all_results.extend(rows)

    out_path = results_dir / "ingest_results.jsonl"
    with open(out_path, "w") as f:
        for row in all_results:
            f.write(json.dumps(row) + "\n")

    ok = sum(1 for r in all_results if r["status"] == "ok")
    print(f"\nDone: {ok}/{len(all_results)} sessions ingested OK → {out_path}")


if __name__ == "__main__":
    main()
