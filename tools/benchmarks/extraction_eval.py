#!/usr/bin/env python3
"""
Fast extraction eval loop.

Usage:
  python extraction_eval.py                        # 1 session, sessions 1-3, cat 1 QA
  python extraction_eval.py --sessions 1-5         # sessions 1-5
  python extraction_eval.py --qa-limit 20          # cap QA pairs evaluated
  python extraction_eval.py --skip-ingest          # query only (reuse existing graph)
  python extraction_eval.py --agent-def-id <id>    # use specific agent definition
  python extraction_eval.py --sample-id 0          # which LoCoMo sample (0-9)
"""

import argparse
import json
import os
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent / "shared"))

# Patch relative imports in shared modules
import importlib, types  # noqa: E402
_shared_pkg = types.ModuleType("shared")
_shared_pkg.__path__ = [str(Path(__file__).parent / "shared")]
sys.modules["shared"] = _shared_pkg

from shared.memory_client import remember, query  # noqa: E402
from shared.metrics import token_f1, exact_match  # noqa: E402
import requests  # noqa: E402


def _sse_lines(resp):
    for raw in resp.iter_lines():
        if not raw:
            continue
        line = raw.decode("utf-8") if isinstance(raw, bytes) else raw
        if not line.startswith("data: "):
            continue
        data = line[6:]
        if not data:
            continue
        try:
            yield json.loads(data)
        except json.JSONDecodeError:
            continue


def remember_with_agent(text: str, agent_def_id: str, project_id: str, token: str,
                        server: str, timeout: int = 300) -> dict:
    """POST /api/chat/stream with agentDefinitionId — bypasses default graph-insert-agent."""
    body = {
        "message": text,
        "agentDefinitionId": agent_def_id,
    }
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
        "x-project-id": project_id,
    }
    url = f"{server}/api/chat/stream"
    start = time.time()
    resp = requests.post(url, json=body, headers=headers, stream=True, timeout=timeout)
    resp.raise_for_status()

    parts, tools = [], []
    for event in _sse_lines(resp):
        etype = event.get("type", "")
        if etype == "token":
            parts.append(event.get("token", ""))
        elif etype == "mcp_tool" and event.get("status") == "started":
            tools.append(event.get("tool", ""))
        elif etype == "error":
            return {"response": "", "tools": tools, "elapsed_ms": int((time.time()-start)*1000),
                    "error": event.get("error")}

    return {
        "response": "".join(parts),
        "tools": tools,
        "elapsed_ms": int((time.time() - start) * 1000),
        "error": None,
    }

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
DATA_FILE = Path(__file__).parent / "locomo" / "data" / "locomo10.json"

SERVER = os.environ.get("MEMORY_API_URL", "https://memory.emergent-company.ai")
PROJECT_ID = os.environ.get("MEMORY_PROJECT_ID", "48998641-6740-4511-a0fe-4a5b35f45c50")


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
def load_sample(sample_idx: int):
    with open(DATA_FILE) as f:
        data = json.load(f)
    sessions = data if isinstance(data, list) else data.get("sessions", [])
    return sessions[sample_idx]


def get_utterances(sample, session_nums: list[int]) -> list[str]:
    conv = sample["conversation"]
    lines = []
    for n in session_nums:
        key = f"session_{n}"
        if key not in conv:
            continue
        date = conv.get(f"session_{n}_date_time", f"Session {n}")
        lines.append(f"[{date}]")
        for u in conv[key]:
            lines.append(f"{u['speaker']}: {u['text']}")
    return lines


def get_qa(sample, categories=None, limit=None) -> list[dict]:
    qa = sample["qa"]
    if categories:
        qa = [q for q in qa if q.get("category") in categories]
    if limit:
        qa = qa[:limit]
    return qa


def score(predictions: list[str], references: list[str]) -> dict:
    f1s = [token_f1(p, r) for p, r in zip(predictions, references)]
    ems = [exact_match(p, r) for p, r in zip(predictions, references)]
    return {
        "token_f1": round(sum(f1s) / len(f1s), 4) if f1s else 0.0,
        "exact_match": round(sum(ems) / len(ems), 4) if ems else 0.0,
        "n": len(f1s),
    }


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--sample-id", type=int, default=0)
    parser.add_argument("--sessions", default="1-3",
                        help="Session range e.g. 1-3 or 1,2,3")
    parser.add_argument("--qa-limit", type=int, default=20)
    parser.add_argument("--qa-categories", default="1",
                        help="Comma-separated category ints (1=single-hop facts)")
    parser.add_argument("--skip-ingest", action="store_true")
    parser.add_argument("--agent-def-id", default=None,
                        help="Override agent definition ID for remember calls")
    parser.add_argument("--query-agent-def-id",
                        default="78e6e510-48f7-4bb8-9181-65d992abc9c0",
                        help="Agent def ID for query (default: eval-query-fast)")
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args()

    # Parse session range
    if "-" in args.sessions:
        a, b = args.sessions.split("-")
        session_nums = list(range(int(a), int(b) + 1))
    else:
        session_nums = [int(x) for x in args.sessions.split(",")]

    # Parse QA categories
    qa_cats = [int(x) for x in args.qa_categories.split(",")]

    print(f"=== Extraction Eval ===")
    print(f"  Sample:   {args.sample_id}")
    print(f"  Sessions: {session_nums}")
    print(f"  QA cats:  {qa_cats}  limit={args.qa_limit}")
    print(f"  Server:   {SERVER}")
    print(f"  Project:  {PROJECT_ID}")
    print()

    sample = load_sample(args.sample_id)
    utterances = get_utterances(sample, session_nums)
    qa_pairs = get_qa(sample, categories=qa_cats, limit=args.qa_limit)

    # -----------------------------------------------------------------------
    # Ingest
    # -----------------------------------------------------------------------
    if not args.skip_ingest:
        dialogue = "\n".join(utterances)
        print(f"[ingest] {len(utterances)} lines → remember ...", flush=True)
        t0 = time.time()
        if args.agent_def_id:
            cfg = __import__("shared.config", fromlist=["get_config"]).get_config()
            resp = remember_with_agent(
                text=dialogue,
                agent_def_id=args.agent_def_id,
                project_id=PROJECT_ID,
                token=cfg.api_key,
                server=SERVER,
            )
        else:
            resp = remember(text=dialogue, project_id=PROJECT_ID)
        elapsed = time.time() - t0
        tools_used = resp.get("tools", [])
        print(f"[ingest] done in {elapsed:.1f}s  tools={tools_used}", flush=True)
        if args.verbose:
            print(f"[ingest] response:\n{resp.get('response','')[:500]}")
        if resp.get("error"):
            print(f"[ingest] ERROR: {resp['error']}")
        print()
    else:
        print("[ingest] skipped\n")

    # -----------------------------------------------------------------------
    # Query + Score
    # -----------------------------------------------------------------------
    predictions = []
    references = []

    print(f"[query] evaluating {len(qa_pairs)} QA pairs ...", flush=True)
    cfg = __import__("shared.config", fromlist=["get_config"]).get_config()
    concise = ("[IMPORTANT: Answer with the shortest possible phrase or name — "
               "no explanation, no markdown, no sentences.] ")
    for i, qa in enumerate(qa_pairs):
        q_text = qa["question"]
        ref_str = str(qa["answer"])

        t0 = time.time()
        resp_q = remember_with_agent(
            text=concise + q_text,
            agent_def_id=args.query_agent_def_id,
            project_id=PROJECT_ID,
            token=cfg.api_key,
            server=SERVER,
            timeout=60,
        )
        elapsed_q = time.time() - t0
        pred = (resp_q.get("response") or "").strip()
        predictions.append(pred)
        references.append(ref_str)

        f1 = token_f1(pred, ref_str)
        em = exact_match(pred, ref_str)
        if args.verbose or em == 1.0 or f1 > 0.5:
            print(f"  [{i+1}/{len(qa_pairs)}] Q: {q_text}")
            print(f"           A: {ref_str}")
            print(f"           P: {pred}  ({elapsed_q:.1f}s)")
            print(f"           F1={f1:.3f}  EM={em}")
        else:
            print(f"  [{i+1}/{len(qa_pairs)}] F1={f1:.3f} EM={em} ({elapsed_q:.1f}s) | {q_text[:60]}")

    print()
    result = score(predictions, references)
    print("=== RESULTS ===")
    print(f"  Token F1:     {result['token_f1']}")
    print(f"  Exact Match:  {result['exact_match']}")
    print(f"  N questions:  {result['n']}")

    return result


if __name__ == "__main__":
    main()
