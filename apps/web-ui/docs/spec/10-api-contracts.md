# Chat streaming API contracts

This document records the browser-facing Server-Sent Events (SSE) contract used
by the chat transcript. The gateway forwards engine events as JSON frames on the
`/api/chat` turn stream, the conversation/run push streams, and the public share
stream. One frame may carry a `live_replay` envelope whose `frames` array is
replayed in order through the same dispatcher.

## Frame envelope

Every frame is one JSON object with a `type` string. Unknown types are ignored.
The web UI dispatcher (`chat-host.js` → `makeHandleEvent`) is shared by `/chat`,
the assistant side panel, and public share pages.

| `type` | Meaning |
|---|---|
| `meta` | Conversation/session identity (`conversationId`, or `sessionId` on share). |
| `token` | Raw assistant text delta before the authoritative HTML snapshot. |
| `html` | Authoritative sanitized assistant markdown snapshot. |
| `thinking` | One reasoning delta (`id`, `text`, `done`, optional `role`). |
| `mcp_tool` | Tool call lifecycle (`id`, `tool`, `status`, `result`, category/subject hints). |
| `subagent` | Live child/subagent run lifecycle. See below. |
| `question` | Pending `ask_user` question card. |
| `approval` | Pending tool-approval card. |
| `ui` | Declarative A2UI surface update. |
| `citations` | Grounded citations for the turn. |
| `error` | Terminal stream error (optional `code` on share). |
| `live_replay` | `frames: [...]` of the above, replayed in order for a reattach/mid-run open. |
| `done` | The turn stream ended. |

## `subagent`

Projects a child run onto the parent chat stream so the composer can show a
Paseo-style subagent track. `id` is stable for one subagent across updates (the
child run id once known, otherwise a synthetic spawn id). Optional fields are
omitted when unknown, so a failed spawn with no child run is still renderable.

```json
{
  "type": "subagent",
  "id": "<stable>",
  "parentRunId": "<parent run id, optional>",
  "runId": "<child run id, optional>",
  "agentName": "researcher",
  "task": "scan the docs",
  "status": "queued|running|completed|failed|paused|canceled",
  "error": "<message, optional / failed>"
}
```

Client behaviour:

- Upsert by `runId`, falling back to `id`. A terminal status replaces the row's
  working state; it does not create a second row.
- A different `parentRunId` replaces the current track (a new parent run).
- Status buckets: `queued`/`running` → working, `paused` → needs input,
  `failed` → failed. Completed/canceled rows remain listed but do not add an
  active status segment; when no active rows exist the pill falls back to the
  total count (`N subagents`).
- A row with a `runId` links to `/runs/<runId>` in a new tab so the parent chat
  is preserved.
- `live_replay` frames run through the same handler, so a mid-run open or a
  reattached stream rebuilds the same track.
- On history reload the track is rebuilt from the newest run's `spawn_agents`
  and `trigger_agent` tool calls (input requests plus recorded output results).
