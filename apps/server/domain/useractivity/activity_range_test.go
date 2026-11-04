package useractivity

import "testing"

func TestParseActivityRange(t *testing.T) {
	cases := map[string]int{"": 30, "7d": 7, "30d": 30, "90d": 90, "14": 14, "bogus": 30, "0": 30, "-3": 30, "9999": 30}
	for in, want := range cases {
		if got := ParseActivityRange(in); got != want {
			t.Fatalf("ParseActivityRange(%q) = %d, want %d", in, got, want)
		}
	}
}
