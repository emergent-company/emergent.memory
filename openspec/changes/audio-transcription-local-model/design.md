## Context

The document parsing pipeline is a PostgreSQL-backed job queue (`kb.document_parsing_jobs`) consumed by `DocumentParsingWorker`. When a file is uploaded, the worker examines its MIME type and routes it to one of three handlers: email parser (stub), Kreuzberg (binary docs), or plain-text reader. The result — always a `string` — is written to `kb.documents.content` and fed downstream to chunking, embedding, and graph extraction workers unchanged.

Kreuzberg is deployed as a Docker Compose sidecar (`goldziher/kreuzberg:latest`, port 8000). The Go server communicates with it via an HTTP multipart POST client in `pkg/kreuzberg/client.go`. Config lives in `KreuzbergConfig` (struct with `env` tags, parsed via `caarlos0/env`). The fx module (`kreuzberg.Module`) provides the client via `fx.Provide(NewClient)` and is wired in `cmd/server/main.go`.

Audio files have zero current support. No audio MIME types appear in `kreuzberg.KreuzbergSupportedMIMETypes`, and there is no transcription service of any kind in the codebase. The product roadmap already identifies multi-modal (audio) support as a strategic goal.

## Goals / Non-Goals

**Goals:**

- Accept audio file uploads through the existing document upload flow without any frontend changes.
- Transcribe audio to plaintext via a self-hosted, locally-running Whisper-compatible HTTP service.
- Feed transcripts into the existing chunking → embedding → graph pipeline with zero changes to downstream workers.
- Match the Kreuzberg sidecar pattern exactly: Docker Compose service + `pkg/whisper/` client + config struct + fx module.
- Keep the Go binary free of CGo / C++ build dependencies.
- Allow per-deployment model selection (base for CPU-only dev, medium/large-v3 for GPU-capable prod) via an env var.
- Support graceful degradation: if Whisper is disabled or unavailable, audio jobs fail clearly with a logged error rather than silently.

**Non-Goals:**

- Real-time / streaming transcription (all transcription is async via the job queue).
- Speaker diarisation or timestamp metadata (plain transcript text only, matching how Kreuzberg returns plain text).
- Video file transcription (video formats are excluded from initial scope to keep surface area small; audio track extraction from video is a future enhancement).
- Cloud-based speech-to-text fallback (OpenAI Whisper API, Google Speech-to-Text) — this is architecturally possible given the chosen API shape, but not in scope.
- Frontend changes: the upload UI already accepts any MIME type; MIME detection is entirely server-side.
- Database schema changes: transcripts stored in existing `kb.documents.content` field.

## Decisions

### Decision 1: Sidecar HTTP service over embedded Go bindings

**Chosen**: Self-hosted HTTP sidecar (Docker Compose service exposing a REST API), called from a new `pkg/whisper/` HTTP client.

**Rejected**: Direct CGo bindings to `whisper.cpp` (`ggerganov/whisper.cpp/bindings/go`).

**Rationale**: The Kreuzberg sidecar pattern is already proven. CGo introduces a C++ toolchain dependency into every build environment (CI, dev containers, prod image), complicates cross-compilation, and makes the binary non-portable. The sidecar approach keeps the Go binary pure Go, isolates model memory from the server process, and allows model upgrades without recompiling the server. The Docker image size overhead (~1–4 GB depending on model) is acceptable given the existing Kreuzberg sidecar already demonstrates this trade-off.

### Decision 2: Docker image — `onerahmet/openai-whisper-asr-webservice`

**Chosen**: `onerahmet/openai-whisper-asr-webservice` (Python/faster-whisper, OpenAI-compatible API at `/asr`).

**Alternatives considered**:

- `linuxserver/faster-whisper` — less actively maintained, non-standard API shape.
- `ggerganov/whisper.cpp` native server binary — requires manual model download and server compilation; no public maintained image with a stable API.
- Custom Python FastAPI image — unnecessary maintenance burden.

**Rationale**: `onerahmet/openai-whisper-asr-webservice` has an active community, supports CPU and GPU via env var (`ASR_ENGINE=faster_whisper` for speed, `ASR_ENGINE=openai_whisper` for accuracy), and exposes a stable multipart POST endpoint. Its API is not identical to the OpenAI `/v1/audio/transcriptions` shape but is simpler and sufficient for the use case.

### Decision 3: API endpoint and request shape

The `onerahmet` image exposes `POST /asr?output=txt` accepting `multipart/form-data` with field `audio_file`. Response body is plain text (the transcript) when `output=txt`. This is simpler than the OpenAI JSON envelope — no JSON parsing required, the body IS the transcript.

```
POST http://whisper-server:9000/asr?output=txt&task=transcribe&language=en
Content-Type: multipart/form-data; boundary=...

--boundary
Content-Disposition: form-data; name="audio_file"; filename="recording.mp3"
Content-Type: audio/mpeg
<binary audio data>
--boundary--
```

Response: `200 OK`, `Content-Type: text/plain`, body = transcript string.

### Decision 4: Routing position in `DocumentParsingWorker`

The new `isAudio()` branch is inserted **before** the `useKreuzberg` check in `processJob()`:

```
isEmail   → email stub (unchanged)
isAudio   → whisper client (new)
useKreuzberg → kreuzberg client (unchanged)
else      → plain text reader (unchanged)
```

This keeps the routing logic linear and avoids touching the `kreuzberg.ShouldUseKreuzberg()` function.

### Decision 5: Config structure

`WhisperConfig` mirrors `KreuzbergConfig` exactly:

```go
type WhisperConfig struct {
    Enabled   bool   `env:"WHISPER_ENABLED"          envDefault:"false"`
    ServiceURL string `env:"WHISPER_SERVICE_URL"      envDefault:"http://localhost:9000"`
    TimeoutMs  int    `env:"WHISPER_SERVICE_TIMEOUT"  envDefault:"600000"` // 10 min for large files
    Model      string `env:"WHISPER_MODEL"            envDefault:"base"`
    Language   string `env:"WHISPER_LANGUAGE"         envDefault:""`       // empty = auto-detect
}
```

`WHISPER_ENABLED` defaults to `false` to avoid breaking existing deployments that don't run the sidecar.

### Decision 6: Whisper service port

Port `9000` for the Whisper sidecar (Kreuzberg uses `8000`). No conflict with existing services.

## Risks / Trade-offs

**[Risk] Large audio files cause very long job processing times** → The job queue already supports configurable timeouts and exponential backoff. `WHISPER_SERVICE_TIMEOUT` defaults to 10 minutes (vs. 5 minutes for Kreuzberg). Users should be informed that large audio files (>1 hour) may take several minutes to process on CPU-only deployments. The `base` model on CPU processes ~1 minute of audio per ~6–10 seconds real-time.

**[Risk] Whisper sidecar not running causes audio jobs to fail** → `WHISPER_ENABLED` defaults to `false`. When disabled, the worker logs a clear warning and marks the job as failed with message "whisper transcription service is disabled". This prevents silent data loss. Operators must explicitly opt in.

**[Risk] Model selection mismatch between dev and prod** → `WHISPER_MODEL` is an env var. Dev uses `base` (fast, low memory, ~140 MB). Prod can use `medium` or `large-v3` for better accuracy. The Docker Compose template should document this clearly and use `base` as the default.

**[Risk] Audio MIME type detection unreliability** → Browser-reported MIME types for audio can be inconsistent (e.g., `.m4a` uploaded as `audio/mp4` vs `audio/x-m4a`). Mitigation: the `isAudio()` helper checks both MIME type prefix (`strings.HasPrefix(mimeType, "audio/")`) AND file extension fallback for known extensions (`.mp3`, `.wav`, `.m4a`, `.ogg`, `.flac`, `.aac`, `.opus`, `.webm`). Whisper.cpp handles format detection internally from the audio stream.

**[Risk] Docker image download size in CI** → The `base` model image is ~1.5 GB; `medium` is ~3 GB. CI pipelines that pull all images for integration tests will be slower. Mitigation: the Whisper sidecar can be excluded from CI test runs (similar to how Kreuzberg may be mocked in unit tests).

**[Trade-off] No transcript metadata** → Unlike Kreuzberg which can return page count, title, author etc., the Whisper sidecar returns only raw text. The `extractionMethod` metadata field in `MarkCompletedResult` will be set to `"whisper"` to allow filtering in logs/observability, but no audio-specific metadata (duration, detected language) is captured in v1. This can be added in a future iteration by switching to `output=json`.

## Migration Plan

1. Add `WhisperConfig` to `config.go` and register in the `Config` struct.
2. Create `pkg/whisper/client.go` and `pkg/whisper/module.go`.
3. Add `whisper.Module` to `cmd/server/main.go`.
4. Inject `*whisper.Client` into `DocumentParsingWorker` via `domain/extraction/module.go`.
5. Add `isAudio()` helper and routing branch to `document_parsing_worker.go`.
6. Add `whisper-server` service to the Docker Compose template in `tools/emergent-cli/internal/installer/templates.go`.
7. Add `WHISPER_ENABLED`, `WHISPER_SERVICE_URL`, `WHISPER_MODEL` to `.env.example` / installer prompts.

**No database migrations required.** No rollback steps needed — the feature is gated by `WHISPER_ENABLED=false` by default. Disabling it returns the system to exactly its pre-change state. Existing audio uploads will remain as failed jobs and can be retried once the sidecar is enabled.

**Deployment order**: sidecar up first, then `WHISPER_ENABLED=true` set, then server restart (env var change requires restart per hot-reload rules).

## Open Questions

1. **Should the Whisper service URL be validated at startup?** Kreuzberg currently does not perform a startup health check — it only fails at job processing time. Should Whisper follow the same pattern, or should we add an optional startup connectivity check logged as a warning? _Recommendation: match Kreuzberg — fail at job time, not startup._

2. **Language auto-detection vs. per-document language hints?** The initial design uses a global `WHISPER_LANGUAGE` env var (empty = auto-detect). A future iteration could allow per-document language override via upload metadata. Not blocking v1.

3. **Should audio upload size limits differ from document limits?** Kreuzberg has `KREUZBERG_MAX_FILE_SIZE_MB` (default 100 MB). Audio files can easily exceed 100 MB for hour-long recordings. Should `WHISPER_MAX_FILE_SIZE_MB` default higher (e.g., 500 MB)? _Recommendation: add `WHISPER_MAX_FILE_SIZE_MB` defaulting to `500`._
