package provider

import (
	"context"
	"iter"
	"log/slog"
	"time"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// usageRecorder is a minimal interface for dispatching LLM usage events.
// It is satisfied by UsageService (implemented in usage_service.go).
// Defined as a local interface so tracking_model.go compiles independently.
type usageRecorder interface {
	RecordAsync(event *LLMUsageEvent)
}

// TrackingModel wraps an adkmodel.LLM to intercept GenerateContent calls,
// extract UsageMetadata from responses, and asynchronously dispatch
// LLMUsageEvents to the database.
//
// Design notes:
//   - Wraps every GenerateContent call transparently — callers see no change.
//   - Usage events are dispatched asynchronously via the UsageService channel
//     so they never block the agent execution path.
//   - Multimodal token counts are extracted from PromptTokensDetails when present,
//     falling back to PromptTokenCount (text-only) for older response shapes.
type TrackingModel struct {
	inner    adkmodel.LLM
	usage    usageRecorder
	provider ProviderType
	log      *slog.Logger
}

// NewTrackingModel wraps an existing LLM with usage tracking.
func NewTrackingModel(inner adkmodel.LLM, usage usageRecorder, provider ProviderType, log *slog.Logger) *TrackingModel {
	return &TrackingModel{
		inner:    inner,
		usage:    usage,
		provider: provider,
		log:      log.With(logger.Scope("provider.tracking")),
	}
}

// Name satisfies adkmodel.LLM.
func (m *TrackingModel) Name() string {
	return m.inner.Name()
}

// ProviderName returns the provider type string for this model (e.g. "google", "openai-compatible").
// Callers can type-assert adkmodel.LLM to *TrackingModel to access this.
func (m *TrackingModel) ProviderName() string {
	return string(m.provider)
}

// GenerateContent satisfies adkmodel.LLM. It proxies every response item
// through the wrapped LLM and, on the last non-partial response that contains
// UsageMetadata, asynchronously records an LLMUsageEvent.
func (m *TrackingModel) GenerateContent(
	ctx context.Context,
	req *adkmodel.LLMRequest,
	stream bool,
) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		for resp, err := range m.inner.GenerateContent(ctx, req, stream) {
			if !yield(resp, err) {
				return
			}
			if err != nil {
				continue
			}
			// Only record usage on the final, complete response.
			if resp != nil && !resp.Partial && resp.UsageMetadata != nil {
				m.recordUsage(ctx, req, resp)
			}
		}
	}
}

// recordUsage asynchronously dispatches an LLMUsageEvent built from the
// response's UsageMetadata. Project / org IDs are read from the context.
func (m *TrackingModel) recordUsage(ctx context.Context, req *adkmodel.LLMRequest, resp *adkmodel.LLMResponse) {
	projectID := auth.ProjectIDFromContext(ctx)
	orgID := auth.OrgIDFromContext(ctx)

	if projectID == "" || orgID == "" {
		// No tenant context — skip tracking (e.g. background jobs, tests).
		// Log a warning so missing context is visible in server logs.
		m.log.Warn("trackingModel: skipping usage record — missing tenant context",
			"project_id", projectID,
			"org_id", orgID,
			"model", m.inner.Name(),
		)
		return
	}

	meta := resp.UsageMetadata
	modelName := req.Model
	if modelName == "" {
		modelName = m.inner.Name()
	}

	event := &LLMUsageEvent{
		ProjectID:    projectID,
		OrgID:        orgID,
		Provider:     m.provider,
		Model:        modelName,
		Operation:    OperationGenerate,
		OutputTokens: int64(meta.CandidatesTokenCount),
		CreatedAt:    time.Now().UTC(),
	}

	// Attach the agent run ID when present in context.
	if runID := RunIDFromContext(ctx); runID != "" {
		event.RunID = &runID
	}

	// Attach the root orchestration run ID when present in context.
	if rootRunID := RootRunIDFromContext(ctx); rootRunID != "" {
		event.RootRunID = &rootRunID
	}

	// Extract per-modality prompt tokens when the breakdown is available.
	// This is present when the model response includes PromptTokensDetails.
	if len(meta.PromptTokensDetails) > 0 {
		for _, detail := range meta.PromptTokensDetails {
			switch detail.Modality {
			case genai.MediaModalityText:
				event.TextInputTokens += int64(detail.TokenCount)
			case genai.MediaModalityImage:
				event.ImageInputTokens += int64(detail.TokenCount)
			case genai.MediaModalityVideo:
				event.VideoInputTokens += int64(detail.TokenCount)
			case genai.MediaModalityAudio:
				event.AudioInputTokens += int64(detail.TokenCount)
			}
		}
	} else {
		// Fallback: treat all prompt tokens as text (text-only model response)
		event.TextInputTokens = int64(meta.PromptTokenCount)
	}

	// Dispatch asynchronously — do not block the agent path.
	m.usage.RecordAsync(event)
}
