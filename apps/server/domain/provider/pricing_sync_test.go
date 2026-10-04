package provider

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// f64 is a helper for the pointer-based modelsDevCost fields.
func f64(v float64) *float64 { return &v }

// recordingPricingRepo is a hermetic test double for the pricing upsert sink.
// It records the entries passed to UpsertPricing and simulates storage in rows
// so pruning behaviour can be asserted without a database or any network access.
type recordingPricingRepo struct {
	calls   int
	entries []ProviderPricing
	err     error

	pruneCalls int
	pruneErr   error
	rows       map[string]ProviderPricing
}

func pricingRowKey(p ProviderType, model string) string {
	return string(p) + "\x00" + model
}

func (r *recordingPricingRepo) UpsertPricing(_ context.Context, entries []ProviderPricing) error {
	r.calls++
	r.entries = append([]ProviderPricing(nil), entries...)
	if r.rows == nil {
		r.rows = map[string]ProviderPricing{}
	}
	for _, e := range entries {
		r.rows[pricingRowKey(e.Provider, e.Model)] = e
	}
	return r.err
}

func (r *recordingPricingRepo) PruneStalePricing(_ context.Context, managedProviders []ProviderType, keep []ProviderPricing) (int64, error) {
	r.pruneCalls++
	if r.pruneErr != nil {
		return 0, r.pruneErr
	}
	if r.rows == nil {
		r.rows = map[string]ProviderPricing{}
	}

	managed := make(map[ProviderType]struct{}, len(managedProviders))
	for _, p := range managedProviders {
		managed[p] = struct{}{}
	}
	keepSet := make(map[string]struct{}, len(keep))
	for _, e := range keep {
		keepSet[pricingRowKey(e.Provider, e.Model)] = struct{}{}
	}

	var pruned int64
	for key, row := range r.rows {
		if _, isManaged := managed[row.Provider]; !isManaged {
			continue
		}
		if _, kept := keepSet[key]; kept {
			continue
		}
		delete(r.rows, key)
		pruned++
	}
	return pruned, nil
}

// fakePricingFetcher is a hermetic test double for pricingRatesFetcher.
type fakePricingFetcher struct {
	rates []fetchedPricing
	err   error
}

func (f fakePricingFetcher) FetchRates(context.Context) ([]fetchedPricing, error) {
	return f.rates, f.err
}

// TestPricingSync_NoFetcherWritesStaticBaseline verifies that without a
// configured fetcher Sync writes exactly the embedded baseline and makes no
// network call (a nil fetcher is structural proof there is no HTTP client).
func TestPricingSync_NoFetcherWritesStaticBaseline(t *testing.T) {
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
	if repo.pruneCalls != 0 {
		t.Fatalf("PruneStalePricing called %d times without a successful feed, want 0", repo.pruneCalls)
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

// TestPricingSync_OverlaysRemoteRates verifies that fetched rates are merged on
// top of the baseline: a rate for a vendor absent from the baseline is appended,
// and a fetched rate for a baseline (provider, model) overrides it.
func TestPricingSync_OverlaysRemoteRates(t *testing.T) {
	remote := []fetchedPricing{
		{ProviderPricing: ProviderPricing{Provider: ProviderZhipu, Model: "glm-4.6", TextInputPrice: 0.60, OutputPrice: 2.20}, present: dimTextInput | dimOutput},
		{ProviderPricing: ProviderPricing{Provider: ProviderDeepSeek, Model: "deepseek-v4-pro", TextInputPrice: 9.99, OutputPrice: 9.99}, present: dimTextInput | dimOutput},
	}
	repo := &recordingPricingRepo{}
	svc := &PricingSyncService{repo: repo, fetcher: fakePricingFetcher{rates: remote}, log: slog.Default()}

	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// One appended vendor row; the override replaces an existing baseline row.
	if want := len(staticPricing) + 1; len(repo.entries) != want {
		t.Fatalf("UpsertPricing received %d entries, want %d", len(repo.entries), want)
	}

	got := make(map[string]ProviderPricing, len(repo.entries))
	for _, e := range repo.entries {
		got[string(e.Provider)+"/"+e.Model] = e
	}

	if entry, ok := got["zhipu/glm-4.6"]; !ok {
		t.Fatalf("fetched vendor row zhipu/glm-4.6 missing from merged entries")
	} else if entry.TextInputPrice != 0.60 || entry.OutputPrice != 2.20 {
		t.Errorf("zhipu/glm-4.6 = (%v, %v), want (0.6, 2.2)", entry.TextInputPrice, entry.OutputPrice)
	}

	if entry := got["deepseek/deepseek-v4-pro"]; entry.TextInputPrice != 9.99 || entry.OutputPrice != 9.99 {
		t.Errorf("deepseek/deepseek-v4-pro = (%v, %v), want fetched override (9.99, 9.99)",
			entry.TextInputPrice, entry.OutputPrice)
	}
}

// TestPricingSync_PresenceAwareOverlayPreservesOmittedDimensions is the
// regression guard for #1452: the models.dev feed publishes only input/output
// (and sometimes input_audio) for Google models. The overlay must update those
// dimensions without zeroing the image/audio rates carried by the baseline,
// otherwise multimodal cost computes to $0.
func TestPricingSync_PresenceAwareOverlayPreservesOmittedDimensions(t *testing.T) {
	// Feed entry for google/gemini-2.5-pro publishes only text input/output.
	remote := []fetchedPricing{{
		ProviderPricing: ProviderPricing{
			Provider:       ProviderGoogleAI,
			Model:          "gemini-2.5-pro",
			TextInputPrice: 1.50,
			OutputPrice:    6.00,
		},
		present: dimTextInput | dimOutput,
	}}

	repo := &recordingPricingRepo{}
	svc := &PricingSyncService{repo: repo, fetcher: fakePricingFetcher{rates: remote}, log: slog.Default()}
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	var got ProviderPricing
	found := false
	for _, e := range repo.entries {
		if e.Provider == ProviderGoogleAI && e.Model == "gemini-2.5-pro" {
			got, found = e, true
			break
		}
	}
	if !found {
		t.Fatalf("google/gemini-2.5-pro missing from merged entries")
	}

	// Published dimensions are updated from the feed...
	if got.TextInputPrice != 1.50 {
		t.Errorf("TextInputPrice = %v, want feed override 1.50", got.TextInputPrice)
	}
	if got.OutputPrice != 6.00 {
		t.Errorf("OutputPrice = %v, want feed override 6.00", got.OutputPrice)
	}
	// ...while dimensions omitted by the feed keep the baseline rate.
	if got.ImageInputPrice != 1.25 {
		t.Errorf("ImageInputPrice = %v, want baseline 1.25 preserved (overlay must not zero omitted dims)", got.ImageInputPrice)
	}
	if got.AudioInputPrice != 1.25 {
		t.Errorf("AudioInputPrice = %v, want baseline 1.25 preserved (overlay must not zero omitted dims)", got.AudioInputPrice)
	}
}

// TestPricingSync_FetchFailureFallsBackToBaseline verifies a feed error does not
// fail Sync and does not drop the embedded baseline.
func TestPricingSync_FetchFailureFallsBackToBaseline(t *testing.T) {
	repo := &recordingPricingRepo{}
	svc := &PricingSyncService{
		repo:    repo,
		fetcher: fakePricingFetcher{err: errors.New("models.dev down")},
		log:     slog.Default(),
	}

	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v, want nil (feed failure must fall back)", err)
	}
	if len(repo.entries) != len(staticPricing) {
		t.Fatalf("UpsertPricing received %d entries, want %d baseline rows", len(repo.entries), len(staticPricing))
	}
	if repo.pruneCalls != 0 {
		t.Fatalf("PruneStalePricing called %d times after a fetch failure, want 0", repo.pruneCalls)
	}
}

// TestPricingSync_FetchFailureRetainsStaleRows verifies an outage never wipes a
// previously synced row: only a successful fetch+parse triggers pruning.
func TestPricingSync_FetchFailureRetainsStaleRows(t *testing.T) {
	repo := &recordingPricingRepo{}
	seed := fakePricingFetcher{rates: []fetchedPricing{{
		ProviderPricing: ProviderPricing{Provider: ProviderZhipu, Model: "glm-4.6", TextInputPrice: 0.6, OutputPrice: 2.2},
		present:         dimTextInput | dimOutput,
	}}}
	svc := &PricingSyncService{repo: repo, fetcher: seed, log: slog.Default()}
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("seed Sync() error = %v", err)
	}

	svc.fetcher = fakePricingFetcher{err: errors.New("models.dev down")}
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v, want nil (feed failure must fall back)", err)
	}
	if _, ok := repo.rows[pricingRowKey(ProviderZhipu, "glm-4.6")]; !ok {
		t.Errorf("zhipu/glm-4.6 removed after fetch failure; an outage must retain rows")
	}
}

// TestPricingSync_PrunesStaleRowsWhenFeedDropsModel verifies that a model which
// disappears from a successfully parsed feed (removed or now $0) is pruned,
// while models still present are kept.
func TestPricingSync_PrunesStaleRowsWhenFeedDropsModel(t *testing.T) {
	repo := &recordingPricingRepo{}
	svc := &PricingSyncService{
		repo: repo,
		fetcher: fakePricingFetcher{rates: []fetchedPricing{
			{ProviderPricing: ProviderPricing{Provider: ProviderZhipu, Model: "glm-4.6", TextInputPrice: 0.6, OutputPrice: 2.2}, present: dimTextInput | dimOutput},
			{ProviderPricing: ProviderPricing{Provider: ProviderZhipu, Model: "glm-4.7", TextInputPrice: 0.7, OutputPrice: 2.7}, present: dimTextInput | dimOutput},
		}},
		log: slog.Default(),
	}
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("seed Sync() error = %v", err)
	}
	if _, ok := repo.rows[pricingRowKey(ProviderZhipu, "glm-4.6")]; !ok {
		t.Fatalf("seed row zhipu/glm-4.6 missing after first sync")
	}

	// Feed now drops glm-4.6 (or prices it at $0, so the parser omits it).
	svc.fetcher = fakePricingFetcher{rates: []fetchedPricing{{
		ProviderPricing: ProviderPricing{Provider: ProviderZhipu, Model: "glm-4.7", TextInputPrice: 0.7, OutputPrice: 2.7},
		present:         dimTextInput | dimOutput,
	}}}
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if _, ok := repo.rows[pricingRowKey(ProviderZhipu, "glm-4.6")]; ok {
		t.Errorf("stale row zhipu/glm-4.6 not pruned after it left the feed")
	}
	if _, ok := repo.rows[pricingRowKey(ProviderZhipu, "glm-4.7")]; !ok {
		t.Errorf("row zhipu/glm-4.7 still in the feed but was pruned")
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

// TestParseModelsDevPricing exercises the feed parser: mapped vendors produce
// rows, zero/missing cost is skipped (no silent $0), audio input is carried, and
// unmapped providers are ignored.
func TestParseModelsDevPricing(t *testing.T) {
	raw := modelsDevPricingResponse{
		"zhipuai": {
			Models: map[string]modelsDevPricingModel{
				"glm-4.6":       {Cost: &modelsDevCost{Input: f64(0.60), Output: f64(2.20)}},
				"glm-free":      {Cost: &modelsDevCost{Input: f64(0), Output: f64(0)}},
				"glm-no-cost":   {},
				"glm-audio-4.5": {Cost: &modelsDevCost{Input: f64(0.10), InputAudio: f64(0.50)}},
			},
		},
		"zai": {
			Models: map[string]modelsDevPricingModel{
				// Duplicate model id across zai/zhipuai slugs: first slug wins.
				"glm-4.6": {Cost: &modelsDevCost{Input: f64(9.99), Output: f64(9.99)}},
			},
		},
		"someproxy": {
			Models: map[string]modelsDevPricingModel{
				"whatever": {Cost: &modelsDevCost{Input: f64(1), Output: f64(1)}},
			},
		},
	}

	got := make(map[string]ProviderPricing)
	for _, e := range parseModelsDevPricing(raw) {
		got[string(e.Provider)+"/"+e.Model] = e.ProviderPricing
	}

	glm, ok := got["zhipu/glm-4.6"]
	if !ok {
		t.Fatalf("zhipu/glm-4.6 missing from parsed pricing")
	}
	if glm.TextInputPrice != 0.60 || glm.OutputPrice != 2.20 {
		t.Errorf("zhipu/glm-4.6 = (%v, %v), want (0.6, 2.2)", glm.TextInputPrice, glm.OutputPrice)
	}

	if audio, ok := got["zhipu/glm-audio-4.5"]; !ok {
		t.Errorf("zhipu/glm-audio-4.5 missing (audio input price should be carried)")
	} else if audio.AudioInputPrice != 0.50 {
		t.Errorf("zhipu/glm-audio-4.5 AudioInputPrice = %v, want 0.50", audio.AudioInputPrice)
	}

	if _, ok := got["zhipu/glm-free"]; ok {
		t.Errorf("zero-cost model zhipu/glm-free should be omitted, not stored as $0")
	}
	if _, ok := got["zhipu/glm-no-cost"]; ok {
		t.Errorf("cost-less model zhipu/glm-no-cost should be omitted")
	}
	for key := range got {
		if strings.HasPrefix(key, "someproxy/") || strings.HasPrefix(key, "zai/") {
			t.Errorf("unexpected parsed row %q (unmapped provider or non-first slug)", key)
		}
	}
}

// TestParseModelsDevPricing_PresenceFlags verifies the parser records only the
// dimensions the feed actually published, so absent dimensions (e.g. Google's
// missing input_audio) are not treated as a published $0.
func TestParseModelsDevPricing_PresenceFlags(t *testing.T) {
	raw := modelsDevPricingResponse{
		"google": {
			Models: map[string]modelsDevPricingModel{
				"gemini-2.5-pro": {Cost: &modelsDevCost{Input: f64(1.25), Output: f64(10)}},
			},
		},
	}

	entries := parseModelsDevPricing(raw)
	if len(entries) != 1 {
		t.Fatalf("parseModelsDevPricing returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.present&dimTextInput == 0 || entry.present&dimOutput == 0 {
		t.Errorf("present = %b, want text-input and output flags set", entry.present)
	}
	if entry.present&(dimImageInput|dimVideoInput|dimAudioInput) != 0 {
		t.Errorf("present = %b, want no image/video/audio flags (feed publishes none)", entry.present)
	}
}

// TestModelsDevPricing_PrefersDefaultRegionSlug verifies the vendor→slug order
// matches the registry's default endpoint region: aliyun's default is mainland
// dashscope.aliyuncs.com, so the `alibaba-cn` rate must win over `alibaba`.
func TestModelsDevPricing_PrefersDefaultRegionSlug(t *testing.T) {
	raw := modelsDevPricingResponse{
		"alibaba": {
			Models: map[string]modelsDevPricingModel{
				"qwen-max": {Cost: &modelsDevCost{Input: f64(1.6), Output: f64(6.4)}},
			},
		},
		"alibaba-cn": {
			Models: map[string]modelsDevPricingModel{
				"qwen-max": {Cost: &modelsDevCost{Input: f64(0.345), Output: f64(1.377)}},
			},
		},
		"siliconflow": {
			Models: map[string]modelsDevPricingModel{
				"Qwen/Qwen3-8B": {Cost: &modelsDevCost{Input: f64(0.10)}},
			},
		},
		"siliconflow-cn": {
			Models: map[string]modelsDevPricingModel{
				"Qwen/Qwen3-8B": {Cost: &modelsDevCost{Input: f64(0.06)}},
			},
		},
	}

	got := make(map[string]ProviderPricing)
	for _, e := range parseModelsDevPricing(raw) {
		got[string(e.Provider)+"/"+e.Model] = e.ProviderPricing
	}

	if qwen, ok := got["aliyun/qwen-max"]; !ok {
		t.Fatalf("aliyun/qwen-max missing from parsed pricing")
	} else if qwen.TextInputPrice != 0.345 {
		t.Errorf("aliyun/qwen-max TextInputPrice = %v, want mainland (alibaba-cn) rate 0.345", qwen.TextInputPrice)
	}

	if sf, ok := got["siliconflow/Qwen/Qwen3-8B"]; !ok {
		t.Fatalf("siliconflow/Qwen/Qwen3-8B missing from parsed pricing")
	} else if sf.TextInputPrice != 0.06 {
		t.Errorf("siliconflow/Qwen/Qwen3-8B TextInputPrice = %v, want mainland (siliconflow-cn) rate 0.06", sf.TextInputPrice)
	}
}

// TestStaticPricingCoversEmbeddings guards the embedded baseline list:
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
