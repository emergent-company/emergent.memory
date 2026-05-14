#!/usr/bin/env python3
"""
memory_session_dump.py — Dump entities and relationships extracted into a Memory
namespace during a bench run (or any extraction session).

Usage:
    python3 memory_session_dump.py --namespace NAMESPACE [OPTIONS]

    --namespace NS      Namespace prefix to query (e.g. conv-26-q0-run-20260511-160636)
    --url URL           Memory API base URL (default: https://memory.emergent-company.ai)
    --token TOKEN       Memory API token (emt_...)
    --project ID        Memory project UUID
    --output FILE       Write dump to file (default: stdout)
    --limit N           Max objects/relationships to fetch per page (default: 200)
    --stats-only        Print counts only, no full object list

Defaults (hardcoded for bench project):
    URL:     https://memory.emergent-company.ai
    TOKEN:   emt_90e466b66031ef242148336a85152d30f78ba3e723fb81dc7ebed0fefc9156de
    PROJECT: ea1fe3b1-6ec9-48a0-8469-46211895f3be
"""

import argparse
import json
import sys
import urllib.request
import urllib.parse
from collections import defaultdict
from pathlib import Path

DEFAULT_URL     = "https://memory.emergent-company.ai"
DEFAULT_TOKEN   = "emt_90e466b66031ef242148336a85152d30f78ba3e723fb81dc7ebed0fefc9156de"
DEFAULT_PROJECT = "ea1fe3b1-6ec9-48a0-8469-46211895f3be"


def api_get(base_url: str, path: str, token: str, project_id: str, params: dict) -> dict:
    qs = urllib.parse.urlencode({k: v for k, v in params.items() if v is not None})
    url = f"{base_url}{path}?{qs}"
    req = urllib.request.Request(url, headers={
        "Authorization": f"Bearer {token}",
        "X-Project-ID": project_id,
    })
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read())


def fetch_all(base_url: str, path: str, token: str, project_id: str,
              params: dict, page_size: int = 200) -> list:
    """Paginate through all results using cursor."""
    results = []
    cursor = None
    while True:
        p = {**params, "limit": page_size}
        if cursor:
            p["cursor"] = cursor
        data = api_get(base_url, path, token, project_id, p)
        items = data.get("items") or []
        results.extend(items)
        cursor = data.get("next_cursor")
        if not cursor or not items:
            break
    return results


def dump(args, out):
    ns = args.namespace
    url = args.url
    token = args.token
    project = args.project

    lines = [
        f"# Memory Session Dump",
        f"**Namespace:** `{ns}`",
        f"**Project:**   `{project}`",
        f"**Server:**    {url}",
        "",
        "---",
        "",
    ]

    # ── Entities ──────────────────────────────────────────────────────────────
    print(f"Fetching entities for namespace '{ns}'...", file=sys.stderr)
    entities = fetch_all(url, "/api/graph/objects/search", token, project,
                         {"namespace": ns, "order": "asc"}, args.limit)

    by_type: dict = defaultdict(list)
    for e in entities:
        by_type[e.get("type", "?")].append(e)

    lines.append(f"## Entities  ({len(entities)} total, {len(by_type)} types)")
    lines.append("")

    for typ, objs in sorted(by_type.items()):
        lines.append(f"### {typ}  ({len(objs)})")
        if not args.stats_only:
            for o in objs:
                key = o.get("key") or o.get("id", "?")
                props = o.get("properties") or {}
                name = props.get("name", "")
                prop_str = ", ".join(f"{k}={v}" for k, v in props.items() if k != "name") if props else ""
                detail = f"`{key}`"
                if name:
                    detail += f"  **{name}**"
                if prop_str:
                    detail += f"  _{prop_str}_"
                lines.append(f"- {detail}")
        lines.append("")

    # ── Relationships ─────────────────────────────────────────────────────────
    print(f"Fetching relationships for namespace '{ns}'...", file=sys.stderr)
    rels = fetch_all(url, "/api/graph/relationships/search", token, project,
                     {"source_namespace": ns, "order": "asc"}, args.limit)

    by_reltype: dict = defaultdict(list)
    for r in rels:
        by_reltype[r.get("type", "?")].append(r)

    # Build id→key map from entities for readable output
    id_to_key = {e["id"]: (e.get("key") or e["id"]) for e in entities}

    lines.append(f"## Relationships  ({len(rels)} total, {len(by_reltype)} types)")
    lines.append("")

    for rtype, rs in sorted(by_reltype.items()):
        lines.append(f"### {rtype}  ({len(rs)})")
        if not args.stats_only:
            for r in rs:
                src = id_to_key.get(r.get("src_id", ""), r.get("src_id", "?"))
                dst = id_to_key.get(r.get("dst_id", ""), r.get("dst_id", "?"))
                props = r.get("properties") or {}
                prop_str = f"  _{json.dumps(props)}_" if props else ""
                lines.append(f"- `{src}` → `{dst}`{prop_str}")
        lines.append("")

    # ── Summary ───────────────────────────────────────────────────────────────
    lines.append("## Summary")
    lines.append("")
    lines.append(f"| | Count |")
    lines.append(f"|---|---|")
    lines.append(f"| Entities | {len(entities)} |")
    lines.append(f"| Entity types | {len(by_type)} |")
    lines.append(f"| Relationships | {len(rels)} |")
    lines.append(f"| Relationship types | {len(by_reltype)} |")
    lines.append("")
    lines.append("**Entity type breakdown:**")
    for typ, objs in sorted(by_type.items(), key=lambda x: -len(x[1])):
        lines.append(f"- {typ}: {len(objs)}")
    lines.append("")
    lines.append("**Relationship type breakdown:**")
    for rtype, rs in sorted(by_reltype.items(), key=lambda x: -len(x[1])):
        lines.append(f"- {rtype}: {len(rs)}")

    output = "\n".join(lines)

    if args.output:
        Path(args.output).write_text(output)
        print(f"Dumped → {args.output}  ({len(entities)} entities, {len(rels)} relationships)", file=sys.stderr)
    else:
        out.write(output)


def main():
    p = argparse.ArgumentParser(description="Dump Memory graph entities/relationships for a namespace")
    p.add_argument("--namespace", required=True, help="Namespace to query")
    p.add_argument("--url",     default=DEFAULT_URL,     help="Memory API base URL")
    p.add_argument("--token",   default=DEFAULT_TOKEN,   help="Memory API token")
    p.add_argument("--project", default=DEFAULT_PROJECT, help="Memory project UUID")
    p.add_argument("--output",  default="",              help="Output file path (default: stdout)")
    p.add_argument("--limit",   type=int, default=200,   help="Page size for API calls")
    p.add_argument("--stats-only", action="store_true",  help="Print counts only, no object list")
    args = p.parse_args()

    dump(args, sys.stdout)


if __name__ == "__main__":
    main()
