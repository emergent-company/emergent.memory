package embeddings

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// stubResolver is a fixed EmbeddingResolver for the fail-closed cases.
type stubResolver struct {
	cred *ResolvedEmbeddingCredential
	err  error
}

func (s stubResolver) ResolveEmbedding(context.Context) (*ResolvedEmbeddingCredential, error) {
	return s.cred, s.err
}

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestResolveFailsClosedWithoutEmbeddingModel pins that a project with no
// embedding model returns the typed sentinel (so extraction workers classify it
// as permanent) instead of falling back to any other client, and logs the
// "model was not set" warning exactly once per project.
func TestResolveFailsClosedWithoutEmbeddingModel(t *testing.T) {
	var buf bytes.Buffer
	svc := NewResolverService(
		stubResolver{err: fmt.Errorf("%w for project p", ErrEmbeddingModelNotSet)},
		testLogger(&buf),
	)
	ctx := auth.ContextWithProjectID(context.Background(), "11111111-1111-1111-1111-111111111111")

	for i := 0; i < 2; i++ {
		vec, err := svc.EmbedQuery(ctx, "hello")
		if err == nil {
			t.Fatal("EmbedQuery() error = nil, want a not-configured error")
		}
		if vec != nil {
			t.Errorf("EmbedQuery() vec = %v, want nil", vec)
		}
		if !strings.Contains(err.Error(), "no embedding model configured") {
			t.Errorf("error = %q, want it to carry the permanent-failure phrase", err.Error())
		}
	}

	if n := strings.Count(buf.String(), "embedding model was not set"); n != 1 {
		t.Errorf("warning emitted %d times, want exactly 1 (deduplicated per project)", n)
	}
}

// TestResolveFailsClosedWithoutCredential covers a resolver that returns no
// credential: there is no environment client to fall back to any more.
func TestResolveFailsClosedWithoutCredential(t *testing.T) {
	var buf bytes.Buffer
	svc := NewResolverService(stubResolver{}, testLogger(&buf))
	ctx := auth.ContextWithProjectID(context.Background(), "22222222-2222-2222-2222-222222222222")

	if _, err := svc.EmbedDocuments(ctx, []string{"doc"}); err == nil {
		t.Fatal("EmbedDocuments() error = nil, want a not-configured error")
	}
}

// TestResolveFailsClosedWithoutModelOnCredential covers a credential that
// resolved from a project but carries no model (e.g. a provider config without
// an embedding model).
func TestResolveFailsClosedWithoutModelOnCredential(t *testing.T) {
	var buf bytes.Buffer
	svc := NewResolverService(
		stubResolver{cred: &ResolvedEmbeddingCredential{IsGoogleAI: true, APIKey: "k", ProviderSlug: "google"}},
		testLogger(&buf),
	)

	_, _, _, err := svc.resolveClientWithMeta(context.Background())
	if err == nil {
		t.Fatal("resolveClientWithMeta() error = nil, want a not-configured error")
	}
	if !strings.Contains(err.Error(), "no embedding model configured") {
		t.Errorf("error = %q, want the no-model sentinel", err.Error())
	}
}

// TestResolveUsesProjectCredential pins that the per-request client and its
// reported model/provider come from the resolved project credential.
func TestResolveUsesProjectCredential(t *testing.T) {
	var buf bytes.Buffer
	svc := NewResolverService(
		stubResolver{cred: &ResolvedEmbeddingCredential{
			IsGoogleAI:     true,
			APIKey:         "test-key",
			EmbeddingModel: "gemini-embedding-2",
			ProviderSlug:   "google",
		}},
		testLogger(&buf),
	)

	client, model, provider, err := svc.resolveClientWithMeta(context.Background())
	if err != nil {
		t.Fatalf("resolveClientWithMeta() error = %v", err)
	}
	if client == nil {
		t.Fatal("client = nil, want a Google AI client")
	}
	if model != "gemini-embedding-2" || provider != "googleai" {
		t.Errorf("model/provider = %q/%q, want gemini-embedding-2/googleai", model, provider)
	}
	if buf.Len() != 0 {
		t.Errorf("unexpected log output: %s", buf.String())
	}
}

// TestResolveRejectsUnusableCredential pins that an empty credential fails
// closed instead of falling back to a deployment-wide client.
func TestResolveRejectsUnusableCredential(t *testing.T) {
	var buf bytes.Buffer
	svc := NewResolverService(
		stubResolver{cred: &ResolvedEmbeddingCredential{EmbeddingModel: "m", ProviderSlug: "google"}},
		testLogger(&buf),
	)

	if _, _, _, err := svc.resolveClientWithMeta(context.Background()); err == nil {
		t.Fatal("resolveClientWithMeta() error = nil, want an unusable-credential error")
	} else if !strings.Contains(err.Error(), "no usable credentials") {
		t.Errorf("error = %q, want an unusable-credentials message", err.Error())
	}
}

// stubTestLLM reports every project as test-LLM mode, which is the seam that
// makes EmbedDocumentWithUsage testable without a provider.
type stubTestLLM struct{}

func (stubTestLLM) IsTestLLM(context.Context, string) bool { return true }

// TestEmbedDocumentWithUsageTakesTheFirstBatchVector pins the single-result
// shape against the batch call it delegates to. The ingest workers depend on
// EmbedDocumentWithUsage returning exactly the vector for their text (not the
// whole batch) and on EmbedDocument returning the vector alone; a change to the
// batch return shape would otherwise break ingest silently.
func TestEmbedDocumentWithUsageTakesTheFirstBatchVector(t *testing.T) {
	svc := &Service{
		client:  NewNoopClient(),
		log:     slog.Default(),
		testLLM: stubTestLLM{},
		enabled: true,
	}
	ctx := auth.ContextWithProjectID(context.Background(), "33333333-3333-3333-3333-333333333333")

	const text = "a stored chunk of text"
	want := cannedEmbedding(text)

	result, err := svc.EmbedDocumentWithUsage(ctx, text)
	if err != nil {
		t.Fatalf("EmbedDocumentWithUsage() error = %v", err)
	}
	if result == nil {
		t.Fatal("EmbedDocumentWithUsage() = nil, want a result")
	}
	if len(result.Embedding) != len(want) || result.Embedding[0] != want[0] {
		t.Errorf("Embedding = %v, want the vector for %q", result.Embedding, text)
	}
	if result.Provider != "test-llm" {
		t.Errorf("Provider = %q, want test-llm", result.Provider)
	}

	vec, err := svc.EmbedDocument(ctx, text)
	if err != nil {
		t.Fatalf("EmbedDocument() error = %v", err)
	}
	if len(vec) != len(want) || vec[0] != want[0] {
		t.Errorf("EmbedDocument() = %v, want the vector for %q", vec, text)
	}

	// Two different texts must not collapse to the same vector through the hop.
	other, err := svc.EmbedDocument(ctx, "a different chunk")
	if err != nil {
		t.Fatalf("EmbedDocument() error = %v", err)
	}
	if len(other) == len(vec) && other[0] == vec[0] && len(other) > 1 && other[1] == vec[1] {
		t.Error("two different texts produced the same vector")
	}
}
