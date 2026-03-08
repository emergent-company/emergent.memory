# Documents

Documents are files, URLs, or plain text that you upload into a project. The platform automatically chunks and indexes them so their content becomes searchable and extractable into the knowledge graph.

## Supported sources

| Source type | Description |
|---|---|
| File upload | PDF, Word, Markdown, plain text, and other formats |
| URL | Web pages fetched and converted to text |
| Text | Raw text content submitted directly via API |
| Data source sync | Documents imported automatically from connected integrations (see [Data Sources](datasources.md)) |

---

## Uploading a Document

=== "CLI"
    ```bash
    memory documents upload ./report.pdf --project my-project
    memory documents upload https://example.com/spec.html --project my-project
    ```

=== "API (file)"
    ```http
    POST /api/documents/upload
    Content-Type: multipart/form-data

    file=@report.pdf
    ```

=== "API (URL)"
    ```http
    POST /api/documents
    Content-Type: application/json

    {
      "sourceUrl": "https://example.com/spec.html",
      "sourceType": "url"
    }
    ```

After upload the document enters a **conversion pipeline**: the file is parsed, split into chunks, and each chunk is embedded for semantic search.

---

## Document fields

| Field | Description |
|---|---|
| `filename` | Original file name |
| `sourceUrl` | URL if the document came from a web source |
| `mimeType` | Detected MIME type |
| `fileSizeBytes` | File size |
| `conversionStatus` | `pending` · `processing` · `completed` · `failed` |
| `chunks` | Number of text chunks created |
| `embeddedChunks` | Number of chunks with embeddings (ready for semantic search) |
| `totalChars` | Total character count across all chunks |
| `extractionStatus` | Status of AI entity extraction (if auto-extraction is enabled) |

---

## Listing Documents

=== "CLI"
    ```bash
    memory documents list --project my-project
    ```

=== "API"
    ```http
    GET /api/documents?projectId={id}&limit=50
    ```

---

## Viewing Document Content

Preview the converted text content of a document:

```http
GET /api/documents/{id}/content
```

Download the original file:

```http
GET /api/documents/{id}/download
```

---

## Automatic Extraction

If the project has `auto_extract_objects` enabled, the platform runs AI extraction on every new document to populate the knowledge graph with typed objects and relationships.

You can also trigger extraction manually from the document detail view in the admin UI, or via the extraction API.

!!! info "Extraction pipeline"
    Extraction runs asynchronously. Monitor progress via [Tasks](tasks.md) or the admin extraction panel.

---

## Deletion Impact

Before deleting a document, check what would be affected:

```http
GET /api/documents/{id}/deletion-impact
```

Returns counts of chunks, embeddings, and graph objects that were sourced from this document.

Bulk impact check:

```http
POST /api/documents/deletion-impact
{ "ids": ["doc_1", "doc_2"] }
```

---

## Deleting Documents

=== "CLI"
    ```bash
    memory documents delete <doc-id> --project my-project
    ```

=== "API"
    ```http
    DELETE /api/documents/{id}
    ```

    Bulk delete:
    ```http
    DELETE /api/documents
    { "ids": ["doc_1", "doc_2"] }
    ```

!!! warning
    Deleting a document removes its chunks and embeddings. Graph objects extracted from it are **not** automatically deleted — they remain in the graph.

---

## Chunking

Documents are split into overlapping text segments (**chunks**) for retrieval. Chunking behavior is configured per project:

| Setting | Description |
|---|---|
| `strategy` | `character` (default), `sentence`, or `paragraph` |
| `maxChunkSize` | Maximum characters per chunk |
| `minChunkSize` | Minimum characters per chunk |
| `overlap` | Character overlap between consecutive chunks |

Configure via **Project Settings → Chunking** or via `PATCH /api/projects/{id}` with the `chunking_config` field.

To re-chunk a specific document after changing the config:

```http
POST /api/documents/{id}/recreate-chunks
```
