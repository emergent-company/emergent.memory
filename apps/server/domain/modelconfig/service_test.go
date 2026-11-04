package modelconfig

import (
	"context"
	"errors"
	"io"
	"log/slog"
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

func (f *fakeStore) UpsertProjectModelConfig(_ context.Context, cfg *ProjectModelConfig) error {
	f.cfg = cfg
	return nil
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
