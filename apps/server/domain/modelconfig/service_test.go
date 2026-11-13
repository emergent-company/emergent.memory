package modelconfig

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/provider"
)

// fakeStore implements modelConfigStore in memory.
type fakeStore struct {
	cfg *ProjectModelConfig
	err error
}

func (f *fakeStore) GetProjectModelConfig(_ context.Context, _ uuid.UUID) (*ProjectModelConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.cfg, nil
}

func (f *fakeStore) UpsertProjectModelConfig(_ context.Context, projectID uuid.UUID, patch ProjectModelConfigPatch) (*ProjectModelConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.cfg == nil {
		f.cfg = &ProjectModelConfig{ProjectID: projectID}
	}
	// Mirror the real store's partial-update semantics: only named fields move.
	if patch.GenerativeModel != nil {
		f.cfg.GenerativeModel = *patch.GenerativeModel
	}
	if patch.GenerativeProviderSlug != nil {
		f.cfg.GenerativeProviderSlug = *patch.GenerativeProviderSlug
	}
	if patch.EmbeddingModel != nil {
		f.cfg.EmbeddingModel = *patch.EmbeddingModel
	}
	if patch.EmbeddingProviderSlug != nil {
		f.cfg.EmbeddingProviderSlug = *patch.EmbeddingProviderSlug
	}
	return f.cfg, nil
}

func (f *fakeStore) DeleteProjectModelConfig(_ context.Context, _ uuid.UUID) error {
	f.cfg = nil
	return nil
}

// fakeResolver implements generativeDefaultResolver.
type fakeResolver struct {
	model string
	err   error
}

func (f *fakeResolver) DefaultGenerativeModel(_ context.Context, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.model, nil
}

// fakeEmbeddingResolver implements embeddingDefaultResolver.
type fakeEmbeddingResolver struct {
	model string
	err   error
}

func (f *fakeEmbeddingResolver) DefaultEmbeddingModel(_ context.Context, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.model, nil
}

func testService(store modelConfigStore, resolver generativeDefaultResolver) *Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(store, log).WithGenerativeDefaultResolver(resolver)
}

func testServiceEmbedding(store modelConfigStore, resolver embeddingDefaultResolver) *Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(store, log).WithEmbeddingDefaultResolver(resolver)
}

func TestResolveGenerativeModelProjectConfigWins(t *testing.T) {
	projectID := uuid.New()
	svc := testService(&fakeStore{cfg: &ProjectModelConfig{
		ProjectID:       projectID,
		GenerativeModel: "deepseek/deepseek-v4-flash",
	}}, &fakeResolver{model: "google/gemini-2.5-flash"})

	model, source, err := svc.ResolveGenerativeModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "deepseek/deepseek-v4-flash" || source != ModelSourceProject {
		t.Errorf("ResolveGenerativeModel = (%q, %q), want project config (deepseek/deepseek-v4-flash, project)", model, source)
	}
}

func TestResolveGenerativeModelFallsBackToProviderCredential(t *testing.T) {
	projectID := uuid.New()
	svc := testService(&fakeStore{cfg: nil}, &fakeResolver{model: "deepseek/deepseek-v4-flash"})

	model, source, err := svc.ResolveGenerativeModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "deepseek/deepseek-v4-flash" || source != ModelSourceProvider {
		t.Errorf("ResolveGenerativeModel = (%q, %q), want provider fallback (deepseek/deepseek-v4-flash, provider)", model, source)
	}
}

func TestResolveGenerativeModelNoneWhenNothingConfigured(t *testing.T) {
	projectID := uuid.New()
	svc := testService(&fakeStore{cfg: nil}, &fakeResolver{model: ""})

	model, source, err := svc.ResolveGenerativeModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "" || source != ModelSourceNone {
		t.Errorf("ResolveGenerativeModel = (%q, %q), want (\"\", none)", model, source)
	}
}

func TestResolveGenerativeModelNilResolverStaysProjectOnly(t *testing.T) {
	projectID := uuid.New()
	svc := NewService(&fakeStore{cfg: nil}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	model, source, err := svc.ResolveGenerativeModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "" || source != ModelSourceNone {
		t.Errorf("without resolver ResolveGenerativeModel = (%q, %q), want (\"\", none)", model, source)
	}
}

func TestResolveGenerativeModelPropagatesStoreError(t *testing.T) {
	projectID := uuid.New()
	want := errors.New("db down")
	svc := testService(&fakeStore{err: want}, &fakeResolver{model: "google/gemini-2.5-flash"})

	if _, _, err := svc.ResolveGenerativeModel(context.Background(), projectID); err == nil {
		t.Fatal("expected store error to propagate")
	}
}

func TestResolveGenerativeModelIgnoresResolverError(t *testing.T) {
	projectID := uuid.New()
	// The executor ignores resolver failures on its fallback; the reporting
	// path must not fail the request because a provider lookup errored.
	svc := testService(&fakeStore{cfg: nil}, &fakeResolver{err: errors.New("boom")})

	model, source, err := svc.ResolveGenerativeModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "" || source != ModelSourceNone {
		t.Errorf("on resolver error ResolveGenerativeModel = (%q, %q), want (\"\", none)", model, source)
	}
}

func TestResolveEmbeddingModelProjectConfigWins(t *testing.T) {
	projectID := uuid.New()
	svc := testServiceEmbedding(&fakeStore{cfg: &ProjectModelConfig{
		ProjectID:      projectID,
		EmbeddingModel: "google/gemini-embedding-001",
	}}, &fakeEmbeddingResolver{model: "google-vertex/text-embedding-005"})

	model, source, err := svc.ResolveEmbeddingModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "google/gemini-embedding-001" || source != ModelSourceProject {
		t.Errorf("ResolveEmbeddingModel = (%q, %q), want project config (google/gemini-embedding-001, project)", model, source)
	}
}

func TestResolveEmbeddingModelFallsBackToProviderCredential(t *testing.T) {
	projectID := uuid.New()
	svc := testServiceEmbedding(&fakeStore{cfg: nil}, &fakeEmbeddingResolver{model: "google/gemini-embedding-001"})

	model, source, err := svc.ResolveEmbeddingModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "google/gemini-embedding-001" || source != ModelSourceProvider {
		t.Errorf("ResolveEmbeddingModel = (%q, %q), want provider fallback (google/gemini-embedding-001, provider)", model, source)
	}
}

func TestResolveEmbeddingModelNoneWhenNothingConfigured(t *testing.T) {
	projectID := uuid.New()
	svc := testServiceEmbedding(&fakeStore{cfg: nil}, &fakeEmbeddingResolver{model: ""})

	model, source, err := svc.ResolveEmbeddingModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "" || source != ModelSourceNone {
		t.Errorf("ResolveEmbeddingModel = (%q, %q), want (\"\", none)", model, source)
	}
}

func TestResolveEmbeddingModelNilResolverStaysProjectOnly(t *testing.T) {
	projectID := uuid.New()
	svc := NewService(&fakeStore{cfg: nil}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	model, source, err := svc.ResolveEmbeddingModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "" || source != ModelSourceNone {
		t.Errorf("without resolver ResolveEmbeddingModel = (%q, %q), want (\"\", none)", model, source)
	}
}

func TestResolveEmbeddingModelPropagatesStoreError(t *testing.T) {
	projectID := uuid.New()
	want := errors.New("db down")
	svc := testServiceEmbedding(&fakeStore{err: want}, &fakeEmbeddingResolver{model: "google/gemini-embedding-001"})

	if _, _, err := svc.ResolveEmbeddingModel(context.Background(), projectID); err == nil {
		t.Fatal("expected store error to propagate")
	}
}

func TestResolveEmbeddingModelIgnoresResolverError(t *testing.T) {
	projectID := uuid.New()
	// Mirror ResolveGenerativeModel: a provider lookup error must not fail the
	// reporting path — treat as unknown and fall through to ModelSourceNone.
	svc := testServiceEmbedding(&fakeStore{cfg: nil}, &fakeEmbeddingResolver{err: errors.New("boom")})

	model, source, err := svc.ResolveEmbeddingModel(context.Background(), projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "" || source != ModelSourceNone {
		t.Errorf("on resolver error ResolveEmbeddingModel = (%q, %q), want (\"\", none)", model, source)
	}
}

// TestBuildEmbeddingCredentialCarriesExtraConfig pins that the adapter forwards
// vendor extra config (Azure api_version, declared extra headers) from the
// resolved provider credential into the embeddings credential, so the runtime
// OpenAI-compatible embeddings client can apply it. Regression guard: these
// fields were previously dropped, so only the catalog test path applied them.
func TestBuildEmbeddingCredentialCarriesExtraConfig(t *testing.T) {
	a := &EmbeddingResolverAdapter{}
	cred := &provider.ResolvedCredential{
		Provider:       provider.ProviderAzureOpenAI,
		APIKey:         "sk",
		BaseURL:        "https://x.openai.azure.com/openai/v1",
		Auth:           provider.AuthAPIKeyHeader,
		EmbeddingModel: "text-embedding-3-small",
		Extra:          map[string]string{"api_version": "2024-10-21"},
		ExtraHeaders:   map[string]string{"X-Secret-Key": "sk-secondary"},
		Source:         provider.SourceProject,
	}
	got := a.buildEmbeddingCredential(cred, "text-embedding-3-small")
	if got.Extra["api_version"] != "2024-10-21" {
		t.Errorf("Extra[api_version] = %q, want 2024-10-21", got.Extra["api_version"])
	}
	if got.ExtraHeaders["X-Secret-Key"] != "sk-secondary" {
		t.Errorf("ExtraHeaders[X-Secret-Key] = %q, want sk-secondary", got.ExtraHeaders["X-Secret-Key"])
	}
}

// fakeProviderValidator implements modelProviderValidator.
type fakeProviderValidator struct {
	known map[string]bool
	err   error
	calls []string
}

func (f *fakeProviderValidator) ModelProviderKnown(_ context.Context, _, providerName string) (bool, error) {
	f.calls = append(f.calls, providerName)
	if f.err != nil {
		return false, f.err
	}
	return f.known[providerName], nil
}

func testServiceWithValidator(store modelConfigStore, v modelProviderValidator) *Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(store, log).WithProviderValidator(v)
}

// TestUpsertProjectModelConfigValidatesProvider pins the write-time provider
// check: a non-empty reference must name a supported dialect or an existing
// project provider instance slug. The credential is intentionally not checked,
// so a known provider passes even though the fake validator has no credentials.
func TestUpsertProjectModelConfigValidatesProvider(t *testing.T) {
	projectID := uuid.New()
	cases := []struct {
		name    string
		known   map[string]bool
		req     UpsertModelConfigRequest
		wantErr string
	}{
		{
			name:  "supported dialect",
			known: map[string]bool{"google": true},
			req:   UpsertModelConfigRequest{EmbeddingModel: "google/gemini-embedding-001"},
		},
		{
			name:  "existing provider instance slug",
			known: map[string]bool{"google-eu": true},
			req:   UpsertModelConfigRequest{EmbeddingModel: "google-eu/gemini-embedding-001"},
		},
		{
			name:    "unknown provider rejected",
			known:   map[string]bool{"google": true},
			req:     UpsertModelConfigRequest{EmbeddingModel: "typo/gemini-embedding-001"},
			wantErr: "unknown provider",
		},
		{
			name:    "unknown generative provider rejected",
			known:   map[string]bool{"google": true},
			req:     UpsertModelConfigRequest{GenerativeModel: "typo/gemini-2.5-flash"},
			wantErr: "unknown provider",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			svc := testServiceWithValidator(store, &fakeProviderValidator{known: tc.known})

			_, err := svc.UpsertProjectModelConfig(context.Background(), projectID, tc.req)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
			if store.cfg != nil {
				t.Errorf("store was written on a rejected request: %+v", store.cfg)
			}
		})
	}
}

// TestUpsertProjectModelConfigPropagatesValidatorError proves a registry/slug
// lookup failure surfaces instead of being reported as an unknown provider.
func TestUpsertProjectModelConfigPropagatesValidatorError(t *testing.T) {
	projectID := uuid.New()
	want := errors.New("db down")
	store := &fakeStore{}
	svc := testServiceWithValidator(store, &fakeProviderValidator{err: want})

	_, err := svc.UpsertProjectModelConfig(context.Background(), projectID, UpsertModelConfigRequest{
		EmbeddingModel: "google/gemini-embedding-001",
	})
	if err == nil {
		t.Fatal("expected the validator error to propagate")
	}
	if !errors.Is(err, want) {
		t.Errorf("error = %v, want it to wrap the cause", err)
	}
}

// TestUpsertProjectModelConfigPartialUpdate pins the partial PUT semantics:
// a field the caller omits keeps its stored value (issue #687 item 2).
func TestUpsertProjectModelConfigPartialUpdate(t *testing.T) {
	projectID := uuid.New()
	base := func() *fakeStore {
		return &fakeStore{cfg: &ProjectModelConfig{
			ProjectID:              projectID,
			GenerativeModel:        "deepseek-v4-flash",
			GenerativeProviderSlug: "deepseek",
			EmbeddingModel:         "gemini-embedding-001",
			EmbeddingProviderSlug:  "google",
		}}
	}
	validator := &fakeProviderValidator{known: map[string]bool{"deepseek": true, "google-vertex": true}}

	t.Run("generative only preserves embedding", func(t *testing.T) {
		store := base()
		svc := testServiceWithValidator(store, validator)
		resp, err := svc.UpsertProjectModelConfig(context.Background(), projectID, UpsertModelConfigRequest{
			GenerativeModel: "deepseek/deepseek-v4-flash",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store.cfg.GenerativeModel != "deepseek-v4-flash" || store.cfg.GenerativeProviderSlug != "deepseek" {
			t.Errorf("generative = (%q, %q), want (deepseek-v4-flash, deepseek)", store.cfg.GenerativeModel, store.cfg.GenerativeProviderSlug)
		}
		if store.cfg.EmbeddingModel != "gemini-embedding-001" || store.cfg.EmbeddingProviderSlug != "google" {
			t.Errorf("embedding = (%q, %q), want the stored (gemini-embedding-001, google)", store.cfg.EmbeddingModel, store.cfg.EmbeddingProviderSlug)
		}
		if resp.EmbeddingModel != "google/gemini-embedding-001" {
			t.Errorf("response embedding = %q, want google/gemini-embedding-001", resp.EmbeddingModel)
		}
	})

	t.Run("embedding only preserves generative", func(t *testing.T) {
		store := base()
		svc := testServiceWithValidator(store, validator)
		resp, err := svc.UpsertProjectModelConfig(context.Background(), projectID, UpsertModelConfigRequest{
			EmbeddingModel: "google-vertex/text-embedding-005",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store.cfg.GenerativeModel != "deepseek-v4-flash" || store.cfg.GenerativeProviderSlug != "deepseek" {
			t.Errorf("generative = (%q, %q), want the stored (deepseek-v4-flash, deepseek)", store.cfg.GenerativeModel, store.cfg.GenerativeProviderSlug)
		}
		if store.cfg.EmbeddingModel != "text-embedding-005" || store.cfg.EmbeddingProviderSlug != "google-vertex" {
			t.Errorf("embedding = (%q, %q), want (text-embedding-005, google-vertex)", store.cfg.EmbeddingModel, store.cfg.EmbeddingProviderSlug)
		}
		if resp.GenerativeModel != "deepseek/deepseek-v4-flash" {
			t.Errorf("response generative = %q, want deepseek/deepseek-v4-flash", resp.GenerativeModel)
		}
	})

	t.Run("both supplied replace both", func(t *testing.T) {
		store := base()
		svc := testServiceWithValidator(store, validator)
		_, err := svc.UpsertProjectModelConfig(context.Background(), projectID, UpsertModelConfigRequest{
			GenerativeModel: "deepseek/deepseek-chat",
			EmbeddingModel:  "google-vertex/text-embedding-005",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store.cfg.GenerativeModel != "deepseek-chat" || store.cfg.EmbeddingModel != "text-embedding-005" {
			t.Errorf("stored = (%q, %q), want (deepseek-chat, text-embedding-005)", store.cfg.GenerativeModel, store.cfg.EmbeddingModel)
		}
	})

	t.Run("empty request preserves both", func(t *testing.T) {
		store := base()
		svc := testServiceWithValidator(store, validator)
		_, err := svc.UpsertProjectModelConfig(context.Background(), projectID, UpsertModelConfigRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store.cfg.GenerativeModel != "deepseek-v4-flash" || store.cfg.EmbeddingModel != "gemini-embedding-001" {
			t.Errorf("stored = (%q, %q), want both preserved", store.cfg.GenerativeModel, store.cfg.EmbeddingModel)
		}
	})

	t.Run("clear generative keeps embedding", func(t *testing.T) {
		store := base()
		svc := testServiceWithValidator(store, validator)
		resp, err := svc.UpsertProjectModelConfig(context.Background(), projectID, UpsertModelConfigRequest{
			ClearGenerative: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store.cfg.GenerativeModel != "" || store.cfg.GenerativeProviderSlug != "" {
			t.Errorf("generative = (%q, %q), want cleared", store.cfg.GenerativeModel, store.cfg.GenerativeProviderSlug)
		}
		if store.cfg.EmbeddingModel != "gemini-embedding-001" || store.cfg.EmbeddingProviderSlug != "google" {
			t.Errorf("embedding = (%q, %q), want the stored value preserved", store.cfg.EmbeddingModel, store.cfg.EmbeddingProviderSlug)
		}
		if resp.GenerativeModel != "" || resp.EmbeddingModel != "google/gemini-embedding-001" {
			t.Errorf("response = (%q, %q), want (\"\", google/gemini-embedding-001)", resp.GenerativeModel, resp.EmbeddingModel)
		}
	})

	t.Run("clear embedding keeps generative", func(t *testing.T) {
		store := base()
		svc := testServiceWithValidator(store, validator)
		_, err := svc.UpsertProjectModelConfig(context.Background(), projectID, UpsertModelConfigRequest{
			ClearEmbedding: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store.cfg.EmbeddingModel != "" || store.cfg.EmbeddingProviderSlug != "" {
			t.Errorf("embedding = (%q, %q), want cleared", store.cfg.EmbeddingModel, store.cfg.EmbeddingProviderSlug)
		}
		if store.cfg.GenerativeModel != "deepseek-v4-flash" {
			t.Errorf("generative = %q, want the stored value preserved", store.cfg.GenerativeModel)
		}
	})

	t.Run("set and clear the same field is rejected", func(t *testing.T) {
		store := base()
		svc := testServiceWithValidator(store, validator)
		_, err := svc.UpsertProjectModelConfig(context.Background(), projectID, UpsertModelConfigRequest{
			GenerativeModel: "deepseek/deepseek-chat",
			ClearGenerative: true,
		})
		if err == nil {
			t.Fatal("expected an error for a model and clearGenerative on the same field")
		}
		if store.cfg.GenerativeModel != "deepseek-v4-flash" {
			t.Errorf("generative = %q, want the stored value untouched after a rejected request", store.cfg.GenerativeModel)
		}
	})
}

// TestDeleteProjectModelConfigClearsRow pins that DELETE remains the way to
// clear the whole config now that PUT preserves unsupplied fields.
func TestDeleteProjectModelConfigClearsRow(t *testing.T) {
	projectID := uuid.New()
	store := &fakeStore{cfg: &ProjectModelConfig{ProjectID: projectID, EmbeddingModel: "gemini-embedding-001"}}
	svc := testServiceWithValidator(store, nil)

	if err := svc.DeleteProjectModelConfig(context.Background(), projectID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.cfg != nil {
		t.Errorf("store config = %+v, want nil after delete", store.cfg)
	}
}
