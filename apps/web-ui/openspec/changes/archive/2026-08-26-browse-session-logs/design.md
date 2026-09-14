## Context

The agents already write every conversation to an append-only JSONL log (`SESSION_LOG`, records of `kind` = `turn` / `tools_executed` / `usage` / `session_start` / `session_end`, keyed by `room`). `agent/admin.py` already reads it and serves `/api/sessions` (list, grouped by room) and `/api/session?room=` (chronological timeline) for the internal admin page — unauthenticated, Tailscale-only. The iOS app already talks to `admin.py` (:8080) with `X-API-Key` for the token endpoint and the memories API. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Expose the existing session log through the same authenticated `X-API-Key` channel the iOS app already uses.
- Add a native chat-style browser: session list → conversation view with per-turn tool-call details.

**Non-Goals:**
- No session-log editing, pruning, or export (read-only).
- No change to how/when the agents write the log.
- No real-time streaming — the browser shows recorded sessions on open (pull, not push).

## Decisions

### 1. Session API lives in `agent/admin.py`, reusing the existing reader and endpoints

`/api/sessions` (list) and `/api/session?room=` (timeline) already do the work. Add the same `X-API-Key` gate as `/api/token` and `/api/memories`, and keep the raw-chronological record shape (turns, tool calls, usage) rather than inventing a new transformation layer.

- **Alternative considered:** new endpoints alongside the admin-page ones to avoid touching the admin page. Rejected — the reader and shape are already correct; duplicating them for one auth flag is churn.
- The internal admin page's sessions viewer must keep working: inject `TOKEN_API_KEY` server-side into the served HTML/JS so its fetches send the header (the key is already returned in the clear by `/api/qr-config`, so this adds no exposure).

### 2. Timeline returns raw records; the iOS client interprets by `kind`

The backend returns the chronological records verbatim. The client maps `turn` → chat bubble (user vs assistant), `tools_executed` → an expandable tool-call detail attached to the preceding turn (name, arguments, result, `is_error`), and ignores `usage`/`session_start`/`session_end` (or shows usage as a subtle footer). No backend reshaping.

- **Alternative considered:** backend transforms records into a `{messages: [{role, text, toolCalls}]}` envelope. Rejected — it duplicates parsing the client already must do to render, and the raw form is already the source of truth.

### 3. Native SwiftUI chat rendering

Session list via `List`; the conversation via a `ScrollView`/`List` of message bubbles (user right-aligned, assistant left), with `DisclosureGroup` or inline-expandable rows for tool-call details. Matches the memories browser's use of `NavigationStack` + sheets and the existing design system.

## Risks / Trade-offs

- **Large `session_end` payloads** — the `history` blob can be big (full prompts + entity catalog). → Mitigation: the timeline endpoint drops/elides `session_end.history` before returning; the browser only renders `turn`/`tools_executed`.
- **Admin-page auth regression** — gating `/api/sessions` breaks the existing sessions viewer until its JS sends the key. → Mitigation: update the admin HTML in the same change; verify both consumers after deploy.
- **Log growth / read cost** — `_read_sessions_raw()` tail-reads a capped window already. → Acceptable; the browser lists a bounded recent window.

## Migration Plan

1. Add `X-API-Key` gating to `/api/sessions` + `/api/session` in `admin.py`; elide `session_end.history`.
2. Inject the key into the admin page JS so the internal viewer keeps working.
3. Add iOS session-list + chat views; deploy and rebuild.
4. Rollback: revert `admin.py` (endpoints were additive/behavioral only) and the app binary independently.

## Open Questions

None — the remaining choices (exact bubble styling, whether to show usage) are cosmetic and don't affect the specs or task breakdown.
