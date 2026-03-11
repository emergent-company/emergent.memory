package provider

import (
	"log/slog"

	adkmodel "google.golang.org/adk/model"

	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// UsageTrackerAdapter wraps UsageService and *slog.Logger to satisfy the
// adk.ModelWrapper interface. It is injected into adk.ModelFactory via fx so
// that every LLM created by the factory is automatically wrapped with usage
// tracking, without creating an import cycle (pkg/adk cannot import domain/provider).
type UsageTrackerAdapter struct {
	usage *UsageService
	log   *slog.Logger
}

// NewUsageTrackerAdapter creates a new UsageTrackerAdapter.
func NewUsageTrackerAdapter(usage *UsageService, log *slog.Logger) *UsageTrackerAdapter {
	return &UsageTrackerAdapter{usage: usage, log: log}
}

// WrapModel satisfies adk.ModelWrapper.
// The provider parameter is one of "google" or "google-vertex" as a plain string
// (matching ProviderType string values) to avoid leaking domain types into pkg/adk.
func (a *UsageTrackerAdapter) WrapModel(inner adkmodel.LLM, provider string) adkmodel.LLM {
	return NewTrackingModel(inner, a.usage, ProviderType(provider), a.log)
}

// Ensure UsageTrackerAdapter implements adk.ModelWrapper at compile time.
var _ adk.ModelWrapper = (*UsageTrackerAdapter)(nil)
