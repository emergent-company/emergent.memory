# agents

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents`

The `agents` client manages AI agents — their lifecycle, runs, webhook hooks, question-answer interactions, and ADK sessions.

## Methods

### Agent CRUD

```go
func (c *Client) List(ctx context.Context) (*APIResponse[[]Agent], error)
func (c *Client) Get(ctx context.Context, agentID string) (*APIResponse[Agent], error)
func (c *Client) Create(ctx context.Context, req *CreateAgentRequest) (*APIResponse[Agent], error)
func (c *Client) Update(ctx context.Context, agentID string, req *UpdateAgentRequest) (*APIResponse[Agent], error)
func (c *Client) Delete(ctx context.Context, agentID string) error
```

### Triggering

```go
func (c *Client) Trigger(ctx context.Context, agentID string) (*TriggerResponse, error)
func (c *Client) BatchTrigger(ctx context.Context, agentID string, req *BatchTriggerRequest) (*APIResponse[BatchTriggerResponse], error)
func (c *Client) GetPendingEvents(ctx context.Context, agentID string, limit int) (*APIResponse[PendingEventsResponse], error)
```

### Runs

```go
func (c *Client) GetRuns(ctx context.Context, agentID string, limit int) (*APIResponse[[]AgentRun], error)
func (c *Client) ListProjectRuns(ctx context.Context, projectID string, opts *ListRunsOptions) (*APIResponse[PaginatedResponse[AgentRun]], error)
func (c *Client) GetProjectRun(ctx context.Context, projectID, runID string) (*APIResponse[AgentRun], error)
func (c *Client) CancelRun(ctx context.Context, agentID, runID string) error
func (c *Client) GetRunMessages(ctx context.Context, projectID, runID string) (*APIResponse[[]AgentRunMessage], error)
func (c *Client) GetRunToolCalls(ctx context.Context, projectID, runID string) (*APIResponse[[]AgentRunToolCall], error)
```

### Questions

```go
func (c *Client) GetRunQuestions(ctx context.Context, projectID, runID string) (*APIResponse[[]AgentQuestion], error)
func (c *Client) ListProjectQuestions(ctx context.Context, projectID, status string) (*APIResponse[[]AgentQuestion], error)
func (c *Client) RespondToQuestion(ctx context.Context, projectID, questionID string, req *RespondToQuestionRequest) (*APIResponse[AgentQuestion], error)
```

### Webhook Hooks

```go
func (c *Client) CreateWebhookHook(ctx context.Context, agentID string, req *CreateWebhookHookRequest) (*APIResponse[WebhookHook], error)
func (c *Client) ListWebhookHooks(ctx context.Context, agentID string) (*APIResponse[[]WebhookHook], error)
func (c *Client) DeleteWebhookHook(ctx context.Context, agentID, hookID string) error
```

### ADK Sessions

```go
func (c *Client) ListADKSessions(ctx context.Context, projectID string) ([]*ADKSession, error)
func (c *Client) GetADKSession(ctx context.Context, projectID, sessionID string) (*ADKSession, error)
```

## Key Types

### Agent

```go
type Agent struct {
    ID             string
    ProjectID      string
    Name           string
    StrategyType   string
    Prompt         *string
    CronSchedule   string
    Enabled        bool
    TriggerType    string
    ReactionConfig *ReactionConfig
    ExecutionMode  string
    Capabilities   *AgentCapabilities
    Config         map[string]any
    Description    *string
    LastRunAt      *time.Time
    LastRunStatus  *string
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

### ReactionConfig

```go
type ReactionConfig struct {
    ObjectTypes          []string
    Events               []string
    ConcurrencyStrategy  string
    IgnoreAgentTriggered bool
    IgnoreSelfTriggered  bool
}
```

### AgentCapabilities

```go
type AgentCapabilities struct {
    CanCreateObjects       *bool
    CanUpdateObjects       *bool
    CanDeleteObjects       *bool
    CanCreateRelationships *bool
    AllowedObjectTypes     []string
}
```

### CreateAgentRequest

```go
type CreateAgentRequest struct {
    ProjectID      string
    Name           string
    StrategyType   string
    Prompt         *string
    CronSchedule   string
    Enabled        *bool
    TriggerType    string
    ReactionConfig *ReactionConfig
    ExecutionMode  string
    Capabilities   *AgentCapabilities
    Config         map[string]any
    Description    *string
}
```

### UpdateAgentRequest

All fields are optional (pointer or omitempty).

```go
type UpdateAgentRequest struct {
    Name           *string
    Prompt         *string
    Enabled        *bool
    CronSchedule   *string
    TriggerType    *string
    ReactionConfig *ReactionConfig
    ExecutionMode  *string
    Capabilities   *AgentCapabilities
    Config         map[string]any
    Description    *string
}
```

### AgentRun

```go
type AgentRun struct {
    ID              string
    AgentID         string
    Status          string         // "pending", "running", "completed", "failed", "cancelled"
    StartedAt       time.Time
    CompletedAt     *time.Time
    DurationMs      *int
    Summary         map[string]any
    ErrorMessage    *string
    SkipReason      *string
    StepCount       int
    MaxSteps        *int
    ParentRunID     *string
    ResumedFrom     *string
    TriggerSource   *string
    TriggerMetadata map[string]any
}
```

### AgentRunMessage

```go
type AgentRunMessage struct {
    ID         string
    RunID      string
    Role       string
    Content    map[string]any
    StepNumber int
    CreatedAt  time.Time
}
```

### AgentRunToolCall

```go
type AgentRunToolCall struct {
    ID         string
    RunID      string
    MessageID  *string
    ToolName   string
    Input      map[string]any
    Output     map[string]any
    Status     string
    DurationMs *int
    StepNumber int
    CreatedAt  time.Time
}
```

### AgentQuestion

```go
type AgentQuestion struct {
    ID             string
    RunID          string
    AgentID        string
    ProjectID      string
    Question       string
    Options        []AgentQuestionOption
    Response       *string
    RespondedBy    *string
    RespondedAt    *time.Time
    Status         string
    NotificationID *string
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type AgentQuestionOption struct {
    Label       string
    Value       string
    Description string
}

type RespondToQuestionRequest struct {
    Response string
}
```

### ListRunsOptions

```go
type ListRunsOptions struct {
    Limit   int
    Offset  int
    AgentID string
    Status  string
}
```

### Trigger / Batch Types

```go
type TriggerResponse struct {
    Success bool
    RunID   *string
    Message *string
    Error   *string
}

type BatchTriggerRequest struct {
    ObjectIDs []string
}

type BatchTriggerResponse struct {
    Queued         int
    Skipped        int
    SkippedDetails []struct {
        ObjectID string
        Reason   string
    }
}
```

### WebhookHook

```go
type WebhookHook struct {
    ID              string
    AgentID         string
    ProjectID       string
    Label           string
    Enabled         bool
    RateLimitConfig *RateLimitConfig
    CreatedAt       time.Time
    UpdatedAt       time.Time
    Token           *string  // Only present on creation
}

type RateLimitConfig struct {
    RequestsPerMinute int
    BurstSize         int
}

type CreateWebhookHookRequest struct {
    Label           string
    RateLimitConfig *RateLimitConfig
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

type PaginatedResponse[T any] struct {
    Items      []T
    TotalCount int
    Limit      int
    Offset     int
}
```

## Examples

```go
// Create a scheduled agent
resp, err := client.Agents.Create(ctx, &agents.CreateAgentRequest{
    Name:         "Daily Summarizer",
    StrategyType: "adk",
    CronSchedule: "0 9 * * *",
    TriggerType:  "schedule",
    Enabled:      boolPtr(true),
})
fmt.Printf("Created: %s\n", resp.Data.ID)

// Trigger an agent manually
trigger, err := client.Agents.Trigger(ctx, agentID)
fmt.Printf("Run ID: %s\n", *trigger.RunID)

// Cancel a run
err = client.Agents.CancelRun(ctx, agentID, runID)

// List all runs with filter
runs, err := client.Agents.ListProjectRuns(ctx, projectID, &agents.ListRunsOptions{
    Status: "failed",
    Limit:  50,
})
for _, run := range runs.Data.Items {
    fmt.Printf("Run %s failed: %v\n", run.ID, run.ErrorMessage)
}

// Respond to a pending question
_, err = client.Agents.RespondToQuestion(ctx, projectID, questionID,
    &agents.RespondToQuestionRequest{Response: "yes"})

// Create a webhook hook
hook, err := client.Agents.CreateWebhookHook(ctx, agentID, &agents.CreateWebhookHookRequest{
    Label: "My Webhook",
    RateLimitConfig: &agents.RateLimitConfig{
        RequestsPerMinute: 60,
        BurstSize:         10,
    },
})
fmt.Printf("Token (save this): %s\n", *hook.Data.Token)
```
