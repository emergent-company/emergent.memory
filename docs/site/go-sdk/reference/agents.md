# agents

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/agents`

The `agents` client manages AI agents — their lifecycle, runs, and question-answer interactions.

## Methods

```go
func (c *Client) List(ctx context.Context) (*APIResponse[[]Agent], error)
func (c *Client) Get(ctx context.Context, agentID string) (*APIResponse[Agent], error)
func (c *Client) GetRunQuestions(ctx context.Context, projectID, runID string) (*APIResponse[[]AgentQuestion], error)
func (c *Client) ListProjectQuestions(ctx context.Context, projectID, status string) (*APIResponse[[]AgentQuestion], error)
```

## Key Types

### Agent

```go
type Agent struct {
    ID           string
    Name         string
    ProjectID    string
    DefinitionID string
    Status       string
    TriggerType  string
    Schedule     string
    IsActive     bool
    Capabilities []string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

### CreateAgentRequest

```go
type CreateAgentRequest struct {
    Name         string
    DefinitionID string
    ProjectID    string
    TriggerType  string // "manual", "schedule", "reaction"
    Schedule     string // Cron expression for schedule triggers
    IsActive     bool
}
```

### UpdateAgentRequest

```go
type UpdateAgentRequest struct {
    Name        string
    TriggerType string
    Schedule    string
    IsActive    *bool
}
```

### AgentRun

```go
type AgentRun struct {
    ID         string
    AgentID    string
    Status     string // "pending", "running", "completed", "failed"
    StartedAt  time.Time
    FinishedAt *time.Time
    Error      string
}
```

### AgentQuestion

```go
type AgentQuestion struct {
    ID        string
    RunID     string
    AgentID   string
    Question  string
    Options   []AgentQuestionOption
    Status    string
    CreatedAt time.Time
}

type AgentQuestionOption struct {
    ID    string
    Label string
    Value string
}
```

### APIResponse

```go
type APIResponse[T any] struct {
    Data    T
    Message string
    Status  string
}
```

## Example

```go
// List all agents in the current project
resp, err := client.Agents.List(ctx)
if err != nil {
    return err
}
for _, agent := range resp.Data {
    fmt.Printf("%s (%s) — active: %v\n", agent.Name, agent.ID, agent.IsActive)
}

// Get pending questions for a project
questions, err := client.Agents.ListProjectQuestions(ctx, "proj_123", "pending")
for _, q := range questions.Data {
    fmt.Printf("Q: %s\n", q.Question)
}
```
