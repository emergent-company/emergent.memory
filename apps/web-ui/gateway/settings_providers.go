package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// providerWhitelist is the set of provider types the provider-config form may
// persist. Anything else is rejected up front — never forwarded to the memory
// backend.
var providerWhitelist = map[string]bool{"google": true, "google-vertex": true, "openai": true, "deepseek": true}

// --- Providers settings panel (project provider configs + model rates) ---

// providerRateRow is one merged (provider, model) rate row shown in the
// Providers panel. Rates are USD per 1M tokens. Auto* come from the retail
// pricing rows (HasAuto false = memory has no retail price for the model);
// Override* + IsCustom come from the project pricing override.
type providerRateRow struct {
	Provider       string
	Model          string
	AutoInput      float64
	AutoOutput     float64
	OverrideInput  float64
	OverrideOutput float64
	IsCustom       bool
	HasAuto        bool
}

// providerRateGroup is one configured provider's rows, grouped for display.
type providerRateGroup struct {
	Provider string
	Rows     []providerRateRow
}

// providerPanelData is the payload of the Providers panel: grouped rate rows,
// the raw provider configs (for management), the project's default models, the
// global model catalog, each configured provider's per-provider model catalog
// (keyed by the configured provider name — the prefix default-model options
// must carry), plus a best-effort fetch error.
type providerPanelData struct {
	Groups         []providerRateGroup
	Providers      []ProjectProviderConfig
	ModelConfig    *ProjectModelConfig
	Models         []Model
	ProviderModels map[string][]ProviderSupportedModel
	Err            error
}

// providerModelKey joins provider + model into a map key.
func providerModelKey(provider, model string) string {
	return provider + "\x00" + model
}

// stripVendorModelName reduces a model name to its bare, vendor-free form for
// optimistic rate matching: keep only the last '/'-separated segment, drop a
// trailing ':tag' or '@version', then lower-case and trim. Mirrors the memory
// backend's stripVendorModelName so the gateway's rate panel resolves the same
// retail rows it does. Used only for the fallback lookup key — the recorded
// and displayed model name is never mutated.
func stripVendorModelName(model string) string {
	if idx := strings.LastIndex(model, "/"); idx != -1 {
		model = model[idx+1:]
	}
	for _, sep := range []string{":", "@"} {
		if idx := strings.Index(model, sep); idx != -1 {
			model = model[:idx]
		}
	}
	return strings.ToLower(strings.TrimSpace(model))
}

// mergeProviderRates joins each configured provider's models (its cached
// catalog plus its configured generative/embedding models) with the retail
// pricing rows and the project pricing overrides into per-model rows. A row
// exists for every model of every configured provider; a project override
// wins over the retail rate; a model with neither rate renders as "unknown"
// (HasAuto false, IsCustom false). Retail-rate lookup falls back to a
// model-only match when the exact provider+model pair is absent, so models
// served through an OpenAI-compatible/LiteLLM proxy still surface their
// retail rate (mirrors cost resolution in the memory backend). Duplicate
// catalog entries for a model are collapsed.
func mergeProviderRates(providers []ProjectProviderConfig, modelsByProvider map[string][]ProviderSupportedModel, pricing []ProviderPricing, overrides []ProjectCustomPricing) []providerRateRow {
	auto := make(map[string]ProviderPricing, len(pricing))
	autoByModel := make(map[string]ProviderPricing, len(pricing))
	for _, p := range pricing {
		auto[providerModelKey(p.Provider, p.Model)] = p
		if _, ok := autoByModel[p.Model]; !ok {
			autoByModel[p.Model] = p
		}
	}
	custom := make(map[string]ProjectCustomPricing, len(overrides))
	for _, o := range overrides {
		custom[providerModelKey(o.Provider, o.Model)] = o
	}

	var rows []providerRateRow
	for _, prov := range providers {
		seen := map[string]bool{}
		// Model names to show: cached catalog models first, then the provider's
		// configured generative/embedding models (covers LiteLLM proxies whose
		// catalog is empty but whose configured model is real).
		var names []string
		for _, mdl := range modelsByProvider[prov.Provider] {
			if mdl.ModelName == "" || seen[mdl.ModelName] {
				continue
			}
			seen[mdl.ModelName] = true
			names = append(names, mdl.ModelName)
		}
		for _, m := range []string{prov.GenerativeModel, prov.EmbeddingModel} {
			if m != "" && !seen[m] {
				seen[m] = true
				names = append(names, m)
			}
		}
		for _, model := range names {
			key := providerModelKey(prov.Provider, model)
			row := providerRateRow{Provider: prov.Provider, Model: model}
			if p, ok := auto[key]; ok {
				row.HasAuto = true
				row.AutoInput = p.TextInputPrice
				row.AutoOutput = p.OutputPrice
			} else if p, ok := autoByModel[stripVendorModelName(model)]; ok {
				row.HasAuto = true
				row.AutoInput = p.TextInputPrice
				row.AutoOutput = p.OutputPrice
			}
			if o, ok := custom[key]; ok {
				row.IsCustom = true
				row.OverrideInput = o.TextInputPrice
				row.OverrideOutput = o.OutputPrice
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// groupProviderRows buckets rows by provider, preserving first-seen provider
// and row order. Providers with no rows contribute no group.
func groupProviderRows(rows []providerRateRow) []providerRateGroup {
	var groups []providerRateGroup
	index := map[string]int{}
	for _, r := range rows {
		i, ok := index[r.Provider]
		if !ok {
			i = len(groups)
			index[r.Provider] = i
			groups = append(groups, providerRateGroup{Provider: r.Provider})
		}
		groups[i].Rows = append(groups[i].Rows, r)
	}
	return groups
}

// loadProviderPanel loads and merges the Providers panel data. A failure of
// the providers/pricing/overrides reads surfaces as d.Err (rendered inside the
// panel, not as a page error); a per-provider model-catalog failure only skips
// that provider's models so the rest of the panel still renders.
func (s *Server) loadProviderPanel(ctx context.Context) providerPanelData {
	d := providerPanelData{}
	providers, err := s.memory.ListProjectProviders(ctx)
	if err != nil {
		d.Err = err
		return d
	}
	d.Providers = providers
	pricing, err := s.memory.ListPricing(ctx)
	if err != nil {
		d.Err = err
		return d
	}
	overrides, err := s.memory.ListProjectPricingOverrides(ctx)
	if err != nil {
		d.Err = err
		return d
	}
	if mc, err := s.memory.GetProjectModelConfig(ctx); err == nil && mc != nil {
		d.ModelConfig = mc
	}
	if models, err := s.memory.ListModels(ctx); err == nil {
		d.Models = models
	}
	modelsByProvider := map[string][]ProviderSupportedModel{}
	for _, p := range providers {
		models, err := s.memory.ListProviderModels(ctx, p.Provider)
		if err != nil {
			continue
		}
		modelsByProvider[p.Provider] = models
	}
	d.ProviderModels = modelsByProvider
	d.Groups = groupProviderRows(mergeProviderRates(providers, modelsByProvider, pricing, overrides))
	return d
}

// providerSupportsEmbedding reports whether one configured provider can serve
// document embeddings. Signals are checked strongest first:
//   - an explicitly configured embedding model (an OpenAI-compatible proxy can
//     serve embeddings even though OpenAI's own API has no embedding endpoint),
//   - a cached catalog entry memory classified as an embedding model, then
//   - the provider type's own embedding capability.
func providerSupportsEmbedding(p ProjectProviderConfig, models []ProviderSupportedModel) bool {
	if p.EmbeddingModel != "" {
		return true
	}
	if slices.ContainsFunc(models, func(m ProviderSupportedModel) bool {
		return m.ModelType == "embedding"
	}) {
		return true
	}
	return providerTypeSupportsEmbedding(p.Provider)
}

// providerTypeSupportsEmbedding is the type-level embedding capability map, the
// fallback when a provider has neither a configured embedding model nor a
// cached embedding catalog entry. It mirrors memory's provider service
// (domain/provider/service.go), which treats OpenAI and DeepSeek as having no
// embedding API ("configure a separate embedding provider for document
// indexing") while Google and Google Vertex expose one. Centralized here so the
// classification never drifts across call sites.
func providerTypeSupportsEmbedding(provider string) bool {
	switch provider {
	case "google", "google-vertex":
		return true
	default:
		return false
	}
}

// providersNeedEmbeddingProvider reports whether the Providers panel should
// surface the embedding guidance callout: at least one provider is configured
// and none of them is embedding-capable. An errored load yields false — the
// panel already shows the load error, so never nag on unreliable data.
func providersNeedEmbeddingProvider(d providerPanelData) bool {
	if d.Err != nil || len(d.Providers) == 0 {
		return false
	}
	for _, p := range d.Providers {
		if providerSupportsEmbedding(p, d.ProviderModels[p.Provider]) {
			return false
		}
	}
	return true
}

// providersMissingCacheTTL bounds how long a project's "has no providers"
// result is cached before projectHasNoProviders re-reads the backend.
const providersMissingCacheTTL = 10 * time.Second

// providerMissingEntry is one cached projectHasNoProviders result.
type providerMissingEntry struct {
	missing bool
	at      time.Time
}

// projectHasNoProviders reports whether the current project has ZERO
// configured LLM providers (drives the settings sub-nav warning badge). It
// returns true only when the provider list loads successfully and is empty; on
// error it returns false — fail safe, never nag on transient errors. Results
// are cached ~10s per project id (Server.missingProvidersCache, invalidated on
// provider save/remove) so full page loads don't each pay a ListProjectProviders
// round trip.
func (s *Server) projectHasNoProviders(ctx context.Context) bool {
	var projectID string
	if sc, ok := sessionContextFrom(ctx); ok {
		projectID = sc.ProjectID
	} else {
		projectID = s.cfg.MemoryProjectID
	}
	if projectID == "" {
		return false
	}
	s.missingProvidersMu.Lock()
	defer s.missingProvidersMu.Unlock()
	if s.missingProvidersCache == nil {
		s.missingProvidersCache = map[string]providerMissingEntry{}
	} else if e, ok := s.missingProvidersCache[projectID]; ok && time.Since(e.at) < providersMissingCacheTTL {
		return e.missing
	}
	providers, err := s.memory.ListProjectProviders(ctx)
	missing := err == nil && len(providers) == 0
	s.missingProvidersCache[projectID] = providerMissingEntry{missing: missing, at: time.Now()}
	return missing
}

// invalidateProvidersMissingCache drops all cached provider-presence results so
// the next page load re-reads the backend. Called after a project provider
// config is saved or removed (the project's zero-provider state just changed).
func (s *Server) invalidateProvidersMissingCache() {
	s.missingProvidersMu.Lock()
	s.missingProvidersCache = nil
	s.missingProvidersMu.Unlock()
}

// uiProjectSettingsProviderOverride handles the per-model override inline save
// (HTMX → POST /settings/providers/:provider/:model). The input and output
// prices must be non-negative numbers — an invalid value is rejected before
// anything is written. Persists via the memory pricing-overrides API and
// surfaces the outcome as a toast via the HX-Trigger header.
func (s *Server) uiProjectSettingsProviderOverride(c echo.Context) error {
	ctx := c.Request().Context()
	provider := strings.TrimSpace(c.Param("provider"))
	model := strings.TrimSpace(c.Param("model"))
	if provider == "" || model == "" {
		return toastTrigger(c, "error", "provider and model are required")
	}
	rates, err := overrideRatesFromForm(c)
	if err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	if _, err := s.memory.UpsertProjectPricingOverride(ctx, provider, model, rates); err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	return toastTrigger(c, "success", "Saved")
}

// uiProjectSettingsProviderOverrideDelete handles the per-model remove action
// (HTMX → POST /settings/providers/:provider/:model/delete), reverting the
// model to its automatic retail rate. The outcome surfaces as a toast.
func (s *Server) uiProjectSettingsProviderOverrideDelete(c echo.Context) error {
	ctx := c.Request().Context()
	provider := strings.TrimSpace(c.Param("provider"))
	model := strings.TrimSpace(c.Param("model"))
	if provider == "" || model == "" {
		return toastTrigger(c, "error", "provider and model are required")
	}
	if err := s.memory.DeleteProjectPricingOverride(ctx, provider, model); err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	return toastTrigger(c, "success", "Removed custom rate")
}

// uiProjectProviderConfig upserts a project provider's credentials + model
// selections (POST /settings/providers/config). The provider type is required
// and must be known; credential fields differ by type (API key for google/
// openai/deepseek, service-account JSON + GCP project + location for
// google-vertex). An optional base_url must be a valid http(s) URL so the
// backend can actually reach it. Validation fails fast — an invalid config is
// never forwarded to the memory backend. On validation or backend error the
// form page is re-rendered with the submitted values + error (see
// renderProviderConfigError) instead of redirecting away and losing the form;
// only success redirects (PRG → /settings/providers?updated=1). Because provider
// presence is shell chrome (the sidebar settings-row warning badge lives outside
// #main-content), success forces a full reload for HTMX/boosted submits via
// render.RedirectAfterMutation — a boosted 303 would swap only #main-content and
// leave the badge stale until a manual refresh.
func (s *Server) uiProjectProviderConfig(c echo.Context) error {
	ctx := c.Request().Context()
	provider := strings.TrimSpace(c.FormValue("provider"))
	if provider == "" {
		return s.renderProviderConfigError(c, "", ProviderConfigInput{}, fmt.Errorf("provider is required"))
	}
	in := ProviderConfigInput{
		APIKey:             strings.TrimSpace(c.FormValue("api_key")),
		ServiceAccountJSON: strings.TrimSpace(c.FormValue("service_account_json")),
		GCPProject:         strings.TrimSpace(c.FormValue("gcp_project")),
		Location:           strings.TrimSpace(c.FormValue("location")),
		BaseURL:            strings.TrimSpace(c.FormValue("base_url")),
		GenerativeModel:    strings.TrimSpace(c.FormValue("generative_model")),
		EmbeddingModel:     strings.TrimSpace(c.FormValue("embedding_model")),
	}
	if !providerWhitelist[provider] {
		return s.renderProviderConfigError(c, provider, in, fmt.Errorf("unsupported provider %q", provider))
	}
	if in.BaseURL != "" {
		u, err := url.Parse(in.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return s.renderProviderConfigError(c, provider, in, fmt.Errorf("base_url must be a valid http(s) URL (e.g. http://litellm:4000/v1)"))
		}
	}
	if _, err := s.memory.UpsertProjectProviderConfig(ctx, provider, in); err != nil {
		return s.renderProviderConfigError(c, provider, in, providerConfigSaveError(in, err))
	}
	s.invalidateProvidersMissingCache()
	// Shell chrome (sidebar provider warning badge) lives outside #main-content;
	// a boosted 303 would leave it stale. Full-load PRG for HTMX submits, 303 for
	// plain ones — same shape as org-create / document-delete.
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/settings/providers?updated=1")
	return nil
}

// providerConfigSaveError wraps a backend save failure with the original cause
// preserved for display. It deliberately does NOT blame base_url — the memory
// backend's generic 500 "internal_error" is unrelated to base_url reachability
// (the /settings/providers/check-url endpoint covers that separately).
func providerConfigSaveError(_ ProviderConfigInput, err error) error {
	return fmt.Errorf("could not save provider: %w", err)
}

// renderProviderConfigError re-renders the add/edit provider form with the
// submitted draft values and the error, so the user can correct input in place
// instead of losing the form. An existing provider re-renders in edit mode
// (title + hidden provider field), otherwise add mode (provider select).
func (s *Server) renderProviderConfigError(c echo.Context, provider string, in ProviderConfigInput, err error) error {
	ctx := c.Request().Context()
	data := providerConfigPageData{
		Draft:         &in,
		DraftProvider: provider,
		FlashErr:      err,
	}
	data.GenerativeModels, data.EmbeddingModels = s.providerModelOptions(ctx)
	if providers, lerr := s.memory.ListProjectProviders(ctx); lerr == nil {
		for i := range providers {
			if providers[i].Provider == provider {
				data.Provider = &providers[i]
				break
			}
		}
	}
	return s.page(c, pageTitle("Configure provider"), providerConfigPage(data))
}

// uiProjectProviderRemove deletes a project provider config (PRG → POST
// /settings/providers/:provider/remove).
func (s *Server) uiProjectProviderRemove(c echo.Context) error {
	ctx := c.Request().Context()
	provider := strings.TrimSpace(c.Param("provider"))
	if provider == "" {
		return redirectWithError(c, "/settings/providers", fmt.Errorf("provider is required"))
	}
	if err := s.memory.DeleteProjectProviderConfig(ctx, provider); err != nil {
		return redirectWithError(c, "/settings/providers", err)
	}
	s.invalidateProvidersMissingCache()
	// Removing the last provider changes shell chrome (sidebar warning badge) —
	// full-load PRG for HTMX submits so the badge appears without a manual refresh.
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/settings/providers?updated=1")
	return nil
}

// uiProjectProviderTest runs a live generate+embed call for a provider and
// surfaces the result as a toast (HTMX → POST /settings/providers/:provider/test).
func (s *Server) uiProjectProviderTest(c echo.Context) error {
	ctx := c.Request().Context()
	provider := strings.TrimSpace(c.Param("provider"))
	if provider == "" {
		return toastTrigger(c, "error", "provider is required")
	}
	res, err := s.memory.TestProjectProvider(ctx, provider)
	if err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	return toastTrigger(c, "success", fmt.Sprintf("%s OK: %s replied %q in %dms", provider, res.Model, res.Reply, res.LatencyMs))
}

// uiProjectProviderTestConnection probes the submitted (unsaved) provider
// input for reachability + auth (HTMX → POST /settings/providers/test). It
// persists nothing and does not go through the memory backend — a direct
// gateway-side request so a bad base_url/key fails fast with a clear message.
//
// Every response — validation failure, probe failure, or success — carries the
// providerFallbackModelOptions fragment as the body (htmx swaps it into
// #provider-fallback-models) plus the memory-toast HX-Trigger header. Empty
// bodies would wipe the fallback selects, so failed/validation paths return
// the fragment with empty model lists. On a successful probe the fragment is
// seeded from the endpoint's OpenAI-compatible /models catalog.
//
// An empty base_url for openai is NOT rejected: memory treats an empty stored
// base_url as "use the provider's official default endpoint", so the probe
// substitutes the official OpenAI endpoint (effectiveTestBaseURL) and runs
// against it. The base_url-required guard only survives for non-openai
// providers (defensive — today only openai reaches this handler).
func (s *Server) uiProjectProviderTestConnection(c echo.Context) error {
	provider := strings.TrimSpace(c.FormValue("provider"))
	apiKey := strings.TrimSpace(c.FormValue("api_key"))
	if provider == "" {
		return providerTestFragmentResponse(c, "error", "provider is required", nil, nil)
	}
	if apiKey == "" {
		// No key submitted — never probe (an empty Authorization header would
		// only draw a 401). Return before any network call.
		return providerTestFragmentResponse(c, "error", "Enter an API key before testing the connection.", nil, nil)
	}
	baseURL := effectiveTestBaseURL(provider, strings.TrimSpace(c.FormValue("base_url")))
	if baseURL == "" {
		return providerTestFragmentResponse(c, "error", "base_url is required to test the connection", nil, nil)
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return providerTestFragmentResponse(c, "error", "base_url must be a valid http(s) URL (e.g. http://litellm:4000/v1)", nil, nil)
	}
	start := time.Now()
	ok, status, detail := probeProviderEndpoint(baseURL, apiKey)
	latency := time.Since(start).Milliseconds()
	if ok {
		gen, emb := fetchOpenAICompatFallbackModels(baseURL, apiKey, provider)
		return providerTestFragmentResponse(c, "success", fmt.Sprintf("Connection OK (HTTP %d) in %dms", status, latency), gen, emb)
	}
	if status == http.StatusUnauthorized {
		// 401 = the key was rejected; the upstream body is a machine-oriented
		// error blob, so swap it for a friendly message instead.
		return providerTestFragmentResponse(c, "error", "Authentication failed (HTTP 401) — check that the API key is correct.", nil, nil)
	}
	return providerTestFragmentResponse(c, "error", fmt.Sprintf("Connection failed (HTTP %d) in %dms: %s", status, latency, detail), nil, nil)
}

// openAIOfficialBaseURL is the official OpenAI API endpoint. Memory treats an
// empty stored base_url as "use the provider's official default endpoint"; for
// openai that endpoint is this URL. A package var (not const) so tests can
// redirect it to an httptest server and exercise the empty-base_url path
// without dialing the real API.
var openAIOfficialBaseURL = "https://api.openai.com/v1"

// effectiveTestBaseURL returns the base_url the test-connection probe should
// hit: the submitted baseURL when present, otherwise the provider's official
// default endpoint. openai is the only provider that shows the base-url field
// and gates save on this test; its official default substitutes an empty form
// value. Every other provider keeps the required-base_url guard ("" is
// returned so the caller rejects).
func effectiveTestBaseURL(provider, baseURL string) string {
	if baseURL != "" {
		return baseURL
	}
	if provider == "openai" {
		return openAIOfficialBaseURL
	}
	return ""
}

// providerTestFragmentResponse sends the test-connection response: the
// memory-toast HX-Trigger header (same shape as toastTrigger) followed by the
// providerFallbackModelOptions fragment body (the two fallback model selects,
// seeded when gen/emb carry the endpoint's catalog). Set before WriteHeader so
// the header actually reaches the client on every path.
func providerTestFragmentResponse(c echo.Context, kind, msg string, gen, emb []Model) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	if err := toastTrigger(c, kind, msg); err != nil {
		return err
	}
	// Render into a buffer first so an empty body never wipes the selects the
	// htmx response swaps into.
	var buf bytes.Buffer
	if err := providerFallbackModelOptions(gen, emb).Render(c.Request().Context(), &buf); err != nil {
		return err
	}
	return c.HTML(http.StatusOK, buf.String())
}

// openAIModelList is the subset of the OpenAI-compatible GET /models response
// the fallback seeding reads.
type openAIModelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// providerProbeHTTPClient dials user-supplied provider base_urls through an
// SSRF-guarded transport (loopback/private/metadata blocked). Declared as a var
// so tests can substitute a loopback-permitting client for httptest servers.
var providerProbeHTTPClient = newSSRFSafeHTTPClient(10 * time.Second)

// fetchOpenAICompatFallbackModels seeds the fallback dropdowns after a
// successful connection test: it fetches the endpoint's OpenAI-compatible
// /models catalog (same candidate URLs as probeProviderEndpoint, bearer auth)
// and offers each id as a generative Model option prefixed with the provider
// name. Ids matching nameLooksEmbedding are ADDITIONALLY offered as embedding
// options. The generative list is thus a strict superset of the embedding
// list: a heuristic miss never makes a fetched model disappear, it only means
// the embedding opt-in is missed (the user can still select it as generative
// or leave "None — auto-select"). This is the interim bound; memory's
// authoritative classification (or a future server-side catalog resolve) is
// the durable fix — see docs/tasks/provider-model-classification-parity.md.
// Any fetch/parse error degrades to empty lists — the connection test already
// succeeded and seeding is best-effort.
func fetchOpenAICompatFallbackModels(baseURL, apiKey, provider string) (gen, emb []Model) {
	client := providerProbeHTTPClient
	base := strings.TrimRight(baseURL, "/")
	var candidates []string
	if strings.HasSuffix(base, "/v1") {
		candidates = []string{base + "/models"}
	} else {
		candidates = []string{base + "/v1/models", base + "/models"}
	}
	var list openAIModelList
	for _, u := range candidates {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, nil
		}
		break
	}
	for _, m := range list.Data {
		if m.ID == "" {
			continue
		}
		model := Model{
			Provider:    provider,
			ModelName:   m.ID,
			DisplayName: displayNameForOpenAICompatible(m.ID),
			ModelType:   "generative",
		}
		// Strict superset: every fetched id is offered as generative, and
		// embedding matches are additionally offered as embedding. Both lists
		// preserve the fetched order; see the function comment for why.
		gen = append(gen, model)
		if nameLooksEmbedding(m.ID) {
			model.ModelType = "embedding"
			emb = append(emb, model)
		}
	}
	return gen, emb
}

// nameLooksEmbedding applies the same cheap name-based heuristic memory's
// provider catalog uses to detect embedding models (mirrors
// domain/provider/catalog.go nameLooksEmbedding — covers the common
// Google/Vertex and OpenAI families): embedding iff the lowercase name
// contains "embedding" or "text-embed". It only adds embedding candidates on
// top of the unconditional generative list (see
// fetchOpenAICompatFallbackModels), so a future divergence from memory's rule
// downgrades to a missed embedding opt-in, never a dropped model. Durable fix:
// memory-side catalog resolve — docs/tasks/provider-model-classification-parity.md.
func nameLooksEmbedding(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "embedding") || strings.Contains(lower, "text-embed")
}

// displayNameForOpenAICompatible strips a leading "vendor/" prefix (everything
// up to and including the first "/") from an OpenAI-compatible model id for
// display, keeping the full id as the model name (mirrors memory's
// displayNameForOpenAICompatible). ids without a prefix are unchanged.
func displayNameForOpenAICompatible(id string) string {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		return id[i+1:]
	}
	return id
}

// baseURLCheckResult is the JSON body for the inline base_url reachability
// check (POST /settings/providers/check-url). The designer's debounced inline
// JS renders ok/detail as a warning under the base_url field.
type baseURLCheckResult struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

// uiProjectProviderCheckBaseURL probes the base_url field for reachability
// (debounced inline check, not persisted). Returns JSON: ok=true on a 2xx
// /models response, ok=false with status+detail otherwise (detail carries the
// dial error or the non-2xx body snippet). Empty base_url returns a neutral
// {ok:false} with empty detail so the UI renders nothing.
func (s *Server) uiProjectProviderCheckBaseURL(c echo.Context) error {
	baseURL := strings.TrimSpace(c.FormValue("base_url"))
	apiKey := strings.TrimSpace(c.FormValue("api_key"))
	if baseURL == "" {
		return c.JSON(http.StatusOK, baseURLCheckResult{})
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c.JSON(http.StatusOK, baseURLCheckResult{OK: false, Detail: "must be a valid http(s) URL"})
	}
	ok, status, detail := probeProviderEndpoint(baseURL, apiKey)
	return c.JSON(http.StatusOK, baseURLCheckResult{OK: ok, Status: status, Detail: detail})
}

// probeProviderEndpoint verifies a provider base_url + api_key by requesting
// the OpenAI-compatible /models endpoint. It tries base_url as given, then
// with a trailing /v1 prepended, and reports success on any 2xx response.
func probeProviderEndpoint(baseURL, apiKey string) (ok bool, status int, detail string) {
	client := providerProbeHTTPClient
	base := strings.TrimRight(baseURL, "/")
	candidates := []string{base + "/models"}
	if !strings.HasSuffix(base, "/v1") {
		candidates = []string{base + "/v1/models", base + "/models"}
	}
	var lastStatus int
	var lastBody string
	for _, u := range candidates {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return false, 0, err.Error()
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		lastStatus = resp.StatusCode
		lastBody = strings.TrimSpace(string(body))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return true, resp.StatusCode, ""
		}
	}
	return false, lastStatus, truncateString(lastBody, 200)
}

// uiProjectModelConfig sets the project's default generative + embedding
// models (HTMX → POST /settings/providers/model-config), persisted via the
// model-config API. Both fields carry a provider prefix.
func (s *Server) uiProjectModelConfig(c echo.Context) error {
	ctx := c.Request().Context()
	gen := strings.TrimSpace(c.FormValue("generative_model"))
	emb := strings.TrimSpace(c.FormValue("embedding_model"))
	if _, err := s.memory.UpsertProjectModelConfig(ctx, gen, emb); err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	return toastTrigger(c, "success", "Saved")
}

// overrideRatesFromForm parses the override form's input/output prices. Both
// fields are required, numeric, and non-negative; the text modality prices are
// what the per-model row exposes (image/video/audio stay 0 — the UI only edits
// text input + output rates).
func overrideRatesFromForm(c echo.Context) (modelPriceRates, error) {
	parse := func(label, name string) (float64, error) {
		raw := strings.TrimSpace(c.FormValue(name))
		if raw == "" {
			return 0, fmt.Errorf("%s price is required", label)
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("%s price must be a number", label)
		}
		if v < 0 {
			return 0, fmt.Errorf("%s price must be non-negative", label)
		}
		return v, nil
	}
	textIn, err := parse("input", "textInputPrice")
	if err != nil {
		return modelPriceRates{}, err
	}
	out, err := parse("output", "outputPrice")
	if err != nil {
		return modelPriceRates{}, err
	}
	return modelPriceRates{TextInputPrice: textIn, OutputPrice: out}, nil
}

// formatRateUSD renders a USD-per-1M-token rate, e.g. "$1.74".
func formatRateUSD(v float64) string {
	return "$" + strconv.FormatFloat(v, 'f', -1, 64)
}

// rateInputValue is the prefill for the override form's price inputs: the
// current custom value, else the automatic value, else empty.
func rateInputValue(custom bool, hasAuto bool, customV, autoV float64) string {
	switch {
	case custom:
		return strconv.FormatFloat(customV, 'f', -1, 64)
	case hasAuto:
		return strconv.FormatFloat(autoV, 'f', -1, 64)
	default:
		return ""
	}
}

// defaultModelCatalog returns the reachable default-model options for the
// given modelType ("generative" or "embedding"), each prefixed by the
// CREDENTIAL provider (the configured provider name) rather than the ROUTING
// provider of the global catalog. The default-model config is stored as
// "provider/model" and memory resolves that prefix against the configured
// project providers, so a routing-provider prefix (e.g. "deepseek/…") breaks
// at save time with "no deepseek provider config found" when deepseek models
// are actually served through an "openai" LiteLLM-proxy credential provider.
//
// Options, in stable order (configured providers first, in config order):
//   - each configured provider's per-provider catalog entries of the requested
//     ModelType (deduped by provider+model), and
//   - the provider's configured bare generative/embedding model — LiteLLM
//     proxies often have an empty catalog yet a real configured model — and
//   - the currently stored default (prefix + name), appended only when no
//     configured provider surfaced it, so the select keeps it as selected
//     (e.g. a legacy routing-provider value pending migration).
//
// Catalog entries are filtered strictly by ModelType: production
// ListProviderModels responses always set it ("generative"/"embedding"), and
// test fixtures must too — an empty ModelType never matches.
func defaultModelCatalog(d providerPanelData, modelType string) []Model {
	var out []Model
	seen := map[string]bool{}
	add := func(provider, name, display string) {
		if provider == "" || name == "" {
			return
		}
		key := provider + "/" + name
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Model{Provider: provider, ModelName: name, ModelType: modelType, DisplayName: display})
	}
	for _, p := range d.Providers {
		displays := map[string]string{}
		for _, m := range d.ProviderModels[p.Provider] {
			if m.ModelType != modelType {
				continue
			}
			displays[m.ModelName] = m.DisplayName
			add(p.Provider, m.ModelName, m.DisplayName)
		}
		configured := ""
		if modelType == "generative" {
			configured = p.GenerativeModel
		} else {
			configured = p.EmbeddingModel
		}
		add(p.Provider, configured, displays[configured])
	}
	current := ""
	if modelType == "generative" {
		current = modelConfigGen(d)
	} else {
		current = modelConfigEmb(d)
	}
	if provider, name, ok := strings.Cut(current, "/"); ok {
		add(provider, name, "")
	}
	return out
}

// providerModelOptions returns the credential-prefixed model options
// ("provider/model") for the provider config form's fallback dropdowns
// (generative + embedding), derived from the configured providers.
func (s *Server) providerModelOptions(ctx context.Context) (gen, emb []Model) {
	d := s.loadProviderPanel(ctx)
	return defaultModelCatalog(d, "generative"), defaultModelCatalog(d, "embedding")
}

// prefixedModelInCatalog reports whether a provider-prefixed model name
// ("provider/model") is present in the given catalog options.
func prefixedModelInCatalog(models []Model, name string) bool {
	for _, m := range models {
		if m.Provider+"/"+m.ModelName == name {
			return true
		}
	}
	return false
}

// providerFallbackCurrent returns the current value for a provider fallback
// model dropdown: the submitted draft when re-rendering after a failed save,
// otherwise the stored provider config value (embedding selects the embedding
// field).
func providerFallbackCurrent(draft *ProviderConfigInput, p *ProjectProviderConfig, embedding bool) string {
	if draft != nil {
		if embedding {
			return draft.EmbeddingModel
		}
		return draft.GenerativeModel
	}
	if embedding {
		return providerConfigEmbeddingModel(p)
	}
	return providerConfigGenerativeModel(p)
}

// modelConfigGen/Emb return the stored default generative/embedding model, or
// "" when no config is set.
func modelConfigGen(d providerPanelData) string {
	if d.ModelConfig != nil {
		return d.ModelConfig.GenerativeModel
	}
	return ""
}

func modelConfigEmb(d providerPanelData) string {
	if d.ModelConfig != nil {
		return d.ModelConfig.EmbeddingModel
	}
	return ""
}

// vertexFieldsStyle returns the initial display style for the google-vertex
// credential fields (visible only when editing a google-vertex provider).
func vertexFieldsStyle(p *ProjectProviderConfig) string {
	if p != nil && p.Provider == "google-vertex" {
		return "display:flex"
	}
	return "display:none"
}

// providerConfigBaseURL/GCPProject/Location are nil-safe accessors for the
// provider config form prefill (p is nil on the add form).
func providerConfigBaseURL(p *ProjectProviderConfig) string {
	if p == nil {
		return ""
	}
	return p.BaseURL
}

func providerConfigGCPProject(p *ProjectProviderConfig) string {
	if p == nil {
		return ""
	}
	return p.GCPProject
}

func providerConfigLocation(p *ProjectProviderConfig) string {
	if p == nil {
		return ""
	}
	return p.Location
}

func providerConfigGenerativeModel(p *ProjectProviderConfig) string {
	if p == nil {
		return ""
	}
	return p.GenerativeModel
}

func providerConfigEmbeddingModel(p *ProjectProviderConfig) string {
	if p == nil {
		return ""
	}
	return p.EmbeddingModel
}

// providerFormTitle is the form heading for the add vs edit page.
func providerFormTitle(p *ProjectProviderConfig) string {
	if p == nil {
		return "Add a provider"
	}
	return "Edit " + p.Provider
}
