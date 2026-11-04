package agentcompat

import "encoding/json"

// ChatCompletionRequest mirrors the OpenAI /v1/chat/completions request body.
// Unrecognised fields are silently ignored; only fields relevant to agent
// execution are acted upon.
type ChatCompletionRequest struct {
	// Model selects the agent: "agent:<name>" or bare "<name>".
	Model string `json:"model"`

	Messages []ChatMessage `json:"messages"`

	// LLM sampling params — passed through to the underlying model.
	Temperature         *float64 `json:"temperature,omitempty"`
	TopP                *float64 `json:"top_p,omitempty"`
	MaxTokens           *int     `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int     `json:"max_completion_tokens,omitempty"`
	Stop                []string `json:"stop,omitempty"`
	FrequencyPenalty    *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64 `json:"presence_penalty,omitempty"`
	Seed                *int     `json:"seed,omitempty"`
	ReasoningEffort     string   `json:"reasoning_effort,omitempty"`

	// Streaming.
	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`

	// Client-supplied tools. When present the agent can call these and the
	// response will include tool_calls for the client to execute.
	// Internal (memory_*) tools are always available alongside these.
	Tools         []ClientToolDef  `json:"tools,omitempty"`
	ToolChoice    *json.RawMessage `json:"tool_choice,omitempty"`
	ParallelTools *bool            `json:"parallel_tool_calls,omitempty"`

	// ResponseFormat lets the client request JSON output.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// User is an opaque identifier for abuse-detection, forwarded to the run.
	User string `json:"user,omitempty"`

	// Metadata is stored as run trigger_metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// SystemFingerprint is overloaded as a run-resume token.
	// When the server pauses an agent run waiting for client tool execution
	// it sets SystemFingerprint = "run_<runID>". The client must echo this
	// value back in the next request to resume the same run.
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
}

// ChatMessage is one message in the conversation, matching OpenAI's schema.
type ChatMessage struct {
	Role    string `json:"role"` // system | user | assistant | tool
	Content string `json:"content,omitempty"`
	// Tool calls produced by the assistant (finish_reason == "tool_calls").
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Tool result sent by the caller in a follow-up request.
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

// ToolCall is an assistant-generated tool invocation inside a message.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the name and JSON-encoded arguments for a tool_call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ClientToolDef is the OpenAI tool definition format (type=="function").
type ClientToolDef struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef describes a callable function the client can supply.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // JSON Schema object
	Strict      bool            `json:"strict,omitempty"`
}

// StreamOptions configures streaming behaviour.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ResponseFormat lets the caller request structured output.
type ResponseFormat struct {
	Type       string           `json:"type"`                  // "text" | "json_object" | "json_schema"
	JSONSchema *json.RawMessage `json:"json_schema,omitempty"` // when type=="json_schema"
}

// ─── Non-streaming response ────────────────────────────────────────────────

// ChatCompletionResponse is the non-streaming OpenAI response shape.
type ChatCompletionResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`  // "chat.completion"
	Created           int64    `json:"created"` // unix seconds
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             *Usage   `json:"usage,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

// Choice is a single candidate completion.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"` // "stop" | "tool_calls" | "length"
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ─── Streaming chunks ─────────────────────────────────────────────────────

// ChatCompletionChunk is a single SSE data payload when streaming.
type ChatCompletionChunk struct {
	ID                string        `json:"id"`
	Object            string        `json:"object"`  // "chat.completion.chunk"
	Created           int64         `json:"created"` // unix seconds
	Model             string        `json:"model"`
	Choices           []ChunkChoice `json:"choices"`
	Usage             *Usage        `json:"usage,omitempty"`
	SystemFingerprint string        `json:"system_fingerprint,omitempty"`
}

// ChunkChoice carries a delta inside a streaming chunk.
type ChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"` // null until the last chunk
}

// Delta is the incremental content of a streaming chunk.
type Delta struct {
	Role      string          `json:"role,omitempty"`
	Content   *string         `json:"content,omitempty"`
	ToolCalls []DeltaToolCall `json:"tool_calls,omitempty"`
}

// DeltaToolCall streams a tool_call incrementally (id+name first, then arguments).
type DeltaToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function *DeltaFunctionCall `json:"function,omitempty"`
}

// DeltaFunctionCall streams name and arguments deltas.
type DeltaFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ─── Model listing ────────────────────────────────────────────────────────

// ModelList is returned by GET /v1/models.
type ModelList struct {
	Object string  `json:"object"` // "list"
	Data   []Model `json:"data"`
}

// Model describes a single available model (backed by an agent definition).
type Model struct {
	ID      string `json:"id"`      // "agent:<name>"
	Object  string `json:"object"`  // "model"
	Created int64  `json:"created"` // unix seconds (agent createdAt)
	OwnedBy string `json:"owned_by"`
}

// ─── Error response ───────────────────────────────────────────────────────

// APIError is the OpenAI-format error envelope.
type APIError struct {
	Error APIErrorDetail `json:"error"`
}

// APIErrorDetail holds the machine- and human-readable error info.
type APIErrorDetail struct {
	Message string  `json:"message"`
	Type    string  `json:"type,omitempty"`
	Param   *string `json:"param,omitempty"`
	Code    *string `json:"code,omitempty"`
}
