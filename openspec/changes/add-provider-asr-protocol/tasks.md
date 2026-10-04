## 1. Registry transcription dimension

- [x] 1.1 Add `TranscriptionAPI` type + `TranscriptionAPIOpenAITranscriptions` constant (`domain/provider/protocol.go`).
- [x] 1.2 Add `ProviderDefinition.TranscriptionAPI` and `Registry.TranscriptionAPIFor` (`registry.go`); add `ModelTypeTranscription` (`entity.go`).
- [x] 1.3 Declare transcription support on the built-ins: OpenAI and the generic OpenAI-compatible vendor, with `DefaultBaseURLs[ModelTypeTranscription]` (`builtin.go`).
- [x] 1.4 Expose `transcriptionApi` on the API-facing `ProviderDefinitionResponse` (`handler.go`).
- [x] 1.5 (TDD) Unit tests: per-vendor `TranscriptionAPIFor`, transcription model type + default base URL, `transcriptionApi` JSON key/omit-empty.

## 2. Transcription adapters (`pkg/asr`)

- [x] 2.1 `Transcriber` interface + `Options`/`Result`/`Config` + `New` dispatcher (`pkg/asr/asr.go`).
- [x] 2.2 OpenAI-compatible multipart `POST {base}/audio/transcriptions` adapter with `json`/`text`/`srt`/`vtt`/`verbose_json` response handling (`pkg/asr/openai.go`).
- [x] 2.3 (TDD) `httptest` unit tests: request shape (multipart field names + presence/absence, bearer + custom header, path), response parsing, non-2xx error, empty-audio short-circuit, endpoint suffix dedup, default model.

## 3. Registry-keyed selection

- [x] 3.1 `provider.NewTranscriber(def, cred, model)` mapping `TranscriptionAPI` → adapter, reusing `APIKey`/`BaseURL`/`ExtraHeaders`/default base URL; `Registry.NewTranscriberFor`.
- [x] 3.2 (TDD) Selection tests: supported vendor builds, unsupported vendor/nil def/nil credential error, default base URL fallback, credential `BaseURL`/`ExtraHeaders` plumbing.

## 4. Voice pipeline wiring (default unchanged)

- [x] 4.1 Add opt-in `ASR_*` selection (`domain/extraction/asr.go`) mirroring `SEARCH_RERANK_*`.
- [x] 4.2 Add optional `*vendorASR` to `DocumentParsingWorker`; use it only when configured, else the existing Whisper client (`document_parsing_worker.go`).
- [x] 4.3 Inject `*provider.Registry` and wire `vendorTranscriberFromEnv` in the extraction fx module (`module.go`).
- [x] 4.4 (TDD) Tests: env unset / unknown vendor / non-transcription vendor all yield nil (Whisper default); openai + generic select the adapter; default worker has nil ASR and the vendor entry point refuses to run.

## 5. Verify

- [x] 5.1 `go build ./...` (server compile).
- [x] 5.2 `go test -count=1 ./pkg/asr/... ./domain/provider/... ./domain/extraction/... ./pkg/whisper/...`.
- [x] 5.3 `gofmt` on touched files; `apps/server/scripts/lint-ratchet.sh`.
- [x] 5.4 Mutation-check the multipart request shape and the vendor selection mapping (break, observe failure, restore).
- [x] 5.5 Confirm no migration is added (env-opt-in only).
