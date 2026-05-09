#!/usr/bin/env python3
"""
LoCoMo ingest — feeds conversation sessions into Memory via `remember`.

Each session is formatted as a timestamped dialogue block and sent as one
remember() call. The agent extracts entities, deduplicates, and merges to main.

Usage:
    python ingest.py --project <id> [options]

Options:
    --project       Memory project ID (or set MEMORY_PROJECT_ID)
    --data          Path to locomo10.json (default: data/locomo10.json)
    --limit         Max conversations to ingest (default: all=10)
    --conversations Comma-separated conversation indices 0-based (e.g. "0,1,2")
    --sessions      Session range to ingest per conversation, e.g. "1-3" (default: all)
    --ingest-mode   raw | observations | both  (default: raw)
    --schema-policy auto | reuse_only  (default: auto)
    --dry-run       Branch+write but do not merge
    --parallel      Parallel workers at conversation level (default: 1)
    --results-dir   Where to write ingest_results.jsonl (default: results/)
    --namespace     Key prefix for entities (default: conv<idx>)
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


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

CATEGORY_NAMES = {
    1: "single-hop",
    2: "temporal",
    3: "open-domain",
    4: "single-session",
    5: "adversarial",
}


def parse_session_range(s: str) -> tuple[int, int] | None:
    """Parse "1-3" → (1, 3). Returns None if s is empty/all."""
    if not s or s.lower() == "all":
        return None
    parts = s.split("-")
    if len(parts) == 2:
        return int(parts[0]), int(parts[1])
    v = int(parts[0])
    return v, v


def format_session(turns: list[dict], date_str: str, speaker_a: str, speaker_b: str) -> str:
    """Format a list of dialogue turns into a readable block for the insert agent."""
    lines = [f"Conversation session on {date_str}:"]
    lines.append(f"Participants: {speaker_a} and {speaker_b}")
    lines.append("")
    for turn in turns:
        speaker = turn.get("speaker", "Unknown")
        text = turn.get("text", "").strip()
        if not text:
            continue
        line = f"{speaker}: {text}"
        # Append image caption if present
        if turn.get("blip_caption"):
            line += f" [shared image: {turn['blip_caption']}]"
        lines.append(line)
    return "\n".join(lines)


def format_observations(obs_text: str, date_str: str) -> str:
    """Format a session observation block."""
    return f"Facts extracted from conversation on {date_str}:\n\n{obs_text}"


def get_session_ids_in_conversation(conv: dict) -> list[int]:
    """Return sorted list of session numbers present in a conversation dict."""
    ids = []
    for key in conv["conversation"]:
        if key.startswith("session_") and not key.endswith(("_date_time", "_observation", "_summary")):
            try:
                n = int(key.split("_")[1])
                ids.append(n)
            except (ValueError, IndexError):
                pass
    return sorted(ids)


def ingest_conversation(
    conv: dict,
    conv_idx: int,
    session_range: tuple[int, int] | None,
    ingest_mode: str,
    schema_policy: str,
    dry_run: bool,
    project_id: str,
    results_dir: Path,
    namespace: str | None = None,
) -> list[dict]:
    """Ingest all sessions of one conversation. Returns list of per-session result dicts."""
    ns = namespace or f"conv{conv_idx:02d}"
    speaker_a = conv["conversation"].get("speaker_a", "Person A")
    speaker_b = conv["conversation"].get("speaker_b", "Person B")
    sample_id = conv.get("sample_id", str(conv_idx))

    all_session_ids = get_session_ids_in_conversation(conv)
    if session_range:
        lo, hi = session_range
        all_session_ids = [s for s in all_session_ids if lo <= s <= hi]

    results = []
    for sid in all_session_ids:
        turns = conv["conversation"].get(f"session_{sid}", [])
        date_str = conv["conversation"].get(f"session_{sid}_date_time", f"session {sid}")
        obs_text = conv.get("observation", {}).get(f"session_{sid}_observation", "")

        texts_to_send: list[str] = []
        if ingest_mode in ("raw", "both"):
            texts_to_send.append(format_session(turns, date_str, speaker_a, speaker_b))
        if ingest_mode in ("observations", "both") and obs_text:
            texts_to_send.append(format_observations(obs_text, date_str))

        for text in texts_to_send:
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
                    "sample_id": sample_id,
                    "conv_idx": conv_idx,
                    "session_id": sid,
                    "namespace": ns,
                    "status": "error" if result.get("error") else "ok",
                    "error": result.get("error"),
                    "elapsed_ms": result.get("elapsed_ms", int((time.time() - t0) * 1000)),
                    "tools": result.get("tools", []),
                }
            except Exception as exc:
                row = {
                    "sample_id": sample_id,
                    "conv_idx": conv_idx,
                    "session_id": sid,
                    "namespace": ns,
                    "status": "error",
                    "error": str(exc),
                    "elapsed_ms": int((time.time() - t0) * 1000),
                    "tools": [],
                }
            results.append(row)
            print(f"  conv={conv_idx} session={sid} → {row['status']}  ({row['elapsed_ms']}ms)")

    return results


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> None:
    ap = argparse.ArgumentParser(description="Ingest LoCoMo conversations into Memory")
    ap.add_argument("--project", default=os.environ.get("MEMORY_PROJECT_ID", ""))
    ap.add_argument("--data", default="data/locomo10.json")
    ap.add_argument("--limit", type=int, default=None, help="Max conversations (default: all)")
    ap.add_argument("--conversations", default="", help="Comma-separated 0-based indices, e.g. '0,1,2'")
    ap.add_argument("--sessions", default="", help="Session range, e.g. '1-3' (default: all)")
    ap.add_argument("--ingest-mode", choices=["raw", "observations", "both"], default="raw")
    ap.add_argument("--schema-policy", choices=["auto", "reuse_only"], default="auto")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--parallel", type=int, default=1)
    ap.add_argument("--results-dir", default="results")
    ap.add_argument("--namespace", default="", help="Override namespace prefix")
    args = ap.parse_args()

    data_path = Path(args.data)
    if not data_path.exists():
        print(f"ERROR: {data_path} not found. Download locomo10.json from https://github.com/snap-research/locomo and place it in locomo/data/", file=sys.stderr)
        sys.exit(1)

    results_dir = Path(args.results_dir)
    results_dir.mkdir(parents=True, exist_ok=True)

    with open(data_path) as f:
        data = json.load(f)

    # data may be a list or a dict with a key
    conversations = data if isinstance(data, list) else data.get("data", list(data.values()))

    # Filter conversations
    if args.conversations:
        indices = [int(x) for x in args.conversations.split(",")]
        conversations = [conversations[i] for i in indices if i < len(conversations)]
    if args.limit:
        conversations = conversations[: args.limit]

    session_range = parse_session_range(args.sessions)

    print(f"Ingesting {len(conversations)} conversation(s), sessions={args.sessions or 'all'}, mode={args.ingest_mode}, schema_policy={args.schema_policy}, dry_run={args.dry_run}")

    all_results: list[dict] = []

    if args.parallel > 1:
        with ThreadPoolExecutor(max_workers=args.parallel) as pool:
            futures = {
                pool.submit(
                    ingest_conversation,
                    conv, idx, session_range, args.ingest_mode,
                    args.schema_policy, args.dry_run, args.project,
                    results_dir, args.namespace or None,
                ): idx
                for idx, conv in enumerate(conversations)
            }
            for fut in as_completed(futures):
                all_results.extend(fut.result())
    else:
        for idx, conv in enumerate(conversations):
            print(f"\n[conv {idx}] {conv.get('sample_id', idx)}")
            rows = ingest_conversation(
                conv, idx, session_range, args.ingest_mode,
                args.schema_policy, args.dry_run, args.project,
                results_dir, args.namespace or None,
            )
            all_results.extend(rows)

    out_path = results_dir / "ingest_results.jsonl"
    with open(out_path, "w") as f:
        for row in all_results:
            f.write(json.dumps(row) + "\n")

    ok = sum(1 for r in all_results if r["status"] == "ok")
    print(f"\nDone: {ok}/{len(all_results)} sessions ingested OK → {out_path}")


if __name__ == "__main__":
    main()
