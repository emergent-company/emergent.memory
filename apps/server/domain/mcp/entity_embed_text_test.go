package mcp

import (
	"strings"
	"testing"
)

func TestBuildEntityEmbedTextIncludesTypeKeyAndProps(t *testing.T) {
	key := "preference-ice-cream"
	props := map[string]any{
		"category": "preference",
		"content":  "User really likes ice cream",
	}

	out := buildEntityEmbedText("Note", &key, props)

	if !strings.Contains(out, "Note") {
		t.Fatalf("expected type in embed text, got %q", out)
	}
	if !strings.Contains(out, key) {
		t.Fatalf("expected key in embed text, got %q", out)
	}
	if !strings.Contains(out, "content") || !strings.Contains(out, "ice cream") {
		t.Fatalf("expected flattened property in embed text, got %q", out)
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
	if !strings.Contains(out, "content") {
		t.Fatalf("expected flattened property with nil key, got %q", out)
	}
}
