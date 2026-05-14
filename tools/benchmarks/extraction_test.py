#!/usr/bin/env python3
"""
Standalone extraction quality test - no graph storage.

Scoring: text-grounded. Facts are defined from actual session text.
Score = fraction of expected facts found in extraction JSON.

Usage:
  python extraction_test.py                    # sessions 1-3, prompt v1
  python extraction_test.py --all-prompts      # compare all variants
  python extraction_test.py --prompt v3 --verbose
"""

import argparse
import json
import re
import time
from pathlib import Path

import requests

DATA_FILE     = Path(__file__).parent / "locomo" / "data" / "locomo10.json"
DEEPSEEK_BASE = "https://api.deepseek.com/v1"
DEEPSEEK_KEY  = "sk-bfa5b8465aad4a1e907474714936b0ff"
MODEL         = "deepseek-v4-flash"

# ---------------------------------------------------------------------------
# Ground-truth facts - manually verified against sessions 1-3 source text.
# Each tuple: (label, [needle strings - any match = found])
# Needles are matched against the full JSON dump (lowercased) of the extraction.
# Integer values like 3, 4 appear as ': 3,' in JSON dump.
# ---------------------------------------------------------------------------
FACTS_S1_3 = [
    # --- Caroline ---
    ("Caroline attended LGBTQ support group",             ["lgbtq support group", "support group"]),
    ("Caroline felt accepted/inspired by support group",  ["accepted", "inspiring", "powerful"]),
    ("Caroline interested in counseling / mental health",  ["counsel", "mental health"]),
    ("Caroline researching adoption agencies",             ["adoption"]),
    ("Caroline wants to adopt children",                  ["adopt"]),
    ("Caroline chose LGBTQ-inclusive adoption agency",    ["lgbtq", "inclusive"]),
    ("Caroline is a single parent",                       ["single"]),
    ("Caroline gave school talk about transgender journey",["school", "transgender", "journey"]),
    ("Caroline transitioning for 3 years",                ["three years", "3 years", "years_since_transition", ": 3,"]),
    ("Caroline has friends for 4 years",                  ["4 years", "four years", "friends_known_duration", ": 4,"]),
    ("Caroline moved from home country",                  ["home country", "moved", "sweden"]),
    ("Caroline had a tough breakup",                      ["breakup", "break-up"]),
    ("Caroline support system: friends, family, mentors", ["mentor", "support system"]),
    # --- Melanie ---
    ("Melanie painted a lake sunrise",                    ["lake sunrise"]),
    ("Melanie painted it last year",                      ["last year", "lake sunrise.*last", "painted.*last"]),
    ("Melanie ran charity race for mental health",         ["charity race", "mental health"]),
    ("Melanie self-care: running",                        ["running"]),
    ("Melanie self-care: reading",                        ["reading"]),
    ("Melanie self-care: violin",                         ["violin"]),
    ("Melanie planning camping next month",               ["camping"]),
    ("Melanie married for 5 years",                       ["5 years", "five years", "years_married", ": 5,"]),
    ("Melanie has kids",                                  ["kids", "children"]),
    ("Melanie has husband",                               ["husband"]),
    ("Melanie goes swimming with kids",                   ["swimming"]),
    ("Melanie's kids excited about summer break",         ["summer break", "excited.*summer"]),
]

# Facts grounded in sessions 1-6 text
FACTS_S1_6 = FACTS_S1_3 + [
    # --- Session 4 new facts ---
    ("Caroline has hand-painted bowl from friend",        ["hand-painted bowl", "painted bowl", "bowl"]),
    ("Caroline's bowl was birthday gift 10 years ago",    ["birthday", "10 years", "ten years"]),
    ("Caroline attended LGBTQ counseling workshop",       ["counseling workshop", "lgbtq.*workshop", "workshop"]),
    ("Melanie took family camping in mountains",          ["mountain"]),
    ("Melanie family hiked and roasted marshmallows",     ["marshmallow", "hike", "hiking"]),
    ("Melanie's kids love nature",                        ["nature", "love.*nature"]),
    # --- Session 5 new facts ---
    ("Caroline attended LGBTQ pride parade",              ["pride parade", "parade"]),
    ("Caroline learning piano",                           ["piano"]),
    ("Caroline attending transgender conference",         ["transgender conference", "conference"]),
    ("Melanie signed up for pottery class",               ["pottery"]),
    ("Melanie made a bowl in pottery class",              ["pottery.*bowl", "bowl.*pottery", "bowl.*class", "made.*bowl"]),
    ("Melanie finds pottery calming / therapeutic",       ["calming", "therapy", "therapeutic"]),
    # --- Session 6 new facts ---
    ("Melanie took kids to museum",                       ["museum"]),
    ("Melanie's kids love dinosaurs",                     ["dinosaur"]),
    ("Melanie read Charlotte's Web as a kid",             ["charlotte", "charlotte's web"]),
    ("Melanie's family camped at beach",                  ["beach"]),
    ("Caroline building a library for future kids",       ["library"]),
    # --- Inverse relationship checks ---
    ("support_group WAS_ATTENDED_BY caroline (inverse)",  ["was_attended_by", "had_participant", "attended_by"]),
    ("charity_race HAD_PARTICIPANT melanie (inverse)",    ["had_participant", "was_participated_in", "participant"]),
    ("painting WAS_PAINTED_BY melanie (inverse)",         ["was_painted_by", "painted_by", "creator"]),
    ("pottery_bowl WAS_MADE_BY melanie (inverse)",        ["was_made_by", "made_by", "was_created_by", "created_by", "produced_by"]),
]

FACTS_BY_SESSIONS = {
    "1-3": FACTS_S1_3,
    "1-6": FACTS_S1_6,
}

# ---------------------------------------------------------------------------
# Prompt variants - loaded from prompts/<name>.txt
# ---------------------------------------------------------------------------
PROMPTS_DIR = Path(__file__).parent / "prompts"

PROMPTS: dict[str, str] = {
    p.stem: p.read_text()
    for p in sorted(PROMPTS_DIR.glob("*.txt"))
    if not p.stem.endswith(("_entities", "_relationships"))
}

# Two-pass prompt pairs: prefix -> (entities_tmpl, relationships_tmpl)
TWO_PASS: dict[str, tuple[str, str]] = {}
for _p in sorted(PROMPTS_DIR.glob("*_entities.txt")):
    _prefix = _p.stem.replace("_entities", "")
    _rel = PROMPTS_DIR / f"{_prefix}_relationships.txt"
    if _rel.exists():
        TWO_PASS[_prefix] = (_p.read_text(), _rel.read_text())

# ---------------------------------------------------------------------------
# Core functions
# ---------------------------------------------------------------------------
def load_dialogue(sample_idx: int, session_nums: list[int]) -> str:
    data = json.load(open(DATA_FILE))
    sample = data[sample_idx]
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
    return "\n".join(lines)


def load_sessions(sample_idx: int, session_nums: list[int]) -> list[tuple[int, str]]:
    """Return list of (session_num, dialogue_text) for per-session extraction."""
    data = json.load(open(DATA_FILE))
    sample = data[sample_idx]
    conv = sample["conversation"]
    sessions = []
    for n in session_nums:
        key = f"session_{n}"
        if key not in conv:
            continue
        date = conv.get(f"session_{n}_date_time", f"Session {n}")
        lines = [f"[{date}]"]
        for u in conv[key]:
            lines.append(f"{u['speaker']}: {u['text']}")
        sessions.append((n, "\n".join(lines)))
    return sessions


def merge_extractions(per_session: list[dict]) -> dict:
    """Merge per-session entities/relationships. Entities deduped by key."""
    entities_by_key: dict[str, dict] = {}
    relationships: list[dict] = []

    for parsed in per_session:
        ents = parsed.get("entities", [])
        if isinstance(ents, dict):
            ents = ents.get("entities", []) if "entities" in ents else []
        for e in ents if isinstance(ents, list) else []:
            if not isinstance(e, dict):
                continue
            k = e.get("key") or e.get("name") or json.dumps(e)[:40]
            if k in entities_by_key:
                # merge properties (lists union, scalars: keep first non-null then add as list)
                existing = entities_by_key[k].setdefault("properties", {})
                for pk, pv in (e.get("properties") or {}).items():
                    if pk not in existing:
                        existing[pk] = pv
                    elif existing[pk] != pv:
                        prev = existing[pk]
                        if not isinstance(prev, list):
                            prev = [prev]
                        if isinstance(pv, list):
                            for v in pv:
                                if v not in prev:
                                    prev.append(v)
                        elif pv not in prev:
                            prev.append(pv)
                        existing[pk] = prev
            else:
                entities_by_key[k] = e

        rels = parsed.get("relationships", [])
        if isinstance(rels, dict):
            rels = rels.get("relationships", []) if "relationships" in rels else []
        if isinstance(rels, list):
            relationships.extend(r for r in rels if isinstance(r, dict))

    return {
        "entities": list(entities_by_key.values()),
        "relationships": relationships,
    }


def call_llm(prompt: str, timeout: int = 90, json_mode: bool = True) -> str:
    body: dict = {
        "model": MODEL,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0.1,
    }
    if json_mode:
        body["response_format"] = {"type": "json_object"}
    resp = requests.post(
        f"{DEEPSEEK_BASE}/chat/completions",
        headers={"Authorization": f"Bearer {DEEPSEEK_KEY}", "Content-Type": "application/json"},
        json=body,
        timeout=timeout,
    )
    resp.raise_for_status()
    return resp.json()["choices"][0]["message"]["content"]


def extract(dialogue: str, prompt_tmpl: str) -> dict:
    wants_json = "json" in prompt_tmpl.lower()
    raw = call_llm(prompt_tmpl.format(dialogue=dialogue), json_mode=wants_json)
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return {"_raw": raw}


def extract_two_pass(dialogue: str, ent_tmpl: str, rel_tmpl: str) -> dict:
    # Pass 1 — entities
    ent_json = "json" in ent_tmpl.lower()
    ent_raw = call_llm(ent_tmpl.format(dialogue=dialogue), json_mode=ent_json)
    try:
        ent_parsed = json.loads(ent_raw)
        entities = ent_parsed.get("entities", ent_parsed)
    except json.JSONDecodeError:
        entities = {"_raw": ent_raw}

    # Pass 2 — relationships, given entity list as context
    rel_json = "json" in rel_tmpl.lower()
    rel_raw = call_llm(rel_tmpl.format(entities=json.dumps(entities, indent=2)), json_mode=rel_json)
    try:
        rel_parsed = json.loads(rel_raw)
        relationships = rel_parsed.get("relationships", rel_parsed)
    except json.JSONDecodeError:
        relationships = {"_raw": rel_raw}

    return {"entities": entities, "relationships": relationships}


def flatten(parsed: dict) -> str:
    """Full JSON dump lowercased - preserves integers as e.g. ': 3,'"""
    return json.dumps(parsed, ensure_ascii=False).lower()


def score_facts(flat: str, facts: list) -> dict:
    results = []
    for label, needles in facts:
        found = any(re.search(needle.lower(), flat) for needle in needles)
        results.append({"label": label, "found": found})
    hits = sum(r["found"] for r in results)
    return {
        "score": round(hits / len(results), 4),
        "hits": hits,
        "total": len(results),
        "per_fact": results,
    }


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--sample-id", type=int, default=0)
    parser.add_argument("--sessions", default="1-3")
    parser.add_argument("--prompt", default="v1", choices=list(PROMPTS.keys()) + list(TWO_PASS.keys()))
    parser.add_argument("--all-prompts", action="store_true")
    parser.add_argument("--verbose", action="store_true")
    parser.add_argument("--dump", action="store_true", help="Print raw extraction output")
    parser.add_argument("--per-session", action="store_true", help="Run extraction per session and merge")
    args = parser.parse_args()

    if "-" in args.sessions:
        a, b = args.sessions.split("-")
        session_nums = list(range(int(a), int(b) + 1))
    else:
        session_nums = [int(x) for x in args.sessions.split(",")]

    dialogue = load_dialogue(args.sample_id, session_nums)
    sessions = load_sessions(args.sample_id, session_nums) if args.per_session else None

    # Pick fact set based on session range
    max_sess = max(session_nums)
    if max_sess <= 3:
        facts = FACTS_S1_3
        facts_label = "1-3"
    else:
        facts = FACTS_S1_6
        facts_label = "1-6"

    print(f"=== Extraction Quality Test (text-grounded) ===")
    print(f"  Model:    {MODEL}  (deepseek-v4-flash)")
    print(f"  Sessions: {session_nums}  ({len(dialogue.splitlines())} lines)")
    print(f"  Mode:     {'per-session + merge' if args.per_session else 'single-shot'}")
    print(f"  Facts:    {len(facts)} verified from sessions {facts_label}")
    print()

    variants = list(PROMPTS.keys()) + list(TWO_PASS.keys()) if args.all_prompts else [args.prompt]
    summary = []

    for variant in variants:
        print(f"--- Prompt: {variant} ---")
        t0 = time.time()
        try:
            if args.per_session:
                # Per-session extraction + merge (mimics production ingestion)
                per_sess = []
                for n, sess_text in sessions:
                    if variant in TWO_PASS:
                        ent_tmpl, rel_tmpl = TWO_PASS[variant]
                        p = extract_two_pass(sess_text, ent_tmpl, rel_tmpl)
                    else:
                        p = extract(sess_text, PROMPTS[variant])
                    per_sess.append(p)
                    print(f"    session {n}: {len(p.get('entities', []) if isinstance(p.get('entities'), list) else [])} ents")
                parsed = merge_extractions(per_sess)
            else:
                if variant in TWO_PASS:
                    ent_tmpl, rel_tmpl = TWO_PASS[variant]
                    parsed = extract_two_pass(dialogue, ent_tmpl, rel_tmpl)
                else:
                    parsed = extract(dialogue, PROMPTS[variant])
            elapsed = time.time() - t0
        except Exception as e:
            print(f"  ERROR: {e}")
            continue

        if args.dump:
            print("--- RAW OUTPUT ---")
            print(json.dumps(parsed, indent=2))
            print("------------------")

        n_ent = len(parsed.get("entities", []))
        n_rel = len(parsed.get("relationships", []))
        flat = flatten(parsed)
        result = score_facts(flat, facts)

        print(f"  Entities: {n_ent}  Rels: {n_rel}  Time: {elapsed:.1f}s")
        print(f"  Score:    {result['hits']}/{result['total']} = {result['score']:.0%}")
        summary.append((variant, result["score"], n_ent, n_rel, elapsed))

        if args.verbose:
            for r in result["per_fact"]:
                marker = "✓" if r["found"] else "✗"
                print(f"    {marker} {r['label']}")
        print()

    if len(summary) > 1:
        print("=== Summary ===")
        print(f"  {'Variant':<12} {'Score':>7} {'Ents':>5} {'Rels':>5} {'Time':>7}")
        for v, s, e, r, t in sorted(summary, key=lambda x: -x[1]):
            print(f"  {v:<12} {s:>6.0%} {e:>5} {r:>5} {t:>6.1f}s")


if __name__ == "__main__":
    main()
