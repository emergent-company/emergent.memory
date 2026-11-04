package embeddingtext

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func strptr(s string) *string { return &s }

func TestText_DeterministicAcrossMapOrder(t *testing.T) {
	// Same logical content, different Go map insertion order. The builder must
	// visit keys in sorted order, so both produce byte-identical text/hash.
	a := map[string]any{
		"name":        "Alice",
		"description": "Engineer",
		"zeta":        "last",
		"alpha":       "first",
	}
	b := map[string]any{
		"alpha":       "first",
		"zeta":        "last",
		"description": "Engineer",
		"name":        "Alice",
	}

	ta := Text("Person", strptr("ns:alice"), a)
	tb := Text("Person", strptr("ns:alice"), b)
	require.Equal(t, ta, tb)
	require.Equal(t, Hash("Person", strptr("ns:alice"), a), Hash("Person", strptr("ns:alice"), b))
}

func TestText_OnlyEmbeddedFieldsAffectHash(t *testing.T) {
	base := map[string]any{
		"name":          "Alice",
		"description":   "Engineer",
		"section_label": "Kapittel 1",
	}
	baseHash := Hash("Person", strptr("k1"), base)

	t.Run("boosted field changes hash", func(t *testing.T) {
		next := map[string]any{"name": "Alice B", "description": "Engineer", "section_label": "Kapittel 1"}
		require.NotEqual(t, baseHash, Hash("Person", strptr("k1"), next))
	})

	t.Run("remaining semantic field changes hash", func(t *testing.T) {
		next := map[string]any{"name": "Alice", "description": "Engineer", "section_label": "Kapittel 2"}
		require.NotEqual(t, baseHash, Hash("Person", strptr("k1"), next))
	})

	t.Run("skipped key does not change hash", func(t *testing.T) {
		next := map[string]any{"name": "Alice", "description": "Engineer", "section_label": "Kapittel 1", "version": "2", "checksum": "abc"}
		require.Equal(t, baseHash, Hash("Person", strptr("k1"), next))
	})

	t.Run("url-valued field does not change hash", func(t *testing.T) {
		next := map[string]any{"name": "Alice", "description": "Engineer", "section_label": "Kapittel 1", "source": "https://example.com/doc"}
		require.Equal(t, baseHash, Hash("Person", strptr("k1"), next))
	})

	t.Run("large numeric id does not change hash", func(t *testing.T) {
		next := map[string]any{"name": "Alice", "description": "Engineer", "section_label": "Kapittel 1", "external_id": float64(1e12)}
		require.Equal(t, baseHash, Hash("Person", strptr("k1"), next))
	})

	t.Run("object type and key affect hash", func(t *testing.T) {
		require.NotEqual(t, baseHash, Hash("Company", strptr("k1"), base))
		require.NotEqual(t, baseHash, Hash("Person", strptr("k2"), base))
		require.NotEqual(t, baseHash, Hash("Person", nil, base))
	})
}

func TestText_KeyHandling(t *testing.T) {
	t.Run("namespace prefix is stripped", func(t *testing.T) {
		require.Contains(t, Text("Person", strptr("ns:sarah"), nil), "sarah")
		require.NotContains(t, Text("Person", strptr("ns:sarah"), nil), "ns:sarah")
	})

	t.Run("uuid key is omitted", func(t *testing.T) {
		require.NotContains(t, Text("Person", strptr("550e8400-e29b-41d4-a716-446655440000"), nil), "550e8400")
	})
}

func TestNormalize_ProgrammaticTypesMatchJSONDecoded(t *testing.T) {
	// The write path may hash caller-supplied Go values; the worker hashes the
	// same properties after a JSONB round-trip. Normalize must make the two
	// views produce identical text (#1488 review).
	typed := map[string]any{
		"count": 5,
		"tags":  []string{"alpha", "beta"},
		"meta":  map[string]string{"role": "admin"},
	}
	decoded := map[string]any{
		"count": float64(5),
		"tags":  []any{"alpha", "beta"},
		"meta":  map[string]any{"role": "admin"},
	}
	require.Equal(t, Text("T", nil, decoded), Text("T", nil, Normalize(typed)))
	require.Equal(t, Hash("T", nil, decoded), Hash("T", nil, Normalize(typed)))

	// Normalize must not hide a real semantic difference.
	require.NotEqual(t, Hash("T", nil, Normalize(typed)),
		Hash("T", nil, Normalize(map[string]any{"tags": []string{"alpha", "gamma"}})))
}

func TestText_CapAtMaxChars(t *testing.T) {
	var sb strings.Builder
	for i := 0; sb.Len() < MaxChars+500; i++ {
		sb.WriteString("word ")
	}
	text := Text("Doc", nil, map[string]any{"summary": sb.String()})
	require.LessOrEqual(t, len(text), MaxChars+10)
	require.True(t, strings.HasSuffix(text, "..."))
}
