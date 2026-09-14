# Record usage for googleai (genai) embeddings

**Status:** done
**Created:** 2026-09-09
**Landed:** 2026-09-10 (emergent.memory PR #414)
**Source:** [2026-09-09-embedding-pricing-usage](sessions/2026-09-09-embedding-pricing-usage.md)

## Done (2026-09-10)

The genai client implements `usageReportingClient` (`EmbedQueryWithUsage` /
`EmbedDocumentsWithUsage`, `Provider: "googleai"`), threading summed prompt
tokens from the SDK's `EmbedContentResponse.Embeddings[0].Statistics.TokenCount`
(→ `usageMetadata.promptTokenCount`). `EmbedQuery`/`EmbedDocuments` now delegate
to the usage variants; vectors/semantics unchanged; absent statistics → 0 tokens
→ recorder skips, graceful. Compile-time assertions added for genai/vertex/openai
in `module.go`; extraction recorder already mapped `googleai` →
`ProviderGoogleAI`. Build / vet / tests green (PR #414).

## What

Make the official Google API embedding client (`pkg/embeddings/genai`) report
token usage, mirroring what PR #408 did for the OpenAI-compatible client, so
embeddings run through the `googleai` provider appear in `llm_usage_events`.

## Why

After #408 only vertex and openai embedding clients are usage-capable
(`usageReportingClient` in `pkg/embeddings/module.go`). The `genai` client
(`apps/server/pkg/embeddings/genai/client.go`) still returns bare embeddings
with no usage, so official-Google embedding spend is silently untracked. Its
`EmbedContent` response carries usage metadata, so this is plumbing, not guesswork.

## Depends on
- Memory PR #408 (interface-based usage dispatch + recorder mapping) — merged.

## Notes
- genai client embeds one text per `EmbedContent` call in a loop; sum the
  per-call prompt-token counts.
- Extraction recorder (`domain/extraction/usage.go`) already maps provider
  `"googleai"` → `ProviderGoogleAI`, so only the client + dispatch need work.
- Lower priority than the LiteLLM path (which #408 fixed) if Google official
  API embeddings are not a primary spend surface.
