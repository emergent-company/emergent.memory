package main

import "context"

// --- fakeMemory processing-problem methods ---

func (f *fakeMemory) GetProcessingProblems(context.Context) (*ProcessingProblemsReport, error) {
	return f.problems, f.problemsErr
}

func (f *fakeMemory) RetryExtractionJob(_ context.Context, jobID string) error {
	if f.retryJobErr != nil {
		return f.retryJobErr
	}
	f.retriedJobIDs = append(f.retriedJobIDs, jobID)
	return nil
}

func (f *fakeMemory) ListDiscoveryJobs(context.Context) ([]DiscoveryJob, error) {
	return f.discoveryJobs, f.discoveryJobsErr
}

func (f *fakeMemory) RetryDiscoveryJob(_ context.Context, jobID string) (string, error) {
	if f.retryDiscoveryErr != nil {
		return "", f.retryDiscoveryErr
	}
	f.retriedDiscoveryIDs = append(f.retriedDiscoveryIDs, jobID)
	return "new-" + jobID, nil
}

func (f *fakeMemory) RetryParsingJob(_ context.Context, jobID string) error {
	if f.retryJobErr != nil {
		return f.retryJobErr
	}
	f.retriedParsingJobIDs = append(f.retriedParsingJobIDs, jobID)
	return nil
}

func (f *fakeMemory) RetryAllExtractionJobs(_ context.Context, includeNonRetryable bool) (int, error) {
	f.bulkIncludeNonRetryable = append(f.bulkIncludeNonRetryable, includeNonRetryable)
	return f.bulkRetried, nil
}

func (f *fakeMemory) RetryAllParsingJobs(_ context.Context, includeNonRetryable bool) (int, error) {
	f.bulkIncludeNonRetryable = append(f.bulkIncludeNonRetryable, includeNonRetryable)
	return f.bulkRetried, nil
}

func (f *fakeMemory) RetriggerEmbeddings(_ context.Context, includeNonRetryable bool) (int, error) {
	f.embeddingIncludeNonRetryable = append(f.embeddingIncludeNonRetryable, includeNonRetryable)
	return f.embeddingsRetriggered, nil
}
