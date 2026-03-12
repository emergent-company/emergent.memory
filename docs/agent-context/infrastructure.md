# Infrastructure Reference

Concise facts about the local dev infrastructure for the emergent server-go app.
Read this before working with storage, uploads, transcription, or E2E tests.

---

## MinIO / Object Storage

- **MinIO container:** `docker-minio-1`
- **Correct endpoint:** `http://[::1]:9000` (IPv6 only — Docker binds to `[::1]`, not `127.0.0.1`)
- **WRONG:** `http://localhost:9000` or `http://127.0.0.1:9000` — these fail with `context canceled` after ~2 min
- **Env var:** `STORAGE_ENDPOINT=http://[::1]:9000`
- **AWS SDK v2:** must set `DisableHTTPS: true` in `s3.Options` when the endpoint is `http://`

## Upload Endpoints

Two upload endpoints exist — use the correct one:

| Endpoint | What it does |
|----------|-------------|
| `POST /api/document-parsing-jobs/upload` | Stores file AND creates a parsing job → use for files needing parsing/transcription |
| `POST /api/documents/upload` | Stores file only, no parsing job created → `conversionStatus` stays `not_required` |

Always include these headers:
```
Authorization: Bearer <token>
X-Project-Id: <project-id>
Content-Type: multipart/form-data
```

Example curl:
```bash
curl -X POST http://localhost:3012/api/document-parsing-jobs/upload \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-Id: $PROJECT_ID" \
  -F "file=@/path/to/audio.mp3;type=audio/mpeg"
```

## Whisper Transcription

- **Container:** `emergent-whisper` (image: `onerahmet/openai-whisper-asr-webservice`)
- **Port:** `9876`
- **Default:** disabled — `WHISPER_ENABLED` defaults to `false` in `.env`
- **Timeout:** set `WHISPER_SERVICE_TIMEOUT=7200000` (2 hours) for full-length audio files
- **Testing:** use a clip shorter than 30s (~500KB) to avoid multi-minute CPU waits

To enable locally, add to `.env`:
```
WHISPER_ENABLED=true
WHISPER_ENDPOINT=http://localhost:9876
WHISPER_SERVICE_TIMEOUT=7200000
```

## Test Token Limitations

- `e2e-test-user` token has **no OrgID** — any table with `organization_id NOT NULL` will fail
- Tables that must allow NULL `organization_id`: `document_parsing_jobs`, and any new tables in org-scoped schemas

## PostgreSQL (local dev)

- **Port:** `5436` (not the default 5432)
- **DSN:** `host=127.0.0.1 port=5436 user=emergent password=local-test-password dbname=emergent sslmode=disable`
- **psql shorthand:** `PGPASSWORD=local-test-password psql -h 127.0.0.1 -p 5436 -U emergent -d emergent`
- The `postgres` MCP tool is **READ-ONLY** — use psql directly for schema changes

## Local Server

- **Port:** `3012`
- **Start:** `air` (with hot-reload) or `go run ./cmd/main.go`
- **Remote staging:** set `TEST_SERVER_URL` env var to your remote server URL
- CLI default points to remote staging — override with `--server http://localhost:3012`

## MCP Server

- The emergent MCP server exposes **29 tools**
- When using the Task tool to explore MCP tool coverage, scope subagent prompts to specific tool categories — requesting all 29 tools at once causes truncation
