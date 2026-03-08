# agentdefinitions

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions`

The `agentdefinitions` client manages agent definitions — reusable configurations that store system prompts, model settings, tools, and flow type. Agent definitions are scoped to a project.

See also: [agents](agents.md) for the runtime agents that use these definitions.

## Methods

```go
func (c *Client) List(ctx context.Context) (*APIResponse[[]AgentDefinitionSummary], error)
func (c *Client) Get(ctx context.Context, definitionID string) (*APIResponse[AgentDefinition], error)
func (c *Client) Create(ctx context.Context, req *CreateAgentDefinitionRequest) (*APIResponse[AgentDefinition], error)
func (c *Client) Update(ctx context.Context, definitionID string, req *UpdateAgentDefinitionRequest) (*APIResponse[AgentDefinition], error)
func (c *Client) Delete(ctx context.Context, definitionID string) error
```

## Key Types

### AgentDefinition

```go
type AgentDefinition struct {
    ID             string
    ProductID      *string
    ProjectID      string
    Name           string
    Description    *string
    SystemPrompt   *string
    Model          *ModelConfig
    Tools          []string
    FlowType       string
    IsDefault      bool
    MaxSteps       *int
    DefaultTimeout *int
    Visibility     string
    ACPConfig      *ACPConfig
    Config         map[string]any
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

### AgentDefinitionSummary

Lightweight representation returned by `List`.

```go
type AgentDefinitionSummary struct {
    ID          string
    ProjectID   string
    Name        string
    Description *string
    FlowType    string
    Visibility  string
    IsDefault   bool
    ToolCount   int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### ModelConfig

```go
type ModelConfig struct {
    Name        string   // Model name, e.g. "gemini-2.0-flash"
    Temperature *float32
    MaxTokens   *int
}
```

### ACPConfig

Agent Card Protocol metadata, used when exposing the agent via ACP.

```go
type ACPConfig struct {
    DisplayName  string
    Description  string
    Capabilities []string
    InputModes   []string
    OutputModes  []string
}
```

### CreateAgentDefinitionRequest

```go
type CreateAgentDefinitionRequest struct {
    Name           string
    Description    *string
    SystemPrompt   *string
    Model          *ModelConfig
    Tools          []string
    FlowType       string
    IsDefault      *bool
    MaxSteps       *int
    DefaultTimeout *int
    Visibility     string
    ACPConfig      *ACPConfig
    Config         map[string]any
}
```

### UpdateAgentDefinitionRequest

All fields are optional (pointer or omitempty).

```go
type UpdateAgentDefinitionRequest struct {
    Name           *string
    Description    *string
    SystemPrompt   *string
    Model          *ModelConfig
    Tools          []string
    FlowType       *string
    IsDefault      *bool
    MaxSteps       *int
    DefaultTimeout *int
    Visibility     *string
    ACPConfig      *ACPConfig
    Config         map[string]any
}
```

### APIResponse

```go
type APIResponse[T any] struct {
    Success bool
    Data    T
    Error   *string
    Message *string
}
```

## Example

```go
// Create an agent definition
resp, err := client.AgentDefinitions.Create(ctx, &agentdefinitions.CreateAgentDefinitionRequest{
    Name:         "Document Summarizer",
    Description:  strPtr("Summarizes new documents as they are ingested"),
    SystemPrompt: strPtr("You are a summarization agent. For each document, produce a concise summary..."),
    Model: &agentdefinitions.ModelConfig{
        Name:        "gemini-2.0-flash",
        Temperature: float32Ptr(0.3),
        MaxTokens:   intPtr(1024),
    },
    Tools:    []string{"read_document", "create_graph_object"},
    FlowType: "adk",
})
fmt.Printf("Created definition: %s\n", resp.Data.ID)

// List definitions in the project
list, err := client.AgentDefinitions.List(ctx)
for _, def := range list.Data {
    fmt.Printf("%s (%s) — tools: %d\n", def.Name, def.FlowType, def.ToolCount)
}

// Update the system prompt
_, err = client.AgentDefinitions.Update(ctx, resp.Data.ID, &agentdefinitions.UpdateAgentDefinitionRequest{
    SystemPrompt: strPtr("Updated prompt..."),
})
```
