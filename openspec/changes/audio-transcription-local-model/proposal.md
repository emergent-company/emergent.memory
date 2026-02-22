## Why

The document extraction pipeline (Kreuzberg) handles text, images, and binary documents but has zero support for audio files. Users who upload meeting recordings, voice memos, podcasts, or interview audio cannot extract any content from them. Adding local audio transcription via a self-hosted Whisper-compatible HTTP service mirrors the same sidecar pattern already established by Kreuzberg — keeping data on-premises, avoiding per-request cloud API costs, and making audio a first-class document type in the knowledge base.

## What Changes

- **New audio MIME type routing** in `DocumentParsingWorker`: audio files (mp3, wav, m4a, ogg, flac, aac, opus, webm audio) are routed to the new transcription client instead of Kreuzberg.
- **New `whisper` client package** (`pkg/whisper/client.go`): HTTP multipart client that calls a self-hosted Whisper-compatible REST API (e.g., whisper.cpp server, `faster-whisper` server, or any OpenAI-compatible `/v1/audio/transcriptions` endpoint).
- **New Docker Compose sidecar**: a `whisper-server` container (image: `onerahmet/openai-whisper-asr-webservice` or `linuxserver/faster-whisper`) added to the dev/prod compose stack alongside the existing Kreuzberg container.
- **New config block** (`WhisperConfig`): env vars `WHISPER_ENABLED`, `WHISPER_SERVICE_URL`, `WHISPER_SERVICE_TIMEOUT`, `WHISPER_MODEL` (e.g., `base`, `medium`, `large-v3`).
- **New fx module** (`whisper.Module`) wired into `cmd/server/main.go`, mirroring `kreuzberg.Module`.
- **Extracted text feeds existing pipeline**: transcription result is stored as `kb.documents.content`, then chunked, embedded, and graph-extracted exactly like any other document — no changes needed downstream.
- **Recommendations section** (see Impact): two implementation options are compared; the proposal recommends Option A.

## Capabilities

### New Capabilities

- `audio-transcription-service`: Local, self-hosted audio transcription sidecar service. Accepts audio file uploads, returns plaintext transcript via HTTP API. Covers service availability, model selection, language configuration, supported formats, and timeout/retry behaviour.

### Modified Capabilities

- `document-extraction-service`: Extend routing logic to recognise `audio/*` MIME types and delegate them to the transcription client rather than Kreuzberg. No requirement changes to existing Kreuzberg behaviour; only the routing branching condition is extended.

## Impact

### Affected Code

| File                                                          | Change                                                         |
| ------------------------------------------------------------- | -------------------------------------------------------------- |
| `apps/server-go/pkg/whisper/client.go`                        | New — Whisper HTTP client (mirrors `pkg/kreuzberg/client.go`)  |
| `apps/server-go/pkg/whisper/module.go`                        | New — fx provider                                              |
| `apps/server-go/domain/extraction/document_parsing_worker.go` | Extend routing: add `isAudio()` branch before `useKreuzberg()` |
| `apps/server-go/domain/extraction/module.go`                  | Inject `whisper.Client` into `DocumentParsingWorker`           |
| `apps/server-go/internal/config/config.go`                    | Add `WhisperConfig` struct + env var binding                   |
| `apps/server-go/cmd/server/main.go`                           | Wire `whisper.Module`                                          |
| `tools/emergent-cli/internal/installer/templates.go`          | Add `whisper-server` Docker Compose service                    |

### Implementation Options (Recommendations)

**Option A — Sidecar HTTP service (recommended)**

Run a containerised Whisper server (e.g., `onerahmet/openai-whisper-asr-webservice` or `linuxserver/faster-whisper`) as a Docker Compose sidecar. The Go server communicates via HTTP multipart POST, identical in architecture to Kreuzberg. Models are loaded once at startup; GPU acceleration is available but not required (CPU `base`/`small` models are fast enough for background job workloads).

_Pros_: Zero CGo, no GPU requirement for dev, clear service boundary, model upgradeable via env var, OpenAI API-compatible (future cloud fallback is trivial), matches existing patterns exactly.

_Cons_: Additional Docker image (~2–4 GB for medium model); sidecar must be running for audio to process.

**Option B — Direct Go bindings (whisper.cpp/go)**

Embed `ggerganov/whisper.cpp/bindings/go` directly in the Go binary. Requires CGo, a C++ compiler in CI, and linking `libwhisper.a`.

_Pros_: No extra Docker service.

_Cons_: CGo complicates the build pipeline; model files must be bundled or fetched at startup; the existing Kreuzberg sidecar pattern is already proven and preferred.

**Recommendation**: Option A. It is lower risk, matches the Kreuzberg precedent, keeps the Go service pure Go (no CGo), and the Docker image overhead is acceptable given the existing Kreuzberg image already adds a sidecar.

### Dependencies

- New Docker image for Whisper server (no existing Go dep changes)
- No database schema changes — audio transcripts stored in `kb.documents.content` like any other document
- No frontend changes required — existing document upload UI accepts any file type; MIME detection is server-side

### Supported Audio Formats (initial scope)

`audio/mpeg` (mp3), `audio/wav`, `audio/x-wav`, `audio/mp4` (m4a), `audio/ogg`, `audio/flac`, `audio/aac`, `audio/opus`, `audio/webm`, `video/webm` (audio track only)
