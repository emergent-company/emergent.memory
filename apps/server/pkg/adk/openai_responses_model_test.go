package adk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// responsesCaptured is the raw Responses request the fake server observed.
type responsesCaptured struct {
	path   string
	auth   string
	body   responsesRequest
	rawMap map[string]any
}

// newResponsesServer returns an httptest server that records the incoming
// /responses request and answers with the supplied JSON body.
func newResponsesServer(t *testing.T, responseJSON string, cap *responsesCaptured) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.path = r.URL.Path
		cap.auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &cap.body)
		_ = json.Unmarshal(raw, &cap.rawMap)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseJSON))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestOpenAIResponsesModel_RequestShape asserts the adapter POSTs to /responses
// with the Responses input/message shape and maps the system instruction to the
// `instructions` field (not a system message).
func TestOpenAIResponsesModel_RequestShape(t *testing.T) {
	const responseJSON = `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`
	var cap responsesCaptured
	srv := newResponsesServer(t, responseJSON, &cap)

	m := NewOpenAIResponsesModel(srv.URL+"/v1", "sk-test", "gpt-5", AuthBearer)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Say hello"}}},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "be terse"}}},
			MaxOutputTokens:   64,
		},
	}

	resp, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "/v1/responses", cap.path, "must POST to {base}/responses")
	assert.Equal(t, "Bearer sk-test", cap.auth)
	assert.Equal(t, "gpt-5", cap.body.Model)
	assert.Equal(t, "be terse", cap.body.Instructions)
	assert.EqualValues(t, 64, cap.body.MaxOutputTokens)

	require.Len(t, cap.body.Input, 1)
	item := cap.body.Input[0]
	assert.Equal(t, "message", item.Type)
	assert.Equal(t, "user", item.Role)
	require.Len(t, item.Content, 1)
	assert.Equal(t, "input_text", item.Content[0].Type)
	assert.Equal(t, "Say hello", item.Content[0].Text)

	require.Len(t, resp.Content.Parts, 1)
	assert.Equal(t, "hello", resp.Content.Parts[0].Text)
	require.NotNil(t, resp.UsageMetadata)
	assert.EqualValues(t, 3, resp.UsageMetadata.PromptTokenCount)
	assert.EqualValues(t, 1, resp.UsageMetadata.CandidatesTokenCount)
	assert.EqualValues(t, 4, resp.UsageMetadata.TotalTokenCount)
}

// TestOpenAIResponsesModel_ParsesReasoningAndToolCalls verifies the Responses
// output items (reasoning summary, assistant message, function_call) are all
// surfaced as ADK parts.
func TestOpenAIResponsesModel_ParsesReasoningAndToolCalls(t *testing.T) {
	const responseJSON = `{"output":[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"pondering"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"calling tool"}]},
		{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}
	]}`
	var cap responsesCaptured
	srv := newResponsesServer(t, responseJSON, &cap)

	m := NewOpenAIResponsesModel(srv.URL, "", "gpt-5", AuthBearer)
	req := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "weather?"}}}},
	}
	resp, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Content.Parts, 3)

	assert.True(t, resp.Content.Parts[0].Thought)
	assert.Equal(t, "pondering", resp.Content.Parts[0].Text)
	assert.Equal(t, "calling tool", resp.Content.Parts[1].Text)
	require.NotNil(t, resp.Content.Parts[2].FunctionCall)
	assert.Equal(t, "call_1", resp.Content.Parts[2].FunctionCall.ID)
	assert.Equal(t, "get_weather", resp.Content.Parts[2].FunctionCall.Name)
	assert.Equal(t, "Paris", resp.Content.Parts[2].FunctionCall.Args["city"])
}

// TestOpenAIResponsesModel_ToolsAndReasoningEffort is the core #1455 assertion:
// the Responses path sends `tools` and a graded reasoning effort in the SAME
// request — which Chat Completions rejects.
func TestOpenAIResponsesModel_ToolsAndReasoningEffort(t *testing.T) {
	const responseJSON = `{"output":[{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{}"}]}`
	var cap responsesCaptured
	srv := newResponsesServer(t, responseJSON, &cap)

	m := NewOpenAIResponsesModel(srv.URL, "sk-test", "gpt-5", AuthBearer)
	eff, ok := m.(ReasoningEffortConfigurator)
	require.True(t, ok, "adapter must implement ReasoningEffortConfigurator")
	eff.SetReasoningEffort("max")

	req := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "look it up"}}}},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{{
				FunctionDeclarations: []*genai.FunctionDeclaration{{
					Name:        "lookup",
					Description: "look up a value",
					ParametersJsonSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"q": map[string]any{"type": "string"}},
					},
				}},
			}},
		},
	}
	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)

	// tools present, in the flat Responses shape.
	require.Len(t, cap.body.Tools, 1)
	assert.Equal(t, "function", cap.body.Tools[0].Type)
	assert.Equal(t, "lookup", cap.body.Tools[0].Name)
	assert.NotNil(t, cap.body.Tools[0].Parameters)

	// graded reasoning effort present in the same request.
	require.NotNil(t, cap.body.Reasoning, "reasoning must be sent alongside tools")
	assert.Equal(t, "max", cap.body.Reasoning.Effort)

	// and it is a top-level `reasoning.effort`, not a nested chat field.
	reasoning, ok := cap.rawMap["reasoning"].(map[string]any)
	require.True(t, ok, "wire body must carry a reasoning object")
	assert.Equal(t, "max", reasoning["effort"])
	assert.Contains(t, cap.rawMap, "tools")
}

// TestOpenAIResponsesModel_OrphanToolCallPairing verifies a function_call with
// no matching output gets a synthetic function_call_output so the Responses
// conversation stays valid.
func TestOpenAIResponsesModel_OrphanToolCallPairing(t *testing.T) {
	const responseJSON = `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`
	var cap responsesCaptured
	srv := newResponsesServer(t, responseJSON, &cap)

	m := NewOpenAIResponsesModel(srv.URL, "", "gpt-5", AuthBearer)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
				ID: "call_orphan", Name: "do_thing", Args: map[string]any{"x": 1},
			}}}},
			// no matching FunctionResponse
		},
	}
	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)

	var call, output *responsesInputItem
	for i := range cap.body.Input {
		switch cap.body.Input[i].Type {
		case "function_call":
			call = &cap.body.Input[i]
		case "function_call_output":
			output = &cap.body.Input[i]
		}
	}
	require.NotNil(t, call, "function_call item must be present")
	require.NotNil(t, output, "orphan function_call must get a synthetic function_call_output")
	assert.Equal(t, "call_orphan", output.CallID)
}

// TestOpenAIResponsesModel_CarriesReasoningIDOntoFunctionCall verifies the
// opaque reasoning item id returned by the Responses API is preserved on the
// following function_call part (via PartMetadata) so it can be replayed (#1463).
func TestOpenAIResponsesModel_CarriesReasoningIDOntoFunctionCall(t *testing.T) {
	const responseJSON = `{"output":[
		{"type":"reasoning","id":"rs_abc","summary":[{"type":"summary_text","text":"pondering"}]},
		{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}
	]}`
	var cap responsesCaptured
	srv := newResponsesServer(t, responseJSON, &cap)

	m := NewOpenAIResponsesModel(srv.URL, "", "gpt-5", AuthBearer)
	resp, err := collectResponse(m.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "weather?"}}}},
	}, false))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Content.Parts, 2)

	// reasoning summary is still surfaced as a Thought part.
	require.True(t, resp.Content.Parts[0].Thought)
	assert.Equal(t, "pondering", resp.Content.Parts[0].Text)

	// the function_call part carries the opaque reasoning item id.
	fc := resp.Content.Parts[1]
	require.NotNil(t, fc.FunctionCall)
	require.NotNil(t, fc.PartMetadata)
	assert.Equal(t, "rs_abc", fc.PartMetadata[responsesReasoningIDMetadataKey])
	assert.Equal(t, "pondering", fc.PartMetadata[responsesReasoningSummaryMetadataKey])
}

// TestOpenAIResponsesModel_ReplaysReasoningBeforeFunctionCall is the core #1463
// assertion: a function_call replayed from history is immediately preceded by
// its reasoning item in `input`. Without the replay, multi-step Responses tool
// loops lose reasoning state (and can 400 on a dangling/short-circuited pair).
func TestOpenAIResponsesModel_ReplaysReasoningBeforeFunctionCall(t *testing.T) {
	// Step 1: a reasoning model returns reasoning + function_call.
	const turn1JSON = `{"output":[
		{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"need weather"}]},
		{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}
	]}`
	var cap1 responsesCaptured
	srv1 := newResponsesServer(t, turn1JSON, &cap1)
	m1 := NewOpenAIResponsesModel(srv1.URL, "", "gpt-5", AuthBearer)
	resp1, err := collectResponse(m1.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "weather?"}}}},
	}, false))
	require.NoError(t, err)
	require.NotNil(t, resp1)

	// Step 2: replay the assistant turn (reasoning + function_call) plus the
	// tool output on the next request.
	const turn2JSON = `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"18C"}]}]}`
	var cap2 responsesCaptured
	srv2 := newResponsesServer(t, turn2JSON, &cap2)
	m2 := NewOpenAIResponsesModel(srv2.URL, "", "gpt-5", AuthBearer)

	_, err = collectResponse(m2.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "weather?"}}},
			resp1.Content,
			{Role: "tool", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
				ID: "call_1", Name: "get_weather", Response: map[string]any{"temp": 18},
			}}}},
		},
	}, false))
	require.NoError(t, err)

	reasoningIdx, callIdx := -1, -1
	for i, item := range cap2.body.Input {
		switch item.Type {
		case "reasoning":
			reasoningIdx = i
			assert.Equal(t, "rs_1", item.ID)
			require.Len(t, item.Summary, 1)
			assert.Equal(t, "need weather", item.Summary[0].Text)
		case "function_call":
			if item.CallID == "call_1" {
				callIdx = i
			}
		}
	}
	require.NotEqual(t, -1, reasoningIdx, "reasoning item must be replayed")
	require.NotEqual(t, -1, callIdx, "function_call must be replayed")
	assert.Equal(t, callIdx, reasoningIdx+1, "reasoning item must immediately precede its function_call")
}

// TestOpenAIResponsesModel_RefusalContent verifies an assistant refusal (the
// Responses message content part {"type":"refusal","refusal":...}) is surfaced
// as text rather than dropped — a refusal-only response must not error, and a
// refusal mixed with output_text must not lose the refusal.
func TestOpenAIResponsesModel_RefusalContent(t *testing.T) {
	const responseJSON = `{"output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Sure: "},{"type":"refusal","refusal":"I cannot reveal that."}]}]}`
	var cap responsesCaptured
	srv := newResponsesServer(t, responseJSON, &cap)

	m := NewOpenAIResponsesModel(srv.URL, "", "gpt-5", AuthBearer)
	resp, err := collectResponse(m.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "secret?"}}}},
	}, false))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content.Parts)
	assert.Equal(t, "Sure: I cannot reveal that.", resp.Content.Parts[0].Text)

	// A refusal-only response must yield the refusal, not a spurious error.
	const refusalOnlyJSON = `{"output":[{"type":"message","id":"msg_2","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"I cannot help with that."}]}]}`
	var cap2 responsesCaptured
	srv2 := newResponsesServer(t, refusalOnlyJSON, &cap2)
	m2 := NewOpenAIResponsesModel(srv2.URL, "", "gpt-5", AuthBearer)
	resp2, err := collectResponse(m2.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "nope"}}}},
	}, false))
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.NotEmpty(t, resp2.Content.Parts)
	assert.Equal(t, "I cannot help with that.", resp2.Content.Parts[0].Text)
}

// TestOpenAIResponsesModel_RequestsEncryptedContent verifies the adapter asks
// for `reasoning.encrypted_content` so stateless/ZDR (`store:false`) orgs get
// the opaque blob required to replay reasoning.
func TestOpenAIResponsesModel_RequestsEncryptedContent(t *testing.T) {
	const responseJSON = `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}`
	var cap responsesCaptured
	srv := newResponsesServer(t, responseJSON, &cap)

	m := NewOpenAIResponsesModel(srv.URL, "", "gpt-5", AuthBearer)
	_, err := collectResponse(m.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}},
	}, false))
	require.NoError(t, err)

	assert.Contains(t, cap.body.Include, "reasoning.encrypted_content",
		"request must include reasoning.encrypted_content for ZDR replay")
}

// TestOpenAIResponsesModel_ReplaysEncryptedContentNotID is the ZDR regression:
// a reasoning item returned under `store:false` carries encrypted_content, and
// the replay must use that blob instead of the (unresolvable) id. Replaying an
// id-only reasoning item under ZDR is rejected with 404 "Items are not persisted
// when `store` is set to false".
func TestOpenAIResponsesModel_ReplaysEncryptedContentNotID(t *testing.T) {
	const turn1JSON = `{"output":[
		{"type":"reasoning","id":"rs_zdr","summary":[{"type":"summary_text","text":"need weather"}],"encrypted_content":"gAAAA-opaque-zdr-blob"},
		{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}
	]}`
	var cap1 responsesCaptured
	srv1 := newResponsesServer(t, turn1JSON, &cap1)
	m1 := NewOpenAIResponsesModel(srv1.URL, "", "gpt-5", AuthBearer)
	resp1, err := collectResponse(m1.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "weather?"}}}},
	}, false))
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.Len(t, resp1.Content.Parts, 2)

	// The blob must be carried on the function_call part's metadata so it
	// survives session persistence.
	fc := resp1.Content.Parts[1]
	require.NotNil(t, fc.PartMetadata)
	assert.Equal(t, "gAAAA-opaque-zdr-blob", fc.PartMetadata[responsesReasoningEncryptedMetadataKey])
	assert.Equal(t, "rs_zdr", fc.PartMetadata[responsesReasoningIDMetadataKey])

	// Replay: reasoning item must carry encrypted_content and MUST NOT carry the
	// id (an id under store:false is a dangling handle the API rejects).
	const turn2JSON = `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"18C"}]}]}`
	var cap2 responsesCaptured
	srv2 := newResponsesServer(t, turn2JSON, &cap2)
	m2 := NewOpenAIResponsesModel(srv2.URL, "", "gpt-5", AuthBearer)
	_, err = collectResponse(m2.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "weather?"}}},
			resp1.Content,
			{Role: "tool", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
				ID: "call_1", Name: "get_weather", Response: map[string]any{"temp": 18},
			}}}},
		},
	}, false))
	require.NoError(t, err)

	reasoningIdx, callIdx := -1, -1
	for i, item := range cap2.body.Input {
		switch item.Type {
		case "reasoning":
			reasoningIdx = i
			assert.Equal(t, "gAAAA-opaque-zdr-blob", item.EncryptedContent,
				"replay must carry the encrypted blob")
			assert.Empty(t, item.ID, "id must not be replayed when encrypted_content is available")
			require.Len(t, item.Summary, 1)
			assert.Equal(t, "need weather", item.Summary[0].Text)
		case "function_call":
			if item.CallID == "call_1" {
				callIdx = i
			}
		}
	}
	require.NotEqual(t, -1, reasoningIdx, "reasoning item must be replayed")
	require.NotEqual(t, -1, callIdx, "function_call must be replayed")
	assert.Equal(t, callIdx, reasoningIdx+1, "reasoning item must immediately precede its function_call")
}

// TestOpenAIResponsesModel_ReplaysEncryptedContentWithoutID covers a reasoning
// item that has no id at all (only encrypted_content). The replay must still be
// emitted, keyed off the encrypted blob.
func TestOpenAIResponsesModel_ReplaysEncryptedContentWithoutID(t *testing.T) {
	const turn1JSON = `{"output":[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}],"encrypted_content":"blob-only"},
		{"type":"function_call","call_id":"call_9","name":"noop","arguments":"{}"}
	]}`
	var cap1 responsesCaptured
	srv1 := newResponsesServer(t, turn1JSON, &cap1)
	m1 := NewOpenAIResponsesModel(srv1.URL, "", "gpt-5", AuthBearer)
	resp1, err := collectResponse(m1.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "go"}}}},
	}, false))
	require.NoError(t, err)
	require.Len(t, resp1.Content.Parts, 2)
	fc := resp1.Content.Parts[1]
	require.NotNil(t, fc.PartMetadata)
	assert.Equal(t, "blob-only", fc.PartMetadata[responsesReasoningEncryptedMetadataKey])
	_, hasID := fc.PartMetadata[responsesReasoningIDMetadataKey]
	assert.False(t, hasID, "no id metadata key when the reasoning item had no id")

	const turn2JSON = `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`
	var cap2 responsesCaptured
	srv2 := newResponsesServer(t, turn2JSON, &cap2)
	m2 := NewOpenAIResponsesModel(srv2.URL, "", "gpt-5", AuthBearer)
	_, err = collectResponse(m2.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "go"}}},
			resp1.Content,
			{Role: "tool", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
				ID: "call_9", Name: "noop", Response: map[string]any{},
			}}}},
		},
	}, false))
	require.NoError(t, err)

	found := false
	for i, item := range cap2.body.Input {
		if item.Type == "reasoning" {
			found = true
			assert.Equal(t, "blob-only", item.EncryptedContent)
			assert.Empty(t, item.ID)
			if i+1 < len(cap2.body.Input) {
				assert.Equal(t, "call_9", cap2.body.Input[i+1].CallID, "must be contiguous with its call")
			}
		}
	}
	require.True(t, found, "reasoning with encrypted_content but no id must still be replayed")
}
