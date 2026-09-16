## Why

The gateway Go app (served on `:8082`) is Alfred's single web interface — agents, chat, conversations, MCP servers, models — all backed by the memory service. But it has no per-agent screen that shows one agent's summary, its configured tools, its recent chats, and its memories. Meanwhile two legacy web surfaces linger: the `ui/` Go app (a superseded control-plane dashboard) and the Python `agent/admin.py` HTML frontend. This consolidates the dashboard into the gateway and removes the other interfaces.

## What Changes

- New per-agent **dashboard** in the gateway (`/agents/{id}`): agent summary (name, model, flow, visibility, tools count, description), **configured tools** (allowed + banned tool names), and **recent chats** (that agent's conversations, linking to `/chat?c=`).
- New **memories browser** (`/agents/{id}/memories`) mirroring the iOS memory browser: a searchable list of memory objects (content, category, confidence) with a detail view for full content.
- Remove the `ui/` Go app (superseded by the gateway).
- Retire the Python `agent/admin.py` HTML frontend (Overview page, Sessions page, prompt editor, catalog preview); keep its iOS-facing JSON endpoints.

## Capabilities

### New Capabilities

- `agent-dashboard-ui`: the gateway agent dashboard (summary, configured tools, recent chats) and its memories subpage (searchable list + detail).

### Modified Capabilities

None.

## Impact

- `gateway/`: new `/agents/{id}` and `/agents/{id}/memories` routes + templates, `MemoryClient` methods to list/search memory objects, `MemoryBackend` interface + fake extended, and the agents table links to the dashboard.
- `ui/`: deleted.
- `agent/admin.py`: HTML pages and the prompt-editor/catalog routes removed; `/api/token`, `/api/qr-config`, `/api/qr.svg`, `/api/sessions`, `/api/session`, `/api/memories`, `/api/memories/capability` kept for the iOS client.
- No breaking change to the iOS app.
