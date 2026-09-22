package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfiguredGraphIVFFlatProbes(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want int
	}{
		{name: "default when unset", val: "", want: 5},
		{name: "explicit lower value", val: "2", want: 2},
		{name: "1 is the minimum valid value", val: "1", want: 1},
		{name: "explicit higher value is honoured", val: "10", want: 10},
		{name: "non-numeric falls back to default", val: "abc", want: 5},
		{name: "zero falls back to default", val: "0", want: 5},
		{name: "negative falls back to default", val: "-4", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SEARCH_GRAPH_IVFFLAT_PROBES", tt.val)
			assert.Equal(t, tt.want, configuredGraphIVFFlatProbes())
		})
	}
}

// TestGraphProbesBelowGlobalDefault guards the graph-object vector leg against
// being raised back to the global default. pgvector costs the ivfflat index
// roughly linearly in probes, so at probes=10 the scan over
// kb.graph_objects.embedding_v2 does about twice the work of probes=5 for the
// same top-k (measured ~2.7s vs ~0.38s on a fully embedded project).
func TestGraphProbesBelowGlobalDefault(t *testing.T) {
	t.Setenv("SEARCH_GRAPH_IVFFLAT_PROBES", "")
	t.Setenv("SEARCH_IVFFLAT_PROBES", "")
	assert.Less(t, configuredGraphIVFFlatProbes(), configuredIVFFlatProbes(),
		"graph-object ivfflat.probes must stay below the global default or the vector leg pays for probes it does not need")
}
