## Why

AI agent memory systems universally need to store conversations, but the Memory Platform has no native conversation primitives — every consumer reinvents the same Session/Message pattern on top of generic graph objects, with manual embedding setup and no atomic message-append. This blocks Diane and similar agents from using Memory as a first-class conversation store.

## What Changes

- New built-in schema types: `Session` and `Message` pre-registered in schema registry at startup
- New dedicated API endpoints for session/message CRUD that wrap graph object operations atomically
- Auto-embedding policy on `Message.content` — triggered without manual embedding policy configuration
- Auto-relationship wiring: appending a message to a session creates `session → has_message → message` automatically
- New CLI commands: `memory sessions create/list/get`, `memory sessions messages add/list`

## Capabilities

### New Capabilities

- `session-message-types`: Built-in `Session` and `Message` schema types with defined property contracts, pre-registered at server startup
- `session-api`: Dedicated REST endpoints for session and message lifecycle (`POST /sessions`, `POST /sessions/:id/messages`, `GET /sessions/:id/messages`)
- `session-cli`: CLI commands for session and message management (`memory sessions ...`)

### Modified Capabilities

- `memory-graph-skill`: Session/Message objects should be usable via the existing graph skill with semantic search across message content

## Impact

- `apps/server/domain/schemas/` — built-in type registration
- `apps/server/domain/graph/` — new session handler, service methods
- `apps/server/domain/embeddingpolicies/` — auto-policy for Message.content
- `tools/cli/` — new `sessions` subcommand
- No breaking changes — purely additive
