package extraction

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/model"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	embgenai "github.com/emergent-company/emergent.memory/pkg/embeddings/genai"
)

// thinkingModelWrapper applies an explicit thinking toggle to every model the
// factory creates. DocumentClassifier builds its LLM internally through the
// ModelFactory, so the toggle is injected via the factory's ModelWrapper hook
// (the current API — config.LLMConfig has no thinking/reasoning-effort fields).
type thinkingModelWrapper struct {
	enableThinking *bool
}

// WrapModel implements adk.ModelWrapper. It applies the thinking override on
// models that support it (DeepSeek/Qwen3 via ThinkingConfigurator) and returns
// the model unchanged otherwise.
func (w thinkingModelWrapper) WrapModel(inner adkmodel.LLM, _ string) adkmodel.LLM {
	if tc, ok := inner.(adk.ThinkingConfigurator); ok {
		tc.SetEnableThinking(w.enableThinking)
	}
	return inner
}

// buildClassifierWithThinking creates a DocumentClassifier with explicit thinking
// control. enableThinking nil = auto (server default), true = always on, false = off.
func buildClassifierWithThinking(t *testing.T, enableThinking *bool) *DocumentClassifier {
	t.Helper()
	loadEnvFiles()

	deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
	deepseekModel := os.Getenv("DEEPSEEK_MODEL")
	googleAPIKey := os.Getenv("GOOGLE_API_KEY")

	if deepseekKey == "" || deepseekModel == "" {
		t.Skip("DEEPSEEK_API_KEY / DEEPSEEK_MODEL not set, skipping thinking e2e test")
	}
	if googleAPIKey == "" {
		t.Skip("GOOGLE_API_KEY not set, skipping thinking e2e test")
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	llmCfg := &config.LLMConfig{
		DeepSeekAPIKey: deepseekKey,
		DeepSeekModel:  deepseekModel,
	}
	modelFactory := adk.NewModelFactory(llmCfg, log, nil, thinkingModelWrapper{enableThinking: enableThinking}, nil)

	embClient, err := embgenai.NewClient(context.Background(), embgenai.Config{
		APIKey: googleAPIKey,
	})
	require.NoError(t, err)
	embSvc := &genaiEmbeddingService{client: embClient}

	return NewDocumentClassifier(modelFactory, embSvc, log)
}

// boolPtr is a small helper to take the address of a bool literal.
func boolPtr(v bool) *bool { return &v }

// TestClassifier_ThinkingModes runs the same set of classification inputs under
// thinking disabled and enabled and checks that:
//
//  1. Both thinking=false and thinking=true yield a valid match (non-empty DomainName).
//  2. thinking=false completes faster on average — no chain-of-thought overhead.
//
// The test is a live network test (DeepSeek API + Google genai). It is skipped
// unless DEEPSEEK_API_KEY, DEEPSEEK_MODEL, and GOOGLE_API_KEY are present.
func TestClassifier_ThinkingModes(t *testing.T) {
	schema := hermesAgentMemorySchema()
	packs := []InstalledSchemaSummary{schema}
	ctx := context.Background()

	cases := []struct {
		name string
		text string
	}{
		{
			name: "short user preference",
			text: "User mcj prefers short responses and technical depth",
		},
		{
			name: "single decision fact",
			text: "Decision: Use Go for the backend API because it offers the best performance",
		},
		{
			name: "infrastructure fact",
			text: "Host mcj-one runs Ubuntu 22.04 and is used for all production deployments",
		},
	}

	type modeResult struct {
		matched  bool
		duration time.Duration
	}

	runMode := func(t *testing.T, label string, enableThinking *bool) []modeResult {
		t.Helper()
		classifier := buildClassifierWithThinking(t, enableThinking)
		results := make([]modeResult, len(cases))
		for i, tc := range cases {
			start := time.Now()
			result, err := classifier.Classify(ctx, tc.text, packs)
			elapsed := time.Since(start)
			require.NoError(t, err, "mode=%s case=%s", label, tc.name)
			results[i] = modeResult{
				matched:  result.DomainName != "",
				duration: elapsed,
			}
			t.Logf("mode=%-16s case=%-35s matched=%v confidence=%.2f latency=%s",
				label, tc.name, results[i].matched, result.Confidence, elapsed.Round(time.Millisecond))
		}
		return results
	}

	// Run both modes. Each subtest independently skips if credentials absent.
	var (
		noThinking  []modeResult
		yesThinking []modeResult
	)

	t.Run("thinking=disabled", func(t *testing.T) {
		noThinking = runMode(t, "disabled", boolPtr(false))
		for i, tc := range cases {
			assert.True(t, noThinking[i].matched,
				"thinking=disabled: expected match for %q", tc.name)
		}
	})

	t.Run("thinking=enabled", func(t *testing.T) {
		yesThinking = runMode(t, "enabled", boolPtr(true))
		for i, tc := range cases {
			assert.True(t, yesThinking[i].matched,
				"thinking=enabled: expected match for %q", tc.name)
		}
	})

	// Latency comparison: thinking=false should be faster on average.
	// We only compare if both sub-tests ran (both sets non-nil).
	if noThinking != nil && yesThinking != nil {
		var totalNo, totalYes time.Duration
		for i := range cases {
			totalNo += noThinking[i].duration
			totalYes += yesThinking[i].duration
		}
		avgNo := totalNo / time.Duration(len(cases))
		avgYes := totalYes / time.Duration(len(cases))
		t.Logf("average latency: thinking=disabled %s  thinking=enabled %s", avgNo.Round(time.Millisecond), avgYes.Round(time.Millisecond))
		// Soft assertion: disabled is faster, but don't hard-fail — network variance.
		if avgNo >= avgYes {
			t.Logf("WARNING: thinking=disabled was NOT faster (disabled=%s enabled=%s) — may be network jitter", avgNo, avgYes)
		}
	}
}
