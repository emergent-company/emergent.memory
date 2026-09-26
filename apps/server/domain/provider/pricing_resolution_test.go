package provider

import (
	"context"
	"log/slog"
	"testing"
)

// fakePricingLookup is a test double for pricingLookup with per-method function
// fields. Unset fields behave as a total miss (nil, nil).
type fakePricingLookup struct {
	projectPricing func(ctx context.Context, projectID string, provider ProviderType, model string) (*ProjectCustomPricing, error)
	pricing        func(ctx context.Context, provider ProviderType, model string) (*ProviderPricing, error)
	pricingByModel func(ctx context.Context, model string) (*ProviderPricing, error)
}

func (f fakePricingLookup) GetProjectCustomPricing(ctx context.Context, projectID string, providerSlug string, model string) (*ProjectCustomPricing, error) {
	if f.projectPricing == nil {
		return nil, nil
	}
	return f.projectPricing(ctx, projectID, ProviderType(providerSlug), model)
}

func (f fakePricingLookup) GetPricing(ctx context.Context, provider ProviderType, model string) (*ProviderPricing, error) {
	if f.pricing == nil {
		return nil, nil
	}
	return f.pricing(ctx, provider, model)
}

func (f fakePricingLookup) GetPricingByModel(ctx context.Context, model string) (*ProviderPricing, error) {
	if f.pricingByModel == nil {
		return nil, nil
	}
	return f.pricingByModel(ctx, model)
}

// TestStripVendorModelName exercises the pure normalization helper used for the
// fallback optimistic price lookup. Mirrors TestNormalizeModelName.
func TestStripVendorModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Vendor-prefixed names (last path segment is the model).
		{"deepseek/deepseek-v4-pro", "deepseek-v4-pro"},
		{"azure/gpt-4o", "gpt-4o"},
		{"openai/gpt-4o-mini", "gpt-4o-mini"},
		{"litellm/deepseek/deepseek-chat", "deepseek-chat"},
		// Trailing :tag.
		{"gpt-4o:2024-11-20", "gpt-4o"},
		{"deepseek-v4-pro:latest", "deepseek-v4-pro"},
		// Trailing @version.
		{"gpt-4o@2024-11-20", "gpt-4o"},
		{"gemini-2.5-flash@latest", "gemini-2.5-flash"},
		// Case and whitespace.
		{"DeepSeek/DeepSeek-V4-Pro", "deepseek-v4-pro"},
		{"  gpt-4o  ", "gpt-4o"},
		// Already-clean names pass through.
		{"deepseek-v4-pro", "deepseek-v4-pro"},
		{"gpt-4o", "gpt-4o"},
		{"", ""},
		// normalizeModelName-style path prefixes are also handled (last segment).
		{"models/gemini-2.0-flash", "gemini-2.0-flash"},
		{"publishers/google/models/gemini-2.0-flash", "gemini-2.0-flash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripVendorModelName(tt.input)
			if got != tt.expected {
				t.Errorf("stripVendorModelName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// testUsageEvent returns a usage event with 1M text input tokens and 500K
// output tokens, so costs are trivially textPrice + 0.5*outputPrice.
func testUsageEvent(provider ProviderType, model string) *LLMUsageEvent {
	return &LLMUsageEvent{
		ProjectID:       "proj-1",
		Provider:        provider,
		Model:           model,
		TextInputTokens: 1_000_000,
		OutputTokens:    500_000,
	}
}

func TestCalculateCostWith_ProjectOverrideWins(t *testing.T) {
	lookup := fakePricingLookup{
		projectPricing: func(ctx context.Context, projectID string, provider ProviderType, model string) (*ProjectCustomPricing, error) {
			if provider == ProviderOpenAI && model == "gpt-4o" {
				return &ProjectCustomPricing{
					TextInputPrice: 1.00,
					OutputPrice:    2.00,
				}, nil
			}
			return nil, nil
		},
		pricing: func(ctx context.Context, provider ProviderType, model string) (*ProviderPricing, error) {
			// Global retail would give a different (higher) cost; the
			// override must win.
			return &ProviderPricing{TextInputPrice: 99.0, OutputPrice: 99.0}, nil
		},
	}
	svc := &UsageService{log: slog.Default()}
	event := testUsageEvent(ProviderOpenAI, "gpt-4o")
	event.ProjectID = "proj-1"

	// 1.00 * 1 + 2.00 * 0.5 = 2.00
	got := svc.calculateCostWith(context.Background(), lookup, event)
	if got != 2.00 {
		t.Errorf("calculateCostWith() = %v, want 2.00 (override wins over global)", got)
	}
}

func TestCalculateCostWith_ExactHit(t *testing.T) {
	lookup := fakePricingLookup{
		pricing: func(ctx context.Context, provider ProviderType, model string) (*ProviderPricing, error) {
			if provider == ProviderOpenAI && model == "gpt-4o" {
				return &ProviderPricing{TextInputPrice: 2.50, OutputPrice: 10.00}, nil
			}
			return nil, nil
		},
	}
	svc := &UsageService{log: slog.Default()}

	// 2.50 * 1 + 10.00 * 0.5 = 7.50
	got := svc.calculateCostWith(context.Background(), lookup, testUsageEvent(ProviderOpenAI, "gpt-4o"))
	if got != 7.50 {
		t.Errorf("calculateCostWith() = %v, want 7.50", got)
	}
}

func TestCalculateCostWith_ModelOnlyHit(t *testing.T) {
	lookup := fakePricingLookup{
		// Exact (openai, deepseek-v4-pro) misses — the model is served through
		// an OpenAI-compatible proxy, so the model-only lookup finds the
		// deepseek entry.
		pricingByModel: func(ctx context.Context, model string) (*ProviderPricing, error) {
			if model == "deepseek-v4-pro" {
				return &ProviderPricing{Provider: ProviderDeepSeek, TextInputPrice: 1.74, OutputPrice: 3.48}, nil
			}
			return nil, nil
		},
	}
	svc := &UsageService{log: slog.Default()}

	// 1.74 * 1 + 3.48 * 0.5 = 3.48
	got := svc.calculateCostWith(context.Background(), lookup, testUsageEvent(ProviderOpenAI, "deepseek-v4-pro"))
	if got != 3.48 {
		t.Errorf("calculateCostWith() = %v, want 3.48 (model-only hit)", got)
	}
}

func TestCalculateCostWith_StripVendorHit(t *testing.T) {
	lookup := fakePricingLookup{
		pricingByModel: func(ctx context.Context, model string) (*ProviderPricing, error) {
			// The event model is vendor-prefixed; only the stripped name matches.
			if model == "deepseek-v4-pro" {
				return &ProviderPricing{Provider: ProviderDeepSeek, TextInputPrice: 1.74, OutputPrice: 3.48}, nil
			}
			return nil, nil
		},
	}
	svc := &UsageService{log: slog.Default()}

	event := testUsageEvent(ProviderOpenAI, "deepseek/deepseek-v4-pro")
	got := svc.calculateCostWith(context.Background(), lookup, event)
	if got != 3.48 {
		t.Errorf("calculateCostWith() = %v, want 3.48 (strip-vendor hit)", got)
	}
}

func TestCalculateCostWith_NormalizeHit(t *testing.T) {
	lookup := fakePricingLookup{
		pricingByModel: func(ctx context.Context, model string) (*ProviderPricing, error) {
			// Only the normalizeModelName form matches.
			if model == "gemini-2.5-flash" {
				return &ProviderPricing{Provider: ProviderGoogleAI, TextInputPrice: 0.15, OutputPrice: 0.60}, nil
			}
			return nil, nil
		},
	}
	svc := &UsageService{log: slog.Default()}

	event := testUsageEvent(ProviderGoogleAI, "models/gemini-2.5-flash")
	// stripVendorModelName("models/gemini-2.5-flash") = "gemini-2.5-flash" — it
	// would also match, so this test asserts the fallback works regardless of
	// which normalization step produces the hit.
	got := svc.calculateCostWith(context.Background(), lookup, event)
	const epsilon = 1e-9
	if diff := got - (0.15 + 0.30); diff < -epsilon || diff > epsilon {
		t.Errorf("calculateCostWith() = %v, want %v (normalize hit)", got, 0.15+0.30)
	}
}

func TestCalculateCostWith_TotalMissReturnsZero(t *testing.T) {
	lookup := fakePricingLookup{} // every lookup misses
	svc := &UsageService{log: slog.Default()}

	event := testUsageEvent(ProviderOpenAI, "completely-unknown-model")
	got := svc.calculateCostWith(context.Background(), lookup, event)
	if got != 0.0 {
		t.Errorf("calculateCostWith() = %v, want 0.0 (total miss)", got)
	}
}
