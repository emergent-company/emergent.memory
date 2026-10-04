## Why

`domain/provider` routes chat, embedding, and (since #1477) rerank through a
data-driven vendor registry: each `ProviderDefinition` declares the wire shape
for the dimension and dispatch reads that definition. Speech-to-text has no such
dimension. The server's only audio transcription path is the in-house Whisper
client (`pkg/whisper`, `internal/config.WhisperConfig`), hard-wired into the
document parsing worker; there is no way to route transcription through a
configured vendor even though OpenAI (and OpenAI-compatible relays such as
Groq) expose a stable `POST /v1/audio/transcriptions` multipart protocol.

## What Changes

- Add a `TranscriptionAPI` protocol dimension to `ProviderDefinition` and a
  `TranscriptionAPIFor` dispatch helper, mirroring the rerank `RerankAPI` axis.
- Add `ModelTypeTranscription` and declare transcription support on the vendors
  that expose the verified OpenAI-compatible shape: **OpenAI** and the generic
  OpenAI-compatible vendor (which covers any relay that speaks the same shape).
- Add `pkg/asr`: a provider-agnostic `Transcriber` interface plus an
  OpenAI-compatible adapter that issues `POST {base}/audio/transcriptions` as
  `multipart/form-data` (`file`, `model`, optional `language` /
  `response_format` / `prompt`) and parses `text` / `verbose_json` / plain-text
  responses.
- Add a provider-side factory (`NewTranscriber` / `Registry.NewTranscriberFor`)
  that selects the adapter from the registry definition and reuses the existing
  credential plumbing (`ResolvedCredential.APIKey`/`BaseURL`/`Extra`/
  `ExtraHeaders` from #1451/#1452).
- Wire the vendor ASR into the audio branch of the document parsing worker
  (`domain/extraction`) as an **alternative** path, gated by the opt-in
  `ASR_*` config. The default in-house Whisper path is byte-for-byte unchanged
  when unconfigured.

## Non-Goals

- Replacing or modifying the in-house Whisper client or `WHISPER_*` config.
- Vendors whose transcription shape is not verified as OpenAI-compatible
  (for example Deepgram, whose `/v1/listen` uses query params, not multipart)
  are left **unsupported** rather than guessed at.
- Per-project transcription configuration and model resolution — the selector
  is the global opt-in env, matching the `SEARCH_RERANK_*` precedent.
- Realtime/streaming transcription and translation (`/audio/translations`).

## Capabilities

### Modified Capabilities

- `multivendor-provider-registry`: the registry gains a transcription API
  dimension and per-vendor transcription declarations, and adapter selection
  reads the definition.

### Added Capabilities

- `document-extraction` (delta): the audio branch MAY use a registry-selected
  vendor transcription adapter, defaulting to the existing Whisper client.

## Related / Overlap

- **`add-provider-rerank-protocol` (#1477, merged)** established the orthogonal
  per-dimension registry axis this change mirrors. The mapping helper, adapter
  package shape, and env-gated wiring deliberately follow it.
- **`add-source-ingestion` (in flight)** consumes the document pipeline but does
  not modify transcription; the only overlap is the `domain/extraction` audio
  branch, which this change touches additively (Whisper default preserved). No
  migration collision: **no migration is required** — transcription selection
  reuses the existing provider credential plumbing plus opt-in env vars, and
  nothing new is persisted.
- **`add-document-multimodal-parsing`** owns document/audio parsing breadth; this
  change adds the vendor transcription axis it can later build on.
