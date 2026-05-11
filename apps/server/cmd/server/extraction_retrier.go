package main

import (
	"context"
	"fmt"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/extraction"
)

// extractionRetrierAdapter adapts ObjectExtractionJobsService to the documents.ExtractionRetrier interface
type extractionRetrierAdapter struct {
	svc *extraction.ObjectExtractionJobsService
}

// NewExtractionRetrierAdapter creates an adapter that satisfies documents.ExtractionRetrier
func NewExtractionRetrierAdapter(svc *extraction.ObjectExtractionJobsService) documents.ExtractionRetrier {
	return &extractionRetrierAdapter{svc: svc}
}

// FindRetryableByDocument finds the most recent failed/dead_letter/processing job for a document
func (a *extractionRetrierAdapter) FindRetryableByDocument(ctx context.Context, documentID, projectID string) (*documents.RetryableJob, error) {
	jobs, err := a.svc.FindByDocument(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("find by document: %w", err)
	}
	for _, j := range jobs {
		if j.ProjectID == projectID &&
			(j.Status == extraction.JobStatusFailed ||
				j.Status == extraction.JobStatusDeadLetter ||
				j.Status == extraction.JobStatusProcessing) {
			return &documents.RetryableJob{
				ID:        j.ID,
				ProjectID: j.ProjectID,
				Status:    string(j.Status),
			}, nil
		}
	}
	return nil, nil
}

// RetryByJobID retries an extraction job by its ID
func (a *extractionRetrierAdapter) RetryByJobID(ctx context.Context, jobID, projectID string) (*documents.RetryableJob, error) {
	job, err := a.svc.RetryJob(ctx, jobID, projectID)
	if err != nil {
		return nil, err
	}
	return &documents.RetryableJob{
		ID:        job.ID,
		ProjectID: job.ProjectID,
		Status:    string(job.Status),
	}, nil
}
