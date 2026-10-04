package extraction

import "testing"

func TestSkipEmbeddingReason(t *testing.T) {
	cases := []struct {
		name      string
		isParent  bool
		oversized bool
		textLen   int
		want      string
	}{
		{"normal", false, false, 1000, ""},
		{"parent excluded", true, false, 1000, "parent_chunk"},
		{"oversized within context still embedded", false, true, 1000, ""},
		{"oversized over context skipped", false, true, maxEmbedChars + 1, "oversized_over_context"},
		{"parent wins over oversized", true, true, maxEmbedChars + 1, "parent_chunk"},
	}
	for _, tc := range cases {
		if got := skipEmbeddingReason(tc.isParent, tc.oversized, tc.textLen); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
