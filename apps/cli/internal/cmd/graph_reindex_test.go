package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGraphReindexEmbeddingsFlags(t *testing.T) {
	for _, name := range []string{"namespace", "force", "chunks"} {
		require.NotNil(t, graphReindexEmbeddingsCmd.Flags().Lookup(name),
			"%s flag should be defined on reindex-embeddings", name)
	}

	// --chunks is the whole point of the command for a project that changed
	// embedding model, so it must default to off rather than surprising a
	// caller who only meant to fill in NULL embeddings.
	chunks := graphReindexEmbeddingsCmd.Flags().Lookup("chunks")
	require.Equal(t, "false", chunks.DefValue)
	force := graphReindexEmbeddingsCmd.Flags().Lookup("force")
	require.Equal(t, "false", force.DefValue)
}

func TestGraphReindexEmbeddingsRegisteredOnGraph(t *testing.T) {
	var found bool
	for _, c := range graphCmd.Commands() {
		if c.Name() == "reindex-embeddings" {
			found = true
			break
		}
	}
	require.True(t, found, "reindex-embeddings should be registered under `memory graph`")
}
