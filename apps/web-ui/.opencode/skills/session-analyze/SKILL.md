---
name: session-analyze
description: Analyze a recorded Memory agent session and suggest concrete improvements. Use when the user asks to "analyze session <id>", "review session <id>", "what went wrong in session <id>", "suggest improvements from this session", or wants a coding agent to inspect a recorded Memory conversation by ID.
---

# Session Analyze

Analyze one recorded Memory session (chat/voice conversation) and produce concrete, evidence-backed improvement suggestions.

## 1. Fetch the session dump

The Memory gateway serves a formatted dump. Read the gateway host/port and API key from `/root/alfred/.env` (`MEMORY_PORT`, default 8095; `TOKEN_API_KEY`, empty in dev). The live instance serves on host `livekit` port 8082. Try, in order:

```bash
curl -sS "http://livekit:8082/api/conversations/<SESSION_ID>/dump"
curl -sS "http://localhost:${MEMORY_PORT:-8095}/api/conversations/<SESSION_ID>/dump"
```

If `TOKEN_API_KEY` is non-empty, add `-H "X-API-Key: $TOKEN_API_KEY"`.

`?format=json` returns structured JSON; the default is human-readable text. Prefer text.

Fallback (gateway unreachable) — query memory directly:

```bash
set -a; . /root/alfred/.env; set +a
curl -sS -H "Authorization: Bearer $MEMORY_TOKEN" "$MEMORY_URL/api/chat/<SESSION_ID>/history"
```

## 2. Analyze

From the dump, determine:

- **Goal** — what the user actually wanted.
- **Outcome** — succeeded / partial / failed.
- **Frictions** — each with evidence:
  - tool errors (`error:` lines, `failed` in tool output)
  - agent loops / repeated tool calls with near-identical args
  - user re-asks (repeated near-identical user turns)
  - wrong or missing tool
  - misunderstanding / language mismatch
  - over-long or rambling responses
- **Root cause** — assistant text may include reasoning; read it to see why the agent acted as it did.

## 3. Suggest improvements

For each friction, propose the SMALLEST useful fix, in priority order:

1. prompt rule (agent system prompt)
2. tool fix or ban (`bannedTools`)
3. skill
4. memory/config change
5. playbook / doc

Each suggestion: one line of **what + why + where** (agent definition id, tool name). Confidence-gate: mark weak or one-off signals "skip" or "needs more evidence". Prefer no-change over speculative change.

## 4. Output

```text
Session <id> — <title>  (agent=<agentDefinitionId>, model=<m>)
Goal: ...
Outcome: ...

Findings
- <friction>: <evidence>

Recommended changes
- <change>: <why> → <where>

Skipped / needs more evidence
- ...
```

Do NOT modify agent configs, tools, or code without proposing first — report suggestions, then ask.
