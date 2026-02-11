# Emergent Go SDK Examples

This directory contains working examples demonstrating how to use the Emergent Go SDK.

## Prerequisites

1. **Set environment variables:**

```bash
export EMERGENT_API_KEY="your_api_key_here"
export EMERGENT_SERVER_URL="http://localhost:3002"  # Optional, defaults to localhost
export EMERGENT_ORG_ID="your_org_id"                # Optional
export EMERGENT_PROJECT_ID="your_project_id"        # Optional
```

2. **Ensure the Emergent server is running:**

```bash
# Check server health
curl http://localhost:3002/health
```

## Examples

### 1. Basic Usage (`basic/`)

**What it demonstrates:**

- Creating an SDK client with API key authentication
- Setting organization and project context
- Performing a basic health check

**Run:**

```bash
cd basic
go run main.go
```

**Expected output:**

```
Server Status: ok
Server Version: 2.0.0
Server Uptime: 2h30m
✓ SDK connection successful!
```

---

### 2. Document Management (`documents/`)

**What it demonstrates:**

- Listing documents with pagination
- Fetching a specific document's details
- Listing chunks for a document

**Run:**

```bash
cd documents
go run main.go
```

**Expected output:**

```
=== Listing Documents ===
Found 3 documents

- Machine Learning Guide (ID: doc_123)
  Type: application/pdf, Source: upload
  Created: 2026-02-10 14:30:00

=== Document Details: Machine Learning Guide ===
Title: Machine Learning Guide
Source URL: https://example.com/ml-guide.pdf
Content Type: application/pdf

=== Listing Chunks ===
Found 5 chunks for this document

1. Position: 0
   Preview: Machine learning is a subset of artificial intelligence...

2. Position: 1
   Preview: Supervised learning involves training a model on labeled data...
```

---

### 3. Search (`search/`)

**What it demonstrates:**

- Performing hybrid search (lexical + semantic)
- Processing search results
- Handling command-line arguments

**Run:**

```bash
cd search
go run main.go "neural networks"
```

**Expected output:**

```
Searching for: "neural networks"

Found 5 results

1. Score: 0.9234
   Document: doc_456
   Chunk: chunk_789
   Preview: Neural networks are computing systems inspired by biological neural networks...

2. Score: 0.8912
   Document: doc_123
   Chunk: chunk_456
   Preview: Deep learning uses artificial neural networks with multiple layers...
```

---

### 4. Project Management (`projects/`)

**What it demonstrates:**

- Listing projects
- Creating a new project
- Updating project settings
- Deleting a project

**Run:**

```bash
cd projects
go run main.go
```

**Expected output:**

```
=== Listing Projects ===
- Production KB (ID: proj_abc)
- Development KB (ID: proj_def)

=== Creating New Project ===
Created project: SDK Example (ID: proj_xyz)

=== Updating Project ===
Updated project name to: SDK Example (Updated)

=== Deleting Project ===
Project deleted successfully
```

## Common Patterns

### Error Handling

All examples use basic error handling:

```go
result, err := client.SomeService.SomeMethod(ctx, ...)
if err != nil {
    log.Fatalf("Operation failed: %v", err)
}
```

For production code, use more sophisticated error handling:

```go
import sdkerrors "github.com/emergent/emergent-core/pkg/sdk/errors"

result, err := client.Documents.Get(ctx, documentID)
if err != nil {
    if sdkerrors.IsNotFound(err) {
        fmt.Println("Document not found")
        return
    }
    if sdkerrors.IsUnauthorized(err) {
        fmt.Println("Authentication failed - check your API key")
        return
    }
    log.Fatalf("Unexpected error: %v", err)
}
```

### Context Management

All API calls accept a `context.Context` for cancellation and timeouts:

```go
import "time"

// With timeout
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

result, err := client.Search.Search(ctx, req)
```

### Changing Organization/Project Context

```go
// Set default context for all requests
client.SetContext("org_abc", "proj_xyz")

// All subsequent calls will use this context
docs, _ := client.Documents.List(ctx, nil)
```

## Building the Examples

To build all examples:

```bash
cd /root/emergent/apps/server-go/pkg/sdk/examples

# Build basic
go build -o bin/basic ./basic/

# Build documents
go build -o bin/documents ./documents/

# Build search
go build -o bin/search ./search/

# Build projects
go build -o bin/projects ./projects/
```

## Next Steps

- **Read the full SDK documentation:** `/root/emergent/apps/server-go/pkg/sdk/README.md`
- **Explore all service clients:** Documents, Chunks, Search, Graph, Chat, Projects, Orgs, Users, API Tokens, Health, MCP
- **Check the test files:** `/root/emergent/apps/server-go/pkg/sdk/*/client_test.go` for more usage examples

## Troubleshooting

**"Failed to create SDK client: authentication failed"**

- Check that `EMERGENT_API_KEY` is set correctly
- Verify the API key is valid by testing it with curl:
  ```bash
  curl -H "X-API-Key: $EMERGENT_API_KEY" http://localhost:3002/health
  ```

**"Failed to list documents: request failed"**

- Ensure the server is running: `curl http://localhost:3002/health`
- Check `EMERGENT_SERVER_URL` is set correctly
- Verify `EMERGENT_PROJECT_ID` is set and valid

**"No documents found"**

- Upload some documents first using the Emergent CLI or admin UI
- Ensure you're using the correct project ID

## License

See the main [Emergent LICENSE](../../../../../LICENSE) file.
