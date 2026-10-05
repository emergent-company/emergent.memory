package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseModelLimits_TypeLessFeed is the core #1464 assertion: the models.dev
// feed no longer emits a top-level `type` field, so the parser must key off the
// limit block rather than a `type == "generative"` filter. A regression that
// reintroduces the type filter yields an empty map and forces the static
// fallback on every run.
func TestParseModelLimits_TypeLessFeed(t *testing.T) {
	raw := modelsDevResponse{
		"openai": {Models: map[string]modelsDevModel{
			// Note: no `type` field anywhere — matches the live feed.
			"gpt-5.4": {Limit: modelsDevLimit{Context: 1_050_000, Output: 128_000}},
		}},
		"google": {Models: map[string]modelsDevModel{
			"gemini-3.1-pro": {Limit: modelsDevLimit{Context: 1_000_000, Output: 65_536}},
		}},
	}

	limits := parseModelLimits(raw)

	require.Len(t, limits, 2, "type-less generative models must be included")
	assert.Equal(t, modelLimits{MaxInputTokens: 1_050_000, MaxOutputTokens: 128_000}, limits["gpt-5.4"])
	assert.Equal(t, modelLimits{MaxInputTokens: 1_000_000, MaxOutputTokens: 65_536}, limits["gemini-3.1-pro"])
}

// TestParseModelLimits_SkipsZeroOutput verifies entries with no positive output
// limit (minimal/fixture entries; embedding vector dims are not output tokens)
// are skipped rather than recorded with a bogus limit.
func TestParseModelLimits_SkipsZeroOutput(t *testing.T) {
	raw := modelsDevResponse{
		"p": {Models: map[string]modelsDevModel{
			"has-limits":    {Limit: modelsDevLimit{Context: 8192, Output: 4096}},
			"zero-output":   {Limit: modelsDevLimit{Context: 8192, Output: 0}},
			"no-limit-node": {},
		}},
	}

	limits := parseModelLimits(raw)

	require.Len(t, limits, 1)
	assert.Contains(t, limits, "has-limits")
	assert.NotContains(t, limits, "zero-output")
	assert.NotContains(t, limits, "no-limit-node")
}

// TestFetchRemoteLimits_BodyLargerThanFourMB is the body-cap assertion: the
// live feed is currently ~5.3 MB, so a 4 MB LimitReader truncates the JSON and
// the parse fails. The fetcher must read the full body (64 MB cap).
func TestFetchRemoteLimits_BodyLargerThanFourMB(t *testing.T) {
	// A ~5 MB body: the large ignored field is what pushes it past the old cap.
	big := strings.Repeat("a", 5<<20)
	body := fmt.Sprintf(`{"p":{"models":{"big-model":{"extra":%q,"limit":{"context":100,"output":7}}}}}`, big)
	require.Greater(t, len(body), 4<<20)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	s := &ModelLimitsSyncService{client: srv.Client(), url: srv.URL}
	limits, err := s.fetchRemoteLimits(context.Background())
	require.NoError(t, err, "a >4MB body must not be truncated")
	assert.Equal(t, modelLimits{MaxInputTokens: 100, MaxOutputTokens: 7}, limits["big-model"])
}
