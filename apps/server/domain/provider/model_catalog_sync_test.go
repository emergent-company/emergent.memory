package provider

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// ---------------------------------------------------------------------------
// In-memory repository fake
//
// Satisfies both modelCatalogRepo (used by ModelCatalogService) and
// modelCatalogSyncRepo (used by ModelCatalogSyncService) so the resync pass and
// SyncModels can be exercised end-to-end without a database.
// ---------------------------------------------------------------------------

type modelCatalogFakeRepo struct {
	configs     []ProjectProviderConfig
	stored      map[string]ProviderSupportedModel // key: provider + "\x00" + model_name
	upsertCalls [][]ProviderSupportedModel
	pruneCalls  []fakePruneCall
}

type fakePruneCall struct {
	provider ProviderType
	names    []string
}

func newModelCatalogFakeRepo(configs ...ProjectProviderConfig) *modelCatalogFakeRepo {
	return &modelCatalogFakeRepo{
		configs: configs,
		stored:  make(map[string]ProviderSupportedModel),
	}
}

func modelStoreKey(provider ProviderType, name string) string {
	return string(provider) + "\x00" + name
}

func (f *modelCatalogFakeRepo) ListProjectProviderConfigsByProvider(_ context.Context, provider ProviderType) ([]ProjectProviderConfig, error) {
	var out []ProjectProviderConfig
	for _, c := range f.configs {
		if c.Provider == provider {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *modelCatalogFakeRepo) UpsertSupportedModels(_ context.Context, models []ProviderSupportedModel) error {
	f.upsertCalls = append(f.upsertCalls, append([]ProviderSupportedModel(nil), models...))
	for _, m := range models {
		f.stored[modelStoreKey(m.Provider, m.ModelName)] = m
	}
	return nil
}

func (f *modelCatalogFakeRepo) DeleteSupportedModelsNotIn(_ context.Context, provider ProviderType, modelNames []string) error {
	f.pruneCalls = append(f.pruneCalls, fakePruneCall{
		provider: provider,
		names:    append([]string(nil), modelNames...),
	})
	keep := make(map[string]bool, len(modelNames))
	for _, n := range modelNames {
		keep[n] = true
	}
	for k, m := range f.stored {
		if m.Provider == provider && !keep[m.ModelName] {
			delete(f.stored, k)
		}
	}
	return nil
}

func (f *modelCatalogFakeRepo) ListSupportedModels(_ context.Context, provider ProviderType, _ *ModelType) ([]ProviderSupportedModel, error) {
	var out []ProviderSupportedModel
	for _, m := range f.stored {
		if m.Provider == provider {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *modelCatalogFakeRepo) ListAllSupportedModels(_ context.Context, _ *ModelType) ([]ProviderSupportedModel, error) {
	out := make([]ProviderSupportedModel, 0, len(f.stored))
	for _, m := range f.stored {
		out = append(out, m)
	}
	return out, nil
}

// storedNames returns the model names stored for the provider, sorted.
func (f *modelCatalogFakeRepo) storedNames(provider ProviderType) []string {
	var names []string
	for _, m := range f.stored {
		if m.Provider == provider {
			names = append(names, m.ModelName)
		}
	}
	sort.Strings(names)
	return names
}

func (f *modelCatalogFakeRepo) upsertCount() int { return len(f.upsertCalls) }

func (f *modelCatalogFakeRepo) pruneCount() int { return len(f.pruneCalls) }

// ---------------------------------------------------------------------------
// Harness helpers
// ---------------------------------------------------------------------------

// newTestCatalogSyncCredentialService returns a CredentialService whose
// encryptor is armed with a fresh key, so test configs can be encrypted.
func newTestCatalogSyncCredentialService(t *testing.T) *CredentialService {
	t.Helper()
	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate encryption key: %v", err)
	}
	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{EncryptionKey: hexKey},
	}
	return NewCredentialService(nil, NewRegistry(), nil, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// encryptedOpenAIConfig builds a stored ProjectProviderConfig whose credential
// is encrypted with the given service's key.
func encryptedOpenAIConfig(t *testing.T, credsvc *CredentialService, projectID, baseURL, generativeModel string) ProjectProviderConfig {
	t.Helper()
	return encryptedProviderConfig(t, credsvc, ProviderOpenAI, projectID, baseURL, generativeModel)
}

// encryptedProviderConfig builds a stored ProjectProviderConfig for an arbitrary
// vendor whose credential is encrypted with the given service's key.
func encryptedProviderConfig(t *testing.T, credsvc *CredentialService, provider ProviderType, projectID, baseURL, generativeModel string) ProjectProviderConfig {
	t.Helper()
	ciphertext, nonce, err := credsvc.EncryptCredential([]byte("sk-" + projectID))
	if err != nil {
		t.Fatalf("failed to encrypt test credential: %v", err)
	}
	return ProjectProviderConfig{
		ProjectID:           projectID,
		Provider:            provider,
		Slug:                ProviderSlug(provider),
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		BaseURL:             baseURL,
		GenerativeModel:     generativeModel,
	}
}

// openAIModelServer returns an httptest server answering GET /models with the
// given model IDs.
func openAIModelServer(ids ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			data = append(data, map[string]any{"id": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func newModelCatalogSyncHarness(t *testing.T, credsvc *CredentialService, repo *modelCatalogFakeRepo) *ModelCatalogSyncService {
	t.Helper()
	return &ModelCatalogSyncService{
		repo:    repo,
		credsvc: credsvc,
		catalog: &ModelCatalogService{
			repo: repo,
			log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// ---------------------------------------------------------------------------
// ModelCatalogSyncService.Sync end-to-end (fake repo + real HTTP /v1/models)
// ---------------------------------------------------------------------------

// TestModelCatalogSync_UnionsDisjointConfigs is a regression test for the
// per-config prune that destroyed the shared catalog: two configs with disjoint
// model sets must both survive a sync. The pass must resolve all configs, union
// the sets, and persist+prune exactly once against the union.
func TestModelCatalogSync_UnionsDisjointConfigs(t *testing.T) {
	srvA := openAIModelServer("proj-a-model-1", "proj-a-model-2")
	defer srvA.Close()
	srvB := openAIModelServer("proj-b-model-1", "proj-b-model-2")
	defer srvB.Close()

	credsvc := newTestCatalogSyncCredentialService(t)
	configA := encryptedOpenAIConfig(t, credsvc, "proj-a", srvA.URL, "proj-a-model-1")
	configB := encryptedOpenAIConfig(t, credsvc, "proj-b", srvB.URL, "proj-b-model-1")

	repo := newModelCatalogFakeRepo(configA, configB)
	svc := newModelCatalogSyncHarness(t, credsvc, repo)

	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	want := []string{"proj-a-model-1", "proj-a-model-2", "proj-b-model-1", "proj-b-model-2"}
	if got := repo.storedNames(ProviderOpenAI); !equalStrings(got, want) {
		t.Errorf("stored models = %v, want %v (both configs' models must survive)", got, want)
	}

	// Exactly one persist for the whole pass — never one per config.
	if repo.upsertCount() != 1 {
		t.Errorf("UpsertSupportedModels called %d times, want 1", repo.upsertCount())
	}
	if repo.pruneCount() != 1 {
		t.Fatalf("DeleteSupportedModelsNotIn called %d times, want 1", repo.pruneCount())
	}
	pc := repo.pruneCalls[0]
	if pc.provider != ProviderOpenAI {
		t.Errorf("prune provider = %q, want %q", pc.provider, ProviderOpenAI)
	}
	if !equalStrings(pc.names, want) {
		t.Errorf("prune names = %v, want %v (union of both configs)", pc.names, want)
	}
}

// TestModelCatalogSync_FetchFailure_UpsertsFallbackAndSkipsPrune is a regression
// test for the fallback prune that wiped the whole provider catalog: one
// unreachable proxy must not delete the other config's (or any stale) models.
// The failed config's fallback models are upserted and the prune is skipped.
func TestModelCatalogSync_FetchFailure_UpsertsFallbackAndSkipsPrune(t *testing.T) {
	srvA := openAIModelServer("proj-a-model-1", "proj-a-model-2")
	defer srvA.Close()

	// Simulate an unreachable/flaky proxy: a server that is closed before sync.
	srvB := openAIModelServer("unused")
	closedURL := srvB.URL
	srvB.Close()

	credsvc := newTestCatalogSyncCredentialService(t)
	configA := encryptedOpenAIConfig(t, credsvc, "proj-a", srvA.URL, "proj-a-model-1")
	configB := encryptedOpenAIConfig(t, credsvc, "proj-b", closedURL, "proj-b-fallback-model")

	repo := newModelCatalogFakeRepo(configA, configB)
	svc := newModelCatalogSyncHarness(t, credsvc, repo)

	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Healthy config's full set + failed config's configured-model fallback.
	want := []string{"proj-a-model-1", "proj-a-model-2", "proj-b-fallback-model"}
	if got := repo.storedNames(ProviderOpenAI); !equalStrings(got, want) {
		t.Errorf("stored models = %v, want %v", got, want)
	}
	if repo.upsertCount() != 1 {
		t.Errorf("UpsertSupportedModels called %d times, want 1", repo.upsertCount())
	}
	// Any fetch failure in the pass ⇒ upsert-only, no prune.
	if repo.pruneCount() != 0 {
		t.Errorf("DeleteSupportedModelsNotIn called %d times, want 0 (never prune on fetch fallback)", repo.pruneCount())
	}
}

// TestModelCatalogSync_DecryptFailure_SkipsConfig verifies that a config whose
// credential cannot be decrypted is skipped without error while other configs
// are still processed — and that the skip suppresses the prune (the skipped
// config's previously synced rows must survive because their set is unknown).
func TestModelCatalogSync_DecryptFailure_SkipsConfig(t *testing.T) {
	srvA := openAIModelServer("proj-a-model-1")
	defer srvA.Close()

	credsvc := newTestCatalogSyncCredentialService(t)
	good := encryptedOpenAIConfig(t, credsvc, "proj-a", srvA.URL, "proj-a-model-1")
	bad := encryptedOpenAIConfig(t, credsvc, "proj-b", "http://unreachable.invalid", "proj-b-model")
	bad.EncryptedCredential[0] ^= 0xff // corrupt so decryption fails

	repo := newModelCatalogFakeRepo(good, bad)
	svc := newModelCatalogSyncHarness(t, credsvc, repo)

	// No error: decrypt failure is logged and skipped, others processed.
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v, want nil (decrypt failure must be non-fatal)", err)
	}

	if got := repo.storedNames(ProviderOpenAI); !equalStrings(got, []string{"proj-a-model-1"}) {
		t.Errorf("stored models = %v, want [proj-a-model-1] (healthy config still processed)", got)
	}
	if repo.upsertCount() != 1 {
		t.Errorf("UpsertSupportedModels called %d times, want 1", repo.upsertCount())
	}
	// Skipped config means the union is incomplete — never prune.
	if repo.pruneCount() != 0 {
		t.Errorf("DeleteSupportedModelsNotIn called %d times, want 0", repo.pruneCount())
	}
}

// TestModelCatalogSync_PerProviderPruneCompleteness verifies prune completeness
// is tracked per provider: a fallback for one OpenAI-compatible vendor must not
// suppress pruning for a different vendor whose live fetch was complete.
func TestModelCatalogSync_PerProviderPruneCompleteness(t *testing.T) {
	srvA := openAIModelServer("openai-fresh-1", "openai-fresh-2")
	defer srvA.Close()

	// LiteLLM proxy is unreachable → its config falls back to configured models.
	closed := openAIModelServer("unused")
	closedURL := closed.URL
	closed.Close()

	credsvc := newTestCatalogSyncCredentialService(t)
	cfgOpenAI := encryptedProviderConfig(t, credsvc, ProviderOpenAI, "proj-openai", srvA.URL, "openai-fresh-1")
	cfgLiteLLM := encryptedProviderConfig(t, credsvc, ProviderLiteLLM, "proj-litellm", closedURL, "litellm-fallback")

	repo := newModelCatalogFakeRepo(cfgOpenAI, cfgLiteLLM)
	// Pre-existing stale rows for both vendors.
	if err := repo.UpsertSupportedModels(context.Background(), []ProviderSupportedModel{
		{Provider: ProviderOpenAI, ModelName: "openai-stale", ModelType: ModelTypeGenerative},
		{Provider: ProviderLiteLLM, ModelName: "litellm-stale", ModelType: ModelTypeGenerative},
	}); err != nil {
		t.Fatalf("seed stale models: %v", err)
	}

	svc := newModelCatalogSyncHarness(t, credsvc, repo)
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// OpenAI fetched completely → its stale row is pruned.
	if got := repo.storedNames(ProviderOpenAI); !equalStrings(got, []string{"openai-fresh-1", "openai-fresh-2"}) {
		t.Errorf("OpenAI models = %v, want [openai-fresh-1 openai-fresh-2] (complete fetch prunes)", got)
	}
	// LiteLLM fell back → its stale row must survive.
	if got := repo.storedNames(ProviderLiteLLM); !equalStrings(got, []string{"litellm-fallback", "litellm-stale"}) {
		t.Errorf("LiteLLM models = %v, want [litellm-fallback litellm-stale] (fallback suppresses only its own prune)", got)
	}
	// Exactly one prune, for OpenAI only.
	if len(repo.pruneCalls) != 1 {
		t.Fatalf("prune calls = %d, want 1 (OpenAI only)", len(repo.pruneCalls))
	}
	if repo.pruneCalls[0].provider != ProviderOpenAI {
		t.Errorf("prune provider = %q, want %q", repo.pruneCalls[0].provider, ProviderOpenAI)
	}
}

// TestModelCatalogSync_NoConfigs verifies an empty pass is a no-op.
func TestModelCatalogSync_NoConfigs(t *testing.T) {
	credsvc := newTestCatalogSyncCredentialService(t)
	repo := newModelCatalogFakeRepo()
	svc := newModelCatalogSyncHarness(t, credsvc, repo)

	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v, want nil", err)
	}
	if repo.upsertCount() != 0 || repo.pruneCount() != 0 {
		t.Errorf("expected no repo writes, got upserts=%d prunes=%d", repo.upsertCount(), repo.pruneCount())
	}
}

// ---------------------------------------------------------------------------
// SyncModels (manual provider-config-save path) — prune decisions
// ---------------------------------------------------------------------------

// TestSyncModels_FetchFailure_DoesNotPrune is a regression test for the manual
// save path: when /v1/models is unreachable the fallback rows are upserted but
// stale rows are never deleted.
func TestSyncModels_FetchFailure_DoesNotPrune(t *testing.T) {
	srv := openAIModelServer("unused")
	closedURL := srv.URL
	srv.Close()

	repo := newModelCatalogFakeRepo()
	svc := &ModelCatalogService{repo: repo, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	cred := &ResolvedCredential{
		BaseURL:         closedURL,
		APIKey:          "sk-test",
		GenerativeModel: "fallback-gen-model",
	}
	if err := svc.SyncModels(context.Background(), ProviderOpenAI, cred); err != nil {
		t.Fatalf("SyncModels() error = %v", err)
	}
	if got := repo.storedNames(ProviderOpenAI); !equalStrings(got, []string{"fallback-gen-model"}) {
		t.Errorf("stored models = %v, want [fallback-gen-model]", got)
	}
	if repo.pruneCount() != 0 {
		t.Errorf("DeleteSupportedModelsNotIn called %d times, want 0 (never prune on fetch fallback)", repo.pruneCount())
	}
}

// TestSyncModels_FetchSuccess_Prunes verifies the existing manual-save prune
// behavior is preserved when the live fetch succeeds: stale rows not in the
// fetched set are deleted.
func TestSyncModels_FetchSuccess_Prunes(t *testing.T) {
	srv := openAIModelServer("fresh-model")
	defer srv.Close()

	repo := newModelCatalogFakeRepo()
	// Pre-existing stale row that the successful fetch does not return.
	if err := repo.UpsertSupportedModels(context.Background(), []ProviderSupportedModel{{
		Provider:  ProviderOpenAI,
		ModelName: "stale-model",
		ModelType: ModelTypeGenerative,
	}}); err != nil {
		t.Fatalf("failed to seed stale model: %v", err)
	}

	svc := &ModelCatalogService{repo: repo, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cred := &ResolvedCredential{BaseURL: srv.URL, APIKey: "sk-test"}
	if err := svc.SyncModels(context.Background(), ProviderOpenAI, cred); err != nil {
		t.Fatalf("SyncModels() error = %v", err)
	}

	if got := repo.storedNames(ProviderOpenAI); !equalStrings(got, []string{"fresh-model"}) {
		t.Errorf("stored models = %v, want [fresh-model] (stale row pruned)", got)
	}
	if repo.pruneCount() != 1 {
		t.Errorf("DeleteSupportedModelsNotIn called %d times, want 1", repo.pruneCount())
	}
}

// ---------------------------------------------------------------------------
// ResolveModels — pruneOK flag semantics
// ---------------------------------------------------------------------------

func TestResolveModels_OpenAI_FetchSuccess_PruneOK(t *testing.T) {
	srv := openAIModelServer("resolved-model")
	defer srv.Close()

	svc := &ModelCatalogService{repo: newModelCatalogFakeRepo(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	models, pruneOK, err := svc.ResolveModels(context.Background(), ProviderOpenAI, &ResolvedCredential{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	if !pruneOK {
		t.Error("pruneOK = false, want true (live fetch succeeded)")
	}
	if len(models) == 0 || models[0].ModelName != "resolved-model" {
		t.Errorf("models = %+v, want [resolved-model]", models)
	}
}

func TestResolveModels_OpenAI_FetchFailure_FallbackNoPrune(t *testing.T) {
	srv := openAIModelServer("unused")
	closedURL := srv.URL
	srv.Close()

	svc := &ModelCatalogService{repo: newModelCatalogFakeRepo(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	models, pruneOK, err := svc.ResolveModels(context.Background(), ProviderOpenAI, &ResolvedCredential{
		BaseURL:         closedURL,
		GenerativeModel: "fallback-model",
	})
	if err != nil {
		t.Fatalf("ResolveModels() error = %v", err)
	}
	if pruneOK {
		t.Error("pruneOK = true, want false (fallback snapshot used)")
	}
	if len(models) != 1 || models[0].ModelName != "fallback-model" {
		t.Errorf("models = %+v, want [fallback-model]", models)
	}
}

// ---------------------------------------------------------------------------
// unionSupportedModels (pure helper)
// ---------------------------------------------------------------------------

func TestUnionSupportedModels(t *testing.T) {
	gen := func(name string) ProviderSupportedModel {
		return ProviderSupportedModel{Provider: ProviderOpenAI, ModelName: name, ModelType: ModelTypeGenerative}
	}

	t.Run("disjoint sets are both retained in order", func(t *testing.T) {
		got := unionSupportedModels([]ProviderSupportedModel{gen("a"), gen("b")}, []ProviderSupportedModel{gen("c")})
		names := make([]string, len(got))
		for i, m := range got {
			names[i] = m.ModelName
		}
		if !equalStrings(names, []string{"a", "b", "c"}) {
			t.Errorf("union = %v, want [a b c]", names)
		}
	})

	t.Run("duplicate model names dedupe across sets", func(t *testing.T) {
		got := unionSupportedModels(
			[]ProviderSupportedModel{gen("dup"), gen("b")},
			[]ProviderSupportedModel{gen("dup"), gen("c")},
		)
		if len(got) != 3 {
			t.Fatalf("union len = %d, want 3 (duplicate collapsed): %+v", len(got), got)
		}
	})

	t.Run("single set passed through unchanged", func(t *testing.T) {
		got := unionSupportedModels([]ProviderSupportedModel{gen("a")})
		if len(got) != 1 || got[0].ModelName != "a" {
			t.Errorf("union = %+v, want [a]", got)
		}
	})

	t.Run("empty provider or model name rows dropped", func(t *testing.T) {
		got := unionSupportedModels([]ProviderSupportedModel{
			{Provider: ProviderOpenAI, ModelName: "", ModelType: ModelTypeGenerative},
			{ModelName: "noname-provider", ModelType: ModelTypeGenerative},
			gen("valid"),
		})
		if len(got) != 1 || got[0].ModelName != "valid" {
			t.Errorf("union = %+v, want [valid]", got)
		}
	})

	t.Run("row with token limits upgrades earlier token-less row", func(t *testing.T) {
		in := 1000
		plain := gen("shared")
		rich := gen("shared")
		rich.MaxInputTokens = &in
		got := unionSupportedModels([]ProviderSupportedModel{plain}, []ProviderSupportedModel{rich})
		if len(got) != 1 || got[0].MaxInputTokens == nil || *got[0].MaxInputTokens != in {
			t.Errorf("union = %+v, want shared model with MaxInputTokens=%d", got, in)
		}
	})

	t.Run("same detail row keeps first occurrence", func(t *testing.T) {
		a := gen("shared")
		b := gen("shared")
		b.DisplayName = "second"
		got := unionSupportedModels([]ProviderSupportedModel{a}, []ProviderSupportedModel{b})
		if len(got) != 1 || got[0].DisplayName != "" {
			t.Errorf("union = %+v, want first row to win", got)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
