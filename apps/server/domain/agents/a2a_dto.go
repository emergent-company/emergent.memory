package agents

import "time"

// A2A v1.0 wire types (package lf.a2a.v1). JSON is camelCase; enums serialize
// as SCREAMING_SNAKE_CASE strings. Discriminated unions (Part, StreamResponse,
// SendMessageResponse) are represented by pointer members + omitempty so that
// exactly one variant serializes at a time — A2A uses member presence, not a
// `kind` discriminator.

// --- Enums ---

// TaskState is the A2A task lifecycle state.
type TaskState string

const (
	TaskStateUnspecified   TaskState = "TASK_STATE_UNSPECIFIED"
	TaskStateSubmitted     TaskState = "TASK_STATE_SUBMITTED"
	TaskStateWorking       TaskState = "TASK_STATE_WORKING"
	TaskStateCompleted     TaskState = "TASK_STATE_COMPLETED"
	TaskStateFailed        TaskState = "TASK_STATE_FAILED"
	TaskStateCanceled      TaskState = "TASK_STATE_CANCELED"
	TaskStateInputRequired TaskState = "TASK_STATE_INPUT_REQUIRED"
	TaskStateRejected      TaskState = "TASK_STATE_REJECTED"
	TaskStateAuthRequired  TaskState = "TASK_STATE_AUTH_REQUIRED"
)

// Role is the A2A message role.
type Role string

const (
	RoleUnspecified Role = "ROLE_UNSPECIFIED"
	RoleUser        Role = "ROLE_USER"
	RoleAgent       Role = "ROLE_AGENT"
)

// --- Discovery ---

// AgentCard is the A2A agent descriptor.
type AgentCard struct {
	Name                 string                    `json:"name"`
	Description          string                    `json:"description"`
	SupportedInterfaces  []AgentInterface          `json:"supportedInterfaces"`
	Provider             *AgentProvider            `json:"provider,omitempty"`
	Version              string                    `json:"version"`
	DocumentationURL     string                    `json:"documentationUrl,omitempty"`
	Capabilities         A2AAgentCapabilities      `json:"capabilities"`
	SecuritySchemes      map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	SecurityRequirements []SecurityRequirement     `json:"securityRequirements,omitempty"`
	DefaultInputModes    []string                  `json:"defaultInputModes"`
	DefaultOutputModes   []string                  `json:"defaultOutputModes"`
	Skills               []AgentSkill              `json:"skills"`
	IconURL              string                    `json:"iconUrl,omitempty"`
	Signatures           []any                     `json:"signatures,omitempty"`
}

// AgentProvider identifies the agent provider.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// AgentInterface describes one transport binding for the agent.
type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	Tenant          string `json:"tenant,omitempty"`
	ProtocolVersion string `json:"protocolVersion"`
}

// A2AAgentCapabilities describes optional agent capabilities.
type A2AAgentCapabilities struct {
	Streaming         bool             `json:"streaming"`
	PushNotifications bool             `json:"pushNotifications,omitempty"`
	Extensions        []AgentExtension `json:"extensions,omitempty"`
	ExtendedAgentCard bool             `json:"extendedAgentCard"`
}

// AgentExtension describes an agent extension.
type AgentExtension struct {
	URI         string `json:"uri"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Version     string `json:"version,omitempty"`
}

// AgentSkill describes a single agent skill.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// SecurityScheme is an A2A security scheme. Only the HTTP auth variant is
// represented today (bearer); OAuth2/OIDC variants are intentionally absent
// because the server does not implement them.
type SecurityScheme struct {
	HTTPAuth *HTTPAuthSecurityScheme `json:"httpAuthSecurityScheme,omitempty"`
}

// HTTPAuthSecurityScheme is the HTTP authentication security scheme.
type HTTPAuthSecurityScheme struct {
	Scheme       string `json:"scheme"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

// SecurityRequirement declares the schemes required for a given operation.
type SecurityRequirement struct {
	Schemes map[string]SecuritySchemeReference `json:"schemes"`
}

// SecuritySchemeReference is a reference to a declared security scheme.
type SecuritySchemeReference struct {
	List []string `json:"list"`
}

// --- Task / Message / Artifact ---

// Task is the A2A task object.
type Task struct {
	ID        string         `json:"id"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	History   []Message      `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus is the current status of a task.
type TaskStatus struct {
	State     TaskState  `json:"state"`
	Message   *Message   `json:"message,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

// Message is an A2A message.
type Message struct {
	MessageID        string         `json:"messageId"`
	ContextID        string         `json:"contextId,omitempty"`
	TaskID           string         `json:"taskId,omitempty"`
	Role             Role           `json:"role"`
	Parts            []Part         `json:"parts"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Extensions       []string       `json:"extensions,omitempty"`
	ReferenceTaskIDs []string       `json:"referenceTaskIds,omitempty"`
}

// Part is a single content part of a message. Exactly one of Text/Raw/URL/Data
// is set; there is no `kind` discriminator on the wire.
type Part struct {
	Text      *string        `json:"text,omitempty"`
	Raw       *string        `json:"raw,omitempty"` // base64-encoded bytes
	URL       *string        `json:"url,omitempty"`
	Data      any            `json:"data,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Filename  string         `json:"filename,omitempty"`
	MediaType string         `json:"mediaType,omitempty"`
}

// Artifact is a named collection of parts produced by a task.
type Artifact struct {
	ArtifactID  string         `json:"artifactId"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parts       []Part         `json:"parts"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Extensions  []string       `json:"extensions,omitempty"`
}

// --- Streaming ---

// StreamResponse is the SSE payload for message:stream. Exactly one member is
// set; there is no `kind` discriminator on the wire.
type StreamResponse struct {
	Task           *Task                    `json:"task,omitempty"`
	Message        *Message                 `json:"message,omitempty"`
	StatusUpdate   *TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
}

// TaskStatusUpdateEvent is a status transition stream event.
type TaskStatusUpdateEvent struct {
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskArtifactUpdateEvent is an artifact delta stream event.
type TaskArtifactUpdateEvent struct {
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Artifact  Artifact       `json:"artifact"`
	Append    bool           `json:"append,omitempty"`
	LastChunk bool           `json:"lastChunk,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// --- Message flow request/response ---

// SendMessageRequest is the body of POST /message:send.
type SendMessageRequest struct {
	Tenant        string                    `json:"tenant,omitempty"`
	Message       Message                   `json:"message"`
	Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
	Metadata      map[string]any            `json:"metadata,omitempty"`
}

// SendMessageConfiguration configures message.send behaviour.
type SendMessageConfiguration struct {
	AcceptedOutputModes []string `json:"acceptedOutputModes,omitempty"`
	HistoryLength       int      `json:"historyLength,omitempty"`
	ReturnImmediately   bool     `json:"returnImmediately,omitempty"`
}

// SendMessageResponse is the response to POST /message:send. Exactly one of
// Task/Message is set.
type SendMessageResponse struct {
	Task    *Task    `json:"task,omitempty"`
	Message *Message `json:"message,omitempty"`
}

// ListTasksRequest captures the query-bound filters for GET /tasks.
type ListTasksRequest struct {
	ContextID     string `query:"contextId"`
	Status        string `query:"status"`
	PageSize      int    `query:"pageSize"`
	PageToken     string `query:"pageToken"`
	HistoryLength int    `query:"historyLength"`
}

// ListTasksResponse is the response to GET /tasks.
type ListTasksResponse struct {
	Tasks         []Task `json:"tasks"`
	NextPageToken string `json:"nextPageToken,omitempty"`
	PageSize      int    `json:"pageSize,omitempty"`
	TotalSize     int    `json:"totalSize,omitempty"`
}
