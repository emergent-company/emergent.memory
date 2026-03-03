# agentdefinitions

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/agentdefinitions`

The `agentdefinitions` client manages agent definitions — the templates that describe what an agent does, what model it uses, and what capabilities it has. Agents are created from definitions.

## Methods

```go
func (c *Client) List(ctx context.Context) (*APIResponse[[]AgentDefinitionSummary], error)
func (c *Client) Get(ctx context.Context, definitionID string) (*APIResponse[AgentDefinition], error)
func (c *Client) Create(ctx context.Context, createReq *CreateAgentDefinitionRequest) (*APIResponse[AgentDefinition], error)
func (c *Client) Update(ctx context.Context, definitionID string, updateReq *UpdateAgentDefinitionRequest) (*APIResponse[AgentDefinition], error)
func (c *Client) Delete(ctx context.Context, definitionID string) error
```

## Key Types

### AgentDefinition

```go
type AgentDefinition struct {
    ID           string
    Name         string
    Description  string
    Prompt       string
    ModelConfig  ModelConfig
    ACPConfig    *ACPConfig
    Capabilities []string
    Tags         []string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

### AgentDefinitionSummary

```go
type AgentDefinitionSummary struct {
    ID           string
    Name         string
    Description  string
    Capabilities []string
    Tags         []string
    AgentCount   int
    CreatedAt    time.Time
}
```

### ModelConfig

```go
type ModelConfig struct {
    Provider    string
    Model       string
    Temperature float64
    MaxTokens   int
}
```

### ACPConfig

```go
type ACPConfig struct {
    Enabled    bool
    MaxRetries int
    Timeout    int
}
```

### CreateAgentDefinitionRequest

```go
type CreateAgentDefinitionRequest struct {
    Name         string
    Description  string
    Prompt       string
    ModelConfig  ModelConfig
    ACPConfig    *ACPConfig
    Capabilities []string
    Tags         []string
}
```

### UpdateAgentDefinitionRequest

```go
type UpdateAgentDefinitionRequest struct {
    Name         string
    Description  string
    Prompt       string
    ModelConfig  *ModelConfig
    ACPConfig    *ACPConfig
    Capabilities []string
    Tags         []string
}
```

## Example

```go
// Create an agent definition
resp, err := client.AgentDefinitions.Create(ctx, &agentdefinitions.CreateAgentDefinitionRequest{
    Name:        "Document Summarizer",
    Description: "Summarizes new documents as they are ingested",
    Prompt:      "You are a summarization agent. For each document, produce a concise summary...",
    ModelConfig: agentdefinitions.ModelConfig{
        Provider:    "google",
        Model:       "gemini-2.0-flash",
        Temperature: 0.3,
        MaxTokens:   1024,
    },
    Capabilities: []string{"document_read"},
})
fmt.Printf("Created definition: %s\n", resp.Data.ID)
```
