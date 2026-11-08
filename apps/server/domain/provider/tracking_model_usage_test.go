package provider

import (
	"context"
	"iter"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

type captureUsageRecorder struct {
	mu     sync.Mutex
	events []*LLMUsageEvent
}

func (c *captureUsageRecorder) RecordAsync(e *LLMUsageEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *captureUsageRecorder) snapshot() []*LLMUsageEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*LLMUsageEvent(nil), c.events...)
}

type singleResponseModel struct {
	resp *adkmodel.LLMResponse
}

func (m *singleResponseModel) Name() string { return "fake-model" }

func (m *singleResponseModel) GenerateContent(_ context.Context, _ *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		yield(m.resp, nil)
	}
}

func usageTestContext() context.Context {
	ctx := auth.ContextWithProjectID(context.Background(), "project-1")
	return auth.ContextWithOrgID(ctx, "org-1")
}

// TestTrackingModel_RecordsUsageWhenConsumerStopsEarly pins the fix for
// submit-terminated runs: ADK stops iterating a response as soon as a
// run-finalizing tool fires, so usage must be recorded before yielding.
func TestTrackingModel_RecordsUsageWhenConsumerStopsEarly(t *testing.T) {
	resp := &adkmodel.LLMResponse{
		Content: &genai.Content{Parts: []*genai.Part{{Text: "done"}}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     100,
			CandidatesTokenCount: 7,
			TotalTokenCount:      107,
		},
	}
	recorder := &captureUsageRecorder{}
	tm := NewTrackingModel(&singleResponseModel{resp: resp}, recorder, "openai", "openai", slog.New(slog.DiscardHandler))

	seq := tm.GenerateContent(usageTestContext(), &adkmodel.LLMRequest{Model: "fake-model"}, false)
	seen := 0
	seq(func(r *adkmodel.LLMResponse, err error) bool {
		seen++
		require.NoError(t, err)
		require.NotNil(t, r)
		return false // consumer stops while handling the final response
	})

	require.Equal(t, 1, seen)
	events := recorder.snapshot()
	require.Len(t, events, 1, "usage must be recorded even when the consumer stops early")
	assert.EqualValues(t, 100, events[0].TextInputTokens)
	assert.EqualValues(t, 7, events[0].OutputTokens)
	assert.Equal(t, "project-1", events[0].ProjectID)
	assert.Equal(t, "org-1", events[0].OrgID)
}

func TestTrackingModel_RecordsUsageOnNormalCompletion(t *testing.T) {
	resp := &adkmodel.LLMResponse{
		Content: &genai.Content{Parts: []*genai.Part{{Text: "ok"}}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 2,
			TotalTokenCount:      12,
		},
	}
	recorder := &captureUsageRecorder{}
	tm := NewTrackingModel(&singleResponseModel{resp: resp}, recorder, "openai", "openai", slog.New(slog.DiscardHandler))

	seq := tm.GenerateContent(usageTestContext(), &adkmodel.LLMRequest{Model: "fake-model"}, false)
	for range seq {
	}
	require.Len(t, recorder.snapshot(), 1)
}
