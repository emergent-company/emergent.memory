#!/usr/bin/env python3
"""
session_dump.py — Dump and analyze OpenCode sessions from any project.

Usage:
    python3 session_dump.py --project /path/to/project [OPTIONS]

    --project PATH          Path to the project directory (required)
    --session ID            Dump a specific session by ID or partial title match
    --list                  List all sessions for the project
    --list N                List the N most recent sessions
    --since YYYY-MM-DD      Filter sessions after this date
    --search QUERY          Search session titles for a string
    --content-search QUERY  Search session message content for a keyword (use when title is unknown)
    --output FILE           Write dump to a file (default: stdout)
    --stats                 Show usage stats (skills, tools, tokens, cost)
    --top N                 Number of top items in stats tables (default: 15)
"""

import argparse
import json
import re
import sqlite3
import sys
from datetime import datetime, timezone
from pathlib import Path

_ANSI_RE = re.compile(r'\x1b\[[0-9;]*m')

DB_PATH = Path.home() / ".local/share/opencode/opencode.db"


# ── Helpers ─────────────────────────────────────────────────────────────────

def fmt_cost(cost: float) -> str:
    if cost == 0:
        return "$0.00"
    if cost < 0.01:
        return f"${cost:.4f}"
    return f"${cost:.2f}"


def fmt_tokens(n: int) -> str:
    if n >= 1_000_000:
        return f"{n/1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n/1_000:.1f}k"
    return str(n)


def bar(value: int, max_value: int, width: int = 24) -> str:
    filled = int(width * value / max_value) if max_value > 0 else 0
    return "█" * filled + "░" * (width - filled)


def fmt_ts(ms: int) -> str:
    return datetime.fromtimestamp(ms / 1000, tz=timezone.utc).strftime("%Y-%m-%d %H:%M UTC")


def get_project_id(conn: sqlite3.Connection, project_path: str) -> str:
    cur = conn.cursor()
    # Normalize path
    path = str(Path(project_path).resolve())
    cur.execute("SELECT id, worktree FROM project WHERE worktree = ?", [path])
    row = cur.fetchone()
    if row:
        return row["id"]
    # Fuzzy match
    cur.execute("SELECT id, worktree FROM project WHERE worktree LIKE ?", [f"%{path}%"])
    rows = cur.fetchall()
    if len(rows) == 1:
        return rows[0]["id"]
    if len(rows) > 1:
        print(f"Multiple projects match '{path}':", file=sys.stderr)
        for r in rows:
            print(f"  {r['id']} | {r['worktree']}", file=sys.stderr)
        sys.exit(1)
    print(f"ERROR: No project found for path '{path}'", file=sys.stderr)
    print("Available projects:", file=sys.stderr)
    cur.execute("SELECT id, worktree FROM project ORDER BY time_updated DESC")
    for r in cur.fetchall():
        print(f"  {r['worktree']}", file=sys.stderr)
    sys.exit(1)


def get_sessions(conn: sqlite3.Connection, project_id: str, since_ms: int | None = None,
                 limit: int = 0, search: str = "") -> list[sqlite3.Row]:
    cur = conn.cursor()
    params: list = [project_id]
    clauses = ["project_id = ?"]
    if since_ms:
        clauses.append("time_created >= ?")
        params.append(since_ms)
    if search:
        clauses.append("title LIKE ?")
        params.append(f"%{search}%")
    where = " AND ".join(clauses)
    q = f"SELECT id, title, time_created FROM session WHERE {where} ORDER BY time_created DESC"
    if limit:
        q += f" LIMIT {limit}"
    cur.execute(q, params)
    return cur.fetchall()


def get_parts_for_session(conn: sqlite3.Connection, session_id: str) -> list[dict]:
    cur = conn.cursor()
    cur.execute("""
        SELECT m.id as msg_id, m.data as msg_data
        FROM message m
        WHERE m.session_id = ?
        ORDER BY m.rowid ASC
    """, [session_id])
    messages = cur.fetchall()

    result = []
    for msg in messages:
        msg_d = json.loads(msg["msg_data"])
        role = msg_d.get("role", "?")
        model = msg_d.get("modelID", "")

        cur.execute("""
            SELECT data FROM part
            WHERE message_id = ? AND session_id = ?
            ORDER BY rowid ASC
        """, [msg["msg_id"], session_id])
        parts = cur.fetchall()

        text_parts = []
        for p in parts:
            pd = json.loads(p["data"])
            ptype = pd.get("type", "")
            if ptype == "text":
                t = pd.get("text", "").strip()
                if t:
                    text_parts.append(t)
            elif ptype == "tool":
                tool = pd.get("tool", "")
                inp = pd.get("input", {})
                inp_str = json.dumps(inp, indent=2)
                if len(inp_str) > 800:
                    inp_str = inp_str[:800] + "\n  ...(truncated)"
                text_parts.append(f"**[TOOL: {tool}]**\n```json\n{inp_str}\n```")
            elif ptype == "tool-result":
                tool = pd.get("tool", "")
                out = pd.get("output", "")
                out_str = json.dumps(out, indent=2) if isinstance(out, (list, dict)) else str(out)
                if len(out_str) > 1000:
                    out_str = out_str[:1000] + "\n...(truncated)"
                text_parts.append(f"**[RESULT: {tool}]**\n```\n{out_str}\n```")

        combined = "\n\n".join(text_parts).strip()
        if combined:
            result.append({"role": role, "model": model, "content": combined})

    return result


# ── Commands ─────────────────────────────────────────────────────────────────

def cmd_list(conn, project_id, args):
    limit = args.list if isinstance(args.list, int) and args.list > 0 else 0
    since_ms = None
    if args.since:
        dt = datetime.fromisoformat(args.since).replace(tzinfo=timezone.utc)
        since_ms = int(dt.timestamp() * 1000)

    sessions = get_sessions(conn, project_id, since_ms=since_ms, limit=limit, search=args.search or "")
    print(f"\n{'─'*70}")
    print(f"  Sessions for project: {args.project}")
    print(f"  Total: {len(sessions)}")
    print(f"{'─'*70}")
    for s in sessions:
        dt = fmt_ts(s["time_created"])
        title = (s["title"] or "untitled")[:60]
        print(f"  [{dt}]  {s['id']}  {title}")
    print()


def cmd_dump(conn, project_id, args, out):
    # Find session
    cur = conn.cursor()
    session_id = args.session

    # Try exact ID first
    cur.execute("SELECT id, title, time_created FROM session WHERE id = ? AND project_id = ?",
                [session_id, project_id])
    row = cur.fetchone()

    if not row:
        # Try title search
        cur.execute("""SELECT id, title, time_created FROM session
                       WHERE project_id = ? AND title LIKE ?
                       ORDER BY time_created DESC LIMIT 5""",
                    [project_id, f"%{session_id}%"])
        rows = cur.fetchall()
        if not rows:
            print(f"ERROR: No session found matching '{session_id}'", file=sys.stderr)
            sys.exit(1)
        if len(rows) > 1:
            print(f"Multiple sessions match '{session_id}':", file=sys.stderr)
            for r in rows:
                print(f"  [{fmt_ts(r['time_created'])}] {r['id']} | {r['title']}", file=sys.stderr)
            sys.exit(1)
        row = rows[0]

    title = row["title"] or "untitled"
    dt = fmt_ts(row["time_created"])
    sid = row["id"]

    lines = [
        f"# Session: {title}",
        f"**Date:** {dt}  |  **Project:** {args.project}  |  **ID:** {sid}",
        "",
        "---",
        "",
    ]

    parts = get_parts_for_session(conn, sid)
    for p in parts:
        role = p["role"]
        model = p["model"]
        content = p["content"]

        if role == "user":
            lines.append("## 👤 User")
        else:
            label = "## 🤖 Assistant"
            if model:
                label += f" `{model}`"
            lines.append(label)

        lines.append("")
        lines.append(content)
        lines.append("")
        lines.append("---")
        lines.append("")

    output = "\n".join(lines)

    if args.output:
        Path(args.output).write_text(output)
        print(f"Dumped {len(output):,} chars → {args.output}")
        print(f"Messages: {len(parts)}")
    else:
        out.write(output)


def cmd_stats(conn, project_id, args):
    cur = conn.cursor()
    since_ms = None
    if args.since:
        dt = datetime.fromisoformat(args.since).replace(tzinfo=timezone.utc)
        since_ms = int(dt.timestamp() * 1000)

    # Get session IDs
    params: list = [project_id]
    clauses = ["project_id = ?"]
    if since_ms:
        clauses.append("time_created >= ?")
        params.append(since_ms)
    where = " AND ".join(clauses)

    limit_clause = f"LIMIT {args.sessions}" if args.sessions else ""
    cur.execute(f"SELECT id, time_created FROM session WHERE {where} ORDER BY time_created DESC {limit_clause}", params)
    sessions = cur.fetchall()
    if not sessions:
        print("No sessions found.")
        return

    session_ids = [s["id"] for s in sessions]
    placeholders = ",".join("?" * len(session_ids))
    part_params = session_ids
    msg_params = session_ids

    t_min = fmt_ts(min(s["time_created"] for s in sessions))
    t_max = fmt_ts(max(s["time_created"] for s in sessions))

    print(f"\n{'═'*70}")
    print(f"  OpenCode Usage Stats — {args.project}")
    print(f"  Sessions: {len(session_ids)}  |  Period: {t_min} → {t_max}")
    print(f"{'═'*70}")

    top = args.top

    # Skill usage
    cur.execute(f"""
        SELECT json_extract(p.data, '$.state.input.name') AS skill_name,
               COUNT(*) AS calls, COUNT(DISTINCT p.session_id) AS sessions
        FROM part p
        WHERE p.session_id IN ({placeholders})
          AND json_extract(p.data, '$.type') = 'tool'
          AND json_extract(p.data, '$.tool') = 'skill'
          AND skill_name IS NOT NULL
        GROUP BY skill_name ORDER BY calls DESC LIMIT ?
    """, part_params + [top])
    skill_rows = cur.fetchall()

    if skill_rows:
        max_calls = skill_rows[0]["calls"]
        print(f"\n{'━'*70}\n  🎯  Skill Triggers  (top {top})\n{'━'*70}")
        print(f"  {'Skill':<35}  {'Calls':>5}  {'Sessions':>8}")
        print(f"  {'-'*60}")
        for r in skill_rows:
            print(f"  {r['skill_name']:<35}  {r['calls']:>5}  {r['sessions']:>8}  {bar(r['calls'], max_calls, 16)}")

    # Tool usage
    cur.execute(f"""
        SELECT json_extract(p.data, '$.tool') AS tool,
               COUNT(*) AS calls, COUNT(DISTINCT p.session_id) AS sessions
        FROM part p
        WHERE p.session_id IN ({placeholders})
          AND json_extract(p.data, '$.type') = 'tool'
          AND tool IS NOT NULL
        GROUP BY tool ORDER BY calls DESC LIMIT ?
    """, part_params + [top])
    tool_rows = cur.fetchall()

    if tool_rows:
        max_calls = tool_rows[0]["calls"]
        print(f"\n{'━'*70}\n  🔧  Tool Usage  (top {top})\n{'━'*70}")
        print(f"  {'Tool':<35}  {'Calls':>5}  {'Sessions':>8}")
        print(f"  {'-'*60}")
        for r in tool_rows:
            print(f"  {r['tool']:<35}  {r['calls']:>5}  {r['sessions']:>8}  {bar(r['calls'], max_calls, 16)}")

    # Token / cost by model
    cur.execute(f"""
        SELECT json_extract(m.data, '$.providerID') || '/' ||
               json_extract(m.data, '$.modelID')           AS model,
               COUNT(*)                                     AS messages,
               COUNT(DISTINCT m.session_id)                 AS sessions,
               SUM(json_extract(m.data, '$.tokens.total')) AS tokens_total,
               SUM(json_extract(m.data, '$.tokens.input')) AS tokens_in,
               SUM(json_extract(m.data, '$.tokens.output'))AS tokens_out,
               SUM(json_extract(m.data, '$.cost'))         AS cost
        FROM message m
        WHERE m.session_id IN ({placeholders})
          AND json_extract(m.data, '$.role') = 'assistant'
          AND json_extract(m.data, '$.modelID') IS NOT NULL
        GROUP BY model ORDER BY tokens_total DESC LIMIT ?
    """, msg_params + [top])
    model_rows = cur.fetchall()

    if model_rows:
        grand_tokens = sum(r["tokens_total"] or 0 for r in model_rows)
        grand_cost   = sum(r["cost"]         or 0 for r in model_rows)
        max_tok = model_rows[0]["tokens_total"] or 1
        print(f"\n{'━'*70}\n  📊  Tokens & Cost by Model  (top {top})\n{'━'*70}")
        print(f"  {'Model':<45}  {'Tokens':>7}  {'In':>6}  {'Out':>6}  {'Cost':>8}  {'Msgs':>5}")
        print(f"  {'-'*80}")
        for r in model_rows:
            tok = r["tokens_total"] or 0
            print(f"  {r['model']:<45}  {fmt_tokens(tok):>7}  {fmt_tokens(r['tokens_in'] or 0):>6}  "
                  f"{fmt_tokens(r['tokens_out'] or 0):>6}  {fmt_cost(r['cost'] or 0):>8}  {r['messages']:>5}  "
                  f"{bar(tok, max_tok, 14)}")
        print(f"\n  Totals: {fmt_tokens(grand_tokens)} tokens  |  {fmt_cost(grand_cost)}")

    # Top sessions by skill calls
    cur.execute(f"""
        SELECT p.session_id, COUNT(*) AS skill_calls
        FROM part p
        WHERE p.session_id IN ({placeholders})
          AND json_extract(p.data, '$.type') = 'tool'
          AND json_extract(p.data, '$.tool') = 'skill'
        GROUP BY p.session_id ORDER BY skill_calls DESC LIMIT 10
    """, part_params)
    heat_rows = cur.fetchall()

    if heat_rows:
        ids = [r["session_id"] for r in heat_rows]
        cur.execute(f"SELECT id, title, time_created FROM session WHERE id IN ({','.join('?'*len(ids))})", ids)
        title_map = {r["id"]: (r["title"] or "untitled", r["time_created"]) for r in cur.fetchall()}
        max_calls = heat_rows[0]["skill_calls"]
        print(f"\n{'━'*70}\n  🔥  Most Skill-Heavy Sessions  (top 10)\n{'━'*70}")
        for r in heat_rows:
            title, tc = title_map.get(r["session_id"], ("?", 0))
            dt = fmt_ts(tc) if tc else "?"
            print(f"  {r['skill_calls']:>3}  {bar(r['skill_calls'], max_calls, 18)}  [{dt}]  {title[:45]}")

    print(f"\n{'═'*70}\n")


# ── Main ─────────────────────────────────────────────────────────────────────

def cmd_content_search(conn: sqlite3.Connection, project_id: str, args):
    """Search message content across all sessions for a project."""
    cur = conn.cursor()
    keyword = getattr(args, 'content_search', '')

    # Fetch all text parts for this project (no keyword filter in SQL — ANSI codes break LIKE)
    cur.execute("""
        SELECT DISTINCT
            s.id AS session_id,
            s.title,
            s.time_created,
            json_extract(m.data, '$.role') AS role,
            json_extract(pt.data, '$.text') AS text
        FROM session s
        JOIN message m ON m.session_id = s.id
        JOIN part pt ON pt.message_id = m.id
        WHERE s.project_id = ?
          AND json_extract(pt.data, '$.type') = 'text'
        ORDER BY s.time_created DESC
    """, [project_id])
    rows = cur.fetchall()

    # Deduplicate by session, collect snippets — strip ANSI before matching
    seen: dict = {}
    for r in rows:
        text = _ANSI_RE.sub('', r['text'] or '')
        idx = text.lower().find(keyword.lower())
        if idx < 0:
            continue
        sid = r['session_id']
        if sid not in seen:
            seen[sid] = {'title': r['title'], 'time': r['time_created'], 'snippets': []}
        start = max(0, idx - 60)
        end = min(len(text), idx + len(keyword) + 60)
        snippet = ('…' if start > 0 else '') + text[start:end].replace('\n', ' ') + ('…' if end < len(text) else '')
        seen[sid]['snippets'].append(f"[{r['role']}] {snippet}")

    if not seen:
        print(f"No sessions found containing '{keyword}'")
        return

    print(f"\n{'─'*70}")
    print(f"  Content search: '{keyword}'  —  {len(seen)} session(s) found")
    print(f"{'─'*70}")
    for sid, info in seen.items():
        dt = fmt_ts(info['time'])
        title = (info['title'] or 'untitled')[:55]
        print(f"\n  [{dt}]  {sid}")
        print(f"  {title}")
        for snippet in info['snippets'][:3]:
            print(f"    · {snippet[:120]}")
    print()


def parse_args():
    p = argparse.ArgumentParser(description="Analyze OpenCode sessions for any project")
    p.add_argument("--project", required=True, help="Path to the project directory")
    p.add_argument("--session", default="", help="Session ID or title substring to dump")
    p.add_argument("--list", nargs="?", const=0, type=int, metavar="N",
                   help="List sessions (optionally limit to N most recent)")
    p.add_argument("--search", default="", help="Filter sessions by title substring")
    p.add_argument("--content-search", default="", help="Search session message content for a keyword")
    p.add_argument("--since", default="", help="Only sessions after YYYY-MM-DD")
    p.add_argument("--output", default="", help="Write dump to this file path")
    p.add_argument("--stats", action="store_true", help="Show usage statistics")
    p.add_argument("--sessions", type=int, default=0, help="Limit stats to N most recent sessions")
    p.add_argument("--top", type=int, default=15, help="Top N rows in stats tables")
    p.add_argument("--db", default=str(DB_PATH), help="Path to opencode.db")
    return p.parse_args()


def main():
    args = parse_args()

    if not Path(args.db).exists():
        print(f"ERROR: DB not found at {args.db}", file=sys.stderr)
        sys.exit(1)

    conn = sqlite3.connect(args.db)
    conn.row_factory = sqlite3.Row

    project_id = get_project_id(conn, args.project)

    if args.stats:
        cmd_stats(conn, project_id, args)
    elif args.list is not None:
        cmd_list(conn, project_id, args)
    elif getattr(args, 'content_search', ''):
        cmd_content_search(conn, project_id, args)
    elif args.session:
        cmd_dump(conn, project_id, args, sys.stdout)
    else:
        # Default: list recent sessions
        args.list = 20
        cmd_list(conn, project_id, args)

    conn.close()


if __name__ == "__main__":
    main()
