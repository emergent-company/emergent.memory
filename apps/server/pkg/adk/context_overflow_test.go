package adk

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// overflowStubModel is a model.LLM that returns the configured errors on its
// first calls and then a text answer. It exists so the detection tests exercise
// the model-call boundary without a provider.
type overflowStubModel struct {
	name   string
	errs   []error
	calls  int
	answer string
}

func (m *overflowStubModel) Name() string { return m.name }

func (m *overflowStubModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		call := m.calls
		m.calls++
		if call < len(m.errs) && m.errs[call] != nil {
			yield(nil, m.errs[call])
			return
		}
		answer := m.answer
		if answer == "" {
			answer = "ok"
		}
		yield(&model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: answer}}},
		}, nil)
	}
}

// TestIsContextOverflow covers the shapes providers actually return, plus the
// generic 400s that must NOT be mistaken for an overflow.
func TestIsContextOverflow(t *testing.T) {
	require.False(t, IsContextOverflow(nil))

	providerErr := errors.New("openai-compatible: endpoint returned 400: {\"error\":{\"message\":\"This model's maximum context length is 8192 tokens. However, you requested 9000 tokens.\"}}")
	typed := &ContextOverflowError{Model: "openai/gpt-4o", Err: providerErr}
	require.True(t, IsContextOverflow(typed))
	require.True(t, IsContextOverflow(fmt.Errorf("run failed: %w", typed)),
		"a typed overflow wrapped by another layer must still be recognised")

	overflowMessages := []string{
		"openai: context_length_exceeded",
		"anthropic: prompt is too long: 214728 tokens > 200000 maximum",
		"google: input token count (1079349) exceeds the maximum number of tokens allowed (1048576)",
		"deepseek: maximum context length is 65536 tokens",
		"together: request too large for this model",
		"relay: too many tokens in the prompt",
		"openai: string_above_max_length",
	}
	for _, msg := range overflowMessages {
		require.True(t, IsContextOverflow(errors.New(msg)), "should classify as overflow: %s", msg)
	}

	nonOverflow := []string{
		"endpoint returned 400: invalid api key",
		"endpoint returned 400: messages: roles must alternate",
		"endpoint returned 429: rate limit exceeded",
		"openai: request timed out",
		"tool failed: query returned no rows",
	}
	for _, msg := range nonOverflow {
		require.False(t, IsContextOverflow(errors.New(msg)), "must not classify as overflow: %s", msg)
	}
}

// TestWrapContextOverflow is nil-safe, idempotent and only tags overflows.
func TestWrapContextOverflow(t *testing.T) {
	require.NoError(t, WrapContextOverflow("m", nil))

	plain := errors.New("rate limit exceeded")
	require.Equal(t, plain, WrapContextOverflow("m", plain))

	raw := errors.New("prompt is too long: 300000 tokens > 200000 maximum")
	wrapped := WrapContextOverflow("anthropic/claude", raw)
	var typed *ContextOverflowError
	require.ErrorAs(t, wrapped, &typed)
	require.Equal(t, "anthropic/claude", typed.Model)
	require.ErrorIs(t, wrapped, raw, "the provider error must stay in the chain")

	// Idempotent: an already-typed error is returned unchanged, not nested.
	require.Equal(t, wrapped, WrapContextOverflow("other", wrapped))
}

// TestOverflowStubModelReturnsSuccessAfterError pins the stub contract the
// executor integration test relies on: one context-length error, then success.
func TestOverflowStubModelReturnsSuccessAfterError(t *testing.T) {
	stub := &overflowStubModel{
		name:   "stub",
		errs:   []error{errors.New("prompt is too long")},
		answer: "done",
	}

	var gotErr error
	for _, err := range stub.GenerateContent(context.Background(), nil, false) {
		gotErr = err
	}
	require.True(t, IsContextOverflow(gotErr))

	var text string
	for resp, err := range stub.GenerateContent(context.Background(), nil, false) {
		require.NoError(t, err)
		if resp != nil && resp.Content != nil && len(resp.Content.Parts) > 0 {
			text = resp.Content.Parts[0].Text
		}
	}
	require.Equal(t, "done", text)
}
