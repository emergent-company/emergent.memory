---
name: session-analyzer
description: >-
  Analyze and dump OpenCode sessions from any project. Use when the user asks
  to read a session, find a session by title, list sessions for a project,
  dump session content, or show usage stats (skills, tools, tokens, cost) for
  any project directory.
metadata:
  author: emergent
  version: "1.0"
---

# Skill: session-analyzer

Read, search, and dump OpenCode sessions from any project on this machine.

---

## Script location

```
/root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py
```

---

## Commands

### List sessions for a project

```bash
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --list [N]
```

- Omit `N` to list all sessions (most recent first)
- Add `--since YYYY-MM-DD` to filter by date
- Add `--search "query"` to filter by title substring

### Find a session by title

```bash
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --search "visibility of memory"
```

### Find a session by content keyword (when title is unknown)

When the user gives you a keyword from the conversation (not the title), use `--content-search`. This searches the actual message text across all sessions for the project:

```bash
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --content-search "APIEndpoint"
```

Returns matching sessions with the surrounding context snippet. Use this when `--search` (title-only) returns nothing.

### Dump a session (to stdout or file)

```bash
# By session ID
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --session ses_2f374b1c2ffeS3zARC4vnePW3Q

# By title substring (auto-resolves if unique)
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --session "visibility of memory"

# Save to file
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --session "visibility of memory" \
  --output /tmp/session-dump.md
```

### Usage stats for a project

```bash
# All-time stats
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --stats

# Last 30 sessions
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --stats --sessions 30

# Since a date
python3 /root/emergent.memory/.opencode/skills/session-analyzer/session_dump.py \
  --project /path/to/project \
  --stats --since 2026-03-01
```

---

## Workflow

1. **Identify the project path** from the user's request (e.g. `/root/legalplant-api`, `/root/emergent.memory`)
2. **Run the appropriate command** above
3. **Present results** — for dumps, summarize the key conversation beats; for stats, call out top skills/tools/cost

### Known project paths

| Project | Path |
|---|---|
| emergent.memory | `/root/emergent.memory` |
| legalplant-api | `/root/legalplant-api` |
| twentyfirst | `/root/twentyfirst` |
| lawmatics | `/root/lawmatics` |
| specmcp | `/root/specmcp` |
| emergent (UI) | `/root/emergent` |
| doc-processing-suite | `/root/doc-processing-suite` |
| diane | `/root/diane` |

---

## Notes

- DB is at `~/.local/share/opencode/opencode.db` (read-only queries)
- Project is resolved by matching the `worktree` column in the `project` table
- Session content lives in the `part` table (not `message.data`) — the script handles this correctly
- Tool calls are shown inline with `[TOOL: name]` / `[RESULT: name]` markers
- Long tool inputs/outputs are truncated at 800/1000 chars in dumps
