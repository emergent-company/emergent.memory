# Document Upload API

## Overview

This document specifies the REST API endpoint for the Emergent CLI to upload documents to projects. This endpoint is designed for file uploads from the local filesystem, separate from the MCP server which handles document management operations.

## Endpoint

```
POST /api/v2/documents/upload
```

## Authentication

Requires valid API token in `X-API-Key` header and project scope in `X-Project-ID` header.

## Request

### Headers

| Header         | Required | Description              |
| -------------- | -------- | ------------------------ |
| `X-API-Key`    | Yes      | API authentication token |
| `X-Project-ID` | Yes      | Target project UUID      |
| `Content-Type` | Yes      | `multipart/form-data`    |

### Form Data

| Field         | Type    | Required | Description                                       |
| ------------- | ------- | -------- | ------------------------------------------------- |
| `file`        | File    | Yes      | The document file to upload                       |
| `extract`     | Boolean | No       | Trigger extraction immediately (default: `false`) |
| `source_type` | String  | No       | Source type identifier (default: `upload`)        |
| `metadata`    | JSON    | No       | Additional metadata as JSON string                |

### Example Request (cURL)

```bash
curl -X POST http://localhost:3002/api/v2/documents/upload \
  -H "X-API-Key: your-api-key" \
  -H "X-Project-ID: project-uuid-here" \
  -F "file=@/path/to/document.pdf" \
  -F "extract=true" \
  -F "metadata={\"tags\":[\"research\",\"2024\"]}"
```

### Example Request (Go CLI)

```go
file, err := os.Open(filePath)
if err != nil {
    return err
}
defer file.Close()

body := &bytes.Buffer{}
writer := multipart.NewWriter(body)

// Add file
part, err := writer.CreateFormFile("file", filepath.Base(filePath))
if err != nil {
    return err
}
io.Copy(part, file)

// Add extract flag
writer.WriteField("extract", "true")

// Add metadata
writer.WriteField("metadata", `{"source":"cli"}`)

writer.Close()

req, err := http.NewRequest("POST", baseURL+"/api/v2/documents/upload", body)
req.Header.Set("Content-Type", writer.FormDataContentType())
req.Header.Set("X-API-Key", apiKey)
req.Header.Set("X-Project-ID", projectID)

resp, err := client.Do(req)
```

## Response

### Success Response (201 Created)

```json
{
  "document": {
    "id": "doc-uuid-1234",
    "project_id": "project-uuid",
    "filename": "document.pdf",
    "mime_type": "application/pdf",
    "file_size_bytes": 2457600,
    "storage_key": "projects/proj-uuid/docs/doc-uuid-1234.pdf",
    "storage_url": "https://storage.../doc-uuid-1234.pdf",
    "source_type": "upload",
    "created_at": "2026-02-10T20:00:00Z",
    "updated_at": "2026-02-10T20:00:00Z"
  },
  "extraction_job_id": "job-uuid-5678",
  "message": "Document uploaded successfully"
}
```

### Success Response (200 OK - Duplicate)

If a document with the same content hash already exists (deduplication):

```json
{
  "document": {
    "id": "existing-doc-uuid",
    "project_id": "project-uuid",
    "filename": "document.pdf",
    "content_hash": "sha256:abc123...",
    "created_at": "2026-02-09T15:30:00Z",
    "updated_at": "2026-02-09T15:30:00Z"
  },
  "was_created": false,
  "message": "Document already exists (deduplicated by content hash)"
}
```

### Error Responses

#### 400 Bad Request

```json
{
  "error": {
    "code": "bad_request",
    "message": "No file provided in request"
  }
}
```

**Common causes:**

- Missing `file` field
- File too large (> 100MB)
- Invalid metadata JSON
- Unsupported file type

#### 401 Unauthorized

```json
{
  "error": {
    "code": "unauthorized",
    "message": "Invalid or missing API key"
  }
}
```

#### 403 Forbidden

```json
{
  "error": {
    "code": "forbidden",
    "message": "Insufficient permissions for project"
  }
}
```

#### 413 Payload Too Large

```json
{
  "error": {
    "code": "payload_too_large",
    "message": "File size exceeds 100MB limit"
  }
}
```

#### 500 Internal Server Error

```json
{
  "error": {
    "code": "internal_error",
    "message": "Failed to upload document",
    "details": "Storage service unavailable"
  }
}
```

## Implementation Details

### Backend Flow

1. **Receive Upload**

   - Parse multipart form data
   - Validate file type and size
   - Extract metadata

2. **Storage**

   - Upload file to MinIO/S3
   - Generate storage key: `projects/{project_id}/docs/{doc_id}.{ext}`
   - Calculate file hash (SHA-256)

3. **Database**

   - Check for existing document with same content hash (deduplication)
   - If exists, return existing document (200 OK)
   - If new, create document record (201 Created)

4. **Optional Extraction**
   - If `extract=true`, create `DocumentParsingJob`
   - Return job ID in response
   - Job processed asynchronously by worker

### Content Deduplication

Documents are deduplicated based on content hash (SHA-256):

- Same content → returns existing document
- Different content → creates new document
- Prevents storage waste
- Preserves referential integrity

### File Size Limits

| Limit         | Value                    | Configurable                    |
| ------------- | ------------------------ | ------------------------------- |
| Max file size | 100MB                    | Yes (env: `MAX_UPLOAD_SIZE_MB`) |
| Allowed types | PDF, DOCX, TXT, MD, HTML | Yes (env: `ALLOWED_MIME_TYPES`) |

### Storage Structure

```
minio-bucket/
└── projects/
    └── {project-uuid}/
        └── docs/
            ├── {doc-uuid-1}.pdf
            ├── {doc-uuid-2}.docx
            └── {doc-uuid-3}.txt
```

## CLI Integration

### Emergent CLI Commands

```bash
# Single file upload
emergent upload document.pdf --project <project-id>

# With extraction
emergent upload document.pdf --project <project-id> --extract

# Multiple files
emergent upload *.pdf --project <project-id>

# Recursive directory upload
emergent upload ./docs --project <project-id> --recursive

# With metadata
emergent upload file.pdf --project <id> --metadata '{"tags":["research"]}'

# Output format
# ✓ Uploaded document.pdf (2.3 MB) → doc-uuid-1234
# ✓ Triggered extraction job → job-uuid-5678
# ✓ View status: emergent status job-uuid-5678
```

### CLI Error Handling

| Error          | Exit Code | Message                               |
| -------------- | --------- | ------------------------------------- |
| File not found | 1         | `Error: File not found: document.pdf` |
| Auth failed    | 2         | `Error: Invalid API key`              |
| Network error  | 3         | `Error: Failed to connect to server`  |
| Upload failed  | 4         | `Error: Upload failed: {reason}`      |
| File too large | 5         | `Error: File exceeds 100MB limit`     |

### Progress Display

For large files (>10MB), CLI should show progress:

```
Uploading document.pdf...
[████████████████████████████████] 100% (2.3 MB / 2.3 MB)
✓ Upload complete
```

## Testing

### Unit Tests

```bash
# Test upload endpoint
cd apps/server-go
go test ./domain/documents -run TestUploadDocument -v
```

### Integration Tests

```bash
# E2E upload test
go test ./tests/e2e -run TestDocumentUpload -v
```

### Manual Testing

```bash
# Test successful upload
curl -X POST http://localhost:3002/api/v2/documents/upload \
  -H "X-API-Key: test-key" \
  -H "X-Project-ID: test-project-uuid" \
  -F "file=@test.pdf"

# Test with extraction
curl -X POST http://localhost:3002/api/v2/documents/upload \
  -H "X-API-Key: test-key" \
  -H "X-Project-ID: test-project-uuid" \
  -F "file=@test.pdf" \
  -F "extract=true"

# Test deduplication (upload same file twice)
curl -X POST http://localhost:3002/api/v2/documents/upload \
  -H "X-API-Key: test-key" \
  -H "X-Project-ID: test-project-uuid" \
  -F "file=@test.pdf"
```

## Security Considerations

1. **File Validation**

   - Verify MIME type matches file extension
   - Scan for malicious content (future: virus scanning)
   - Reject executable files

2. **Size Limits**

   - Enforce 100MB max size
   - Prevent DoS attacks via large uploads

3. **Access Control**

   - Verify API key has write access to project
   - Enforce project-level permissions

4. **Storage Security**
   - Use signed URLs for temporary access
   - Encrypt files at rest (MinIO/S3 encryption)

## Future Enhancements

### Planned Features

1. **Chunked Upload**

   - For files > 100MB
   - Resume capability

2. **Batch Upload API**

   - Upload multiple files in one request
   - Atomic success/failure

3. **Pre-signed Upload URLs**

   - Direct browser → S3 uploads
   - Bypass server for large files

4. **Webhook Notifications**
   - Notify on upload completion
   - Notify on extraction completion

### Example: Chunked Upload (Future)

```bash
# Request upload session
POST /api/v2/documents/upload/init
Response: {"upload_id": "session-uuid", "chunk_size": 5242880}

# Upload chunks
PUT /api/v2/documents/upload/chunk
Headers: X-Upload-ID, X-Chunk-Number, X-Total-Chunks

# Finalize upload
POST /api/v2/documents/upload/complete
Body: {"upload_id": "session-uuid", "extract": true}
```

## Related Documentation

- **MCP Document Tools**: See `/apps/server-go/domain/mcp/README.md` (planned)
- **Document Schema**: See `/apps/server-go/domain/documents/entity.go`
- **Extraction Pipeline**: See `/apps/server-go/domain/extraction/README.md`

## Changelog

| Date       | Version | Changes                   |
| ---------- | ------- | ------------------------- |
| 2026-02-10 | 1.0.0   | Initial API specification |
