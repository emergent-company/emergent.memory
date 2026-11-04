package extraction

import "github.com/emergent-company/emergent.memory/domain/processingproblems"

// retryCandidate is one terminal failure a bulk retry may re-run: the job id to
// reset and the recorded error the classifier reads.
type retryCandidate struct {
	ID        string  `bun:"id"`
	LastError *string `bun:"last_error"`
}

// filterRetryable keeps only candidates whose recorded failure is classified as
// retryable. Bulk actions use it so a mixed queue does not re-drive failures
// that the per-problem retry button deliberately refuses (missing model,
// unreadable input, permission and conflict classes). A nil error keeps its
// queue's empty-message classification, matching the per-problem path.
func filterRetryable(queue string, candidates []retryCandidate) []retryCandidate {
	kept := make([]retryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		message := ""
		if candidate.LastError != nil {
			message = *candidate.LastError
		}
		if processingproblems.Classify(queue, message).Retryable {
			kept = append(kept, candidate)
		}
	}
	return kept
}

// retryableObjectExtraction is filterRetryable for the extraction loop, which
// already has the job row instead of a retryCandidate.
func retryableObjectExtraction(job *ObjectExtractionJob) bool {
	message := ""
	if job.ErrorMessage != nil {
		message = *job.ErrorMessage
	}
	return processingproblems.Classify(processingproblems.QueueObjectExtraction, message).Retryable
}
