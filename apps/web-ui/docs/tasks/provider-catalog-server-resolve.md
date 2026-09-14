# Server-side provider model catalog resolve (classification parity, durable)

**Status:** proposed
**Created:** 2026-09-13
**Source:** [2026-09-13-review-bot-and-backlog](../sessions/2026-09-13-review-bot-and-backlog.md)

## What

Replace the gateway's mirrored embedding/generative model-classification heuristic with a
memory-side resolve. PR #52 (option 3) made the gateway a strict generative superset so a
heuristic miss can never drop a model, but the gateway still duplicates memory's
`nameLooksEmbedding` rule — which will drift.

Implement option 2: a memory endpoint/field that returns the resolved ephemeral provider catalog
with authoritative model classification (and embedding capability), so the gateway renders it
directly.

## Why

Two copies of the classification rule drift silently; the superset only bounds the downside. The
durable fix is a single source of truth.

## Depends on

- `provider-model-classification-parity` (option 3 interim) — done.
- Memory `/models` / catalog code (`domain/provider/catalog.go`).

## Notes

- Consumed by the provider setup UI (`gateway/settings_providers.go`) and the embedding-provider
  guidance callout (#87).
- Coordinate the API shape with `embedding-provider-guidance` so capability comes from one place.
