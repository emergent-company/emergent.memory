package mcp

import "context"

// EmbeddingJobInfo reports the embedding-generation status for one graph object
// created by a remember run. Embedding generation is a third async stage after
// (1) the agent run and (2) the extraction job: when an extraction job creates
// graph objects it enqueues kb.graph_embedding_jobs rows, and only once those
// complete are the objects semantically searchable via recall/search.
//
// Status is the resolved per-object job status (pending, processing, completed,
// failed, dead_letter, cancelled) or "" when no embedding job exists for the
// object (embedding generation disabled or never enqueued — nothing pending).
type EmbeddingJobInfo struct {
	ObjectID  string
	Status    string
	LastError *string
}

// EmbeddingJobFinder resolves graph-embedding job status for a set of object
// IDs. Implemented by an adapter in the extraction domain; injected into the
// agents MCP tool handler via a setter to avoid an import cycle (agents →
// extraction → projects → agents).
type EmbeddingJobFinder interface {
	// FindByObjectIDs returns one EmbeddingJobInfo per requested object ID,
	// with the object's resolved embedding-job status ("" when no job exists).
	FindByObjectIDs(ctx context.Context, objectIDs []string) ([]EmbeddingJobInfo, error)
}
