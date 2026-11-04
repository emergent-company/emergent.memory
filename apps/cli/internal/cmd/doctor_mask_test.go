package cmd

import (
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/config"
	"github.com/emergent-company/emergent.memory/apps/cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `memory server doctor` reached the same masked-key display through
// cfg.APIKey[:8] and panicked on a short key (#2127). The auth check returns
// before touching the network when an API key is configured, so this is a
// pure display regression test.
func TestCheckAuthDoesNotPanicOnShortAPIKey(t *testing.T) {
	for _, key := range []string{"a", "abc", "abcdefg"} {
		t.Run(key, func(t *testing.T) {
			capture := testutil.CaptureOutput()
			defer capture.Restore()

			var result checkResult
			require.NotPanics(t, func() {
				result = checkAuth(&config.Config{APIKey: key}, filepath.Join(t.TempDir(), "config.yaml"))
			}, "checkAuth must not panic on a %d-character api_key", len(key))

			_, _, readErr := capture.Read()
			require.NoError(t, readErr)

			assert.Equal(t, "pass", result.status)
			assert.Equal(t, "Using API key ("+maskedSecretPlaceholder+")", result.message)
			assert.NotContains(t, result.message, key)
		})
	}
}

// The Google-provider branch used key[:min(8, len(key))] + "...", which did
// not panic but printed short values in full (#2127).
func TestCheckLLMProviderMasksShortGoogleKey(t *testing.T) {
	home := t.TempDir()
	writeEnvLocalFixture(t, home, "GOOGLE_API_KEY=short\n", 0o600)

	capture := testutil.CaptureOutput()
	defer capture.Restore()

	var result checkResult
	require.NotPanics(t, func() { result = checkLLMProvider(filepath.Join(home, ".memory")) })

	stdout, _, readErr := capture.Read()
	require.NoError(t, readErr)

	assert.Equal(t, "Using Google AI ("+maskedSecretPlaceholder+")", result.message)
	assert.NotContains(t, stdout, "short")
	assert.NotContains(t, result.message, "short")
}
