#!/usr/bin/env python3
"""
Simple end-to-end QA bench over extract-bench ground truth.
Generates factual questions from ground_truth.json, queries the agent,
scores token F1 + exact match.

Usage:
  python3 query_bench.py --namespace extract-bench-varL-v2-run1778609154
"""
import argparse, json, os, re, sys, time, requests
from pathlib import Path

TOKEN   = os.environ.get("EMERGENT_MEMORY_TOKEN", "emt_90e466b66031ef242148336a85152d30f78ba3e723fb81dc7ebed0fefc9156de")
PROJECT = os.environ.get("EMERGENT_MEMORY_PROJECT", "ea1fe3b1-6ec9-48a0-8469-46211895f3be")
SERVER  = os.environ.get("EMERGENT_MEMORY_SERVER", "https://memory.emergent-company.ai")

BENCH_DIR    = Path(__file__).parent
GROUND_TRUTH = json.load(open(BENCH_DIR / "ground_truth.json"))

# Factual QA pairs derived from ground truth
# (question, gold_answer, rel_hint)
QA_PAIRS = [
    ("Where does Sarah live?",                  "Portland",                  "sarah lives_in portland"),
    ("Where does Daniel live?",                 "Portland",                  "daniel lives_in portland"),
    ("Where does Priya live?",                  "Austin",                    "priya lives_in austin"),
    ("Where does Tom live?",                    None,                        None),  # not in GT → unknown
    ("Who is Sarah married to?",                "Daniel",                    "sarah is_married_to daniel"),
    ("Where does Sarah work?",                  "Greenfield Architecture",   "sarah works_at greenfield"),
    ("Where does Daniel work?",                 "Nova Labs",                 "daniel works_at nova-labs"),
    ("Who is Priya friends with?",              "Sarah",                     "priya is_friends_with sarah"),
    ("Who is Tom friends with?",                "Daniel",                    "tom is_friends_with daniel"),
    ("What event did Sarah participate in?",    "Portland Marathon 2023",    "sarah participated_in marathon-2023"),
    ("What did Daniel attend in 2023?",         "Portland Marathon 2023",    "daniel attended marathon-2023"),
    ("What country did Tom visit?",             "Kenya",                     "tom visited kenya"),
    ("What cafe does Sarah like?",              "Blue Heron Cafe",           "sarah likes blue-heron"),
    ("What dog does Daniel own?",               "Rover",                     "daniel owns rover"),
    ("When did the wedding happen?",            "June 2024",                 "wedding happened_on june-2024"),
    ("Where did the wedding occur?",            "Portland",                  "wedding occurred_at portland"),
]


def normalize(s: str) -> list[str]:
    s = s.lower()
    s = re.sub(r"[^\w\s]", " ", s)
    return [t for t in s.split() if t not in {"the","a","an","is","was","in","at","of","to","and","or"}]


def token_f1(pred: str, gold: str) -> float:
    p_toks = normalize(pred)
    g_toks = normalize(gold)
    if not p_toks or not g_toks:
        return 0.0
    common = set(p_toks) & set(g_toks)
    if not common:
        return 0.0
    prec = len(common) / len(p_toks)
    rec  = len(common) / len(g_toks)
    return 2 * prec * rec / (prec + rec)


def exact_match(pred: str, gold: str) -> bool:
    return normalize(pred) == normalize(gold)


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


def query_agent(question: str, namespace: str, timeout: int = 120) -> dict:
    url = f"{SERVER}/api/projects/{PROJECT}/query"
    headers = {"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"}
    # Inject namespace as structured field — server auto-injects into all MCP tool calls
    body = {"message": question, "namespace": namespace}
    start = time.time()
    resp = requests.post(url, json=body, headers=headers, stream=True, timeout=timeout)
    resp.raise_for_status()
    parts, tools, error = [], [], None
    for ev in _sse_lines(resp):
        t = ev.get("type", "")
        if t == "token":
            parts.append(ev.get("token", ""))
        elif t == "mcp_tool" and ev.get("status") == "started":
            tools.append(ev.get("tool", ""))
        elif t == "error":
            error = ev.get("error")
    return {
        "answer": "".join(parts).strip(),
        "tools": tools,
        "elapsed_ms": int((time.time() - start) * 1000),
        "error": error,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--namespace", required=True, help="Namespace of the ingested extraction run")
    ap.add_argument("--out", default=None, help="Output JSON path")
    args = ap.parse_args()

    ns = args.namespace
    print(f"Namespace: {ns}")
    print(f"Project:   {PROJECT}")
    print(f"Server:    {SERVER}")
    print()

    # Override prompt already set on project — just run questions
    results = []
    f1s, ems = [], []

    for i, (question, gold, hint) in enumerate(QA_PAIRS):
        if gold is None:
            print(f"[{i+1:2d}] SKIP (no gold): {question}")
            continue

        print(f"[{i+1:2d}] Q: {question}")
        r = query_agent(question, ns)
        pred = r["answer"]
        f1  = token_f1(pred, gold)
        em  = exact_match(pred, gold)
        f1s.append(f1)
        ems.append(float(em))

        status = "✓" if em else ("~" if f1 > 0.5 else "✗")
        print(f"     A: {pred!r}")
        print(f"     G: {gold!r}  F1={f1:.2f} EM={int(em)}  tools={r['tools']}  {status}")
        print()

        results.append({
            "question": question,
            "gold": gold,
            "predicted": pred,
            "token_f1": round(f1, 4),
            "exact_match": em,
            "tools": r["tools"],
            "elapsed_ms": r["elapsed_ms"],
            "error": r["error"],
            "hint": hint,
        })

    mean_f1 = sum(f1s) / len(f1s) if f1s else 0
    mean_em = sum(ems) / len(ems) if ems else 0
    print("=" * 50)
    print(f"Token F1:    {mean_f1:.4f}  ({len(f1s)} questions)")
    print(f"Exact Match: {mean_em:.4f}")

    summary = {
        "namespace": ns,
        "token_f1": round(mean_f1, 4),
        "exact_match": round(mean_em, 4),
        "n": len(f1s),
        "results": results,
    }

    out = args.out or str(BENCH_DIR / f"query_bench_{ns}.json")
    json.dump(summary, open(out, "w"), indent=2)
    print(f"\nSaved → {out}")


if __name__ == "__main__":
    main()
