package adk

import (
	"context"
	"testing"

	"google.golang.org/adk/v2/model"
)

func TestTestModelGenerateContent(t *testing.T) {
	m := newTestModel("test-llm")
	if m.Name() != "test-llm" {
		t.Fatalf("Name() = %q, want test-llm", m.Name())
	}

	collect := func(stream bool) []*model.LLMResponse {
		var out []*model.LLMResponse
		for resp, err := range m.GenerateContent(context.Background(), &model.LLMRequest{}, stream) {
			if err != nil {
				t.Fatalf("GenerateContent error: %v", err)
			}
			out = append(out, resp)
		}
		return out
	}

	for _, stream := range []bool{false, true} {
		resps := collect(stream)
		if len(resps) != 1 {
			t.Fatalf("stream=%v: got %d responses, want 1", stream, len(resps))
		}
		resp := resps[0]
		if resp.Content == nil || len(resp.Content.Parts) != 1 {
			t.Fatalf("stream=%v: unexpected content shape: %+v", stream, resp.Content)
		}
		if got := resp.Content.Parts[0].Text; got != testLLMResponseText {
			t.Fatalf("stream=%v: text = %q, want %q", stream, got, testLLMResponseText)
		}
		if resp.Partial {
			t.Fatalf("stream=%v: expected non-partial response", stream)
		}
	}
}
