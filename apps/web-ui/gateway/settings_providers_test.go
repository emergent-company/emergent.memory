package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- merge logic (task 2.2) ---

func sampleProvidersConfigs() []ProjectProviderConfig {
	return []ProjectProviderConfig{
		{ID: "pc1", ProjectID: "proj", Provider: "openai"},
		{ID: "pc2", ProjectID: "proj", Provider: "deepseek"},
	}
}

func sampleCatalogByProvider() map[string][]ProviderSupportedModel {
	return map[string][]ProviderSupportedModel{
		"openai": {
			{Provider: "openai", ModelName: "gpt-4o", ModelType: "generative", DisplayName: "GPT-4o"},
			{Provider: "openai", ModelName: "gpt-4o-mini", ModelType: "generative", DisplayName: "GPT-4o mini"},
		},
		"deepseek": {
			{Provider: "deepseek", ModelName: "deepseek-v4-pro", ModelType: "generative"},
			{Provider: "deepseek", ModelName: "deepseek-v3", ModelType: "generative"},
		},
	}
}

func sampleRetailPricing() []ProviderPricing {
	return []ProviderPricing{
		{ID: "pr1", Provider: "openai", Model: "gpt-4o", modelPriceRates: modelPriceRates{TextInputPrice: 2.5, OutputPrice: 10}},
		{ID: "pr2", Provider: "openai", Model: "gpt-4o-mini", modelPriceRates: modelPriceRates{TextInputPrice: 0.15, OutputPrice: 0.6}},
	}
}

func TestMergeProviderRatesOverrideWins(t *testing.T) {
	overrides := []ProjectCustomPricing{
		{ID: "o1", ProjectID: "proj", Provider: "openai", Model: "gpt-4o", modelPriceRates: modelPriceRates{TextInputPrice: 1.0, OutputPrice: 4.0}},
	}
	rows := mergeProviderRates(sampleProvidersConfigs(), sampleCatalogByProvider(), sampleRetailPricing(), overrides)
	if len(rows) != 4 {
		t.Fatalf("want 4 rows (2+2), got %+v", rows)
	}
	gpt4o := rows[0]
	if gpt4o.Provider != "openai" || gpt4o.Model != "gpt-4o" {
		t.Fatalf("rows[0] = %+v", gpt4o)
	}
	if !gpt4o.IsCustom {
		t.Error("overridden model must be custom")
	}
	if gpt4o.OverrideInput != 1.0 || gpt4o.OverrideOutput != 4.0 {
		t.Errorf("override rate not applied: %+v", gpt4o)
	}
	// the retail rate is still known underneath (auto column present)
	if !gpt4o.HasAuto || gpt4o.AutoInput != 2.5 || gpt4o.AutoOutput != 10 {
		t.Errorf("auto rate should still be tracked: %+v", gpt4o)
	}
}

func TestMergeProviderRatesNoOverrideUsesAuto(t *testing.T) {
	rows := mergeProviderRates(sampleProvidersConfigs(), sampleCatalogByProvider(), sampleRetailPricing(), nil)
	mini := rows[1] // openai/gpt-4o-mini
	if mini.IsCustom {
		t.Error("no override → row must not be custom")
	}
	if !mini.HasAuto || mini.AutoInput != 0.15 || mini.AutoOutput != 0.6 {
		t.Errorf("auto rate missing: %+v", mini)
	}
}

func TestMergeProviderRatesNoRateUnknown(t *testing.T) {
	// deepseek has catalog models but no retail pricing and no overrides.
	rows := mergeProviderRates(sampleProvidersConfigs(), sampleCatalogByProvider(), sampleRetailPricing(), nil)
	v4 := rows[2] // deepseek/deepseek-v4-pro
	if v4.IsCustom || v4.HasAuto {
		t.Errorf("model with no rate must be unknown (auto/custom off): %+v", v4)
	}
	if v4.AutoInput != 0 || v4.AutoOutput != 0 || v4.OverrideInput != 0 || v4.OverrideOutput != 0 {
		t.Errorf("unknown row rates must be zero: %+v", v4)
	}
}

func TestMergeProviderRatesEmptyProviders(t *testing.T) {
	rows := mergeProviderRates(nil, sampleCatalogByProvider(), sampleRetailPricing(), nil)
	if len(rows) != 0 {
		t.Errorf("no providers → no rows, got %+v", rows)
	}
}

func TestMergeProviderRatesOverridesScopedByProvider(t *testing.T) {
	// An override for openai/gpt-4o must not leak onto deepseek/gpt-4o.
	overrides := []ProjectCustomPricing{
		{ProjectID: "proj", Provider: "openai", Model: "gpt-4o", modelPriceRates: modelPriceRates{TextInputPrice: 1.0, OutputPrice: 4.0}},
	}
	catalog := map[string][]ProviderSupportedModel{
		"openai":   {{Provider: "openai", ModelName: "gpt-4o"}},
		"deepseek": {{Provider: "deepseek", ModelName: "gpt-4o"}},
	}
	rows := mergeProviderRates([]ProjectProviderConfig{
		{Provider: "openai"}, {Provider: "deepseek"},
	}, catalog, nil, overrides)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %+v", rows)
	}
	if !rows[0].IsCustom || rows[1].IsCustom {
		t.Errorf("override must apply only to its provider: %+v", rows)
	}
}

func TestMergeProviderRatesDedupesCatalog(t *testing.T) {
	catalog := map[string][]ProviderSupportedModel{
		"openai": {{Provider: "openai", ModelName: "gpt-4o"}, {Provider: "openai", ModelName: "gpt-4o"}},
	}
	rows := mergeProviderRates([]ProjectProviderConfig{{Provider: "openai"}}, catalog, nil, nil)
	if len(rows) != 1 {
		t.Errorf("duplicate catalog entries must collapse, got %+v", rows)
	}
}

func TestMergeProviderRatesConfiguredModelAndModelOnlyMatch(t *testing.T) {
	// A LiteLLM-proxied "openai" provider serves deepseek-v4-flash (its
	// configured generative model) but has an empty catalog; the retail price
	// lives under the "deepseek" provider. The configured model must appear,
	// and its auto rate must resolve via the model-only fallback.
	providers := []ProjectProviderConfig{
		{Provider: "openai", GenerativeModel: "deepseek-v4-flash"},
	}
	catalog := map[string][]ProviderSupportedModel{} // empty catalog
	pricing := []ProviderPricing{
		{Provider: "deepseek", Model: "deepseek-v4-flash", modelPriceRates: modelPriceRates{TextInputPrice: 0.14, OutputPrice: 0.28}},
	}
	rows := mergeProviderRates(providers, catalog, pricing, nil)
	if len(rows) != 1 {
		t.Fatalf("want 1 row (configured model fallback), got %+v", rows)
	}
	r := rows[0]
	if r.Provider != "openai" || r.Model != "deepseek-v4-flash" {
		t.Errorf("row = %+v", r)
	}
	if !r.HasAuto || r.AutoInput != 0.14 || r.AutoOutput != 0.28 {
		t.Errorf("model-only rate match missing: %+v", r)
	}
	if r.IsCustom {
		t.Errorf("no override → must not be custom: %+v", r)
	}
}

func TestStripVendorModelName(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"gpt-4o", "gpt-4o"},
		{"openai/gemini-embedding-001", "gemini-embedding-001"},
		{"models/gemini-2.5-flash", "gemini-2.5-flash"},
		{"deepseek/deepseek-v4-pro:free", "deepseek-v4-pro"},
		{"openai/gpt-4o@2024-08-06", "gpt-4o"},
		{"  Vendor/Model  ", "model"},
	} {
		if got := stripVendorModelName(tt.in); got != tt.want {
			t.Errorf("stripVendorModelName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMergeProviderRatesPrefixStrippedModelOnlyMatch(t *testing.T) {
	// A LiteLLM catalog lists a vendor-prefixed model name
	// ("openai/gemini-embedding-001") while the retail price row is keyed by the
	// bare model name. The model-only fallback must strip the prefix to match,
	// without mutating the displayed model name.
	providers := []ProjectProviderConfig{{Provider: "openai"}}
	catalog := map[string][]ProviderSupportedModel{
		"openai": {{Provider: "openai", ModelName: "openai/gemini-embedding-001", ModelType: "embedding"}},
	}
	pricing := []ProviderPricing{
		{Provider: "google", Model: "gemini-embedding-001", modelPriceRates: modelPriceRates{TextInputPrice: 0.15, OutputPrice: 0}},
	}
	rows := mergeProviderRates(providers, catalog, pricing, nil)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %+v", rows)
	}
	r := rows[0]
	if r.Model != "openai/gemini-embedding-001" {
		t.Errorf("displayed model must keep its prefix: %+v", r)
	}
	if !r.HasAuto || r.AutoInput != 0.15 {
		t.Errorf("prefix-stripped model-only rate match missing: %+v", r)
	}
}

func TestGroupProviderRows(t *testing.T) {
	groups := groupProviderRows([]providerRateRow{
		{Provider: "openai", Model: "gpt-4o"},
		{Provider: "deepseek", Model: "deepseek-v4-pro"},
		{Provider: "openai", Model: "gpt-4o-mini"},
	})
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %+v", groups)
	}
	if groups[0].Provider != "openai" || len(groups[0].Rows) != 2 {
		t.Errorf("group[0] = %+v", groups[0])
	}
	if groups[1].Provider != "deepseek" || len(groups[1].Rows) != 1 {
		t.Errorf("group[1] = %+v", groups[1])
	}
	if groups[0].Rows[0].Model != "gpt-4o" || groups[0].Rows[1].Model != "gpt-4o-mini" {
		t.Errorf("row order lost: %+v", groups[0].Rows)
	}
}

// --- Providers panel UI (task 3.2) ---

func providersTestData() providerPanelData {
	overrides := []ProjectCustomPricing{
		{Provider: "openai", Model: "gpt-4o", modelPriceRates: modelPriceRates{TextInputPrice: 1.0, OutputPrice: 4.0}},
	}
	rows := mergeProviderRates(sampleProvidersConfigs(), sampleCatalogByProvider(), sampleRetailPricing(), overrides)
	models := []Model{
		{Provider: "openai", ModelName: "gpt-4o", ModelType: "generative", DisplayName: "GPT-4o"},
		{Provider: "deepseek", ModelName: "deepseek-v4-pro", ModelType: "generative", DisplayName: "DeepSeek V4 Pro"},
		{Provider: "google", ModelName: "gemini-embedding-001", ModelType: "embedding"},
	}
	return providerPanelData{
		Groups:         groupProviderRows(rows),
		Providers:      sampleProvidersConfigs(),
		ModelConfig:    &ProjectModelConfig{GenerativeModel: "deepseek/deepseek-v4-pro", EmbeddingModel: "google/gemini-embedding-001"},
		Models:         models,
		ProviderModels: sampleCatalogByProvider(),
	}
}

func TestRenderProvidersPanel(t *testing.T) {
	html := renderHTML(t, providersPanel(providersTestData()))
	for _, want := range []string{
		"Default models", "Provider configuration", "Rates",
		"openai", "deepseek",
		"gpt-4o", "gpt-4o-mini", "deepseek-v4-pro", "deepseek-v3",
		// custom row shows its override rate + custom badge
		"custom", "$1 in / $4 out",
		// automatic row shows the retail rate (no badge)
		"$0.15 in / $0.6 out",
		// no rate at all → unknown, not an error
		"unknown rate",
		// inline auto-save override + remove forms
		`hx-post="/settings/providers/openai/gpt-4o"`,
		`hx-post="/settings/providers/openai/gpt-4o/delete"`,
		`name="textInputPrice"`, `name="outputPrice"`, "Remove", `hx-swap="none"`,
		// default-model selectors (inline save)
		`hx-post="/settings/providers/model-config"`, `name="generative_model"`, `name="embedding_model"`,
		// provider config list: add link, edit link, test/remove actions
		`href="/settings/providers/new"`, `href="/settings/providers/openai/edit"`,
		`hx-post="/settings/providers/openai/test"`, `action="/settings/providers/openai/remove"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("providers panel missing %q", want)
		}
	}
	for _, gone := range []string{"Override", "Update rate", `action="/settings/providers/config"`, "Save provider"} {
		if strings.Contains(html, gone) {
			t.Errorf("providers panel must not contain %q", gone)
		}
	}
}

func TestRenderProvidersPanelEmpty(t *testing.T) {
	html := renderHTML(t, providersPanel(providerPanelData{}))
	// Zero providers renders the first-provider call to action; the
	// rates/default-model boxes are hidden.
	for _, want := range []string{"Connect your first LLM provider", "Add your first provider", `href="/settings/providers/new"`} {
		if !strings.Contains(html, want) {
			t.Errorf("empty panel missing %q", want)
		}
	}
	for _, gone := range []string{"Failed to load providers", "No providers configured yet", "No models to price", "Default models"} {
		if strings.Contains(html, gone) {
			t.Errorf("empty panel must not render %q", gone)
		}
	}
}

func TestRenderProvidersPanelError(t *testing.T) {
	html := renderHTML(t, providersPanel(providerPanelData{Err: errTest}))
	if !strings.Contains(html, "Failed to load providers") {
		t.Error("error state missing")
	}
}

// --- embedding-provider guidance callout ---

func TestProviderSupportsEmbedding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider ProjectProviderConfig
		models   []ProviderSupportedModel
		want     bool
	}{
		{
			name:     "explicit embedding model on otherwise generative provider",
			provider: ProjectProviderConfig{Provider: "openai", EmbeddingModel: "text-embedding-3-small"},
			want:     true,
		},
		{
			name:     "catalog embedding entry",
			provider: ProjectProviderConfig{Provider: "openai"},
			models:   []ProviderSupportedModel{{Provider: "openai", ModelName: "text-embedding-3-small", ModelType: "embedding"}},
			want:     true,
		},
		{
			name:     "generative-only openai",
			provider: ProjectProviderConfig{Provider: "openai"},
			models:   []ProviderSupportedModel{{Provider: "openai", ModelName: "gpt-4o", ModelType: "generative"}},
			want:     false,
		},
		{
			name:     "generative-only deepseek",
			provider: ProjectProviderConfig{Provider: "deepseek", GenerativeModel: "deepseek-v4-pro"},
			want:     false,
		},
		{
			name:     "google is embedding-capable by type",
			provider: ProjectProviderConfig{Provider: "google"},
			want:     true,
		},
		{
			name:     "google-vertex is embedding-capable by type",
			provider: ProjectProviderConfig{Provider: "google-vertex"},
			want:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerSupportsEmbedding(tc.provider, tc.models); got != tc.want {
				t.Errorf("providerSupportsEmbedding(%+v, %+v) = %v, want %v", tc.provider, tc.models, got, tc.want)
			}
		})
	}
}

// TestRenderEmbeddingProviderWarningGenerativeOnly covers the guidance callout:
// providers configured but none embedding-capable (openai + deepseek) renders
// the document-indexing guidance and links to the add-provider flow.
func TestRenderEmbeddingProviderWarningGenerativeOnly(t *testing.T) {
	d := providerPanelData{
		Providers: []ProjectProviderConfig{{Provider: "openai"}, {Provider: "deepseek"}},
		ProviderModels: map[string][]ProviderSupportedModel{
			"openai":   {{Provider: "openai", ModelName: "gpt-4o", ModelType: "generative"}},
			"deepseek": {{Provider: "deepseek", ModelName: "deepseek-v4-pro", ModelType: "generative"}},
		},
	}
	if !providersNeedEmbeddingProvider(d) {
		t.Fatal("generative-only providers must need an embedding provider")
	}
	html := renderHTML(t, providersPanel(d))
	for _, want := range []string{
		"providers can generate embeddings",
		"document indexing is unavailable",
		"Add embedding provider",
		`href="/settings/providers/new"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("guidance callout missing %q", want)
		}
	}
}

// TestRenderEmbeddingProviderWarningAbsentWithEmbeddingProvider covers the
// hidden state: a configured embedding-capable provider suppresses the callout.
func TestRenderEmbeddingProviderWarningAbsentWithEmbeddingProvider(t *testing.T) {
	d := providerPanelData{
		Providers: []ProjectProviderConfig{
			{Provider: "openai"},
			{Provider: "google", EmbeddingModel: "gemini-embedding-001"},
		},
		ProviderModels: map[string][]ProviderSupportedModel{
			"openai": {{Provider: "openai", ModelName: "gpt-4o", ModelType: "generative"}},
		},
	}
	if providersNeedEmbeddingProvider(d) {
		t.Fatal("an embedding-capable provider must suppress the guidance callout")
	}
	html := renderHTML(t, providersPanel(d))
	if strings.Contains(html, "can generate embeddings") {
		t.Errorf("guidance callout must not render, got:\n%s", html)
	}
}

// TestRenderEmbeddingProviderWarningAbsentNoProviders covers the other hidden
// state: zero configured providers renders the first-provider CTA, no guidance.
func TestRenderEmbeddingProviderWarningAbsentNoProviders(t *testing.T) {
	d := providerPanelData{}
	if providersNeedEmbeddingProvider(d) {
		t.Fatal("zero providers must not need an embedding provider")
	}
	html := renderHTML(t, providersPanel(d))
	if strings.Contains(html, "can generate embeddings") {
		t.Errorf("guidance callout must not render with zero providers, got:\n%s", html)
	}
}

// TestProvidersNeedEmbeddingProviderIgnoresLoadError asserts the fail-safe: an
// errored provider load never nags, even when the partial list is empty.
func TestProvidersNeedEmbeddingProviderIgnoresLoadError(t *testing.T) {
	if providersNeedEmbeddingProvider(providerPanelData{Err: errTest}) {
		t.Fatal("load error must not surface embedding guidance")
	}
}

// TestDefaultModelSelectLiteLLMProxyCredentialPrefix covers the LiteLLM-proxy
// case: deepseek models are served through a configured "openai" credential
// provider (empty per-provider catalog, bare configured generative model), so
// the generative dropdown must offer openai/deepseek-v4-flash and nothing
// prefixed with the ROUTING provider "deepseek".
func TestDefaultModelSelectLiteLLMProxyCredentialPrefix(t *testing.T) {
	d := providerPanelData{
		Providers: []ProjectProviderConfig{
			{Provider: "openai", BaseURL: "http://litellm:4000/v1", GenerativeModel: "deepseek-v4-flash"},
		},
		ProviderModels: map[string][]ProviderSupportedModel{},
	}
	html := renderHTML(t, defaultModelsPanel(d))
	if !strings.Contains(html, `value="openai/deepseek-v4-flash"`) {
		t.Errorf("generative dropdown must offer the credential-prefixed model, got:\n%s", html)
	}
	if strings.Contains(html, `value="deepseek/`) {
		t.Errorf("dropdown must not offer routing-provider-prefixed models (deepseek/), got:\n%s", html)
	}
}

func TestProviderModelSelectCurrentFallback(t *testing.T) {
	models := []Model{
		{Provider: "openai", ModelName: "deepseek-v4-flash", ModelType: "generative"},
		{Provider: "google", ModelName: "gemini-embedding-2-preview", ModelType: "embedding"},
	}
	html := renderHTML(t, providerModelSelect("generative_model", "Generative model", "deepseek-v4-flash", models))
	if !strings.Contains(html, `value="openai/deepseek-v4-flash"`) {
		t.Errorf("dropdown must offer prefixed options, got:\n%s", html)
	}
	// A bare legacy value stays selectable via a "(current)" fallback option.
	if !strings.Contains(html, `value="deepseek-v4-flash"`) || !strings.Contains(html, "(current)") {
		t.Errorf("dropdown must keep the bare current value selectable, got:\n%s", html)
	}
}

func TestProviderModelSelectPrefixedCurrent(t *testing.T) {
	models := []Model{{Provider: "openai", ModelName: "deepseek-v4-flash", ModelType: "generative"}}
	html := renderHTML(t, providerModelSelect("generative_model", "Generative model", "openai/deepseek-v4-flash", models))
	if strings.Contains(html, "(current)") {
		t.Errorf("prefixed current must not render a (current) fallback, got:\n%s", html)
	}
}

// TestRenderProvidersPagePanel asserts the Providers panel renders via the
// Providers settings page (data.Providers) and shows its empty state.
func TestRenderProvidersPagePanel(t *testing.T) {
	data := providersSettingsPageData{Providers: providersTestData()}
	html := renderHTML(t, ProvidersSettingsPage(data))
	for _, want := range []string{"Providers", "openai", "gpt-4o", "custom"} {
		if !strings.Contains(html, want) {
			t.Errorf("providers settings page missing %q", want)
		}
	}
	// Without providers the page hides the rates/default-model boxes and shows
	// the first-provider call to action instead.
	plain := renderHTML(t, ProvidersSettingsPage(providersSettingsPageData{}))
	for _, want := range []string{"Connect your first LLM provider", "Add your first provider"} {
		if !strings.Contains(plain, want) {
			t.Errorf("empty providers page missing %q", want)
		}
	}
	if strings.Contains(plain, "No models to price") || strings.Contains(plain, "Default models") {
		t.Error("empty providers page must hide the rates/default-model boxes")
	}
}

// providerSaveButtonTag returns the <button ...> opening tag carrying
// id="provider-save-btn" (attribute-order independent), for disabled-gate
// assertions.
func providerSaveButtonTag(t *testing.T, html string) string {
	t.Helper()
	marker := `id="provider-save-btn"`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("save button missing from markup:\n%s", html)
	}
	open := strings.LastIndex(html[:i], "<button")
	if open < 0 {
		t.Fatalf("save button tag malformed:\n%s", html)
	}
	end := strings.Index(html[i:], ">")
	if end < 0 {
		t.Fatalf("save button tag unterminated:\n%s", html)
	}
	return html[open : i+end+1]
}

// TestRenderProviderConfigPageAddIsCredentialsOnly asserts the add-provider
// form (Provider == nil) is credentials-only: no fallback model pickers, no
// Test-connection button — the connection-gated seeding lives on the edit form
// only.
func TestRenderProviderConfigPageAddIsCredentialsOnly(t *testing.T) {
	html := renderHTML(t, providerConfigPage(providerConfigPageData{}))
	for _, want := range []string{"Add a provider", "Add provider", `name="api_key"`} {
		if !strings.Contains(html, want) {
			t.Errorf("add form missing %q, got:\n%s", want, html)
		}
	}
	for _, banned := range []string{
		"provider-fallback-models",
		"Test connection",
		`name="generative_model"`,
		`name="embedding_model"`,
		"Fallback models used when the project has no default set.",
	} {
		if strings.Contains(html, banned) {
			t.Errorf("add form must be credentials-only but contains %q, got:\n%s", banned, html)
		}
	}
}

// TestRenderProviderConfigPageAddOpenAISaveEnabled asserts the add form is
// never save-gated: even with openai preselected (the provider whose edit form
// gates Save behind a passing connection test), the submit button carries no
// disabled attribute — memory validates credentials on save and the form stays
// retryable after a failed-save re-render.
func TestRenderProviderConfigPageAddOpenAISaveEnabled(t *testing.T) {
	html := renderHTML(t, providerConfigPage(providerConfigPageData{DraftProvider: "openai"}))
	if !strings.Contains(html, `value="openai" selected`) {
		t.Errorf("add form should preselect openai from DraftProvider, got:\n%s", html)
	}
	if tag := providerSaveButtonTag(t, html); strings.Contains(tag, "disabled") {
		t.Errorf("add form save button must not be disabled, got %q", tag)
	}
}

// TestRenderProviderConfigPageEditKeepsModelControls asserts the edit form
// (Provider != nil) keeps the model-seeding surface: the fallback container,
// both model selects, the Test-connection button — and that openai edits gate
// Save behind a passing test (disabled until the connection test enables it).
func TestRenderProviderConfigPageEditKeepsModelControls(t *testing.T) {
	data := providerConfigPageData{Provider: &ProjectProviderConfig{Provider: "openai"}}
	html := renderHTML(t, providerConfigPage(data))
	for _, want := range []string{
		"provider-fallback-models",
		"Test connection",
		`name="generative_model"`,
		`name="embedding_model"`,
		"Fallback models used when the project has no default set.",
		"Save provider",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("edit form missing %q, got:\n%s", want, html)
		}
	}
	if tag := providerSaveButtonTag(t, html); !strings.Contains(tag, "disabled") {
		t.Errorf("openai edit form save button must start disabled (connection-test gate), got %q", tag)
	}
}

// --- override routes (task 4.2) ---

func newProvidersSettingsEcho(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/settings", s.uiProjectSettings)
	e.GET("/settings/providers", s.uiProjectProvidersSettings)
	e.POST("/settings/providers/:provider/:model", s.uiProjectSettingsProviderOverride)
	e.POST("/settings/providers/:provider/:model/delete", s.uiProjectSettingsProviderOverrideDelete)
	e.POST("/settings/providers/config", s.uiProjectProviderConfig)
	e.POST("/settings/providers/test", s.uiProjectProviderTestConnection)
	e.POST("/settings/providers/check-url", s.uiProjectProviderCheckBaseURL)
	e.POST("/settings/providers/model-config", s.uiProjectModelConfig)
	e.POST("/settings/providers/:provider/test", s.uiProjectProviderTest)
	e.POST("/settings/providers/:provider/remove", s.uiProjectProviderRemove)
	return s, e
}

func overrideFormPost(path, textIn, out string) *http.Request {
	form := url.Values{}
	if textIn != "" {
		form.Set("textInputPrice", textIn)
	}
	if out != "" {
		form.Set("outputPrice", out)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestUIProviderOverridePersists(t *testing.T) {
	f := &fakeMemory{
		project:          &Project{ID: "p1", Name: "Home"},
		projectProviders: sampleProvidersConfigs(),
		modelsByProvider: sampleCatalogByProvider(),
		pricing:          sampleRetailPricing(),
	}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, overrideFormPost("/settings/providers/openai/gpt-4o", "1.25", "5"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("override save should trigger a success toast, got %q", hdr)
	}
	if len(f.overrideWrites) != 1 {
		t.Fatalf("override not persisted: %+v", f.overrideWrites)
	}
	w := f.overrideWrites[0]
	if w.Provider != "openai" || w.Model != "gpt-4o" || w.TextInputPrice != 1.25 || w.OutputPrice != 5 {
		t.Errorf("persisted override = %+v", w)
	}

	// The page re-render reflects the custom rate (fake list reflects writes).
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/providers", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "custom") || !strings.Contains(body, "$1.25 in / $5 out") {
		t.Errorf("providers page should reflect the saved override:\n%s", body)
	}
}

func TestUIProviderOverrideValidation(t *testing.T) {
	cases := []struct {
		name string
		path string
		text string
		out  string
		want string
	}{
		{"negative input", "/settings/providers/openai/gpt-4o", "-1", "5", "non-negative"},
		{"negative output", "/settings/providers/openai/gpt-4o", "1", "-0.5", "non-negative"},
		{"non-numeric input", "/settings/providers/openai/gpt-4o", "abc", "5", "number"},
		{"non-numeric output", "/settings/providers/openai/gpt-4o", "1", "NaN", "number"},
		{"missing input", "/settings/providers/openai/gpt-4o", "", "5", "required"},
		{"missing output", "/settings/providers/openai/gpt-4o", "1", "", "required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeMemory{}
			_, e := newProvidersSettingsEcho(f)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, overrideFormPost(c.path, c.text, c.out))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			hdr := rec.Header().Get("HX-Trigger")
			if !strings.Contains(hdr, `"kind":"error"`) || !strings.Contains(hdr, c.want) {
				t.Errorf("HX-Trigger = %q, want error containing %q", hdr, c.want)
			}
			if len(f.overrideWrites) != 0 {
				t.Errorf("no override must be written on invalid input: %+v", f.overrideWrites)
			}
		})
	}
}

func TestUIProviderOverrideBackendError(t *testing.T) {
	f := &fakeMemory{providerErr: fmt.Errorf("memory 503: service down")}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, overrideFormPost("/settings/providers/openai/gpt-4o", "1", "4"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "service down") {
		t.Errorf("backend error must surface in the toast, got HX-Trigger %q", hdr)
	}
}

func TestUIProviderOverrideDelete(t *testing.T) {
	f := &fakeMemory{
		project:          &Project{ID: "p1", Name: "Home"},
		projectProviders: sampleProvidersConfigs(),
		modelsByProvider: sampleCatalogByProvider(),
		pricing:          sampleRetailPricing(),
		pricingOverrides: []ProjectCustomPricing{
			{Provider: "openai", Model: "gpt-4o", modelPriceRates: modelPriceRates{TextInputPrice: 1.0, OutputPrice: 4.0}},
		},
	}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/providers/openai/gpt-4o/delete", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("remove should trigger a success toast, got %q", hdr)
	}
	if len(f.deletedOverrides) != 1 || f.deletedOverrides[0] != "openai\x00gpt-4o" {
		t.Errorf("delete not forwarded: %v", f.deletedOverrides)
	}
	// page re-render reverts to the automatic rate
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/providers", nil))
	body := rec.Body.String()
	if strings.Contains(body, "$1 in / $4 out") || !strings.Contains(body, "$2.5 in / $10 out") {
		t.Errorf("providers page should show the auto rate after removal:\n%s", body)
	}
}

func TestUIProviderOverrideDeleteBackendError(t *testing.T) {
	f := &fakeMemory{providerErr: fmt.Errorf("memory 503: service down")}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/providers/openai/gpt-4o/delete", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "service down") {
		t.Errorf("backend error must surface in the toast, got HX-Trigger %q", hdr)
	}
}

func TestUIProviderConfigPersists(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/config", strings.NewReader("provider=openai&api_key=sk-test&base_url=http://litellm:4000/v1&generative_model=openai/deepseek-v4-flash&embedding_model=google/gemini-embedding-2-preview"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/providers?updated=1" {
		t.Fatalf("config save should redirect, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastProviderConfig != "openai" {
		t.Errorf("provider = %q, want openai", f.lastProviderConfig)
	}
	if f.providerConfigInput.APIKey != "sk-test" || f.providerConfigInput.BaseURL != "http://litellm:4000/v1" ||
		f.providerConfigInput.GenerativeModel != "openai/deepseek-v4-flash" || f.providerConfigInput.EmbeddingModel != "google/gemini-embedding-2-preview" {
		t.Errorf("config input = %+v", f.providerConfigInput)
	}
}

// TestUIProviderConfigPersistsHTMXFullLoad: a boosted/HTMX provider save must
// force a full page load (HX-Redirect) so the shell's sidebar provider warning
// badge updates without a manual refresh. Only #main-content is swapped by a
// boosted 303, which leaves the badge stale.
func TestUIProviderConfigPersistsHTMXFullLoad(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/config", strings.NewReader("provider=openai&api_key=sk-test&base_url=http://litellm:4000/v1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/settings/providers?updated=1" {
		t.Fatalf("HTMX config save should force a full reload, got %d HX-Redirect %q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
	if f.lastProviderConfig != "openai" {
		t.Errorf("provider = %q, want openai", f.lastProviderConfig)
	}
}

func TestUIProviderConfigMissingProvider(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/config", strings.NewReader("api_key=sk-test"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (form re-render)", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, "provider is required") {
		t.Errorf("re-render should surface the error, got body:\n%s", body)
	}
	if f.lastProviderConfig != "" {
		t.Error("missing provider must not upsert")
	}
}

func TestUIProviderConfigInvalidBaseURL(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/config", strings.NewReader("provider=openai&api_key=sk-test&base_url=not-a-url"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (form re-render)", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, "base_url") {
		t.Errorf("invalid base_url should re-render with the error, got body:\n%s", body)
	}
	if f.lastProviderConfig != "" {
		t.Error("invalid base_url must not upsert")
	}
}

func TestUIProviderConfigUnknownProvider(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/config", strings.NewReader("provider=hacker&api_key=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (form re-render)", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, "unsupported provider") {
		t.Errorf("unknown provider should re-render with the error, got body:\n%s", body)
	}
	if f.lastProviderConfig != "" {
		t.Error("unknown provider must not upsert")
	}
}

func TestUIProviderConfigBackendErrorHint(t *testing.T) {
	f := &fakeMemory{providerErr: fmt.Errorf("memory 500 internal_error: An internal error occurred")}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/config", strings.NewReader("provider=openai&api_key=sk-test&base_url=http://litellm:4000"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (form re-render)", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "internal_error") {
		t.Errorf("backend failure should wrap the original error, got body:\n%s", body)
	}
	// The save-error message must NOT blame base_url — reachability has its own
	// inline check (/settings/providers/check-url).
	if strings.Contains(body, "cannot reach") {
		t.Errorf("save error must not claim a base_url reachability problem, got body:\n%s", body)
	}
}

func TestUIProviderCheckBaseURL(t *testing.T) {
	t.Run("empty base_url neutral", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newProvidersSettingsEcho(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/providers/check-url", strings.NewReader("base_url="))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var res baseURLCheckResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("response is not the expected JSON: %v (body %q)", err, rec.Body.String())
		}
		if res.OK || res.Detail != "" {
			t.Errorf("empty base_url must be a neutral {ok:false, detail:\"\"}, got %+v", res)
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newProvidersSettingsEcho(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/providers/check-url", strings.NewReader("base_url=not-a-url"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var res baseURLCheckResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("response is not the expected JSON: %v (body %q)", err, rec.Body.String())
		}
		if res.OK || !strings.Contains(res.Detail, "valid http(s) URL") {
			t.Errorf("invalid url must be ok:false with a url detail, got %+v", res)
		}
	})

	t.Run("reachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		f := &fakeMemory{}
		_, e := newProvidersSettingsEcho(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/providers/check-url", strings.NewReader("base_url="+url.QueryEscape(srv.URL)+"&api_key=sk-test"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var res baseURLCheckResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("response is not the expected JSON: %v (body %q)", err, rec.Body.String())
		}
		if !res.OK || res.Status != http.StatusOK {
			t.Errorf("reachable url must be ok:true status:200, got %+v", res)
		}
	})

	t.Run("auth error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		f := &fakeMemory{}
		_, e := newProvidersSettingsEcho(f)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/providers/check-url", strings.NewReader("base_url="+url.QueryEscape(srv.URL)+"&api_key=bad"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var res baseURLCheckResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("response is not the expected JSON: %v (body %q)", err, rec.Body.String())
		}
		if res.OK || res.Status != http.StatusUnauthorized {
			t.Errorf("unauthorized must be ok:false status:401, got %+v", res)
		}
	})
}

func TestProbeProviderEndpointOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ok, status, detail := probeProviderEndpoint(srv.URL+"/v1", "key")
	if !ok || status != http.StatusOK {
		t.Errorf("probe = ok:%v status:%d detail:%q, want ok:true status:200", ok, status, detail)
	}
}

func TestProbeProviderEndpointAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	ok, status, detail := probeProviderEndpoint(srv.URL, "bad")
	if ok || status != http.StatusUnauthorized {
		t.Errorf("probe = ok:%v status:%d detail:%q, want ok:false status:401", ok, status, detail)
	}
}

// connectionTestPost POSTs a form to the test-connection handler and returns
// the recorder.
func connectionTestPost(t *testing.T, e *echo.Echo, form string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/test", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

// toastHeader extracts the memory-toast HX-Trigger detail so assertions can
// read the toast message without depending on header quoting.
func toastHeader(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return rec.Header().Get("HX-Trigger")
}

// selectMarkup returns the fragment between a select's opening tag and its
// closing </select> (selects never nest inside the fallback fragment).
func selectMarkup(t *testing.T, html, name string) string {
	t.Helper()
	start := strings.Index(html, `<select name="`+name+`"`)
	if start < 0 {
		t.Fatalf("select %q missing from fragment:\n%s", name, html)
	}
	end := strings.Index(html[start:], "</select>")
	if end < 0 {
		t.Fatalf("select %q unterminated:\n%s", name, html)
	}
	return html[start : start+end]
}

// assertFallbackFragmentUnseeded checks the response body still carries both
// fallback selects with no provider model options (validation/probe-failure
// paths must never wipe the selects).
func assertFallbackFragmentUnseeded(t *testing.T, body string) {
	t.Helper()
	gen := selectMarkup(t, body, "generative_model")
	emb := selectMarkup(t, body, "embedding_model")
	if strings.Contains(gen, `value="openai/`) || strings.Contains(emb, `value="openai/`) {
		t.Errorf("failed path must not seed model options:\n%s", body)
	}
}

func TestUIProviderTestConnectionEmptyAPIKey(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	// base url that would fail fast if dialed (nothing listens on :1): the
	// empty-key check must return before any network call.
	rec := connectionTestPost(t, e, "provider=openai&base_url=http://127.0.0.1:1/v1&api_key=")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	hdr := toastHeader(t, rec)
	if !strings.Contains(hdr, "Enter an API key before testing the connection.") {
		t.Errorf("empty key toast missing the message, got %q", hdr)
	}
	if strings.Contains(hdr, "Connection failed") || strings.Contains(hdr, "Connection OK") {
		t.Errorf("empty key must not probe the endpoint, got %q", hdr)
	}
	// The response body still carries the fallback fragment (empty lists) so
	// the selects are not wiped by the failed test.
	body := rec.Body.String()
	if body == "" {
		t.Fatal("empty key path must still return the fallback fragment body")
	}
	if !strings.Contains(body, `name="generative_model"`) || !strings.Contains(body, `name="embedding_model"`) {
		t.Errorf("empty key fragment must keep both selects, got:\n%s", body)
	}
	assertFallbackFragmentUnseeded(t, body)
}

func TestUIProviderTestConnectionSeedsFallbackModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"text-embedding-3-small"},{"id":"vendor/deepseek-v4-flash"}]}`))
	}))
	defer srv.Close()
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := connectionTestPost(t, e, "provider=openai&base_url="+url.QueryEscape(srv.URL)+"&api_key=sk-test")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := toastHeader(t, rec); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("successful test should trigger a success toast, got %q", hdr)
	}
	body := rec.Body.String()
	if body == "" {
		t.Fatal("success path must return the seeded fallback fragment body")
	}
	// Strict-superset contract (interim bound; see
	// docs/tasks/provider-model-classification-parity.md and the
	// fetchOpenAICompatFallbackModels / nameLooksEmbedding mirror comments in
	// settings_providers.go): every fetched id is offered in the generative
	// select, and embedding-heuristic matches are ADDITIONALLY offered in the
	// embedding select. A heuristic miss must never drop a model.
	gen := selectMarkup(t, body, "generative_model")
	for _, want := range []string{
		`value="openai/gpt-4o"`,
		`value="openai/vendor/deepseek-v4-flash"`,
		`value="openai/text-embedding-3-small"`,
	} {
		if !strings.Contains(gen, want) {
			t.Errorf("generative select missing %s, got:\n%s", want, gen)
		}
	}
	if !strings.Contains(gen, ">deepseek-v4-flash</option>") {
		t.Errorf("generative select must display the vendor-stripped name, got:\n%s", gen)
	}
	emb := selectMarkup(t, body, "embedding_model")
	// Heuristic match: appears in BOTH selects.
	if !strings.Contains(emb, `value="openai/text-embedding-3-small"`) {
		t.Errorf("embedding select missing text-embedding-3-small, got:\n%s", emb)
	}
	// Non-matches: generative only, never in the embedding select.
	if strings.Contains(emb, "gpt-4o") || strings.Contains(emb, "deepseek-v4-flash") {
		t.Errorf("embedding select must not contain non-matching models, got:\n%s", emb)
	}
}

func TestUIProviderTestConnectionUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Authentication Error, No api key passed in.","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer srv.Close()
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := connectionTestPost(t, e, "provider=openai&base_url="+url.QueryEscape(srv.URL)+"&api_key=bad-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	hdr := toastHeader(t, rec)
	if !strings.Contains(hdr, "Authentication failed (HTTP 401)") {
		t.Errorf("401 toast missing the friendly message, got %q", hdr)
	}
	if strings.Contains(hdr, "Authentication Error, No api key passed in.") {
		t.Errorf("401 toast must not surface the raw upstream body, got %q", hdr)
	}
	if strings.Contains(hdr, "Connection failed (HTTP 401)") {
		t.Errorf("401 toast must drop the generic failure framing, got %q", hdr)
	}
	// 401 is a probe failure: the body still returns the empty fallback fragment.
	assertFallbackFragmentUnseeded(t, rec.Body.String())
}

func TestUIProviderTestConnectionOtherFailureKeepsFraming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`upstream exploded`))
	}))
	defer srv.Close()
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := connectionTestPost(t, e, "provider=openai&base_url="+url.QueryEscape(srv.URL)+"&api_key=key")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	hdr := toastHeader(t, rec)
	if !strings.Contains(hdr, "Connection failed (HTTP 500)") || !strings.Contains(hdr, "upstream exploded") {
		t.Errorf("non-401 failure must keep the framing + detail, got %q", hdr)
	}
	// Probe failure still returns the empty fallback fragment, not a wiped form.
	body := rec.Body.String()
	if !strings.Contains(body, `name="generative_model"`) || !strings.Contains(body, `name="embedding_model"`) {
		t.Errorf("probe failure must keep both fallback selects, got:\n%s", body)
	}
	assertFallbackFragmentUnseeded(t, body)
}

// withOpenAIOfficialBaseURL redirects the official OpenAI endpoint var for the
// duration of a test so the empty-base_url path can be exercised against an
// httptest server instead of dialing the real API.
func withOpenAIOfficialBaseURL(t *testing.T, u string) {
	t.Helper()
	orig := openAIOfficialBaseURL
	openAIOfficialBaseURL = u
	t.Cleanup(func() { openAIOfficialBaseURL = orig })
}

func TestEffectiveTestBaseURL(t *testing.T) {
	cases := []struct {
		name, provider, baseURL, want string
	}{
		{"openai empty defaults to official endpoint", "openai", "", "https://api.openai.com/v1"},
		{"openai present kept as typed", "openai", "http://litellm:4000/v1", "http://litellm:4000/v1"},
		{"non-openai empty stays empty (guard survives)", "google", "", ""},
		{"non-openai present kept", "google", "http://proxy:4000/v1", "http://proxy:4000/v1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveTestBaseURL(c.provider, c.baseURL); got != c.want {
				t.Errorf("effectiveTestBaseURL(%q, %q) = %q, want %q", c.provider, c.baseURL, got, c.want)
			}
		})
	}
}

func TestUIProviderTestConnectionEmptyBaseURLForOpenAI(t *testing.T) {
	t.Run("probe runs against official endpoint", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided.","type":"invalid_request_error","code":"invalid_api_key"}}`))
		}))
		defer srv.Close()
		withOpenAIOfficialBaseURL(t, srv.URL)
		f := &fakeMemory{}
		_, e := newProvidersSettingsEcho(f)
		rec := connectionTestPost(t, e, "provider=openai&base_url=&api_key=sk-test")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		hdr := toastHeader(t, rec)
		if strings.Contains(hdr, "base_url is required") {
			t.Errorf("openai + empty base_url must not be rejected up front, got %q", hdr)
		}
		// The 401 toast proves the probe actually ran (against the defaulted
		// official endpoint redirected to the test server).
		if !strings.Contains(hdr, "Authentication failed (HTTP 401)") {
			t.Errorf("empty base_url must probe and surface the 401, got %q", hdr)
		}
		assertFallbackFragmentUnseeded(t, rec.Body.String())
	})

	t.Run("seeding uses the defaulted endpoint", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"text-embedding-3-small"}]}`))
		}))
		defer srv.Close()
		withOpenAIOfficialBaseURL(t, srv.URL)
		f := &fakeMemory{}
		_, e := newProvidersSettingsEcho(f)
		rec := connectionTestPost(t, e, "provider=openai&base_url=&api_key=sk-test")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if hdr := toastHeader(t, rec); !strings.Contains(hdr, `"kind":"success"`) {
			t.Errorf("official-endpoint test should succeed, got %q", hdr)
		}
		gen := selectMarkup(t, rec.Body.String(), "generative_model")
		if !strings.Contains(gen, `value="openai/gpt-4o"`) {
			t.Errorf("seeding must run against the defaulted official endpoint, got:\n%s", gen)
		}
	})
}

func TestUIProviderTestConnectionEmptyBaseURLNonOpenAI(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	// Defensive guard: only openai's official default substitutes an empty
	// base_url; other providers are still rejected (probe would have nowhere
	// to go).
	rec := connectionTestPost(t, e, "provider=deepseek&base_url=&api_key=sk-test")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := toastHeader(t, rec); !strings.Contains(hdr, "base_url is required to test the connection") {
		t.Errorf("non-openai empty base_url must keep the guard toast, got %q", hdr)
	}
}

func TestUIProviderRemove(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/providers/openai/remove", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/providers?updated=1" {
		t.Fatalf("remove should redirect, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.deletedProviderConfigs) != 1 || f.deletedProviderConfigs[0] != "openai" {
		t.Errorf("deleted = %v, want [openai]", f.deletedProviderConfigs)
	}
}

// TestUIProviderRemoveHTMXFullLoad: removing the last provider must full-load
// so the sidebar provider warning badge reappears without a manual refresh.
func TestUIProviderRemoveHTMXFullLoad(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/openai/remove", nil)
	req.Header.Set("HX-Request", "true")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/settings/providers?updated=1" {
		t.Fatalf("HTMX remove should force a full reload, got %d HX-Redirect %q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
	if len(f.deletedProviderConfigs) != 1 || f.deletedProviderConfigs[0] != "openai" {
		t.Errorf("deleted = %v, want [openai]", f.deletedProviderConfigs)
	}
}

func TestUIProviderTest(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/providers/openai/test", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("test should trigger a success toast, got %q", hdr)
	}
}

func TestUIProviderTestError(t *testing.T) {
	f := &fakeMemory{providerErr: fmt.Errorf("memory 503: service down")}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/providers/openai/test", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "service down") {
		t.Errorf("failed test should trigger an error toast, got %q", hdr)
	}
}

func TestUIProviderModelConfig(t *testing.T) {
	f := &fakeMemory{}
	_, e := newProvidersSettingsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/providers/model-config", strings.NewReader("generative_model=deepseek/deepseek-v4-pro&embedding_model=google/gemini-embedding-001"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("model config should trigger a success toast, got %q", hdr)
	}
	if f.lastModelConfig == nil || f.lastModelConfig.GenerativeModel != "deepseek/deepseek-v4-pro" || f.lastModelConfig.EmbeddingModel != "google/gemini-embedding-001" {
		t.Errorf("model config = %+v", f.lastModelConfig)
	}
}

// TestSettingsProvidersLoadFailure covers the panel-level error: a failed
// providers read renders the error inside the panel while the Providers page
// stays usable (200).
func TestSettingsProvidersLoadFailure(t *testing.T) {
	f := &fakeMemory{
		project:     &Project{ID: "p1", Name: "Home"},
		providerErr: fmt.Errorf("memory 503: service down"),
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/settings/providers", s.uiProjectProvidersSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/providers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (panel error must not crash the page)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Failed to load providers") || !strings.Contains(body, "service down") {
		t.Errorf("panel error missing:\n%s", body)
	}
}
