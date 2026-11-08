package mcp

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/embeddingtext"
)

// The near-duplicate check embeds a query that is compared with stored object
// vectors by cosine distance, so it must use the exact builder that produced
// those stored vectors. A local re-implementation measured a different text and
// therefore under-detected duplicates; this test pins the two together.
func TestBuildEntityEmbedTextMatchesStoredEmbeddingText(t *testing.T) {
	key := "ns:preference-ice-cream"
	props := map[string]any{
		"name":    "Ice cream preference",
		"url":     "https://example.com/ice-cream",
		"content": "User really likes ice cream",
		"count":   float64(3),
	}

	got := buildEntityEmbedText("Note", &key, props)
	want := embeddingtext.Text("Note", &key, props)

	if got != want {
		t.Fatalf("near-duplicate query text drifted from the stored-embedding builder:\n got %q\nwant %q", got, want)
	}
}

func TestBuildEntityEmbedTextIncludesTypeKeyAndProps(t *testing.T) {
	key := "preference-ice-cream"
	props := map[string]any{
		"category": "preference",
		"content":  "User really likes ice cream",
	}

	out := buildEntityEmbedText("Note", &key, props)

	if !strings.HasPrefix(out, "Note") {
		t.Fatalf("expected type prefix, got %q", out)
	}
	if !strings.Contains(out, key) {
		t.Fatalf("expected key in embed text, got %q", out)
	}
	if !strings.Contains(out, "ice cream") || !strings.Contains(out, "preference") {
		t.Fatalf("expected property values in embed text, got %q", out)
	}
}

// Property keys themselves carry no embedding signal — only their values do —
// and a URL is not semantic content.
func TestBuildEntityEmbedTextOmitsKeysAndURLs(t *testing.T) {
	key := "preference-ice-cream"
	props := map[string]any{
		"content": "likes ice cream",
		"url":     "https://example.com/ice-cream",
	}

	out := buildEntityEmbedText("Note", &key, props)

	for _, unwanted := range []string{"content", "https://example.com"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("embed text should not contain %q, got %q", unwanted, out)
		}
	}
}

// Boosted fields are repeated so they dominate the vector.
func TestBuildEntityEmbedTextRepeatsName(t *testing.T) {
	key := "preference-ice-cream"
	out := buildEntityEmbedText("Note", &key, map[string]any{"name": "Ice cream"})

	if got := strings.Count(out, "Ice cream"); got != 2 {
		t.Fatalf("name should be boosted (repeated twice), got %d occurrences in %q", got, out)
	}
}

func TestBuildEntityEmbedTextPropertyOrderIndependent(t *testing.T) {
	key := "preference-ice-cream"
	a := map[string]any{"category": "preference", "content": "likes ice cream"}
	b := map[string]any{"content": "likes ice cream", "category": "preference"}

	oa := buildEntityEmbedText("Note", &key, a)
	ob := buildEntityEmbedText("Note", &key, b)

	if oa != ob {
		t.Fatalf("property insertion order must not change embed text, got %q vs %q", oa, ob)
	}
}

func TestBuildEntityEmbedTextNilKey(t *testing.T) {
	out := buildEntityEmbedText("Note", nil, map[string]any{"content": "x"})

	if !strings.HasPrefix(out, "Note") {
		t.Fatalf("expected type prefix with nil key, got %q", out)
	}
	if !strings.Contains(out, "x") {
		t.Fatalf("expected property value with nil key, got %q", out)
	}
}
