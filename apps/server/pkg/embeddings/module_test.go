package embeddings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
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
	} else if !errors.Is(err, ErrEmbeddingCredentialUnusable) {
		t.Errorf("error = %q, want it to wrap ErrEmbeddingCredentialUnusable so extraction classifies it permanent", err.Error())
	}
}

// routingEmbeddingClient is a Client whose query and document paths return
// different vectors, so a call that reaches the wrong one is visible in the value
// and not only in a counter.
type routingEmbeddingClient struct {
	queryCalls    int
	documentCalls int
}

func (c *routingEmbeddingClient) EmbedQuery(context.Context, string) ([]float32, error) {
	c.queryCalls++
	return []float32{1, 0}, nil
}

func (c *routingEmbeddingClient) EmbedDocuments(_ context.Context, documents []string) ([][]float32, error) {
	c.documentCalls++
	out := make([][]float32, len(documents))
	for i := range out {
		out[i] = []float32{0, 1}
	}
	return out, nil
}

func (c *routingEmbeddingClient) EmbedQueryWithUsage(context.Context, string) (*vertex.EmbedResult, error) {
	c.queryCalls++
	return &vertex.EmbedResult{Embedding: []float32{1, 0}, Model: "stub", Provider: "stub"}, nil
}

func (c *routingEmbeddingClient) EmbedDocumentsWithUsage(_ context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	c.documentCalls++
	out := make([][]float32, len(documents))
	for i := range out {
		out[i] = []float32{0, 1}
	}
	return &vertex.BatchEmbedResult{Embeddings: out, Model: "stub", Provider: "stub"}, nil
}

// TestEmbedDocumentUsesTheDocumentPath pins that EmbedDocumentWithUsage and
// EmbedDocument reach the *document* path, not the query one. Re-pointing either
// method at EmbedQueryWithUsage would still compile and still return a vector of
// the right shape, so the routing — not the shape — is what this asserts.
func TestEmbedDocumentUsesTheDocumentPath(t *testing.T) {
	client := &routingEmbeddingClient{}
	svc := &Service{client: client, log: slog.Default(), enabled: true}
	ctx := context.Background()

	result, err := svc.EmbedDocumentWithUsage(ctx, "a stored chunk")
	if err != nil {
		t.Fatalf("EmbedDocumentWithUsage() error = %v", err)
	}
	if client.documentCalls != 1 || client.queryCalls != 0 {
		t.Errorf("calls = %d document / %d query, want 1/0", client.documentCalls, client.queryCalls)
	}
	if len(result.Embedding) != 2 || result.Embedding[1] != 1 {
		t.Errorf("Embedding = %v, want the document vector [0 1]", result.Embedding)
	}

	vec, err := svc.EmbedDocument(ctx, "a stored chunk")
	if err != nil {
		t.Fatalf("EmbedDocument() error = %v", err)
	}
	if client.documentCalls != 2 || client.queryCalls != 0 {
		t.Errorf("calls = %d document / %d query, want 2/0", client.documentCalls, client.queryCalls)
	}
	if len(vec) != 2 || vec[1] != 1 {
		t.Errorf("EmbedDocument() = %v, want the document vector [0 1]", vec)
	}
}
