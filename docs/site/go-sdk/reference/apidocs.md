# apidocs

Package `apidocs` provides a client for the Emergent built-in API documentation endpoints.

The apidocs client is a **non-context client** — it requires no org or project context. It provides access to the server's internal documentation registry: a curated index of documentation articles that can be listed, searched, and browsed by category.

> **Not the OpenAPI/Swagger spec.** This client accesses the built-in documentation articles at `/api/docs`. For the OpenAPI specification, fetch `/openapi.json` directly.

## Import

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/apidocs"
```

## Client

```go
type Client struct { /* unexported */ }
```

Obtained via `sdk.Client.APIDocs`. No `SetContext` call is needed or available.

---

## Methods

### ListDocuments

```go
func (c *Client) ListDocuments(ctx context.Context) (*ListDocumentsResponse, error)
```

Returns metadata for all available documentation articles.

**Endpoint:** `GET /api/docs`

---

### GetDocument

```go
func (c *Client) GetDocument(ctx context.Context, slug string) (*Document, error)
```

Returns the full content of a documentation article by its slug.

**Endpoint:** `GET /api/docs/{slug}`

---

### GetCategories

```go
func (c *Client) GetCategories(ctx context.Context) (*CategoriesResponse, error)
```

Returns the list of documentation categories with descriptions.

**Endpoint:** `GET /api/docs/categories`

---

## Types

### DocumentMeta

```go
type DocumentMeta struct {
    ID          string   `json:"id"`
    Slug        string   `json:"slug"`
    Title       string   `json:"title"`
    Category    string   `json:"category"`
    Path        string   `json:"path"`
    Description string   `json:"description"`
    Tags        []string `json:"tags"`
    LastUpdated string   `json:"lastUpdated"`
    ReadTime    int      `json:"readTime"` // estimated minutes
    Related     []string `json:"related"`  // related article slugs
}
```

Returned in list views (no `Content` field).

---

### Document

```go
type Document struct {
    ID          string    `json:"id"`
    Slug        string    `json:"slug"`
    Title       string    `json:"title"`
    Category    string    `json:"category"`
    Path        string    `json:"path"`
    Description string    `json:"description"`
    Tags        []string  `json:"tags"`
    LastUpdated string    `json:"lastUpdated"`
    ReadTime    int       `json:"readTime"`
    Related     []string  `json:"related"`
    Content     string    `json:"content"` // full markdown/HTML body
    ParsedAt    time.Time `json:"parsedAt"`
}
```

---

### ListDocumentsResponse

```go
type ListDocumentsResponse struct {
    Documents []DocumentMeta `json:"documents"`
    Total     int            `json:"total"`
}
```

---

### CategoryInfo

```go
type CategoryInfo struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Icon        string `json:"icon"`
}
```

### CategoriesResponse

```go
type CategoriesResponse struct {
    Categories []CategoryInfo `json:"categories"`
    Total      int            `json:"total"`
}
```

---

## Example

```go
// List all doc articles
docs, err := client.APIDocs.ListDocuments(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("%d documentation articles available\n", docs.Total)
for _, d := range docs.Documents {
    fmt.Printf("  [%s] %s (%d min read)\n", d.Category, d.Title, d.ReadTime)
}

// Fetch a specific article by slug
article, err := client.APIDocs.GetDocument(ctx, "graph-id-model")
if err != nil {
    log.Fatal(err)
}
fmt.Println(article.Content)

// List categories
cats, err := client.APIDocs.GetCategories(ctx)
if err != nil {
    log.Fatal(err)
}
for _, c := range cats.Categories {
    fmt.Printf("%s: %s\n", c.Name, c.Description)
}
```
