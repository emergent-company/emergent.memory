package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const (
	// modelsDevPricingFetchURL is the models.dev catalog endpoint. The same
	// feed drives model token limits (model_limits_sync.go); this sync reads
	// its per-model `cost` block instead of `limit`.
	modelsDevPricingFetchURL = "https://models.dev/api.json"

	// modelsDevPricingFetchTimeout bounds the HTTP fetch.
	modelsDevPricingFetchTimeout = 30 * time.Second

	// modelsDevPricingMaxBytes caps the response body. models.dev/api.json is
	// several MB and grows over time; a cap that is too low silently truncates
	// the JSON and fails the parse.
	modelsDevPricingMaxBytes = 64 << 20
)

// modelsDevCost is a model's published list price from models.dev, in USD per
// 1M tokens. models.dev sources these from each provider's public price list.
// Only the dimensions covered by kb.provider_pricing are decoded. Each field is
// a pointer so the parser can distinguish "the feed published 0" from "the feed
// published nothing": the feed omits dimensions it does not price (e.g. Google
// entries publish no input_audio), and an omitted dimension must never zero the
// baseline value.
type modelsDevCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	InputAudio *float64 `json:"input_audio"`
}

// pricingDimension identifies one per-token rate dimension of a pricing row.
type pricingDimension uint8

const (
	dimTextInput pricingDimension = 1 << iota
	dimImageInput
	dimVideoInput
	dimAudioInput
	dimOutput
)

// fetchedPricing couples a parsed models.dev rate row with the set of rate
// dimensions the feed actually published for that model. Only the published
// dimensions may overwrite the baseline when the row is merged; omitted
// dimensions keep their baseline (or previously stored) values.
type fetchedPricing struct {
	ProviderPricing
	present pricingDimension
}

// modelsDevPricingResponse is the subset of models.dev/api.json used here.
type modelsDevPricingResponse map[string]modelsDevPricingProvider

// modelsDevPricingProvider is one provider entry in the models.dev response.
type modelsDevPricingProvider struct {
	Models map[string]modelsDevPricingModel `json:"models"`
}

// modelsDevPricingModel is one model entry (only the cost block is needed).
type modelsDevPricingModel struct {
	Cost *modelsDevCost `json:"cost"`
}

// modelsDevVendorSlugs maps a registry dialect to the models.dev provider
// slug(s) that publish its prices. Order matters: the first slug to list a
// given model wins. Slugs are ordered so the feed region matching the registry
// vendor's DefaultBaseURLs (see builtin.go) comes first — a mainland-China
// default prefers the `-cn` slug, an international default the global slug.
// Getting this backwards silently charges the wrong region's rate.
//
// Known limitation: the sync has no per-instance base-URL awareness, so a user
// who overrides the base URL to another region still gets the default-region
// rate for that vendor. Per-instance regional rates are out of scope here.
//
// Dialects absent from this map have no models.dev catalog entry, so their
// usage falls through to the $0 default transparently (cost-resolution step 5)
// rather than being assigned an invented rate. As of Oct 2026 those are:
// hunyuan (Tencent's models.dev entries are subscription plans priced at $0),
// qianfan (Baidu), lkeap, jina, litellm, gpustack (self-hosted), and generic
// (no fixed vendor). A mapped vendor can still contribute no rows when every
// models.dev entry it publishes is unpriced (currently modelscope); those
// models likewise fall through to $0.
//
// models.dev lists context-tiered prices (context_over_200k) for some models;
// kb.provider_pricing stores a single flat rate, so the base (lowest) tier is
// used. Prices are per 1M tokens in USD.
var modelsDevVendorSlugs = map[ProviderType][]string{
	ProviderGoogleAI:    {"google"},
	ProviderVertexAI:    {"google-vertex"},
	ProviderOpenAI:      {"openai"},
	ProviderDeepSeek:    {"deepseek"},
	ProviderAliyun:      {"alibaba-cn", "alibaba"},
	ProviderZhipu:       {"zhipuai", "zai"},
	ProviderVolcengine:  {"volcengine"},
	ProviderSiliconFlow: {"siliconflow-cn", "siliconflow"},
	ProviderMiniMax:     {"minimax", "minimax-cn"},
	ProviderMoonshot:    {"moonshotai", "moonshotai-cn"},
	ProviderMimo:        {"xiaomi"},
	ProviderModelScope:  {"modelscope"},
	ProviderQiniu:       {"qiniu-ai"},
	ProviderLongCat:     {"longcat"},
	ProviderAzureOpenAI: {"azure"},
	ProviderAnthropic:   {"anthropic"},
	ProviderOpenRouter:  {"openrouter"},
	ProviderRequesty:    {"requesty"},
	ProviderNVIDIA:      {"nvidia"},
	ProviderNovita:      {"novita-ai"},
}

// pricingRatesFetcher returns retail rates to overlay on the embedded
// staticPricing baseline. A nil fetcher or a fetch error must not fail the
// sync: the baseline list still seeds the table. Entries carry the set of
// dimensions the feed published so the overlay never zeroes omitted ones.
type pricingRatesFetcher interface {
	FetchRates(ctx context.Context) ([]fetchedPricing, error)
}

// modelsDevPricingFetcher fetches per-model retail rates from models.dev.
type modelsDevPricingFetcher struct {
	client *http.Client
	url    string
}

// newModelsDevPricingFetcher creates the production models.dev fetcher.
func newModelsDevPricingFetcher() *modelsDevPricingFetcher {
	return &modelsDevPricingFetcher{
		client: &http.Client{Timeout: modelsDevPricingFetchTimeout},
		url:    modelsDevPricingFetchURL,
	}
}

// FetchRates downloads models.dev/api.json and returns one pricing row per
// model with a sourced, non-zero rate for a mapped registry dialect, carrying
// the set of dimensions the feed published.
func (f *modelsDevPricingFetcher) FetchRates(ctx context.Context) ([]fetchedPricing, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from models.dev", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, modelsDevPricingMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var raw modelsDevPricingResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse models.dev pricing JSON: %w", err)
	}

	return parseModelsDevPricing(raw), nil
}

// parseModelsDevPricing extracts USD-per-1M-token retail rates for the mapped
// registry dialects. Only models with a published non-zero rate are returned:
// a zero or missing cost means "no sourced rate", which must fall through to
// the $0 default rather than be stored as a silent $0. Each returned row
// records which dimensions the feed published (see fetchedPricing) so the
// overlay preserves baseline dimensions the feed omits. Models are emitted in a
// deterministic order (registry order, then slug order, then model ID).
func parseModelsDevPricing(raw modelsDevPricingResponse) []fetchedPricing {
	var out []fetchedPricing
	seen := make(map[string]struct{})

	for _, def := range Builtins() {
		for _, slug := range modelsDevVendorSlugs[def.Type] {
			prov, ok := raw[slug]
			if !ok {
				continue
			}

			ids := make([]string, 0, len(prov.Models))
			for id := range prov.Models {
				ids = append(ids, id)
			}
			sort.Strings(ids)

			for _, id := range ids {
				cost := prov.Models[id].Cost
				if cost == nil {
					continue
				}

				entry := fetchedPricing{
					ProviderPricing: ProviderPricing{Provider: def.Type, Model: id},
				}
				if cost.Input != nil {
					entry.TextInputPrice = *cost.Input
					entry.present |= dimTextInput
				}
				if cost.Output != nil {
					entry.OutputPrice = *cost.Output
					entry.present |= dimOutput
				}
				if cost.InputAudio != nil {
					entry.AudioInputPrice = *cost.InputAudio
					entry.present |= dimAudioInput
				}
				if entry.present == 0 {
					continue
				}
				if entry.TextInputPrice == 0 && entry.OutputPrice == 0 && entry.AudioInputPrice == 0 {
					continue
				}

				key := string(def.Type) + "\x00" + id
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, entry)
			}
		}
	}

	return out
}

// mergePricingEntries stamps LastSynced and merges the embedded static baseline
// with the fetched rates. A remote row overrides the static row for the same
// (provider, model), but only for the dimensions the feed actually published —
// dimensions the feed omits keep the baseline (or previously stored) value, so a
// feed that prices text only never zeroes image/audio/video rates. The static
// list stays as the guaranteed fallback so a feed outage never removes
// already-known rates.
func mergePricingEntries(remote []fetchedPricing) []ProviderPricing {
	now := time.Now().UTC()
	index := make(map[string]int, len(staticPricing)+len(remote))
	out := make([]ProviderPricing, 0, len(staticPricing)+len(remote))

	add := func(p ProviderPricing) {
		p.LastSynced = now
		key := string(p.Provider) + "\x00" + p.Model
		if i, ok := index[key]; ok {
			out[i] = p
			return
		}
		index[key] = len(out)
		out = append(out, p)
	}

	for _, p := range staticPricing {
		add(p)
	}

	for _, r := range remote {
		key := string(r.Provider) + "\x00" + r.Model
		i, ok := index[key]
		if !ok {
			// New vendor/model with no baseline: only the published dimensions
			// are set; the rest stay at the zero value (no borrowed rate).
			add(r.ProviderPricing)
			continue
		}

		merged := out[i]
		if r.present&dimTextInput != 0 {
			merged.TextInputPrice = r.TextInputPrice
		}
		if r.present&dimImageInput != 0 {
			merged.ImageInputPrice = r.ImageInputPrice
		}
		if r.present&dimVideoInput != 0 {
			merged.VideoInputPrice = r.VideoInputPrice
		}
		if r.present&dimAudioInput != 0 {
			merged.AudioInputPrice = r.AudioInputPrice
		}
		if r.present&dimOutput != 0 {
			merged.OutputPrice = r.OutputPrice
		}
		merged.LastSynced = now
		out[i] = merged
	}

	return out
}

// managedPricingProviders returns, in deterministic order, the registry dialects
// whose rates are sourced from the models.dev feed. Only rows for these vendors
// may be pruned when the feed drops a model; baseline-only vendors are left to
// the always-upserted static list.
func managedPricingProviders() []ProviderType {
	providers := make([]ProviderType, 0, len(modelsDevVendorSlugs))
	for p := range modelsDevVendorSlugs {
		providers = append(providers, p)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })
	return providers
}
