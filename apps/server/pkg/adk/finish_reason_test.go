package adk

import (
	"testing"

	"google.golang.org/genai"
)

func TestOpenAIFinishReason(t *testing.T) {
	cases := map[string]genai.FinishReason{
		"length":         genai.FinishReasonMaxTokens,
		"max_tokens":     genai.FinishReasonMaxTokens,
		"stop":           genai.FinishReasonStop,
		"content_filter": genai.FinishReasonSafety,
		"":               genai.FinishReasonUnspecified,
		"tool_calls":     genai.FinishReasonOther,
	}
	for in, want := range cases {
		if got := openAIFinishReason(in); got != want {
			t.Errorf("openAIFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResponsesFinishReason(t *testing.T) {
	maxTokens := responsesResponse{
		Status: "incomplete",
		IncompleteDetails: &struct {
			Reason string `json:"reason,omitempty"`
		}{Reason: "max_output_tokens"},
	}
	if got := responsesFinishReason(maxTokens); got != genai.FinishReasonMaxTokens {
		t.Errorf("incomplete max output = %q, want %q", got, genai.FinishReasonMaxTokens)
	}
	if got := responsesFinishReason(responsesResponse{Status: "completed"}); got != genai.FinishReasonStop {
		t.Errorf("completed = %q, want %q", got, genai.FinishReasonStop)
	}
	if got := responsesFinishReason(responsesResponse{}); got != genai.FinishReasonUnspecified {
		t.Errorf("empty = %q, want %q", got, genai.FinishReasonUnspecified)
	}
}

func TestAnthropicFinishReason(t *testing.T) {
	cases := map[string]genai.FinishReason{
		"max_tokens":    genai.FinishReasonMaxTokens,
		"end_turn":      genai.FinishReasonStop,
		"stop_sequence": genai.FinishReasonStop,
		"tool_use":      genai.FinishReasonStop,
		"refusal":       genai.FinishReasonSafety,
		"":              genai.FinishReasonUnspecified,
		"pause_turn":    genai.FinishReasonStop,
	}
	for in, want := range cases {
		if got := anthropicFinishReason(in); got != want {
			t.Errorf("anthropicFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}
