package extraction

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	embgenai "github.com/emergent-company/emergent.memory/pkg/embeddings/genai"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

// genaiEmbeddingService wraps the genai embeddings client to satisfy EmbeddingService.
type genaiEmbeddingService struct {
	client *embgenai.Client
}

func (s *genaiEmbeddingService) IsEnabled() bool { return true }

func (s *genaiEmbeddingService) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return s.client.EmbedQuery(ctx, query)
}

func (s *genaiEmbeddingService) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	vec, err := s.client.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return &vertex.EmbedResult{Embedding: vec}, nil
}

// loadEnvFiles loads .env and .env.local from the repo root.
// Mirrors the logic in internal/testutil.LoadEnvFiles without importing testutil
// (which causes an import cycle from domain/extraction).
func loadEnvFiles() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	dir := filepath.Dir(thisFile)
	var rootDir string
	for i := 0; i < 15; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			rootDir = dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if rootDir == "" {
		return
	}
	_ = godotenv.Load(filepath.Join(rootDir, ".env"))
	_ = godotenv.Overload(filepath.Join(rootDir, ".env.local"))
}

// buildTestClassifier creates a DocumentClassifier with DeepSeek LLM and Google
// genai embeddings.  Returns the classifier and a skip function — call skip()
// at the top of each test to bail out when credentials are missing.
func buildTestClassifier(t *testing.T) (*DocumentClassifier, func()) {
	t.Helper()
	loadEnvFiles()

	deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
	deepseekModel := os.Getenv("DEEPSEEK_MODEL")
	googleAPIKey := os.Getenv("GOOGLE_API_KEY")

	skip := func() {
		if deepseekKey == "" || deepseekModel == "" {
			t.Skip("DEEPSEEK_API_KEY / DEEPSEEK_MODEL not set, skipping classifier e2e test")
		}
		if googleAPIKey == "" {
			t.Skip("GOOGLE_API_KEY not set, skipping classifier e2e test")
		}
	}
	skip()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// DeepSeek for LLM classification.
	llmCfg := &config.LLMConfig{
		DeepSeekAPIKey: deepseekKey,
		DeepSeekModel:  deepseekModel,
	}
	modelFactory := adk.NewModelFactory(llmCfg, log, nil, nil, nil)

	// Google genai for embeddings.
	embClient, err := embgenai.NewClient(context.Background(), embgenai.Config{
		APIKey: googleAPIKey,
	})
	require.NoError(t, err, "create genai embeddings client")
	embSvc := &genaiEmbeddingService{client: embClient}

	classifier := NewDocumentClassifier(modelFactory, embSvc, log)
	return classifier, skip
}

// hermesAgentMemorySchema returns an InstalledSchemaSummary that mirrors the
// "Hermes Agent Memory" schema described in issue #306 — 10 object types
// installed via `memory schemas install`, never via the /remember discovery flow.
func hermesAgentMemorySchema() InstalledSchemaSummary {
	return InstalledSchemaSummary{
		ID:          "00000000-0000-0000-0000-000000000001",
		Name:        "Hermes Agent Memory",
		Description: "Knowledge graph schema for AI agent memory: users, preferences, decisions, infrastructure, and procedures",
		TypeNames: []string{
			"User",
			"Preference",
			"Decision",
			"Host",
			"Service",
			"Credential",
			"Project",
			"Convention",
			"TechStack",
			"Procedure",
		},
	}
}

// TestClassifier_PreInstalledSchema_ReuseOnly confirms issue #306:
// the DocumentClassifier returns no_match (empty DomainName) for short
// single-type facts when a pre-installed schema has many types, because the
// LLM prompt requires "most" types to be present in the document.
//
// These subtests are expected to FAIL until the prompt / policy is fixed.
func TestClassifier_PreInstalledSchema_ReuseOnly(t *testing.T) {
	classifier, _ := buildTestClassifier(t)
	ctx := context.Background()

	schema := hermesAgentMemorySchema()
	packs := []InstalledSchemaSummary{schema}

	cases := []struct {
		name      string
		text      string
		wantMatch bool // true = DomainName must be non-empty
	}{
		{
			// Reproduces the exact example from the issue report.
			name:      "short user preference statement",
			text:      "User mcj prefers short responses and technical depth",
			wantMatch: true,
		},
		{
			// Second example from the issue report.
			name:      "decision statement",
			text:      "Decision: Use Go for the backend API because it offers the best performance for our throughput needs",
			wantMatch: true,
		},
		{
			// Single-type fact: Convention.
			// DeepSeek emits chain-of-thought prose before the JSON response.
			// With MaxOutputTokens=256 the JSON gets truncated and the parser
			// fails, returning no_match even though the LLM correctly identified
			// the schema.  This confirms the secondary issue in #306.
			name:      "single convention fact",
			text:      "Convention: All Go services must use context.Context as the first parameter",
			wantMatch: true,
		},
		{
			// Control: document that references many types from the schema.
			// Should match even with the current strict prompt.
			name: "multi-type document (control — should always match)",
			text: "User mcj works on Project legalplant using TechStack Go and Postgres. " +
				"He has a Preference for short responses and technical depth. " +
				"A Decision was made to use the Echo framework. " +
				"Convention: REST naming conventions must be followed. " +
				"Service: legalplant-api runs on Host mcj-one. " +
				"Procedure: deploy by running task release.",
			wantMatch: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := classifier.Classify(ctx, tc.text, packs)
			require.NoError(t, err)

			if tc.wantMatch {
				assert.NotEmpty(t, result.DomainName,
					"expected classifier to match %q but got no_match (DomainName empty) — this confirms issue #306",
					tc.name)
				assert.Greater(t, result.Confidence, float32(0.0),
					"matched domain should have non-zero confidence")
			} else {
				assert.Empty(t, result.DomainName)
			}
		})
	}
}
