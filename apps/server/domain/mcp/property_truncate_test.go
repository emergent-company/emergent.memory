package mcp

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/google/uuid"
)

// longStr returns an ASCII string of exactly n runes.
func longStr(n int) string {
	return strings.Repeat("x", n)
}

func TestTruncateString(t *testing.T) {
	t.Run("over cap truncates to 4000 runes with marker", func(t *testing.T) {
		s := longStr(4500)
		got := truncateString(s)

		if !strings.HasPrefix(got, longStr(maxPropertyValueChars)) {
			t.Errorf("truncateString should keep the first %d runes as prefix", maxPropertyValueChars)
		}
		if !strings.Contains(got, "[truncated 500 chars]") {
			t.Errorf("truncateString marker missing: %q", got)
		}
		// total rune length = 4000 + len of marker suffix
		suffix := "… [truncated 500 chars]"
		if got != longStr(maxPropertyValueChars)+suffix {
			t.Errorf("truncateString = %q, want prefix+marker", got)
		}
	})

	t.Run("under cap unchanged", func(t *testing.T) {
		s := longStr(10)
		if got := truncateString(s); got != s {
			t.Errorf("truncateString(short) = %q, want %q", got, s)
		}
	})

	t.Run("exactly at cap unchanged", func(t *testing.T) {
		s := longStr(maxPropertyValueChars)
		if got := truncateString(s); got != s {
			t.Errorf("truncateString(at cap) changed string")
		}
	})
}

func TestTruncateProperties(t *testing.T) {
	t.Run("long value truncated", func(t *testing.T) {
		props := map[string]any{"content": longStr(4500)}
		got := truncateProperties(props)
		content, ok := got["content"].(string)
		if !ok {
			t.Fatalf("content should be a string, got %T", got["content"])
		}
		if !strings.HasPrefix(content, longStr(maxPropertyValueChars)) {
			t.Errorf("content not truncated")
		}
		if !strings.Contains(content, "[truncated 500 chars]") {
			t.Errorf("content marker missing: %q", content)
		}
	})

	t.Run("under cap values unchanged", func(t *testing.T) {
		props := map[string]any{"a": "short", "b": longStr(100)}
		got := truncateProperties(props)
		if got["a"] != "short" {
			t.Errorf("short value changed: %v", got["a"])
		}
		if got["b"] != longStr(100) {
			t.Errorf("under-cap value changed")
		}
	})

	t.Run("nested map value truncated", func(t *testing.T) {
		props := map[string]any{"nested": map[string]any{"content": longStr(4500)}}
		got := truncateProperties(props)
		nested, ok := got["nested"].(map[string]any)
		if !ok {
			t.Fatalf("nested should be map[string]any, got %T", got["nested"])
		}
		content := nested["content"].(string)
		if !strings.Contains(content, "[truncated 500 chars]") {
			t.Errorf("nested content not truncated: %q", content)
		}
	})

	t.Run("nested []any value truncated", func(t *testing.T) {
		props := map[string]any{"arr": []any{longStr(4500)}}
		got := truncateProperties(props)
		arr, ok := got["arr"].([]any)
		if !ok {
			t.Fatalf("arr should be []any, got %T", got["arr"])
		}
		content := arr[0].(string)
		if !strings.Contains(content, "[truncated 500 chars]") {
			t.Errorf("[]any element not truncated: %q", content)
		}
	})

	t.Run("[]string value truncated", func(t *testing.T) {
		props := map[string]any{"arr": []string{longStr(4500)}}
		got := truncateProperties(props)
		arr, ok := got["arr"].([]string)
		if !ok {
			t.Fatalf("arr should be []string, got %T", got["arr"])
		}
		content := arr[0]
		if !strings.Contains(content, "[truncated 500 chars]") {
			t.Errorf("[]string element not truncated: %q", content)
		}
	})

	t.Run("non-string scalars pass through", func(t *testing.T) {
		props := map[string]any{"i": 42, "f": 3.14, "b": true, "s": "kept"}
		got := truncateProperties(props)
		if got["i"] != 42 {
			t.Errorf("int changed: %v", got["i"])
		}
		if got["f"] != 3.14 {
			t.Errorf("float64 changed: %v", got["f"])
		}
		if got["b"] != true {
			t.Errorf("bool changed: %v", got["b"])
		}
		if got["s"] != "kept" {
			t.Errorf("string changed: %v", got["s"])
		}
	})

	t.Run("input map not mutated", func(t *testing.T) {
		full := longStr(4500)
		props := map[string]any{"content": full}
		_ = truncateProperties(props)
		if props["content"] != full {
			t.Errorf("input map was mutated; content no longer holds full string")
		}
	})

	t.Run("empty and nil maps safe", func(t *testing.T) {
		if got := truncateProperties(nil); got != nil {
			t.Errorf("truncateProperties(nil) = %v, want nil", got)
		}
		if got := truncateProperties(map[string]any{}); len(got) != 0 {
			t.Errorf("truncateProperties(empty) = %v, want empty", got)
		}
	})
}

func TestSlimEntityTruncatesProperties(t *testing.T) {
	o := &graph.GraphObjectResponse{
		CanonicalID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Type:        "document",
		Properties:  map[string]any{"content": longStr(4500), "title": "ok"},
	}

	got := slimEntity(o, ResponseOpts{FieldStrategy: "full"})
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", got["properties"])
	}
	content, ok := props["content"].(string)
	if !ok {
		t.Fatalf("expected content string, got %T", props["content"])
	}
	if !strings.HasPrefix(content, longStr(maxPropertyValueChars)) {
		t.Errorf("emitted content not truncated to prefix")
	}
	if !strings.Contains(content, "[truncated 500 chars]") {
		t.Errorf("emitted content marker missing: %q", content)
	}
	if props["title"] != "ok" {
		t.Errorf("short property changed: %v", props["title"])
	}
	// The source object must remain untouched.
	if o.Properties["content"] != longStr(4500) {
		t.Errorf("slimEntity mutated the source properties")
	}
}
