# Document Upload API

## Overview

REST API for uploading documents (single file, batch, or via the /remember/file endpoint).
All upload routes require auth + `X-Project-ID` header + `documents:write` scope.

---

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/documents/upload` | Single file upload (max 500 MB) |
| `POST` | `/api/documents/upload/batch` | Batch upload up to 100 files (max 10 MB each) |
| `POST` | `/api/projects/:id/remember/file` | Upload for the remember agent (no auto-extract) |

The legacy alias `/api/document-parsing-jobs/upload` also maps to the single-file endpoint.

---

## Single File Upload

### `POST /api/documents/upload`

#### Headers

| Header         | Required | Description              |
|----------------|----------|--------------------------|
| `X-API-Key`    | Yes      | API authentication token |
| `X-Project-ID` | Yes      | Target project UUID      |
| `Content-Type` | Yes      | `multipart/form-data`    |

#### Form Fields

| Field         | Type    | Required | Description                                                         |
|---------------|---------|----------|---------------------------------------------------------------------|
| `file`        | File    | Yes      | The document to upload (max 500 MB)                                 |
| `autoExtract` | Boolean | No       | Trigger object extraction after parsing completes (default: `false`)|

#### File Size Limit

Hard limit: **500 MB** per file. Enforced server-side via `http.MaxBytesReader`
(the client-supplied `Content-Length` is not trusted alone).

#### MIME Type Allowlist

By default all MIME types are accepted. To restrict, set the `ALLOWED_MIME_TYPES`
environment variable to a comma-separated list:

```
ALLOWED_MIME_TYPES=application/pdf,image/jpeg,image/png,text/plain,text/csv
```

If a file's detected MIME type is not in the list, the server returns **415**.
MIME is detected from file contents (first 512 bytes) — not from the client header.
Office Open XML formats (`.docx`, `.xlsx`, `.pptx`) are correctly identified even
though they appear as `application/zip` from magic bytes.

#### Example (cURL)

```bash
curl -X POST https://api.dev.emergent-company.ai/api/documents/upload \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT_ID" \
  -F "file=@/path/to/document.pdf" \
  -F "autoExtract=true"
```

#### Responses

**201 Created** — new document uploaded and parsing job queued:
```json
{
  "document": {
    "id": "doc-uuid",
    "name": "document.pdf",
    "mimeType": "application/pdf",
    "fileSizeBytes": 2457600,
    "conversionStatus": "pending",
    "storageKey": "proj-id/org-id/uuid-document.pdf",
    "createdAt": "2026-05-29T12:00:00Z"
  },
  "isDuplicate": false
}
```

**200 OK** — duplicate detected (same file hash already in project):
```json
{
  "document": { ... },
  "isDuplicate": true,
  "existingDocumentId": "existing-doc-uuid"
}
```

**400 Bad Request** — missing file or invalid request  
**413 Request Entity Too Large** — file exceeds 500 MB  
**415 Unsupported Media Type** — MIME type not in allowlist  
**503 Service Unavailable** — storage (MinIO/S3) not configured  

---

## Batch Upload

### `POST /api/documents/upload/batch`

Uploads multiple files in a single request. Always returns **200 OK** with per-file
results — individual file failures do not abort the batch.

#### Form Fields

| Field         | Type    | Required | Description                                    |
|---------------|---------|----------|------------------------------------------------|
| `files`       | File[]  | Yes      | Files to upload (field name must be `files`)   |
| `autoExtract` | Boolean | No       | Trigger extraction for all files (default: `false`) |

#### Limits

- Max **100 files** per request (400 if exceeded)
- Max **10 MB** per file (individual file reports as `"failed"` if exceeded)
- Concurrency: 3 files processed simultaneously

#### Example (cURL)

```bash
curl -X POST https://api.dev.emergent-company.ai/api/documents/upload/batch \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Project-ID: $PROJECT_ID" \
  -F "files=@doc1.pdf" \
  -F "files=@doc2.txt" \
  -F "autoExtract=false"
```

#### Response (200 OK)

```json
{
  "summary": {
    "total": 3,
    "successful": 2,
    "duplicates": 0,
    "failed": 1
  },
  "results": [
    { "filename": "doc1.pdf", "status": "success", "documentId": "uuid-1" },
    { "filename": "doc2.txt", "status": "success", "documentId": "uuid-2" },
    { "filename": "too-large.bin", "status": "failed", "error": "file size exceeds maximum of 10 MB for batch uploads" }
  ]
}
```

**Possible `status` values:** `"success"`, `"duplicate"`, `"failed"`

---

## Storage Key Format

Files are stored in MinIO/S3 under:
```
{projectId}/{orgId}/{uuid}-{sanitized_filename}
```

Example: `proj-abc/org-xyz/550e8400-e29b-41d4-a716-document.pdf`

`SanitizeFilename` rules:
- Non-ASCII and special characters → replaced with `_`
- Consecutive underscores → collapsed to one
- Leading/trailing underscores → trimmed from basename only
- Extension always preserved (unicode-only basenames → `unnamed.<ext>`)
- Max 200 characters total (base + extension)

---

## Parsing / Extraction Pipeline

After a successful upload (non-duplicate), a `DocumentParsingJob` is queued:

1. **Worker downloads** the file from S3 (max 500 MB)
2. **Routes by MIME type:**
   - Audio → Whisper transcription
   - Binary (PDF, Office) → Kreuzberg document parser
   - Plain text / Markdown / HTML / CSV / JSON → direct
3. **Chunks** extracted text and creates `kb.chunks` records
4. **Updates** `kb.documents.conversion_status`: `pending` → `completed` | `failed`
5. If `autoExtract=true`: triggers object extraction from parsed content

Poll `GET /api/documents/:id` and check `conversionStatus == "completed"` before
reading `content`.

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ALLOWED_MIME_TYPES` | `""` (unrestricted) | Comma-separated allowlist of MIME types |
| `SERVER_READ_TIMEOUT` | `3600s` | Request read timeout (1 hour for large uploads) |
| `MINIO_ENDPOINT` | `localhost:9000` | MinIO/S3 endpoint |
| `MINIO_ACCESS_KEY` | — | Storage access key |
| `MINIO_SECRET_KEY` | — | Storage secret key |
| `MINIO_BUCKET` | `emergent` | Bucket name |
