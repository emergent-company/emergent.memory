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

// testModel is a deterministic model.LLM that never calls a real provider.
// It is returned by CreateModel/CreateModelWithName when the project has the
// test_llm feature flag enabled.
type testModel struct {
	name string
}

func newTestModel(name string) *testModel {
	return &testModel{name: name}
}

// Name implements model.LLM.
func (m *testModel) Name() string {
	return m.name
}

// GenerateContent implements model.LLM, yielding a single fixed text part.
// The stream argument is ignored: both streaming and non-streaming calls
// receive the same deterministic response.
func (m *testModel) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: testLLMResponseText}},
			},
			Partial: false,
		}, nil)
	}
}
