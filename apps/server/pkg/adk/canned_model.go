package adk

import (
	"context"
	"iter"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// testLLMResponseText is the fixed content returned by the deterministic test
// model. It is stable across runs so e2e assertions never depend on a live
// provider.
const testLLMResponseText = "test-llm deterministic response"

// TestLLMResponse is one scripted answer for the deterministic test model. When
// Partials is non-empty its chunks are yielded first as partial responses, so an
// integration test can drive the executor's progressive-streaming seams (a real
// model streams token chunks before its final response); Final is then yielded
// as the non-partial final response.
type TestLLMResponse struct {
	Partials []string
	Final    string
}

func defaultTestLLMResponse() TestLLMResponse {
	return TestLLMResponse{Final: testLLMResponseText}
}

// testModel is a deterministic model.LLM that never calls a real provider.
// It is returned by CreateModel/CreateModelWithName when the project has the
// test_llm feature flag enabled.
type testModel struct {
	name string
	resp TestLLMResponse
}

func newTestModel(name string, resp TestLLMResponse) *testModel {
	if resp.Final == "" && len(resp.Partials) == 0 {
		resp = defaultTestLLMResponse()
	}
	return &testModel{name: name, resp: resp}
}

// Name implements model.LLM.
func (m *testModel) Name() string {
	return m.name
}

// GenerateContent implements model.LLM, yielding the configured partial chunks
// (if any) and then the fixed final text. The stream argument is ignored: both
// streaming and non-streaming calls receive the same deterministic response.
func (m *testModel) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		for _, chunk := range m.resp.Partials {
			if !yield(&model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: chunk}},
				},
				Partial: true,
			}, nil) {
				return
			}
		}
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: m.resp.Final}},
			},
			Partial: false,
		}, nil)
	}
}
