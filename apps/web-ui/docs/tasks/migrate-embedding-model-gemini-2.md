# Migrate embedding model to stable gemini-embedding-2 (re-index)

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-provider-model-config](../sessions/2026-09-08-provider-model-config.md)

## What

Plan and execute migration of the project's embedding model from `google/gemini-embedding-001`
(model-config default) to the stable GA `gemini-embedding-2` (`gemini-embedding-002`), including
re-embedding all existing vector data.

## Why

Two mismatched embedding models are in play:

- model-config default (`google/gemini-embedding-001`) — still serving, earliest shutdown
  **May 2028** (not urgent).
- google provider fallback (`gemini-embedding-2-preview`) — a preview variant whose earliest
  shutdown (Aug 2026) has passed; still served a live test as of the session, but at risk.

Google states the embedding **spaces are incompatible** between `gemini-embedding-001` and
`gemini-embedding-2`, so migration requires **re-embedding all documents** (no mixed-index reuse).
Both default to 3072 dims (Matryoshka, truncatable to 768/1536), but same dim ≠ same space.

## Depends on

- none (not urgent; deadline May 2028 for `-001`).

## Notes

- Choose `gemini-embedding-2` (GA), **not** `-2-preview` (at-risk). Align both the model-config
  default and the google provider fallback to the same model.
- `gemini-embedding-2` drops the `task_type` param — task prefixes go in the prompt; and batch
  inputs differ (individual vs aggregated). Memory's embeddings client must match the new query
  convention.
- Re-index procedure lives with memory's document/embedding pipeline; coordinate with the memory
  repo owner for the re-embed job.
- Source: `@librarian` research (Google Gemini API deprecations + embeddings guide, 2026-09-07).
