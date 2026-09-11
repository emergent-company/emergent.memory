package acpslug

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple lowercase", in: "My Agent", want: "my-agent"},
		{name: "mixed case", in: "Gamma Agent", want: "gamma-agent"},
		{name: "punctuation collapsed", in: "Hello,  World!", want: "hello-world"},
		{name: "leading/trailing separators trimmed", in: "  --Foo__Bar--  ", want: "foo-bar"},
		{name: "unicode stripped", in: "Café Agent", want: "caf-agent"},
		{name: "already a slug", in: "already-a-slug", want: "already-a-slug"},
		{name: "empty", in: "", want: ""},
		{name: "only separators", in: "---", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FromName(tt.in))
		})
	}
}

func TestFromNameTruncatesTo63(t *testing.T) {
	// 80 'a' characters -> truncated to 63 with no trailing hyphen inserted.
	got := FromName(strings.Repeat("A", 80))
	if len(got) != 63 {
		t.Fatalf("len = %d, want 63", len(got))
	}
	assert.Equal(t, strings.Repeat("a", 63), got)

	// A hyphen landing at position 63 must be trimmed after truncation.
	in := strings.Repeat("a", 62) + "-bcd"
	got = FromName(in)
	assert.LessOrEqual(t, len(got), 63)
	assert.False(t, strings.HasSuffix(got, "-"))
}

// TestFromNameIdempotent verifies normalization is stable: slugifying an
// already-normalized slug yields the same value (round-trip).
func TestFromNameIdempotent(t *testing.T) {
	for _, in := range []string{"My Agent", "Café  Agent!", "  Mixed--Case  ", "already-a-slug"} {
		once := FromName(in)
		assert.Equal(t, once, FromName(once), "input %q", in)
	}
}
