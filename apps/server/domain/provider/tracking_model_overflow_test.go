package provider

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"

	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// overflowFailingModel is a minimal model.LLM that fails every call with the
// configured error. It stands in for a provider rejecting an oversized prompt.
type overflowFailingModel struct {
	err error
}

func (m *overflowFailingModel) Name() string { return "openai/gpt-test" }

func (m *overflowFailingModel) GenerateContent(context.Context, *adkmodel.LLMRequest, bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		yield(nil, m.err)
	}
}

// TestTrackingModelTagsContextOverflow is the model-call-boundary half of
// #1138: the production model wrapper must turn a provider context-window
// rejection into the typed error the executor checks, without changing the
// error's chain.
func TestTrackingModelTagsContextOverflow(t *testing.T) {
	providerErr := errors.New(`openai-compatible: endpoint returned 400: {"error":{"message":"This model's maximum context length is 8192 tokens."}}`)
	inner := &overflowFailingModel{err: providerErr}
	tm := NewTrackingModel(inner, nopUsageRecorder{}, "openai", "openai", slog.New(slog.DiscardHandler))

	var got error
	for _, err := range tm.GenerateContent(context.Background(), nil, false) {
		got = err
	}

	require.Error(t, got)
	require.True(t, adk.IsContextOverflow(got))
	var typed *adk.ContextOverflowError
	require.ErrorAs(t, got, &typed)
	require.Equal(t, "openai/gpt-test", typed.Model)
	require.ErrorIs(t, got, providerErr)
}

// TestTrackingModelLeavesOtherErrorsAlone guards against over-classification:
// a generic provider failure must reach the executor unchanged.
func TestTrackingModelLeavesOtherErrorsAlone(t *testing.T) {
	providerErr := errors.New("endpoint returned 429: rate limit exceeded")
	inner := &overflowFailingModel{err: providerErr}
	tm := NewTrackingModel(inner, nopUsageRecorder{}, "openai", "openai", slog.New(slog.DiscardHandler))

	var got error
	for _, err := range tm.GenerateContent(context.Background(), nil, false) {
		got = err
	}

	require.Error(t, got)
	require.False(t, adk.IsContextOverflow(got))
	require.ErrorIs(t, got, providerErr)
}
