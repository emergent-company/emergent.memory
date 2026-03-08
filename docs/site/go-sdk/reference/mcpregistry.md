# mcpregistry

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpregistry`

The `mcpregistry` client manages the MCP server registry — external MCP servers that are registered with a project and made available to agents.

## Methods

```go
func (c *Client) List(ctx context.Context) (*APIResponse[[]MCPServer], error)
func (c *Client) Get(ctx context.Context, serverID string) (*APIResponse[MCPServerDetail], error)
func (c *Client) Create(ctx context.Context, createReq *CreateMCPServerRequest) (*APIResponse[MCPServer], error)
func (c *Client) Update(ctx context.Context, serverID string, updateReq *UpdateMCPServerRequest) (*APIResponse[MCPServer], error)
func (c *Client) Delete(ctx context.Context, serverID string) error
func (c *Client) SyncTools(ctx context.Context, serverID string) (*APIResponse[any], error)
func (c *Client) Inspect(ctx context.Context, serverID string) (*APIResponse[MCPServerInspect], error)
func (c *Client) ListTools(ctx context.Context, serverID string) (*APIResponse[[]MCPServerTool], error)
func (c *Client) ToggleTool(ctx context.Context, serverID, toolID string, enabled bool) error
```

## Key Types

### MCPServer

```go
type MCPServer struct {
    ID         string
    Name       string
    ServerType MCPServerType // "sse" or "stdio"
    URL        string        // For SSE servers
    Status     string
    ToolCount  int
    ProjectID  string
    CreatedAt  time.Time
}
```

### MCPServerType

```go
type MCPServerType = string

const (
    MCPServerTypeSSE   MCPServerType = "sse"
    MCPServerTypeStdio MCPServerType = "stdio"
)
```

### MCPServerTool

```go
type MCPServerTool struct {
    ID          string
    Name        string
    Description string
    Enabled     bool
    InputSchema map[string]interface{}
}
```

### CreateMCPServerRequest

```go
type CreateMCPServerRequest struct {
    Name       string
    ServerType MCPServerType
    URL        string // For SSE servers
    Command    string // For stdio servers
    Args       []string
}
```

## Example

```go
// Register an SSE MCP server
resp, err := client.MCPRegistry.Create(ctx, &mcpregistry.CreateMCPServerRequest{
    Name:       "My Tools Server",
    ServerType: mcpregistry.MCPServerTypeSSE,
    URL:        "https://tools.example.com/sse",
})

// Sync available tools from the server
_, err = client.MCPRegistry.SyncTools(ctx, resp.Data.ID)

// List the synced tools
tools, err := client.MCPRegistry.ListTools(ctx, resp.Data.ID)
for _, tool := range tools.Data {
    fmt.Printf("%s — %s (enabled: %v)\n", tool.Name, tool.Description, tool.Enabled)
}
```
