package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSecret builds a deterministic, obviously fake credential of exactly n
// characters with distinct 4-character ends. The filler is a single repeated
// character on purpose: .gitleaks.toml scans this repo, so a test value must
// stay low-entropy and never resemble a real key.
func fakeSecret(n int) string {
	if n < 8 {
		return strings.Repeat("a", n)
	}
	return "0123" + strings.Repeat("z", n-8) + "cdef"
}

// maskSecret must be total over string lengths: the old code sliced
// APIKey[:8] behind a `!= ""` guard, which panicked for any key shorter than
// 8 (#2127), and printed every character of keys up to 12.
func TestMaskSecretLengths(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		want     string
		disclose int // characters of value that appear in want
	}{
		{name: "empty", value: "", want: "***", disclose: 0},
		{name: "one char", value: "a", want: "***", disclose: 0},
		{name: "three chars", value: "abc", want: "***", disclose: 0},
		{name: "seven chars", value: "abcdefg", want: "***", disclose: 0},
		{name: "eight chars would be fully disclosed by 4+4", value: fakeSecret(8), want: "***", disclose: 0},
		{name: "nine chars", value: fakeSecret(9), want: "***", disclose: 0},
		{name: "twelve chars was the old full-disclosure boundary", value: fakeSecret(12), want: "***", disclose: 0},
		{name: "thirteen chars", value: fakeSecret(13), want: "***", disclose: 0},
		{name: "sixteen chars", value: fakeSecret(16), want: "***", disclose: 0},
		{name: "one below threshold", value: fakeSecret(secretMaskMinLen - 1), want: "***", disclose: 0},
		{name: "at threshold", value: fakeSecret(secretMaskMinLen), want: "0123...cdef", disclose: 8},
		{name: "32 chars", value: fakeSecret(32), want: "0123...cdef", disclose: 8},
		{name: "64 chars", value: fakeSecret(64), want: "0123...cdef", disclose: 8},
		{
			// Distinct ends prove the mask slices the value rather than
			// returning a constant.
			name:     "distinct prefix and suffix",
			value:    "tok_" + strings.Repeat("q", 20) + "_end",
			want:     "tok_..._end",
			disclose: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			require.NotPanics(t, func() { got = maskSecret(tt.value) }, "maskSecret must not panic on len=%d", len(tt.value))

			assert.Equal(t, tt.want, got)

			// Never print the whole secret, at any length.
			if tt.value != "" {
				assert.NotEqual(t, tt.value, got, "mask must differ from the value")
			}

			// Invariant: nothing is disclosed below the threshold, and at most
			// 4+4 characters are disclosed at or above it.
			var disclosed int
			if got != maskedSecretPlaceholder {
				disclosed = len(got) - len("...")
			}
			assert.Equal(t, tt.disclose, disclosed)
			if len(tt.value) < secretMaskMinLen {
				assert.Zero(t, disclosed, "a value below the threshold must disclose nothing")
			} else {
				assert.LessOrEqual(t, disclosed, 8, "never disclose more than a prefix and suffix")
			}

			if len(tt.value) < secretMaskMinLen && tt.value != "" {
				assert.NotContains(t, got, tt.value)
			}
		})
	}
}

// A regression guard for the original panic: reintroducing `value[:8]` or
// `value[len(value)-4:]` without a length check makes every subtest below
// panic (and fail) rather than print a placeholder.
func TestMaskSecretNeverSlicesShortValues(t *testing.T) {
	for n := 0; n < secretMaskMinLen; n++ {
		value := fakeSecret(n)
		t.Run(fmt.Sprintf("len=%d", n), func(t *testing.T) {
			var got string
			require.NotPanics(t, func() { got = maskSecret(value) }, "len=%d panicked", n)
			require.Equal(t, maskedSecretPlaceholder, got, "len=%d must disclose nothing", n)
		})
	}
}

// The threshold is only meaningful if the 8 characters the mask reveals stay a
// minority of the value. This pins the arithmetic behind secretMaskMinLen: at
// the threshold 8 visible characters are at most a third of the key, so at
// least twice as many characters (16) stay hidden.
func TestSecretMaskThresholdArithmetic(t *testing.T) {
	visible := 2 * secretMaskEdge
	require.Equal(t, 8, visible)
	require.GreaterOrEqual(t, secretMaskMinLen, 3*visible,
		"at the threshold the revealed characters must be at most a third of the value")
	require.GreaterOrEqual(t, secretMaskMinLen-visible, 2*visible,
		"the hidden span must be at least twice the revealed span")
}
