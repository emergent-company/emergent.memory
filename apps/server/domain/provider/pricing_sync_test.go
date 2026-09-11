package provider

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// recordingPricingRepo is a hermetic test double for the pricing upsert sink.
// It records the entries passed to UpsertPricing so tests can assert Sync's
// behaviour without a database or any network access.
type recordingPricingRepo struct {
	calls   int
	entries []ProviderPricing
	err     error
}

func (r *recordingPricingRepo) UpsertPricing(_ context.Context, entries []ProviderPricing) error {
	r.calls++
	r.entries = append([]ProviderPricing(nil), entries...)
	return r.err
}

// TestPricingSync_UpsertsStaticListWithoutNetwork verifies that Sync writes the
// embedded staticPricing list straight to the repository (the source of truth)
// and does not depend on any remote registry fetch. The recording repo also
// makes the "no network" property structural: Sync has no HTTP client to call.
func TestPricingSync_UpsertsStaticListWithoutNetwork(t *testing.T) {
	repo := &recordingPricingRepo{}
	svc := &PricingSyncService{repo: repo, log: slog.Default()}

	start := time.Now().Add(-time.Minute)
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if repo.calls != 1 {
		t.Fatalf("UpsertPricing called %d times, want 1", repo.calls)
	}
	if len(repo.entries) != len(staticPricing) {
		t.Fatalf("UpsertPricing received %d entries, want %d (len(staticPricing))", len(repo.entries), len(staticPricing))
	}

	got := make(map[string]ProviderPricing, len(repo.entries))
	for _, e := range repo.entries {
		got[string(e.Provider)+"/"+e.Model] = e
	}

	for _, want := range staticPricing {
		key := string(want.Provider) + "/" + want.Model
		entry, ok := got[key]
		if !ok {
			t.Errorf("upserted entries missing static row %s", key)
			continue
		}
		if entry.TextInputPrice != want.TextInputPrice || entry.OutputPrice != want.OutputPrice {
			t.Errorf("%s prices = (%v, %v), want (%v, %v)",
				key, entry.TextInputPrice, entry.OutputPrice, want.TextInputPrice, want.OutputPrice)
		}
		if entry.LastSynced.IsZero() || entry.LastSynced.Before(start) {
			t.Errorf("%s LastSynced = %v, want recent non-zero timestamp", key, entry.LastSynced)
		}
	}
}

// TestPricingSync_PropagatesUpsertError verifies Sync surfaces repository errors.
func TestPricingSync_PropagatesUpsertError(t *testing.T) {
	repo := &recordingPricingRepo{err: errors.New("db down")}
	svc := &PricingSyncService{repo: repo, log: slog.Default()}

	err := svc.Sync(context.Background())
	if err == nil {
		t.Fatalf("Sync() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to upsert pricing") {
		t.Fatalf("Sync() error = %q, want it to wrap the upsert failure", err)
	}
}

// TestStaticPricingCoversEmbeddings guards the embedded canonical pricing list:
// embedding models must carry retail rates so embedding usage events and the
// Providers rate panel resolve costs (model-only fallback also matches when a
// Gemini embedding is served through an OpenAI-compatible/LiteLLM provider).
func TestStaticPricingCoversEmbeddings(t *testing.T) {
	got := map[ProviderType]map[string]float64{}
	for _, p := range staticPricing {
		if got[p.Provider] == nil {
			got[p.Provider] = map[string]float64{}
		}
		got[p.Provider][p.Model] = p.TextInputPrice
	}

	want := []struct {
		provider       ProviderType
		model          string
		textInputPrice float64
	}{
		{ProviderGoogleAI, "gemini-embedding-001", 0.15},
		{ProviderGoogleAI, "gemini-embedding-2", 0.20},
		{ProviderVertexAI, "gemini-embedding-001", 0.15},
		{ProviderVertexAI, "gemini-embedding-2", 0.20},
		{ProviderOpenAI, "text-embedding-3-small", 0.02},
		{ProviderOpenAI, "text-embedding-3-large", 0.13},
	}

	for _, w := range want {
		price, ok := got[w.provider][w.model]
		if !ok {
			t.Errorf("staticPricing missing embedding row (%s, %s)", w.provider, w.model)
			continue
		}
		if price != w.textInputPrice {
			t.Errorf("staticPricing (%s, %s) TextInputPrice = %v, want %v", w.provider, w.model, price, w.textInputPrice)
		}
	}
}
