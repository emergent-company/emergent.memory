## Context

The Memory Platform stores arbitrary typed graph objects with user-defined schemas. There is no native conversation primitive — building a session/message store requires manual schema creation, manual embedding policy setup, and manual relationship wiring on every project. The `HybridSearchRequest` already supports semantic search across objects, which means Message embeddings will work with existing search once types are registered.

Existing graph object pipeline: `POST /graph/objects` → store → optional embedding → searchable. The new session endpoints are a thin ergonomic layer on top.

## Goals / Non-Goals

**Goals:**
- Pre-register `Session` and `Message` schemas at server startup (no per-project setup)
- Atomic `POST /sessions/:id/messages` — creates Message object + `has_message` relationship in one transaction
- Auto-embed `Message.content` via a system-level embedding policy
- CLI convenience commands

**Non-Goals:**
- Custom session storage engine — Sessions and Messages are graph objects, period
- Real-time streaming of messages (separate concern)
- Per-project schema customization of Session/Message (users can extend via regular schemas)
- Message ordering guarantees beyond `sequence_number` assigned at append time

## Decisions

**1. Built-in types as seeded schema registry entries, not hardcoded Go structs**

Options: (a) hardcode Session/Message as Go types with special DB tables, (b) pre-seed them as schema registry entries at startup.

Chose (b). Session/Message become schema registry entries auto-upserted at server startup. Same storage, same indexing, same search — zero divergence from generic objects. Avoids a parallel storage path.

**2. Single consolidated endpoint `POST /api/v1/projects/:project/sessions/:id/messages`**

This endpoint:
1. Creates a Message graph object (type: `Message`, properties from request)
2. Assigns `sequence_number` = current message count + 1 (read under transaction)
3. Creates `has_message` relationship from Session → Message
4. Returns the created Message

Alternative was separate calls to graph API. Rejected — race condition on sequence_number, poor DX.

**3. Auto-embedding via system embedding policy seeded at startup**

At startup, alongside schema seeding, upsert a system embedding policy for `Message` type on field `content`. Marked as `system: true` so users can't delete it. Reuses the existing embedding pipeline — no new code path.

**4. Session handler as thin wrapper in `domain/graph`**

New `session_handler.go` + `session_service.go` in `domain/graph`. Avoids a new domain module. The session concept is a graph domain concern.

## Risks / Trade-offs

- **[sequence_number race]** → Use `SELECT COUNT(*) + 1 ... FOR UPDATE` on the session's message relationships within the message-append transaction
- **[embedding latency]** → Embedding is async (same as other objects) — message is available immediately, embedding populates within seconds
- **[startup seeding idempotency]** → Upsert on schema key, not insert — safe to run on every boot
