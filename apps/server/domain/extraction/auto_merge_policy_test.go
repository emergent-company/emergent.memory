package extraction

import "testing"

func TestResolveAutoMergePolicy(t *testing.T) {
	cases := map[string]string{
		"partial":   "partial",
		"enrich":    "enrich",
		"":          "partial",
		"overwrite": "partial",
	}
	for in, want := range cases {
		if got := resolveAutoMergePolicy(in); got != want {
			t.Fatalf("resolveAutoMergePolicy(%q) = %q, want %q", in, got, want)
		}
	}
}
