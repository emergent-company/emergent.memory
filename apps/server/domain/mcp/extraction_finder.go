package mcp

import "context"

// ExtractionJobInfo is a minimal projection of an extraction job used by the
// remember-status MCP tool to follow queue-reextraction indirection: the
// remember/forget agent queues an async ObjectExtractionJob (via the
// queue-reextraction tool) instead of calling entity-create directly, and the
// actual graph mutations happen when that job runs.
//
// Defined in the mcp package because both the agents domain (which owns the
// remember-status handler) and the extraction domain (which owns the job
// records) already import mcp — this avoids an import cycle between them
// (agents → extraction → projects → agents).
type ExtractionJobInfo struct {
	ID string
	// Status is the raw extraction job status: pending, processing, completed,
	// failed, dead_letter, or cancelled.
	Status string
	// ObjectsCreated / RelationshipsCreated are the counts persisted on the job
	// row when it completes. ObjectsMerged is NOT persisted, so update counts
	// from extraction jobs are not recoverable.
	ObjectsCreated       int
	RelationshipsCreated int
	DiscoveredTypes      []string
	CreatedObjectIDs     []string
	ErrorMessage         *string
}

// ExtractionJobFinder resolves an extraction job by ID for remember-status.
// Implemented by an adapter in the extraction domain; injected into the agents
// MCP tool handler via a setter to avoid an import cycle.
type ExtractionJobFinder interface {
	FindByID(ctx context.Context, jobID string) (*ExtractionJobInfo, error)
}
