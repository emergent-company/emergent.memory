# documents

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/documents`

The `documents` client manages documents stored in the Emergent platform — upload, create, retrieve, delete, and access content.

## Methods

```go
func (c *Client) List(ctx context.Context, opts *ListOptions) (*ListResult, error)
func (c *Client) Get(ctx context.Context, id string) (*Document, error)
func (c *Client) Create(ctx context.Context, req *CreateRequest) (*Document, error)
func (c *Client) Delete(ctx context.Context, id string) (*DeleteResponse, error)
func (c *Client) BulkDelete(ctx context.Context, ids []string) (*DeleteResponse, error)
func (c *Client) GetContent(ctx context.Context, id string) (*string, error)
func (c *Client) Download(ctx context.Context, id string) (string, error)
func (c *Client) GetSourceTypes(ctx context.Context) ([]SourceTypeWithCount, error)
func (c *Client) GetDeletionImpact(ctx context.Context, id string) (*DeletionImpact, error)
func (c *Client) BulkDeletionImpact(ctx context.Context, ids []string) (*BulkDeletionImpact, error)
func (c *Client) Upload(ctx context.Context, input *UploadFileInput) (*UploadResponse, error)
func (c *Client) UploadWithOptions(ctx context.Context, input *UploadFileInput, autoExtract bool) (*UploadResponse, error)
func (c *Client) UploadBatch(ctx context.Context, files []UploadFileInput) (*BatchUploadResult, error)
func (c *Client) UploadBatchWithOptions(ctx context.Context, files []UploadFileInput, autoExtract bool) (*BatchUploadResult, error)
```

## Key Types

### Document

```go
type Document struct {
    ID        string
    ProjectID string

    // Basic metadata
    Filename  *string
    SourceURL *string
    MimeType  *string

    // Content
    Content     *string
    ContentHash *string
    FileHash    *string

    // Timestamps
    CreatedAt time.Time
    UpdatedAt time.Time

    // Hierarchy
    ParentDocumentID *string

    // Conversion status
    ConversionStatus      *string
    ConversionError       *string
    ConversionCompletedAt *time.Time

    // Storage
    StorageKey    *string
    FileSizeBytes *int64
    StorageURL    *string

    // Data source
    SourceType              *string
    DataSourceIntegrationID *string
    ExternalSourceID        *string
    SyncVersion             *int

    // Metadata
    IntegrationMetadata map[string]any
    Metadata            map[string]any

    // Computed counts
    Chunks           int
    EmbeddedChunks   int
    TotalChars       int
    ExtractionStatus *string
}
```

### ListOptions

```go
type ListOptions struct {
    Limit            int
    Cursor           string  // Cursor-based pagination (not offset)
    SourceType       string
    IntegrationID    string
    RootOnly         bool    // Only return top-level documents
    ParentDocumentID string  // Filter by parent document
}
```

### ListResult

```go
type ListResult struct {
    Documents  []Document
    Total      int
    NextCursor *string  // Pass as Cursor in next request; nil when no more pages
}
```

### CreateRequest

Creates a document with inline text content.

```go
type CreateRequest struct {
    Filename string
    Content  string  // Plain text content (not []byte)
}
```

### UploadFileInput

Used with `Upload` / `UploadBatch` for binary file uploads (multipart/form-data).

```go
type UploadFileInput struct {
    Filename    string
    Reader      io.Reader
    ContentType string  // Optional; auto-detected if empty
}
```

### UploadResponse

```go
type UploadResponse struct {
    Document           *DocumentSummary
    IsDuplicate        bool
    ExistingDocumentID *string  // Set when IsDuplicate is true
}

type DocumentSummary struct {
    ID               string
    Name             string
    MimeType         *string
    FileSizeBytes    *int64
    ConversionStatus string
    ConversionError  *string
    StorageKey       *string
    CreatedAt        string
}
```

### BatchUploadResult

```go
type BatchUploadResult struct {
    Summary BatchUploadSummary
    Results []BatchUploadFileResult
}

type BatchUploadSummary struct {
    Total      int
    Successful int
    Duplicates int
    Failed     int
}

type BatchUploadFileResult struct {
    Filename   string
    Status     string  // "success", "duplicate", "failed"
    DocumentID *string
    Chunks     *int
    Error      *string
}
```

### DeleteResponse / DeleteSummary

```go
type DeleteResponse struct {
    Status   string
    Deleted  int
    NotFound []string
    Summary  *DeleteSummary
}

type DeleteSummary struct {
    Chunks             int
    ExtractionJobs     int
    GraphObjects       int
    GraphRelationships int
    Notifications      int
}
```

### DeletionImpact / BulkDeletionImpact

Preview what will be deleted before committing.

```go
type DeletionImpact struct {
    Document DocumentInfo
    Impact   ImpactSummary
}

type DocumentInfo struct {
    ID        string
    Name      string
    CreatedAt string
}

type ImpactSummary struct {
    Chunks             int
    ExtractionJobs     int
    GraphObjects       int
    GraphRelationships int
    Notifications      int
}

type BulkDeletionImpact struct {
    TotalDocuments int
    Impact         ImpactSummary
    Documents      []DeletionImpact
}
```

## Examples

```go
// Upload a file (binary, multipart)
f, _ := os.Open("report.pdf")
defer f.Close()
result, err := client.Documents.Upload(ctx, &documents.UploadFileInput{
    Filename: "report.pdf",
    Reader:   f,
})
if result.IsDuplicate {
    fmt.Printf("Already exists: %s\n", *result.ExistingDocumentID)
} else {
    fmt.Printf("Uploaded: %s\n", result.Document.ID)
}

// Create a document with inline text
doc, err := client.Documents.Create(ctx, &documents.CreateRequest{
    Filename: "notes.txt",
    Content:  "Hello world",
})

// List documents (cursor-based pagination)
page, err := client.Documents.List(ctx, &documents.ListOptions{Limit: 50})
for _, doc := range page.Documents {
    fmt.Printf("%s: %s\n", doc.ID, *doc.Filename)
}
if page.NextCursor != nil {
    next, err := client.Documents.List(ctx, &documents.ListOptions{
        Limit:  50,
        Cursor: *page.NextCursor,
    })
    _ = next
}

// Preview deletion impact before deleting
impact, err := client.Documents.GetDeletionImpact(ctx, doc.ID)
fmt.Printf("Will delete %d graph objects, %d chunks\n",
    impact.Impact.GraphObjects, impact.Impact.Chunks)

// Delete
resp, err := client.Documents.Delete(ctx, doc.ID)
fmt.Printf("Deleted: %d chunks, %d graph objects\n",
    resp.Summary.Chunks, resp.Summary.GraphObjects)

// Download original file (returns signed URL)
downloadURL, err := client.Documents.Download(ctx, doc.ID)

// Batch upload
results, err := client.Documents.UploadBatch(ctx, []documents.UploadFileInput{
    {Filename: "a.txt", Reader: readerA},
    {Filename: "b.txt", Reader: readerB},
})
fmt.Printf("Uploaded %d/%d\n", results.Summary.Successful, results.Summary.Total)
```
