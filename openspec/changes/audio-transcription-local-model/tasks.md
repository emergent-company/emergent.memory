## 1. Configuration

- [x] 1.1 Add `WhisperConfig` struct to `apps/server-go/internal/config/config.go` with fields: `Enabled bool`, `ServiceURL string`, `TimeoutMs int`, `Model string`, `Language string`, `MaxFileSizeMB int` — with `env` tags and defaults (`WHISPER_ENABLED=false`, `WHISPER_SERVICE_URL=http://localhost:9000`, `WHISPER_SERVICE_TIMEOUT=600000`, `WHISPER_MODEL=base`, `WHISPER_LANGUAGE=""`, `WHISPER_MAX_FILE_SIZE_MB=500`)
- [x] 1.2 Add `Timeout() time.Duration` helper method on `WhisperConfig` (mirrors `KreuzbergConfig.Timeout()`)
- [x] 1.3 Register `WhisperConfig` as a field on the root `Config` struct (e.g., `Whisper WhisperConfig`)

## 2. Whisper Client Package

- [x] 2.1 Create `apps/server-go/pkg/whisper/client.go` — define `Client` struct with fields `httpClient`, `baseURL`, `timeout`, `enabled`, `language`, `log`
- [x] 2.2 Implement `NewClient(cfg *config.Config, log *slog.Logger) *Client` constructor (mirrors `kreuzberg.NewClient`)
- [x] 2.3 Implement `IsEnabled() bool` method on `Client`
- [x] 2.4 Implement `Transcribe(ctx context.Context, data []byte, filename, mimeType string) (string, error)` — builds multipart form request with field `audio_file`, sends `POST /asr?output=txt&task=transcribe`, appends `language=<value>` query param when `Language` is non-empty, reads plain-text response body as the transcript
- [x] 2.5 Handle non-200 HTTP responses in `Transcribe` — return wrapped error including status code and response body excerpt
- [x] 2.6 Create `apps/server-go/pkg/whisper/module.go` — define `var Module = fx.Module("whisper", fx.Provide(NewClient))`

## 3. fx Wiring

- [x] 3.1 Add `whisper.Module` to the fx app in `apps/server-go/cmd/server/main.go` (alongside `kreuzberg.Module`)
- [x] 3.2 Add `*whisper.Client` as a field on `DocumentParsingWorker` struct in `apps/server-go/domain/extraction/document_parsing_worker.go`
- [x] 3.3 Inject `*whisper.Client` into `DocumentParsingWorker` via its constructor in `apps/server-go/domain/extraction/module.go`

## 4. Audio Routing in Document Parsing Worker

- [x] 4.1 Add `isAudioFile(mimeType, filename string) bool` helper function — returns `true` if `mimeType` has prefix `audio/` OR if the file extension (lowercased) is one of: `.mp3`, `.wav`, `.m4a`, `.ogg`, `.flac`, `.aac`, `.opus`, `.webm`
- [x] 4.2 Add `isAudio` branch in `processJob()` before the `useKreuzberg` check — if `isAudioFile(mimeType, filename)` is true, route to `extractWithWhisper()`
- [x] 4.3 Implement `extractWithWhisper(ctx context.Context, storageKey, filename, mimeType string) (string, error)` method — downloads file from storage, checks file size against `WHISPER_MAX_FILE_SIZE_MB`, calls `w.whisperClient.Transcribe()`, returns transcript string
- [x] 4.4 Set `extractionMethod = "whisper"` in the metadata map when audio routing is used
- [x] 4.5 Handle `WHISPER_ENABLED=false` in `extractWithWhisper` — return error `"whisper transcription service is disabled"` immediately without downloading the file

## 5. Docker Compose Sidecar

- [x] 5.1 Add `whisper-server` service to the Docker Compose template in `tools/emergent-cli/internal/installer/templates.go` using image `onerahmet/openai-whisper-asr-webservice:latest`, port mapping `9000:9000`, env vars `ASR_MODEL=${WHISPER_MODEL:-base}` and `ASR_ENGINE=faster_whisper`
- [x] 5.2 Add `whisper-server` to the `depends_on` list of the main server service in the Docker Compose template (conditional on `WHISPER_ENABLED`)
- [x] 5.3 Add Whisper env var placeholders (`WHISPER_ENABLED`, `WHISPER_SERVICE_URL`, `WHISPER_MODEL`, `WHISPER_LANGUAGE`, `WHISPER_SERVICE_TIMEOUT`, `WHISPER_MAX_FILE_SIZE_MB`) to the `.env.example` template with comments explaining each

## 6. Build Verification

- [x] 6.1 Run `go run ./cmd/tasks build` (or `go build ./...`) in `apps/server-go` and confirm zero compilation errors
- [x] 6.2 Run `nx run server-go:test` and confirm all existing unit tests still pass

## 7. Manual Integration Test

- [x] 7.1 Start the Docker Compose stack with `WHISPER_ENABLED=true` and confirm the `whisper-server` container starts and loads the model within 60 seconds
- [x] 7.2 Upload an MP3 or WAV audio file via the document upload API and confirm the parsing job completes with `extractionMethod: "whisper"` in the job metadata
- [x] 7.3 Confirm the transcript appears in `kb.documents.content` for the uploaded document (query via Postgres MCP or API)
- [x] 7.4 Confirm chunks are created in `kb.chunks` for the transcript (downstream pipeline ran)
- [x] 7.5 Upload an audio file with `WHISPER_ENABLED=false` and confirm the job is marked `failed` with message "whisper transcription service is disabled"
