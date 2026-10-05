// Package adk provides Google ADK-Go integration for agent workflows.
package adk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// protocolOpenAIResponses is the wire-protocol tag for OpenAI's Responses API.
// It mirrors domain/provider.ProtocolOpenAIResponses as a plain string so
// pkg/adk can reference it without importing domain/provider (import cycle).
const protocolOpenAIResponses = "openai-responses"

// Opaque OpenAI Responses reasoning-item metadata keys stored on the generated
// function_call part's genai.PartMetadata. genai.Content (and its parts) is
// JSON-persisted by the ADK session, so these survive across steps and runs and
// let a replayed function_call be preceded by the reasoning item that produced
// it. OpenAI requires a reasoning item and its following function_call to stay
// contiguous in `input`, and rejects a dangling reasoning item with HTTP 400.
const (
	responsesReasoningIDMetadataKey      = "openai_responses_reasoning_id"
	responsesReasoningSummaryMetadataKey = "openai_responses_reasoning_summary"
	// responsesReasoningEncryptedMetadataKey carries the opaque
	// `encrypted_content` blob from a reasoning output item. Under ZDR
	// (`store:false`, which an org can force) reasoning items are not
	// persisted server-side, so an id-only replay is rejected with HTTP 404
	// ("Item with id 'rs_...' not found. Items are not persisted when `store`
	// is set to false."). Replaying the encrypted blob instead preserves
	// reasoning without server-side storage.
	responsesReasoningEncryptedMetadataKey = "openai_responses_reasoning_encrypted_content"
)

// FirstPartyOpenAIResponseHosts are the hostnames whose base URLs must use the
// OpenAI Responses API instead of Chat Completions. Any other host — a relay,
// proxy, or local server — keeps Chat Completions. It is the single source of
// truth shared by the env-var fallback here and the registry definition
// (domain/provider references this list through ProviderDefinition.ResponsesHosts).
var FirstPartyOpenAIResponseHosts = []string{"api.openai.com"}

// SelectOpenAIProtocol returns the effective protocol string for an
// OpenAI-compatible base URL: "openai-responses" for a first-party host,
// "openai-chat" otherwise. Exposed so the registry and env-var paths share one
// host rule.
func SelectOpenAIProtocol(baseURL string) string {
	if HostMatchesAny(baseURL, FirstPartyOpenAIResponseHosts) {
		return protocolOpenAIResponses
	}
	return "openai-chat"
}

// HostMatchesAny reports whether baseURL's hostname equals (case-insensitively)
// any entry in hosts. It tolerates scheme-less inputs such as
// "api.openai.com/v1". A nil/empty hosts list, an empty base URL, or an
// unparseable URL returns false.
func HostMatchesAny(baseURL string, hosts []string) bool {
	if baseURL == "" || len(hosts) == 0 {
		return false
	}
	host := hostnameOf(baseURL)
	if host == "" {
		return false
	}
	for _, h := range hosts {
		if h == "" {
			continue
		}
		if strings.EqualFold(host, strings.TrimSuffix(h, ".")) {
			return true
		}
	}
	return false
}

// hostnameOf extracts the hostname from a base URL, tolerating a missing
// scheme by retrying with an https:// prefix.
func hostnameOf(baseURL string) string {
	if u, err := url.Parse(baseURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	if u, err := url.Parse("https://" + baseURL); err == nil {
		return u.Hostname()
	}
	return ""
}

// openaiResponsesModel implements model.LLM using OpenAI's Responses API
// (POST {baseURL}/responses). It exists because the newest OpenAI reasoning
// models reject `tools` combined with a graded reasoning effort on
// /v1/chat/completions, and some effort levels ("max") are Responses-only.
type openaiResponsesModel struct {
	baseURL   string
	apiKey    string
	modelName string
	auth      AuthStyle
	client    *http.Client
	// reasoningEffort is the graded reasoning effort sent as
	// {"reasoning":{"effort":...}}. Empty means the field is omitted.
	reasoningEffort string
}

// ReasoningEffortConfigurator is implemented by models that support a graded
// reasoning-effort setting (Responses-only levels such as "max").
type ReasoningEffortConfigurator interface {
	SetReasoningEffort(effort string)
}

// NewOpenAIResponsesModel creates a model that speaks the OpenAI Responses
// protocol. auth selects the credential-injection style (bearer for
// api.openai.com).
func NewOpenAIResponsesModel(baseURL, apiKey, modelName string, auth AuthStyle) model.LLM {
	return &openaiResponsesModel{
		baseURL:   strings.TrimSuffix(baseURL, "/"),
		apiKey:    apiKey,
		modelName: modelName,
		auth:      auth,
		client:    &http.Client{Timeout: 900 * time.Second},
	}
}

// SetReasoningEffort implements ReasoningEffortConfigurator. Pass "" to omit
// the reasoning field.
func (m *openaiResponsesModel) SetReasoningEffort(effort string) {
	m.reasoningEffort = effort
}

// Name returns the model name.
func (m *openaiResponsesModel) Name() string {
	return m.modelName
}

// --- Responses wire types ---

type responsesRequest struct {
	Model           string               `json:"model"`
	Instructions    string               `json:"instructions,omitempty"`
	Input           []responsesInputItem `json:"input"`
	Tools           []responsesTool      `json:"tools,omitempty"`
	ToolChoice      string               `json:"tool_choice,omitempty"`
	MaxOutputTokens int32                `json:"max_output_tokens,omitempty"`
	Reasoning       *responsesReasoning  `json:"reasoning,omitempty"`
	Text            *responsesTextConfig `json:"text,omitempty"`
	// Include requests extra output fields. "reasoning.encrypted_content" makes
	// the API return the opaque reasoning blob needed for stateless/ZDR replay.
	// It is accepted independently of `store` (a no-op on recent API versions,
	// which populate encrypted_content by default) and is harmless for
	// non-reasoning models, which emit no reasoning items.
	Include []string `json:"include,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesTextConfig struct {
	Format *responsesTextFormat `json:"format,omitempty"`
}

type responsesTextFormat struct {
	Type string `json:"type"`
}

// responsesInputItem is the union of the Responses API input item shapes:
// message, reasoning, function_call, and function_call_output.
type responsesInputItem struct {
	Type    string                 `json:"type,omitempty"`
	Role    string                 `json:"role,omitempty"`
	Content []responsesContentPart `json:"content,omitempty"`
	// reasoning: the opaque item id returned by a previous response, replayed
	// so a function_call keeps its reasoning continuity. Summary is optional
	// (models only populate it when reasoning.summary is requested).
	ID      string                 `json:"id,omitempty"`
	Summary []responsesContentPart `json:"summary,omitempty"`
	// EncryptedContent is the opaque, stateless reasoning blob. When present it
	// is replayed instead of ID (the id is a server-side handle that does not
	// resolve under `store:false`/ZDR).
	EncryptedContent string `json:"encrypted_content,omitempty"`
	// function_call / function_call_output
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type responsesContentPart struct {
	Type string `json:"type"` // "input_text" or "output_text"
	Text string `json:"text"`
}

// responsesTool is the flat Responses function-tool shape (the Chat Completions
// protocol nests these under a "function" object).
type responsesTool struct {
	Type        string `json:"type"` // "function"
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type responsesResponse struct {
	Output []responsesOutputItem `json:"output"`
	Usage  *responsesUsage       `json:"usage,omitempty"`
	Error  *responsesAPIError    `json:"error,omitempty"`
}

type responsesOutputItem struct {
	Type string `json:"type"`
	// ID is the opaque item id. Reasoning items carry one (e.g. "rs_...") that
	// must be replayed alongside the function_call it preceded.
	ID string `json:"id,omitempty"`
	// EncryptedContent is the opaque reasoning blob returned for stateless
	// replay (populated under ZDR / `store:false`). See responsesInputItem.
	EncryptedContent string `json:"encrypted_content,omitempty"`
	Role             string `json:"role,omitempty"`
	Content          []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		// Refusal is set when the assistant refused to answer (the OpenAI
		// Responses message content part {"type":"refusal","refusal":...}).
		// It is surfaced as text so a legitimate refusal is never dropped or
		// mistaken for an empty response.
		Refusal string `json:"refusal,omitempty"`
	} `json:"content,omitempty"`
	Summary []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type responsesUsage struct {
	InputTokens  int32 `json:"input_tokens"`
	OutputTokens int32 `json:"output_tokens"`
	TotalTokens  int32 `json:"total_tokens"`
}

type responsesAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// buildResponsesTools converts genai.Tool declarations to the flat Responses
// tool format.
func buildResponsesTools(tools []*genai.Tool) []responsesTool {
	var result []responsesTool
	for _, t := range tools {
		for _, fd := range t.FunctionDeclarations {
			rt := responsesTool{
				Type:        "function",
				Name:        fd.Name,
				Description: fd.Description,
			}
			if fd.ParametersJsonSchema != nil {
				rt.Parameters = fd.ParametersJsonSchema
			} else if fd.Parameters != nil {
				rt.Parameters = fd.Parameters
			} else {
				rt.Parameters = map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				}
			}
			result = append(result, rt)
		}
	}
	return result
}

// buildResponsesInput converts ADK content history to Responses input items.
// Reasoning summary text is not replayed as a message (the Responses API does
// not accept assistant reasoning as text), but the opaque reasoning item id
// carried on each function_call part is replayed as a `reasoning` input item
// immediately before that call: OpenAI requires a reasoning item and its
// function_call to stay contiguous, and dropping it breaks multi-step tool
// loops (#1463).
func buildResponsesInput(contents []*genai.Content) []responsesInputItem {
	var items []responsesInputItem
	for _, content := range contents {
		role := mapRole(content.Role)

		var textParts []string
		var calls []responsesInputItem
		var outputs []responsesInputItem

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Thought && part.Text != "" {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.FunctionCall != nil {
				fc := part.FunctionCall
				id := fc.ID
				if id == "" {
					id = "call_" + fc.Name
				}
				// Replay the reasoning item this call was produced with, directly
				// before it, so the pair stays contiguous on the wire. Prefer the
				// stateless encrypted_content blob over the id: under ZDR
				// (`store:false`) the id is a handle to an item the server never
				// persisted, and replaying it alone is rejected (404).
				rid := reasoningIDFromPart(part)
				renc := reasoningEncryptedFromPart(part)
				if rid != "" || renc != "" {
					ri := responsesInputItem{Type: "reasoning"}
					if renc != "" {
						ri.EncryptedContent = renc
					} else {
						ri.ID = rid
					}
					if summary := reasoningSummaryFromPart(part); summary != "" {
						ri.Summary = []responsesContentPart{{Type: "summary_text", Text: summary}}
					}
					calls = append(calls, ri)
				}
				argsJSON, _ := json.Marshal(fc.Args)
				calls = append(calls, responsesInputItem{
					Type:      "function_call",
					CallID:    id,
					Name:      fc.Name,
					Arguments: string(argsJSON),
				})
			}
			if part.FunctionResponse != nil {
				fr := part.FunctionResponse
				id := fr.ID
				if id == "" {
					id = "call_" + fr.Name
				}
				resultJSON, _ := json.Marshal(fr.Response)
				outputs = append(outputs, responsesInputItem{
					Type:   "function_call_output",
					CallID: id,
					Output: string(resultJSON),
				})
			}
		}

		if len(textParts) > 0 {
			items = append(items, responsesMessageItem(role, textParts))
		}
		items = append(items, calls...)
		items = append(items, outputs...)
	}
	return ensureResponsesCallOutputPairs(items)
}

// responsesMessageItem builds a message input item, using output_text for
// assistant turns and input_text for user/system turns.
func responsesMessageItem(role string, texts []string) responsesInputItem {
	partType := "input_text"
	if role == "assistant" {
		partType = "output_text"
	}
	parts := make([]responsesContentPart, 0, len(texts))
	for _, t := range texts {
		parts = append(parts, responsesContentPart{Type: partType, Text: t})
	}
	return responsesInputItem{Type: "message", Role: role, Content: parts}
}

// ensureResponsesCallOutputPairs injects synthetic function_call_output items
// for any function_call with no matching output, preserving the strict
// call/output pairing the Responses API requires. Orphaned calls can occur when
// ADK session history is reconstructed from persisted events.
func ensureResponsesCallOutputPairs(items []responsesInputItem) []responsesInputItem {
	var callOrder []string
	callSeen := make(map[string]bool)
	outputIDs := make(map[string]bool)
	for _, it := range items {
		switch it.Type {
		case "function_call":
			if !callSeen[it.CallID] {
				callSeen[it.CallID] = true
				callOrder = append(callOrder, it.CallID)
			}
		case "function_call_output":
			outputIDs[it.CallID] = true
		}
	}
	if len(callOrder) == 0 {
		return items
	}
	missing := make(map[string]bool)
	hasOrphans := false
	for _, id := range callOrder {
		if !outputIDs[id] {
			missing[id] = true
			hasOrphans = true
		}
	}
	if !hasOrphans {
		return items
	}
	result := make([]responsesInputItem, 0, len(items)+len(missing))
	for _, it := range items {
		result = append(result, it)
		if it.Type == "function_call" && missing[it.CallID] {
			result = append(result, responsesInputItem{
				Type:   "function_call_output",
				CallID: it.CallID,
				Output: `{"error":"tool response not available","note":"synthetic response inserted to maintain valid conversation structure"}`,
			})
		}
	}
	return result
}

// reasoningIDFromPart returns the opaque reasoning item id carried on a
// function_call part, or "" when the part was not produced by a reasoning item.
func reasoningIDFromPart(part *genai.Part) string {
	if part == nil || part.PartMetadata == nil {
		return ""
	}
	id, _ := part.PartMetadata[responsesReasoningIDMetadataKey].(string)
	return id
}

// reasoningSummaryFromPart returns the reasoning summary text carried alongside
// the id, or "" when none was captured.
func reasoningSummaryFromPart(part *genai.Part) string {
	if part == nil || part.PartMetadata == nil {
		return ""
	}
	summary, _ := part.PartMetadata[responsesReasoningSummaryMetadataKey].(string)
	return summary
}

// reasoningEncryptedFromPart returns the opaque reasoning encrypted_content
// carried on a function_call part, or "" when none was captured.
func reasoningEncryptedFromPart(part *genai.Part) string {
	if part == nil || part.PartMetadata == nil {
		return ""
	}
	enc, _ := part.PartMetadata[responsesReasoningEncryptedMetadataKey].(string)
	return enc
}

// generateContentFromResponsesItems projects Responses output items (reasoning,
// message, function_call) into ADK parts, carrying the opaque reasoning item id
// onto the following function_call part so it can be replayed (#1463).
func generateContentFromResponsesItems(output []responsesOutputItem) []*genai.Part {
	var parts []*genai.Part
	var pendingReasoningID, pendingReasoningSummary, pendingReasoningEncrypted string
	for _, item := range output {
		switch item.Type {
		case "reasoning":
			var summaries []string
			for _, s := range item.Summary {
				if s.Text != "" {
					summaries = append(summaries, s.Text)
				}
			}
			if len(summaries) > 0 {
				parts = append(parts, &genai.Part{
					Text:    strings.Join(summaries, "\n"),
					Thought: true,
				})
			}
			// Remember the item so the next function_call can carry it. OpenAI
			// requires the pair to be contiguous on replay; a reasoning item with
			// no following function_call (a text-only turn) is not replayed.
			// encrypted_content is the stateless carrier that keeps replay
			// working under ZDR (`store:false`), where the id does not resolve.
			if item.ID != "" || item.EncryptedContent != "" {
				pendingReasoningID = item.ID
				pendingReasoningSummary = strings.Join(summaries, "\n")
				pendingReasoningEncrypted = item.EncryptedContent
			}
		case "message":
			var text strings.Builder
			for _, c := range item.Content {
				text.WriteString(c.Text)
				text.WriteString(c.Refusal)
			}
			if text.Len() > 0 {
				parts = append(parts, &genai.Part{Text: text.String()})
			}
		case "function_call":
			var args map[string]any
			if item.Arguments != "" {
				if err := json.Unmarshal([]byte(item.Arguments), &args); err != nil {
					args = map[string]any{"_raw": item.Arguments}
				}
			}
			part := &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   item.CallID,
					Name: item.Name,
					Args: args,
				},
			}
			if pendingReasoningID != "" || pendingReasoningEncrypted != "" {
				part.PartMetadata = map[string]any{}
				if pendingReasoningID != "" {
					part.PartMetadata[responsesReasoningIDMetadataKey] = pendingReasoningID
				}
				if pendingReasoningSummary != "" {
					part.PartMetadata[responsesReasoningSummaryMetadataKey] = pendingReasoningSummary
				}
				if pendingReasoningEncrypted != "" {
					part.PartMetadata[responsesReasoningEncryptedMetadataKey] = pendingReasoningEncrypted
				}
				pendingReasoningID, pendingReasoningSummary, pendingReasoningEncrypted = "", "", ""
			}
			parts = append(parts, part)
		}
	}
	return parts
}

// GenerateContent implements model.LLM by calling the OpenAI Responses API,
// including full function/tool calling support.
func (m *openaiResponsesModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		body := responsesRequest{
			Model:   m.modelName,
			Input:   buildResponsesInput(req.Contents),
			Include: []string{"reasoning.encrypted_content"},
		}

		// System instruction maps to the Responses `instructions` field.
		if req.Config != nil && req.Config.SystemInstruction != nil {
			var siParts []string
			for _, p := range req.Config.SystemInstruction.Parts {
				if p != nil && p.Text != "" {
					siParts = append(siParts, p.Text)
				}
			}
			body.Instructions = strings.Join(siParts, "\n")
		}

		// Attach tool declarations when present. Unlike Chat Completions,
		// Responses accepts tools together with a graded reasoning effort, so
		// tools and reasoning are sent in the same request.
		if req.Config != nil && len(req.Config.Tools) > 0 {
			body.Tools = buildResponsesTools(req.Config.Tools)
			if !isReasonerModel(m.modelName) {
				if hasSubstantiveToolCall(req.Contents) {
					body.ToolChoice = "auto"
				} else {
					body.ToolChoice = "required"
				}
			}
		}

		if req.Config != nil {
			body.MaxOutputTokens = req.Config.MaxOutputTokens
			if req.Config.ResponseMIMEType == "application/json" {
				body.Text = &responsesTextConfig{Format: &responsesTextFormat{Type: "json_object"}}
				if body.Instructions == "" {
					body.Instructions = "Respond in json."
				} else if !strings.Contains(strings.ToLower(body.Instructions), "json") {
					body.Instructions += "\nRespond in json."
				}
			}
		}

		if m.reasoningEffort != "" {
			body.Reasoning = &responsesReasoning{Effort: m.reasoningEffort}
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			yield(nil, fmt.Errorf("openai-responses: failed to marshal request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			m.baseURL+"/responses",
			bytes.NewReader(bodyBytes))
		if err != nil {
			yield(nil, fmt.Errorf("openai-responses: failed to create request: %w", err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if err := ApplyAuth(httpReq, m.auth, m.apiKey); err != nil {
			yield(nil, fmt.Errorf("openai-responses: %w", err))
			return
		}

		resp, err := m.client.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("openai-responses: request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			yield(nil, fmt.Errorf("openai-responses: failed to read response: %w", err))
			return
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			yield(nil, fmt.Errorf("openai-responses: endpoint returned %d (url=%s model=%s req_bytes=%d): %s", resp.StatusCode, m.baseURL, m.modelName, len(bodyBytes), string(respBody)))
			return
		}

		var result responsesResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			yield(nil, fmt.Errorf("openai-responses: failed to decode response: %w", err))
			return
		}
		if result.Error != nil && result.Error.Message != "" {
			yield(nil, fmt.Errorf("openai-responses: API error: %s", result.Error.Message))
			return
		}

		parts := generateContentFromResponsesItems(result.Output)

		if len(parts) == 0 {
			yield(nil, fmt.Errorf("openai-responses: response had no content or tool calls"))
			return
		}

		llmResp := &model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: parts,
			},
			Partial: false,
		}
		if result.Usage != nil {
			llmResp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     result.Usage.InputTokens,
				CandidatesTokenCount: result.Usage.OutputTokens,
				TotalTokenCount:      result.Usage.TotalTokens,
			}
		}
		yield(llmResp, nil)
	}
}
