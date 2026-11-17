package sources

import "testing"

func TestTriggersOwnReviewGate(t *testing.T) {
	tests := []struct {
		name     string
		triggers []*SourceTrigger
		want     bool
	}{
		{"pull request", []*SourceTrigger{{EventType: "pull_request"}}, true},
		{"issues only", []*SourceTrigger{{EventType: "issues"}}, false},
		{"mixed", []*SourceTrigger{{EventType: "issues"}, {EventType: "pull_request"}}, true},
		{"disabled pull request still owns", []*SourceTrigger{{EventType: "pull_request", Enabled: false}}, true},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := triggersOwnReviewGate(tt.triggers); got != tt.want {
				t.Fatalf("triggersOwnReviewGate() = %v, want %v", got, tt.want)
			}
		})
	}
}
