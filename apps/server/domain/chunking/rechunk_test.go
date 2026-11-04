package chunking

import "testing"

func TestSourceTextStillPresent(t *testing.T) {
	cases := []struct {
		name     string
		original string
		content  string
		want     bool
	}{
		{"empty original", "", "anything", true},
		{"present", "hello world", "say hello world now", true},
		{"gone", "removed paragraph", "completely different text", false},
	}
	for _, tc := range cases {
		if got := sourceTextStillPresent(tc.original, tc.content); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
