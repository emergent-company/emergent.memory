package mcp

import (
	"strings"
	"testing"
)

func TestGenerateEntityKeyDeterministic(t *testing.T) {
	props := map[string]any{
		"category": "preference",
		"content":  "User really likes ice cream",
		"source":   "explicit",
	}

	k1 := generateEntityKey("Note", props)
	k2 := generateEntityKey("Note", props)

	if k1 != k2 {
		t.Fatalf("same content must yield same key, got %q vs %q", k1, k2)
	}
	if k1 == "" {
		t.Fatal("generated key must not be empty")
	}
}

func TestGenerateEntityKeyHasTypePrefix(t *testing.T) {
	props := map[string]any{"content": "hello"}

	k := generateEntityKey("Note", props)
	if !strings.HasPrefix(k, "note-") {
		t.Fatalf("expected key to start with slugified type 'note-', got %q", k)
	}
}

func TestGenerateEntityKeyDifferentContent(t *testing.T) {
	a := map[string]any{"content": "likes ice cream"}
	b := map[string]any{"content": "likes cake"}

	ka := generateEntityKey("Note", a)
	kb := generateEntityKey("Note", b)

	if ka == kb {
		t.Fatalf("different content must yield different keys, both were %q", ka)
	}
}

func TestGenerateEntityKeyPropertyOrderIndependent(t *testing.T) {
	a := map[string]any{"category": "preference", "content": "x"}
	b := map[string]any{"content": "x", "category": "preference"}

	ka := generateEntityKey("Note", a)
	kb := generateEntityKey("Note", b)

	if ka != kb {
		t.Fatalf("property insertion order must not change key, got %q vs %q", ka, kb)
	}
}

func TestGenerateEntityKeyEmptyTypeFallback(t *testing.T) {
	k := generateEntityKey("", map[string]any{"content": "x"})
	if !strings.HasPrefix(k, "entity-") {
		t.Fatalf("expected empty type to fall back to 'entity-' prefix, got %q", k)
	}
}
