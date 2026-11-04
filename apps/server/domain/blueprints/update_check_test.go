package blueprints

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSemver(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in           string
		major, minor int
		patch        int
		ok           bool
	}{
		{"1.2.3", 1, 2, 3, true},
		{"v1.2.3", 1, 2, 3, true},
		{"0.0.1", 0, 0, 1, true},
		{"10.20.30", 10, 20, 30, true},
		{"1.2.3-beta.1", 1, 2, 3, true},
		{"1.2.3+build.5", 1, 2, 3, true},
		{"v2.0.0-rc.1", 2, 0, 0, true},
		{"1.2", 0, 0, 0, false},
		{"1", 0, 0, 0, false},
		{"1.2.3.4", 0, 0, 0, false},
		{"latest", 0, 0, 0, false},
		{"", 0, 0, 0, false},
		{"1.x.3", 0, 0, 0, false},
	}
	for _, tc := range cases {
		major, minor, patch, ok := parseSemver(tc.in)
		assert.Equal(t, tc.ok, ok, "input %q", tc.in)
		if tc.ok {
			assert.Equal(t, tc.major, major, "major %q", tc.in)
			assert.Equal(t, tc.minor, minor, "minor %q", tc.in)
			assert.Equal(t, tc.patch, patch, "patch %q", tc.in)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.1.0", true},
		{"1.0.0", "2.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"2.0.0", "1.9.9", false}, // downgrade is not "newer"
		{"v1.0.0", "v1.0.1", true},
		{"1.0.0-beta", "1.0.0", true},
		{"abc", "def", true}, // non-semver: any change is newer
		{"2024-01-01", "2024-01-02", true},
		{"same", "same", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, isNewerVersion(tc.current, tc.latest),
			"current=%q latest=%q", tc.current, tc.latest)
	}
}

func TestUpdateAvailable(t *testing.T) {
	t.Parallel()
	// Same version, different content → update (content change without bump).
	assert.True(t, updateAvailable("1.0.0", "1.0.0", "hash-a", "hash-b"))
	// Same version, same content → no update.
	assert.False(t, updateAvailable("1.0.0", "1.0.0", "hash", "hash"))
	// Newer version → update regardless of content.
	assert.True(t, updateAvailable("1.0.0", "2.0.0", "same", "same"))
	// Older version → no update.
	assert.False(t, updateAvailable("2.0.0", "1.0.0", "different", "different"))
}

func TestCanonicalManifestHash_StableAcrossSerializations(t *testing.T) {
	t.Parallel()
	// jsonb round-trips compact JSON into spaced JSON; the canonical hash must
	// be identical for both forms so content comparison is reliable.
	compact := json.RawMessage(`{"name":"x","version":"1.0.0","objectTypes":[{"name":"Thing"}]}`)
	spaced := json.RawMessage(`{ "name" : "x", "version" : "1.0.0", "objectTypes" : [ { "name" : "Thing" } ] }`)
	assert.Equal(t, canonicalManifestHash(compact), canonicalManifestHash(spaced))

	// Different content hashes differently.
	different := json.RawMessage(`{"name":"x","version":"2.0.0"}`)
	assert.NotEqual(t, canonicalManifestHash(compact), canonicalManifestHash(different))

	// Empty manifest hashes like {}.
	assert.Equal(t, canonicalManifestHash(json.RawMessage(`{}`)), canonicalManifestHash(nil))
}

func TestIsRepoNotFound(t *testing.T) {
	t.Parallel()
	require.True(t, isRepoNotFound(fetchFailed("repository or ref not found (private repositories require a token)", nil)))
	require.False(t, isRepoNotFound(fetchFailed("failed to download repository archive", nil)))
}
