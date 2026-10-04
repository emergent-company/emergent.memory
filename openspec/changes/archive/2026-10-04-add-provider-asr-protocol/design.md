# Design — add-provider-asr-protocol

## Context

The registry already expresses chat protocol (`Protocol`), auth
(`AuthStyle`), and — from #1477 — rerank (`RerankAPI`) as independent
dimensions on `ProviderDefinition`. Transcription is the next such dimension.
The server already transcribes audio in exactly one place: the document parsing
worker's audio branch (`domain/extraction/document_parsing_worker.go`,
`extractWithWhisper`), backed by the self-hosted Whisper HTTP client
(`pkg/whisper`). The task is to add a vendor alternative without disturbing it.

## Decisions

### D1 — Transcription is an orthogonal registry dimension

Add `TranscriptionAPI` (values: `""`, `openai-transcriptions`) alongside
`RerankAPI`, plus `Registry.TranscriptionAPIFor`. Do **not** overload
`Protocol`: an OpenAI-compatible chat vendor may or may not expose audio, and a
relay may expose audio under a different base URL. `ModelTypeTranscription`
joins `ModelTypeGenerative`/`Embedding`/`Rerank`.

### D2 — Only verified shapes are declared

The one verified vendor shape is the OpenAI-compatible multipart
`POST /v1/audio/transcriptions` (fields `file`, `model`, optional `language`,
`response_format`, `prompt`; responses `json`/`text`/`verbose_json`; bearer
auth). OpenAI and the generic OpenAI-compatible vendor declare it. A generic
relay (e.g. Groq) is supported by pointing `ASR_BASE_URL` at its
`/openai/v1` endpoint — no separate vendor entry is fabricated. Non-OpenAI
shapes (Deepgram query-param `/v1/listen`, etc.) are left unsupported rather
than guessed.

### D3 — Adapter package mirrors `pkg/rerank`

`pkg/asr` exposes `Transcriber`, `Config`, `New`, and the OpenAI adapter.
`domain/provider/transcription.go` maps `TranscriptionAPI` → `asr.API` and
builds the adapter with `cred.APIKey` / `cred.BaseURL` /
`DefaultBaseURLs[ModelTypeTranscription]` / `cred.ExtraHeaders`.

### D4 — Selection is an env-gated opt-in; default unchanged

The document parsing worker gains an optional `*vendorASR`. `vendorTranscriberFromEnv`
returns `nil` unless `ASR_PROVIDER` names a registered, transcription-capable
vendor; on any configuration error it logs and returns `nil`. The audio branch
uses the vendor adapter when present, otherwise the existing Whisper client —
so the default path is byte-for-byte unchanged. This mirrors the
`SEARCH_RERANK_*` opt-in rather than introducing per-project DB config, so **no
migration** is needed.

### D5 — Reattach to the existing audio branch (wiring point)

`processJob`'s `isAudio` branch is the single editable wiring point. The vendor
path reuses the same per-project `whisper_initial_prompt` as the decoder prompt
and records `extractionMethod = "asr:<vendor>"` (vs `"whisper"`) so the active
path is observable in job metadata.

## Risks / trade-offs

- **Global opt-in vs per-project config.** Env selection is coarse, but it
  matches the shipped rerank precedent and keeps this change migration-free. A
  follow-up can resolve a transcription provider per project (as
  `add-retrieval-reranking` plans to for rerank).
- **Generic vendor base URL.** `ProviderGeneric` declares an empty transcription
  default base URL; `ASR_BASE_URL` is required for it. This is intentional (no
  fabricated endpoint) and covered by the selection test.
