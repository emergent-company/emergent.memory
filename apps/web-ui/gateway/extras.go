package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// --- conversation detail / history ---

type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	CreatedAt      string `json:"createdAt"`
}

type ConversationDetail struct {
	ID                string    `json:"id"`
	Title             string    `json:"title"`
	AgentDefinitionID string    `json:"agentDefinitionId"`
	ProjectID         string    `json:"projectId"`
	SessionID         string    `json:"sessionId"`
	CanonicalID       string    `json:"canonicalId"`
	CreatedAt         string    `json:"createdAt"`
	UpdatedAt         string    `json:"updatedAt"`
	Messages          []Message `json:"messages"`
}

type ConversationHistory struct {
	SessionID      string `json:"session_id"`
	ConversationID string `json:"conversation_id"`
	// AgentID/AgentName identify the agent that produced a RUN transcript, so
	// the chat workspace can head the pane and label its bubbles with the run's
	// own agent instead of the selected chat agent. Empty for a conversation
	// transcript (the conversation already carries its agent).
	AgentID          string                `json:"agent_id,omitempty"`
	AgentName        string                `json:"agent_name,omitempty"`
	Items            []json.RawMessage     `json:"items"`
	PendingApprovals []PendingApprovalItem `json:"pending_approvals,omitempty"`
}

// PendingApprovalItem is one tool-approval awaiting a human decision, surfaced
// in the history transcript so a paused run shows its Approve/Reject/Cancel
// card even after the live chat SSE stream has ended.
type PendingApprovalItem struct {
	QuestionID string         `json:"questionId"`
	Tool       string         `json:"tool"`
	Input      map[string]any `json:"input"`
}

func (m *MemoryClient) GetConversation(ctx context.Context, id string) (*ConversationDetail, error) {
	var out ConversationDetail
	if err := m.do(ctx, http.MethodGet, "/api/chat/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) GetConversationHistory(ctx context.Context, id string) (*ConversationHistory, error) {
	var out ConversationHistory
	if err := m.do(ctx, http.MethodGet, "/api/chat/"+id+"/history", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// conversationTimeline returns a conversation's transcript. Memory owns the
// transcript merge: its history endpoint already interleaves the run-history
// items (messages, run_start/run_end, tool calls) with the conversation's
// stored chat messages (kb.chat_messages) and suppresses the duplicate stored
// copy of a message that also appears in a run, so the gateway does not merge
// the two sources itself. The gateway only falls back to the stored messages
// when memory returns no run/item history: a missing run timeline (the history
// endpoint reports the conversation has none) uses the stored messages alone,
// as does an empty-item history. Genuine memory-service failures stay fatal so
// callers can surface them instead of rendering a silently empty transcript.
func (s *Server) conversationTimeline(ctx context.Context, id string) (*ConversationHistory, error) {
	hist, err := s.memory.GetConversationHistory(ctx, id)
	if err != nil {
		if !isMemoryNotFound(err) && !strings.Contains(err.Error(), "invalid conversation id") {
			return nil, err
		}
		// No run timeline for this conversation (never ran, or unknown id): the
		// stored chat messages are the whole transcript.
		return s.messageOnlyTimeline(ctx, id)
	}
	// Memory already merged the stored messages into the run history and
	// deduped the overlap; re-merging here would emit the user turn twice.
	if len(hist.Items) > 0 {
		return hist, nil
	}
	detail, err := s.memory.GetConversation(ctx, id)
	if err != nil {
		// Stored messages unavailable: the (empty) run-history items alone are
		// all there is to render.
		return hist, nil
	}
	hist.Items = messageTimelineItems(detail.Messages)
	return hist, nil
}

// messageOnlyTimeline builds a conversation transcript from its stored chat
// messages alone (no run-history items available).
func (s *Server) messageOnlyTimeline(ctx context.Context, id string) (*ConversationHistory, error) {
	detail, err := s.memory.GetConversation(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ConversationHistory{ConversationID: id, Items: messageTimelineItems(detail.Messages)}, nil
}

// messageTimelineItems synthesizes timeline message items from a
// conversation's stored messages, in stored order.
func messageTimelineItems(msgs []Message) []json.RawMessage {
	if len(msgs) == 0 {
		return nil
	}
	items := make([]json.RawMessage, 0, len(msgs))
	for _, m := range msgs {
		raw, err := json.Marshal(map[string]any{
			"kind":    "message",
			"role":    m.Role,
			"content": map[string]any{"text": m.Content},
		})
		if err != nil {
			continue
		}
		items = append(items, raw)
	}
	return items
}

// --- MCP servers ---

type MCPServer struct {
	ID        string            `json:"id,omitempty"`
	ProjectID string            `json:"projectId,omitempty"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Enabled   bool              `json:"enabled"`
	ToolCount int               `json:"toolCount,omitempty"`
	Tools     []MCPTool         `json:"tools,omitempty"`

	// SecretEnvKeys / SecretHeadersKeys name the env/header entries whose
	// values memory stores write-only: responses carry the keys but omit the
	// values from Env/Headers. Requests list the keys here and send the
	// (possibly blank) values inline in Env/Headers — a listed key with a blank
	// value keeps the stored secret on update.
	SecretEnvKeys     []string `json:"secretEnvKeys,omitempty"`
	SecretHeadersKeys []string `json:"secretHeadersKeys,omitempty"`

	// RequiredConfig is the server's declared required configuration (from a
	// registry install): the keys that must be set for the server to work.
	// Memory owns this list — the UI renders it but never sends it back.
	RequiredConfig []MCPRequiredConfigKey `json:"requiredConfig,omitempty"`
	// NeedsConfiguration is true when one or more required keys are blank.
	NeedsConfiguration bool `json:"needsConfiguration,omitempty"`
	// MissingConfigKeys names the required keys whose values are blank.
	MissingConfigKeys []string `json:"missingConfigKeys,omitempty"`

	// RegistryName/RegistryRepositoryURL identify the registry entry a server was
	// installed from (empty for manual creates). RepositoryURL backs the
	// configure form's "where to get a token" link.
	RegistryName          string `json:"registryName,omitempty"`
	RegistryRepositoryURL string `json:"registryRepositoryUrl,omitempty"`

	// Target selects the delivery mode: "server" (connected outbound by the
	// shared host) or "device" (pushed over the relay to a connector). Absent
	// on older records; treated as outbound for rendering.
	Target string `json:"target,omitempty"`
	// TargetInstanceID is the relay instance_id a device row is bound to;
	// empty means every eligible device of the project (broadcast).
	TargetInstanceID string `json:"targetInstanceId,omitempty"`

	// OAuth is the server's OAuth connection summary (present only for remote
	// http/sse servers that participate in OAuth connect; absent for
	// device-targeted and stdio servers). It never carries token values.
	OAuth *MCPServerOAuth `json:"oauth,omitempty"`
}

// MCPRequiredConfigKey is one declared required configuration entry on an MCP
// server (a registry transport's header or env var marked required or secret).
// Kind is "header" or "env". Description is the registry's own guidance for the
// key; ValueHint is the registry's value template (header transports only),
// rendered as an input placeholder so the user knows the expected shape.
type MCPRequiredConfigKey struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	IsSecret    bool   `json:"isSecret"`
	IsRequired  bool   `json:"isRequired"`
	Description string `json:"description,omitempty"`
	ValueHint   string `json:"valueHint,omitempty"`
}

// MCPTool is one tool exposed by an MCP server (the agent tools picker's
// source of truth for what an agent may whitelist).
type MCPTool struct {
	ID          string `json:"id,omitempty"`
	ServerID    string `json:"serverId,omitempty"`
	ToolName    string `json:"toolName"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

func (m *MemoryClient) ListMCPServers(ctx context.Context) ([]MCPServer, error) {
	var env successEnvelope[[]MCPServer]
	if err := m.do(ctx, http.MethodGet, "/api/admin/mcp-servers", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

func (m *MemoryClient) GetMCPServer(ctx context.Context, id string) (*MCPServer, error) {
	var env successEnvelope[MCPServer]
	if err := m.do(ctx, http.MethodGet, "/api/admin/mcp-servers/"+id, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// MCPImportResult mirrors memory's ImportMCPServersResultDTO.
type MCPImportResult struct {
	Created []string           `json:"created"`
	Updated []string           `json:"updated"`
	Skipped []MCPImportSkip    `json:"skipped"`
	Failed  []MCPImportFailure `json:"failed"`
	DryRun  bool               `json:"dryRun"`
}

// MCPImportSkip names one entry left untouched and why.
type MCPImportSkip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// MCPImportFailure names one entry that could not be imported.
type MCPImportFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// ImportMCPServers posts a Claude-format config document to memory's import
// endpoint. target is "auto", "server", or "device"; instanceID binds device
// entries to one connector.
func (m *MemoryClient) ImportMCPServers(ctx context.Context, config json.RawMessage, target, instanceID string, overwrite, dryRun bool) (*MCPImportResult, error) {
	body := struct {
		Config           json.RawMessage `json:"config"`
		Target           string          `json:"target,omitempty"`
		TargetInstanceID string          `json:"targetInstanceId,omitempty"`
		Overwrite        bool            `json:"overwrite,omitempty"`
		DryRun           bool            `json:"dryRun,omitempty"`
	}{
		Config:           config,
		Target:           target,
		TargetInstanceID: instanceID,
		Overwrite:        overwrite,
		DryRun:           dryRun,
	}
	var env successEnvelope[MCPImportResult]
	if err := m.do(ctx, http.MethodPost, "/api/admin/mcp-servers/import", body, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) CreateMCPServer(ctx context.Context, in *MCPServer) (*MCPServer, error) {
	var env successEnvelope[MCPServer]
	if err := m.do(ctx, http.MethodPost, "/api/admin/mcp-servers", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) UpdateMCPServer(ctx context.Context, id string, in *MCPServer) (*MCPServer, error) {
	var env successEnvelope[MCPServer]
	if err := m.do(ctx, http.MethodPatch, "/api/admin/mcp-servers/"+id, in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) DeleteMCPServer(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, "/api/admin/mcp-servers/"+id, nil, nil)
}

// TestMCPServer runs a save-less connection probe against a draft server
// configuration (the create DTO fields plus an optional serverId, used to
// backfill stored secrets left blank in edit mode). Memory always answers 200;
// a connection failure comes back inside the result (Status "error", Error set).
func (m *MemoryClient) TestMCPServer(ctx context.Context, in *MCPServer, serverID string) (*MCPInspectResult, error) {
	body := any(in)
	if serverID != "" {
		body = struct {
			*MCPServer
			ServerID string `json:"serverId"`
		}{MCPServer: in, ServerID: serverID}
	}
	var env successEnvelope[MCPInspectResult]
	if err := m.do(ctx, http.MethodPost, "/api/admin/mcp-servers/test", body, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// --- models (catalog) ---

type Model struct {
	ID              string `json:"id"`
	Provider        string `json:"provider"`
	ModelName       string `json:"modelName"`
	ModelType       string `json:"modelType"`
	DisplayName     string `json:"displayName"`
	MaxOutputTokens int    `json:"maxOutputTokens"`
	MaxInputTokens  int    `json:"maxInputTokens"`
}

func (m *MemoryClient) ListModels(ctx context.Context) ([]Model, error) {
	var out []Model
	if err := m.do(ctx, http.MethodGet, "/api/v1/models", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
