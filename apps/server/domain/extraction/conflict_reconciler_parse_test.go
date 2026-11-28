package extraction

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestParseReconcileResponseTolerant pins that the reconciler accepts the model's
// JSON even when a gateway wraps it in prose or a markdown code fence (the
// OpenAI-compatible path ignores application/json and prefixes explanations).
func TestParseReconcileResponseTolerant(t *testing.T) {
	asked := map[string]graph.ConflictValue{"job_title": {Source: "Masseuse", Target: "Masseuse and musician"}}
	cases := map[string]string{
		"bare":    `{"job_title": "Masseuse and musician"}`,
		"fenced":  "```json\n{\"job_title\": \"Masseuse and musician\"}\n```",
		"prose":   "When combining the two, the best value is: {\"job_title\": \"Masseuse and musician\"} — hope that helps.",
		"wrapper": `{"values": {"job_title": "Masseuse and musician"}}`,
	}
	for name, raw := range cases {
		got, err := parseReconcileResponse(raw, asked)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if got["job_title"] != "Masseuse and musician" {
			t.Errorf("%s: got %v, want job_title='Masseuse and musician'", name, got)
		}
	}
}
