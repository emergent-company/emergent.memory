package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/modelref"
)

// fakeEmbeddingCreds implements embeddingCredentialResolver.
type fakeEmbeddingCreds struct {
	byRefCalls []modelref.Ref
	byRefCred  *provider.ResolvedCredential
	byRefErr   error
	anyCred    *provider.ResolvedCredential
	anyErr     error
}

func (f *fakeEmbeddingCreds) ResolveByRef(_ context.Context, ref modelref.Ref) (*provider.ResolvedCredential, error) {
	f.byRefCalls = append(f.byRefCalls, ref)
	return f.byRefCred, f.byRefErr
}

func (f *fakeEmbeddingCreds) ResolveAnyEmbedding(_ context.Context) (*provider.ResolvedCredential, error) {
	return f.anyCred, f.anyErr
}

func newEmbeddingAdapterTestService(cfg *ProjectModelConfig) *Service {
	return NewService(&fakeStore{cfg: cfg}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestResolveEmbeddingResolvesProviderInstanceSlug proves the embedding fast
// path is slug-aware: a project model config that names a provider instance
// ("google-eu") must pass the full reference — not just the dialect — to
// credential resolution, so "embed with my second Google instance" works
// (#690 item 3).
func TestResolveEmbeddingResolvesProviderInstanceSlug(t *testing.T) {
	projectID := uuid.New()
	svc := newEmbeddingAdapterTestService(&ProjectModelConfig{
		ProjectID:             projectID,
		EmbeddingModel:        "gemini-embedding-001",
		EmbeddingProviderSlug: "google-eu",
	})
	creds := &fakeEmbeddingCreds{byRefCred: &provider.ResolvedCredential{
		Provider: provider.ProviderGoogleAI,
		Slug:     "google-eu",
		APIKey:   "sk-eu",
	}}
	adapter := NewEmbeddingResolverAdapter(svc, creds)

	ctx := auth.ContextWithProjectID(context.Background(), projectID.String())
	got, err := adapter.ResolveEmbedding(ctx)
	if err != nil {
		t.Fatalf("ResolveEmbedding() error = %v", err)
	}
	if len(creds.byRefCalls) != 1 {
		t.Fatalf("ResolveByRef calls = %d, want 1", len(creds.byRefCalls))
	}
	wantRef := modelref.Ref{Provider: "google-eu", Model: "gemini-embedding-001"}
	if creds.byRefCalls[0] != wantRef {
		t.Errorf("ResolveByRef ref = %+v, want %+v", creds.byRefCalls[0], wantRef)
	}
	if got.APIKey != "sk-eu" {
		t.Errorf("APIKey = %q, want sk-eu", got.APIKey)
	}
	if got.EmbeddingModel != "gemini-embedding-001" {
		t.Errorf("EmbeddingModel = %q, want the bare model name", got.EmbeddingModel)
	}
}

// TestResolveEmbeddingResolvesLegacyDialect proves a legacy dialect reference
// still goes through the same slug-first resolver, which falls back to the
// dialect's default instance.
func TestResolveEmbeddingResolvesLegacyDialect(t *testing.T) {
	projectID := uuid.New()
	svc := newEmbeddingAdapterTestService(&ProjectModelConfig{
		ProjectID:             projectID,
		EmbeddingModel:        "gemini-embedding-001",
		EmbeddingProviderSlug: "google",
	})
	creds := &fakeEmbeddingCreds{byRefCred: &provider.ResolvedCredential{
		Provider: provider.ProviderGoogleAI,
		Slug:     "google",
		APIKey:   "sk-google",
	}}
	adapter := NewEmbeddingResolverAdapter(svc, creds)

	ctx := auth.ContextWithProjectID(context.Background(), projectID.String())
	if _, err := adapter.ResolveEmbedding(ctx); err != nil {
		t.Fatalf("ResolveEmbedding() error = %v", err)
	}
	if len(creds.byRefCalls) != 1 {
		t.Fatalf("ResolveByRef calls = %d, want 1", len(creds.byRefCalls))
	}
	if creds.byRefCalls[0].Provider != "google" {
		t.Errorf("ResolveByRef provider = %q, want google", creds.byRefCalls[0].Provider)
	}
}

// TestResolveEmbeddingClassifiesMissingCredential pins that the adapter maps a
// missing project credential to ErrEmbeddingCredentialUnusable through the
// slug-aware path, while a transient failure keeps its own wrapping.
func TestResolveEmbeddingClassifiesMissingCredential(t *testing.T) {
	projectID := uuid.New()
	svc := newEmbeddingAdapterTestService(&ProjectModelConfig{
		ProjectID:             projectID,
		EmbeddingModel:        "gemini-embedding-001",
		EmbeddingProviderSlug: "google-eu",
	})
	ctx := auth.ContextWithProjectID(context.Background(), projectID.String())

	gapCreds := &fakeEmbeddingCreds{byRefErr: provider.ErrNoProjectProviderConfig}
	gapAdapter := NewEmbeddingResolverAdapter(svc, gapCreds)
	if _, err := gapAdapter.ResolveEmbedding(ctx); !errors.Is(err, embeddings.ErrEmbeddingCredentialUnusable) {
		t.Errorf("missing credential error = %v, want ErrEmbeddingCredentialUnusable", err)
	}

	transient := errors.New("failed to get project provider config: connection refused")
	transientAdapter := NewEmbeddingResolverAdapter(svc, &fakeEmbeddingCreds{byRefErr: transient})
	_, err := transientAdapter.ResolveEmbedding(ctx)
	if errors.Is(err, embeddings.ErrEmbeddingCredentialUnusable) {
		t.Errorf("transient error = %v, must not be classified as a configuration gap", err)
	}
	if !errors.Is(err, transient) {
		t.Errorf("transient error = %v, want it to wrap the cause", err)
	}
}

// TestEmbeddingCredentialErrorClassifiesGaps pins the fix that makes a missing
// project credential behave like a missing model: a project whose model config
// names a provider it has no credential for (e.g. configure-project google,
// set-models --embedding google/..., configure-project google --remove) must
// produce a not-configured sentinel so callers stay quiet and extraction treats
// the job as permanent, instead of logging per call and retrying to
// max_attempts. A transient failure must keep its own wrapping.
func TestEmbeddingCredentialErrorClassifiesGaps(t *testing.T) {
	gap := provider.ErrNoProjectProviderConfig
	wrappedGap := fmt.Errorf("lookup: %w", gap)

	got := embeddingCredentialError("google", wrappedGap)
	if !errors.Is(got, embeddings.ErrEmbeddingCredentialUnusable) {
		t.Errorf("missing credential error = %v, want it to wrap ErrEmbeddingCredentialUnusable", got)
	}
	if !embeddings.IsNotConfigured(got) {
		t.Errorf("IsNotConfigured(%v) = false, want true", got)
	}
	if !strings.Contains(got.Error(), "google") {
		t.Errorf("error = %q, want it to name the provider", got)
	}

	transient := errors.New("failed to get project provider config: connection refused")
	got = embeddingCredentialError("google", transient)
	if errors.Is(got, embeddings.ErrEmbeddingCredentialUnusable) {
		t.Errorf("transient error = %v, must not be classified as a configuration gap", got)
	}
	if embeddings.IsNotConfigured(got) {
		t.Errorf("IsNotConfigured(%v) = true, want false", got)
	}
	if !errors.Is(got, transient) {
		t.Errorf("transient error = %v, want it to wrap the cause", got)
	}
}
