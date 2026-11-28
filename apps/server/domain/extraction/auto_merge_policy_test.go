package extraction

import "testing"

func TestResolveAutoMergePolicy(t *testing.T) {
	cases := map[string]string{
		"partial":        "partial",
		"enrich":         "enrich",
		"partial_newest": "partial_newest",
		"newest":         "newest",
		"":               "partial",
		"overwrite":      "partial",
	}
	for in, want := range cases {
		if got := resolveAutoMergePolicy(in); got != want {
			t.Fatalf("resolveAutoMergePolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveAutoMergeThreshold(t *testing.T) {
	cases := map[float32]float32{
		0.5:  0.5,
		0.92: 0.92,
		1.0:  1.0,
		0:    0, // unset → graph default
		-1:   0, // invalid → graph default
		1.5:  0, // out of range → graph default
	}
	for in, want := range cases {
		if got := resolveAutoMergeThreshold(in); got != want {
			t.Fatalf("resolveAutoMergeThreshold(%v) = %v, want %v", in, got, want)
		}
	}
}
