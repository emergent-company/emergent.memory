# mcp

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/mcp`

The `mcp` client is a JSON-RPC 2.0 client for the Model Context Protocol (MCP) endpoint. It allows calling Emergent's built-in MCP server to invoke tools, list resources, and retrieve prompts.

!!! note "Context behavior"
    `MCP.SetContext(projectID string)` — unlike other context-scoped clients, MCP only takes
    `projectID` (not `orgID`). When you call `client.SetContext(orgID, projectID)`, the MCP
    client is updated with `projectID` only.

## Methods

```go
func (c *Client) CallMethod(ctx context.Context, method string, params interface{}) (json.RawMessage, error)
func (c *Client) Initialize(ctx context.Context) error
func (c *Client) ListTools(ctx context.Context) (json.RawMessage, error)
func (c *Client) CallTool(ctx context.Context, name string, arguments interface{}) (json.RawMessage, error)
func (c *Client) ListResources(ctx context.Context) (json.RawMessage, error)
func (c *Client) ReadResource(ctx context.Context, uri string) (json.RawMessage, error)
func (c *Client) ListPrompts(ctx context.Context) (json.RawMessage, error)
func (c *Client) GetPrompt(ctx context.Context, name string, arguments interface{}) (json.RawMessage, error)
```

## Key Types

```go
type JSONRPCRequest struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      int         `json:"id"`
    Method  string      `json:"method"`
    Params  interface{} `json:"params,omitempty"`
}

type JSONRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      int             `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}
```

## Example

```go
// Set project context
client.MCP.SetContext("proj_xyz789")

// Initialize MCP session
if err := client.MCP.Initialize(ctx); err != nil {
    return err
}

// List available tools
tools, err := client.MCP.ListTools(ctx)

// Call a tool
result, err := client.MCP.CallTool(ctx, "search_graph", map[string]any{
    "query": "machine learning",
    "limit": 10,
})
```
