package graph

import "testing"

func f32ptr(v float32) *float32 { return &v }

// minScore is a similarity (higher = more similar) while the repository filters
// on cosine distance (lower = more similar). Forwarding one as the other made
// the filter inert: minScore=0.9 admitted everything above a 0.1 similarity.
func TestSimilarMaxDistance(t *testing.T) {
	cases := []struct {
		name string
		req  SimilarObjectsRequest
		want *float32
	}{
		{
			name: "maxDistance is already a distance",
			req:  SimilarObjectsRequest{MaxDistance: f32ptr(0.2)},
			want: f32ptr(0.2),
		},
		{
			name: "minScore converts to the complementary distance",
			req:  SimilarObjectsRequest{MinScore: f32ptr(0.9)},
			want: f32ptr(0.1),
		},
		{
			name: "minScore 1 becomes distance 0",
			req:  SimilarObjectsRequest{MinScore: f32ptr(1)},
			want: f32ptr(0),
		},
		{
			name: "minScore above 1 clamps to distance 0 rather than going negative",
			req:  SimilarObjectsRequest{MinScore: f32ptr(1.5)},
			want: f32ptr(0),
		},
		{
			name: "minScore 0 means no threshold",
			req:  SimilarObjectsRequest{MinScore: f32ptr(0)},
			want: nil,
		},
		{
			name: "no bound supplied means no threshold",
			req:  SimilarObjectsRequest{},
			want: nil,
		},
		{
			name: "maxDistance wins when both are supplied",
			req:  SimilarObjectsRequest{MaxDistance: f32ptr(0.3), MinScore: f32ptr(0.9)},
			want: f32ptr(0.3),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := similarMaxDistance(&c.req)
			if c.want == nil {
				if got != nil {
					t.Fatalf("similarMaxDistance = %v, want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("similarMaxDistance = nil, want %v", *c.want)
			}
			// float32 arithmetic: 1 - 0.9 is not exactly 0.1.
			if d := *got - *c.want; d > 1e-6 || d < -1e-6 {
				t.Fatalf("similarMaxDistance = %v, want %v", *got, *c.want)
			}
		})
	}
}
