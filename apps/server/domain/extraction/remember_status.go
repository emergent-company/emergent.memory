package extraction

import (
	"context"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// rememberExtractionJobFinder adapts ObjectExtractionJobsService to
// mcp.ExtractionJobFinder for the remember-status MCP tool. It projects the
// ObjectExtractionJob row into the neutral mcp.ExtractionJobInfo shape so the
// agents domain (which cannot import extraction) can aggregate job results
// without an import cycle.
type rememberExtractionJobFinder struct {
	jobs *ObjectExtractionJobsService
}

// FindByID resolves a job by ID, returning nil (no error) when the job row does
// not exist.
func (f *rememberExtractionJobFinder) FindByID(ctx context.Context, jobID string) (*mcp.ExtractionJobInfo, error) {
	job, err := f.jobs.FindByID(ctx, jobID)
	if err != nil || job == nil {
		return nil, err
	}

	types := make([]string, 0, len(job.DiscoveredTypes))
	for _, raw := range job.DiscoveredTypes {
		if s, ok := raw.(string); ok && s != "" {
			types = append(types, s)
		}
	}

	return &mcp.ExtractionJobInfo{
		ID:                   job.ID,
		Status:               string(job.Status),
		ObjectsCreated:       job.ObjectsCreated,
		RelationshipsCreated: job.RelationshipsCreated,
		DiscoveredTypes:      types,
		CreatedObjectIDs:     job.CreatedObjectIDs,
		ErrorMessage:         job.ErrorMessage,
	}, nil
}

// NewRememberExtractionJobFinder creates the remember-status extraction job
// finder. Provided via fx so main.go can inject it into the agents MCP tool
// handler (agents and extraction cannot import each other, so the injection
// happens at the composition root).
func NewRememberExtractionJobFinder(jobs *ObjectExtractionJobsService) mcp.ExtractionJobFinder {
	return &rememberExtractionJobFinder{jobs: jobs}
}

// rememberEmbeddingJobFinder adapts GraphEmbeddingJobsService to
// mcp.EmbeddingJobFinder for the remember-status MCP tool. It resolves each
// requested object's latest embedding-job status (newest row per object, since
// the queue keeps a single row per object that cycles pending→processing→
// completed/failed/dead_letter).
type rememberEmbeddingJobFinder struct {
	jobs *GraphEmbeddingJobsService
}

// FindByObjectIDs returns one EmbeddingJobInfo per requested object ID. Objects
// with no job row get an empty status — nothing is pending for them.
func (f *rememberEmbeddingJobFinder) FindByObjectIDs(ctx context.Context, objectIDs []string) ([]mcp.EmbeddingJobInfo, error) {
	rows, err := f.jobs.FindByObjectIDs(ctx, objectIDs)
	if err != nil {
		return nil, err
	}

	// Keep the newest row per object (query orders created_at DESC).
	byObject := make(map[string]*GraphEmbeddingJob, len(rows))
	for _, job := range rows {
		if job == nil {
			continue
		}
		if _, seen := byObject[job.ObjectID]; seen {
			continue
		}
		byObject[job.ObjectID] = job
	}

	infos := make([]mcp.EmbeddingJobInfo, 0, len(objectIDs))
	for _, oid := range objectIDs {
		info := mcp.EmbeddingJobInfo{ObjectID: oid}
		if job := byObject[oid]; job != nil {
			info.Status = string(job.Status)
			info.LastError = job.LastError
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// NewRememberEmbeddingJobFinder creates the remember-status embedding job
// finder. Provided via fx so main.go can inject it into the agents MCP tool
// handler alongside the extraction job finder.
func NewRememberEmbeddingJobFinder(jobs *GraphEmbeddingJobsService) mcp.EmbeddingJobFinder {
	return &rememberEmbeddingJobFinder{jobs: jobs}
}
