package main

import "context"

// --- fakeMemory processing-problem methods ---

func (f *fakeMemory) GetProcessingProblems(context.Context) (*ProcessingProblemsReport, error) {
	return f.problems, f.problemsErr
}

func (f *fakeMemory) RetryExtractionJob(_ context.Context, jobID string) error {
	f.retriedJobIDs = append(f.retriedJobIDs, jobID)
	return nil
}

func (f *fakeMemory) RetryAllExtractionJobs(context.Context) (int, error) {
	return f.bulkRetried, nil
}

func (f *fakeMemory) RetryAllParsingJobs(context.Context) (int, error) {
	return f.bulkRetried, nil
}

func (f *fakeMemory) RetriggerEmbeddings(context.Context) (int, error) {
	return f.embeddingsRetriggered, nil
}
