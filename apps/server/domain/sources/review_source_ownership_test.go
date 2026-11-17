package sources

import "testing"

func TestSourceOwnsReviewGate(t *testing.T) {
	tests := []struct {
		name string
		src  *Source
		want bool
	}{
		{"nil", nil, true},
		{"no config", &Source{}, true},
		{"unset", &Source{Config: map[string]any{}}, true},
		{"opt in", &Source{Config: map[string]any{"review_gate": true}}, true},
		{"opt out", &Source{Config: map[string]any{"review_gate": false}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sourceOwnsReviewGate(tt.src); got != tt.want {
				t.Fatalf("sourceOwnsReviewGate() = %v, want %v", got, tt.want)
			}
		})
	}
}
