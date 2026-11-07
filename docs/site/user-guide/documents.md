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
| `processingStatus` | One derived state for the whole pipeline (see [Document status](#document-status)) |
| `version` | `1` for an original, `N` for the Nth revision of a data-source document |

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

The listing is cursor-paginated and returns the page's documents, the total
number of documents matching the filters, a per-status breakdown, and the
cursor of the next page:

```json
{
  "documents": [ { "id": "doc_1", "filename": "report.pdf", "processingStatus": "completed", "version": 1 } ],
  "total": 137,
  "statusCounts": { "all": 137, "processing": 4, "ready": 18, "extracted": 112, "failed": 3 },
  "next_cursor": "eyJzb3J0IjoiY3JlYXRlZCIs..."
}
```

### Query parameters

| Parameter | Values | Description |
|---|---|---|
| `limit` | 1–500 (default 100) | Page size |
| `cursor` | opaque string | Next-page cursor from the previous response. Must be used with the same `sort` |
| `status` | `processing` · `ready` · `extracted` · `failed` | One status bucket (omit for all) |
| `sourceType` | `upload`, `drive`, `repo`, `email`, `meeting`, `web` | Only documents from that source kind; `upload` means files uploaded directly |
| `sourceId` | a connected data source id, or `none` | Only documents from that data source; `none` means files uploaded directly with no data source |
| `q` | text (≤ 200 characters) | Case-insensitive match over the file name and the source URL; `%` and `_` are matched literally. Document *content* is not searched — use [Search](knowledge-graph.md) for that |
| `sort` | `created` (default) · `updated` | Timestamp to order by |
| `order` | `desc` (default) · `asc` | Direction |

Pagination is keyset-based, so pages stay stable while documents are being
added: pass `next_cursor` back as `cursor` until it comes back empty. Changing
`sort` invalidates a cursor — request the first page again (a mismatch returns
`400`).

`total` counts every document the filters match, the status filter included.
`statusCounts` deliberately ignores `status` while honouring `q`, `sourceType`
and `sourceId`, so a UI can render the tabs and their counts without the active
tab changing the other numbers. Its `all` entry is therefore the same as `total`
only when no status filter is set.

The breakdown is memoised for up to 30 seconds per filter combination (project,
search, source, parent, root-only, visibility) because it costs O(project) while
a page is O(1); a document uploaded in that window can be missing from the tab
counts until the cache expires. The listing rows and `total` are never cached.

### Document status

`processingStatus` folds conversion and extraction into one state, so a client
does not have to combine the two raw statuses:

| Value | Meaning | Bucket |
|---|---|---|
| `converting` | File is being parsed into text | `processing` |
| `extracting` | Chunks are ready, AI extraction is running | `processing` |
| `ready_for_extraction` | Text and embeddings are ready; extraction has not started | `ready` |
| `completed` | Text, embeddings and extraction are all done | `extracted` |
| `conversion_failed` | Parsing failed | `failed` |
| `extraction_failed` | AI extraction failed | `failed` |

### Browsing documents in the web UI

**Knowledge → Documents** shows the same dataset with controls on top:

- an **Upload** button in the page header that opens the upload form in a dialog,
- a **stats strip** with the project's document total and the per-status counts,
- a **status filter** (All, Processing, Ready, Extracted, Failed) in a dropdown that keeps the active search, source and sort,
- a **source filter** listing the connected data sources that own documents by name, plus an **Uploaded** bucket for files with no source,
- **search** over file names and source URLs,
- **sorting** on the Created and Updated column headers (click again to flip),
- a **Load more** button that appends the next page without losing the filters.

The **Source** column names the origin: `Uploaded` for a file you uploaded, and
the data source's own name (for example `Product Drive`) for a document imported
by a connected integration. A document from a data source links to that
data source's page, so you can jump straight from the row to the sync that
brought it in.

The **Uploaded by** column names the person who uploaded the document. It is
blank (`—`) in three cases, all of them honest gaps rather than unknown data:
a document imported by a data source has no single uploader (its Source column
already answers where it came from), rows written before this column existed
cannot be attributed after the fact, and an upload made through an ephemeral
sandbox token has no person behind it. An `emt_` project or account token does
carry its owner, so uploads made with one name that owner. The API returns the
uploader as `createdByUserId` with the resolved `createdByDisplayName`, so a
client does not have to look the name up itself.

The header count is the number of documents matching every active filter — the
project total when none is set — never just the length of the current page. The
stats strip and the status filter's counts describe the search and source
population, so selecting a status does not shift the other numbers. On narrow screens the
table is replaced by a card list carrying the same fields.

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
| `strategy` | `recursive_character` (default) or `markdown_structure` (splits on headings and paragraphs, keeping code, math, and table blocks intact) |
| `maxChunkSize` | Maximum characters per chunk |
| `overlap` | Character overlap between consecutive chunks |
| `parentChild` | `markdown_structure` only: store a parent chunk per section alongside its children |
| `autoProfile` | Detect each document's shape (prose, markdown, code-heavy, tabular, transcript, email) and use that shape's parameters instead of the project-wide `strategy`/`maxChunkSize`/`overlap` |

Configure with `PUT /api/chunking/config`, and read the effective settings back with `GET /api/chunking/config`. The write is a full replacement: turning `autoProfile` on stores the other fields as sent (they are then unused), so turning it back off lands on those stored values rather than the ones that were in force beforehand.

`autoProfile` is opt-in and replaces the project-wide size and strategy settings; it never blends with them. Documents with no dominant structure stay on the deployment's default split (`CHUNK_SIZE`/`CHUNK_OVERLAP`, 1000/200 when unset), so enabling it is a no-op for plain prose. A read of the project's chunking configuration returns the per-shape profile table when `autoProfile` is on.

To re-chunk a specific document after changing the config:

```http
POST /api/documents/{id}/recreate-chunks
```

The response summary reports the `strategy` actually used, plus `autoProfile` and the detected `shape` when auto-profile chose it. Chunk metadata records the same, so a chunk can be audited back to the rule that produced it.
