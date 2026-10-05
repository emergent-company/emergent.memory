package provider

import (
	"context"
	"iter"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/model"

	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// nopUsageRecorder satisfies usageRecorder for tests that never emit usage.
type nopUsageRecorder struct{}

func (nopUsageRecorder) RecordAsync(*LLMUsageEvent) {}

// reasoningCaptureModel is a minimal model.LLM that records the effort applied
// through the ReasoningEffortConfigurator interface.
type reasoningCaptureModel struct {
	effort string
}

func (m *reasoningCaptureModel) Name() string { return "capture" }

func (m *reasoningCaptureModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(func(*model.LLMResponse, error) bool) {}
}

func (m *reasoningCaptureModel) SetReasoningEffort(effort string) { m.effort = effort }

// Compile-time guard: the production-wrapped model must remain assignable to
// the interface the executor type-asserts on. Without this, wiring effort at
// the executor is unreachable (#1462).
var _ adk.ReasoningEffortConfigurator = (*TrackingModel)(nil)

// TestTrackingModel_DelegatesReasoningEffort is the core #1462 assertion: a
// wrapped model must stay configurable. If TrackingModel stops delegating
// SetReasoningEffort, the executor's type assertion on the returned model never
// matches and the effort never reaches the request.
func TestTrackingModel_DelegatesReasoningEffort(t *testing.T) {
	inner := &reasoningCaptureModel{}
	tm := NewTrackingModel(inner, nopUsageRecorder{}, "openai", "openai", slog.New(slog.DiscardHandler))

	tm.SetReasoningEffort("max")

	require.Equal(t, "max", inner.effort, "TrackingModel must delegate SetReasoningEffort to the inner model")
}
