# Deduplicate gateway/memory embedding-model classification

**Status:** done
**Created:** 2026-09-09
**Source:** [2026-09-09-provider-setup-ux](sessions/2026-09-09-provider-setup-ux.md)

## What

The gateway now mirrors memory's OpenAI-compatible model classification to seed
the edit-form fallback dropdowns after a successful connection test:
`nameLooksEmbedding` (lowercased name contains `embedding` or `text-embed`) and
the `vendor/model` display-name strip in `gateway/settings_providers.go`
(`fetchOpenAICompatFallbackModels`), copied from
`emergent.memory/apps/server/domain/provider/catalog.go`. That is two copies of
one rule.

Options to converge:
1. Gateway keeps fetching `{base_url}/models` itself but classifies via a small
   shared/exported rule (memory is a separate module/repo — would need a
   published helper or a linted copy with a "mirror of X" pointer, which exists
   today).
2. Add a memory endpoint that resolves an ephemeral catalog for unsaved
   credentials (base_url + api_key), reusing the authoritative catalog service —
   the gateway then stops mirroring. This is the durable fix but touches a
   cross-repo deploy surface.
3. Relax the gateway heuristic so it is a strict superset (put every model in
   generative, embedding = heuristic match only), which bounds the downside of a
   misclassification.

## Why

If memory's classification logic evolves (e.g. a new embedding-model family with
no "embed" in the name, or catalog-driven classification wins over the
heuristic), the gateway seeding will silently misplace models.

## Depends on

none

## Notes

- Add/Edit seeding lives only on the provider **edit** form (add form is now
  credentials-only).
- misclassification severity is low (wrong list, user can still leave
  "None — auto-select"), which is why option 3 is a cheap interim.

## Done (2026-09-10)

Shipped **option 3** in `gateway/settings_providers.go`
(`fetchOpenAICompatFallbackModels`): the generative list is now a **strict
superset** of the embedding list.

- Every valid fetched id is appended to `gen` (ModelType `generative`) with no
  heuristic gate.
- Ids matching `nameLooksEmbedding` are **additionally** appended to `emb`
  (ModelType `embedding`). Both lists preserve the fetched order.
- Comment on `fetchOpenAICompatFallbackModels` (and `nameLooksEmbedding`) states
  the superset behavior, why it is only the interim bound, and points here.

Effect: if memory's authoritative classification evolves past this cheap name
heuristic, a divergent gateway no longer silently **drops or misplaces** a
model — the worst case degrades to a missed embedding **opt-in** (the model is
still offered as generative, and the user can leave "None — auto-select").

Tests: `TestUIProviderTestConnectionSeedsFallbackModels` now asserts a
non-matching id (`gpt-4o`, `vendor/deepseek-v4-flash`) appears in `gen` only,
and a matching id (`text-embedding-3-small`) appears in **both** `gen` and
`emb`, with a test comment pointing at the mirror/doc contract.

**Durable follow-up still open:** option 2 — a memory endpoint that resolves an
ephemeral catalog for unsaved credentials (`base_url` + `api_key`) via the
authoritative catalog service, so the gateway stops mirroring the rule at all.
That remains the cross-repo fix; option 3 is only the bound on its downside.
