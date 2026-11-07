package extraction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// projectNotifier is the slice of the notifications service the lifecycle
// producers use. It exists so they can be unit-tested with a fake, and so the
// workers do not grow a dependency on the concrete service beyond what they
// call. *notifications.Service satisfies it.
type projectNotifier interface {
	CreateForProjectMembers(ctx context.Context, projectID string, in notifications.CreateInput, opts ...notifications.FanoutOptions) error
	CreateOrReopenForProjectMembers(ctx context.Context, projectID string, in notifications.CreateInput) error
}

// Processing stages named in a failure notification, so a member reading the
// row knows whether the file could not be read or the extraction itself failed.
const (
	stageParsing    = "Text extraction"
	stageExtraction = "Extraction"
)

// failureCauseMax caps the error text embedded in a notification message. The
// full error stays in the job's error_message; the row needs enough to be
// actionable without pasting a stack trace into the inbox.
const failureCauseMax = 240

// notifyProcessingFailed announces that an upload's processing reached dead
// letter. It is deliberately NOT actor-excluded: the person whose run failed is
// the one who most needs to hear about it. Coalescing is by job id and the
// write reopens an existing active row: a retry keeps the same job id (see
// DocumentParsingJobsService.RetryJob), so a second failure must refresh the
// row's message/details to the current error and mark it unread instead of
// being swallowed by the first failure's group key (#13).
func notifyProcessingFailed(
	ctx context.Context,
	svc projectNotifier,
	log *slog.Logger,
	projectID, documentID, jobID, sourceType, stage, filename, cause string,
) {
	if svc == nil || projectID == "" {
		return
	}
	if filename == "" {
		filename = "A document"
	}

	detail := map[string]any{
		"job_id":  jobID,
		"stage":   stage,
		"error":   cause,
		"project": projectID,
	}
	if documentID != "" {
		detail["document_id"] = documentID
	}
	details, err := json.Marshal(detail)
	if err != nil {
		log.Warn("failed to marshal processing-failure details",
			slog.String("job_id", jobID),
			logger.Error(err))
		details = nil
	}

	groupKey := "document.processing_failed:" + jobID
	in := notifications.CreateInput{
		EventKey:   taxonomy.EventDocumentProcessingFailed,
		Title:      "Document processing failed",
		Message:    fmt.Sprintf("%s · %s failed: %s", filename, stage, truncateCause(cause)),
		Severity:   "error",
		Importance: "important",
		SourceType: &sourceType,
		SourceID:   &jobID,
		GroupKey:   &groupKey,
		Details:    details,
	}
	if documentID != "" {
		actionURL := "/documents/" + documentID
		actionLabel := "Open document"
		relatedType := "document"
		in.ActionURL = &actionURL
		in.ActionLabel = &actionLabel
		in.RelatedResourceType = &relatedType
		in.RelatedResourceID = &documentID
	}

	// No FanoutOptions: a failure is never hidden from the member who caused it.
	// Reopen rather than create: RetryJob reuses the job id, so a second failure
	// must refresh this row (and return it to unread) instead of being coalesced
	// away by the still-active first failure (#13).
	if err := svc.CreateOrReopenForProjectMembers(ctx, projectID, in); err != nil {
		log.Warn("failed to emit processing-failure notification",
			slog.String("job_id", jobID),
			slog.String("document_id", documentID),
			logger.Error(err))
	}
}

// truncateCause shortens an error for a one-line message, keeping the beginning
// (which carries the operation that failed) and dropping the tail.
func truncateCause(cause string) string {
	cause = strings.TrimSpace(strings.ReplaceAll(cause, "\n", " "))
	if len(cause) <= failureCauseMax {
		return cause
	}
	return cause[:failureCauseMax] + "…"
}

// emitExtractionOutcome emits exactly one notification per finished run: a run
// that left a staging branch open announces the review (a human must merge it),
// and a run whose objects landed on the graph announces completion. Emitting both
// would tell a member the same run is both finished and awaiting them.
func (w *ObjectExtractionWorker) emitExtractionOutcome(ctx context.Context, job *ObjectExtractionJob, stagingBranchID *uuid.UUID, result *ObjectExtractionResults) {
	if !w.emitReviewReadyNotification(ctx, job, stagingBranchID, result) {
		w.emitExtractionCompletedNotification(ctx, job, result)
	}
}

// emitExtractionCompletedNotification announces that extraction finished and its
// objects were merged, so the knowledge base changed. Project members other than
// the actor are notified; the actor already knows, having triggered the run.
//
// A run that left a staging branch for review does NOT emit this — it emits
// extraction.review_ready instead, so one extraction produces exactly one row.
func (w *ObjectExtractionWorker) emitExtractionCompletedNotification(ctx context.Context, job *ObjectExtractionJob, result *ObjectExtractionResults) {
	if w.notificationsSvc == nil || job == nil || result == nil {
		return
	}

	filename := ""
	documentVersion := 0
	actorID := ""
	if job.CreatedBy != nil {
		actorID = *job.CreatedBy
	}
	var actionURL string
	if job.DocumentID != nil {
		actionURL = fmt.Sprintf("/documents/%s/extractions", *job.DocumentID)
		if w.docService != nil {
			if doc, err := w.docService.GetByID(ctx, job.ProjectID, *job.DocumentID); err == nil && doc != nil {
				if doc.Filename != nil {
					filename = *doc.Filename
				}
				documentVersion = doc.Version
				// A job triggered by a connector or the API has no created_by
				// (TriggerForDocument leaves it NULL); fall back to whoever owns
				// the document so the uploader is still excluded.
				if actorID == "" && doc.CreatedByUserID != nil {
					actorID = *doc.CreatedByUserID
				}
			}
		}
	}

	message := fmt.Sprintf("%d objects and %d relationships added", result.ObjectsCreated, result.RelationshipsCreated)
	if filename != "" {
		message = fmt.Sprintf("%s · %d objects, %d relationships", filename, result.ObjectsCreated, result.RelationshipsCreated)
	}
	if result.ObjectsCreated == 0 && result.RelationshipsCreated == 0 {
		// Nothing changed in the graph; announcing an empty run would be noise.
		return
	}

	detail := map[string]any{
		"job_id":                job.ID,
		"objects_created":       result.ObjectsCreated,
		"relationships_created": result.RelationshipsCreated,
		"document_version":      documentVersion,
	}
	var groupKey string
	if job.DocumentID != nil {
		detail["document_id"] = *job.DocumentID
		groupKey = fmt.Sprintf("document.extraction_completed:%s:%d", *job.DocumentID, documentVersion)
	}
	severity := "info"
	message, severity = withVarianceWarning(message, severity, detail, result.VarianceWarning)
	details, err := json.Marshal(detail)
	if err != nil {
		w.log.Warn("failed to marshal extraction-completed details",
			slog.String("job_id", job.ID),
			logger.Error(err))
		details = nil
	}

	sourceType := "extraction_job"
	in := notifications.CreateInput{
		EventKey:   taxonomy.EventDocumentExtractionCompleted,
		Title:      "Document extraction finished",
		Message:    message,
		Severity:   severity,
		Importance: "other",
		SourceType: &sourceType,
		SourceID:   &job.ID,
		Details:    details,
	}
	if groupKey != "" {
		in.GroupKey = &groupKey
	}
	if actionURL != "" {
		actionLabel := "View extractions"
		relatedType := "document"
		in.ActionURL = &actionURL
		in.ActionLabel = &actionLabel
		in.RelatedResourceType = &relatedType
		in.RelatedResourceID = job.DocumentID
	}

	var opts []notifications.FanoutOptions
	if actorID != "" && result.VarianceWarning == nil {
		// An ordinary completion is informational and the actor already knows.
		// A variance warning is exactly what the person who re-ran the document
		// needs to see before merging, so it is delivered to them too.
		opts = append(opts, notifications.FanoutOptions{ActorUserID: actorID})
	}
	if err := w.notificationsSvc.CreateForProjectMembers(ctx, job.ProjectID, in, opts...); err != nil {
		w.log.Warn("failed to emit extraction-completed notification",
			slog.String("job_id", job.ID),
			logger.Error(err))
	}
}

// emitProcessingFailedNotification announces a dead-lettered extraction run. It
// resolves the document so the message names the file; a job without a document
// (manual/chunk job) still notifies, with the job id as its subject.
func (w *ObjectExtractionWorker) emitProcessingFailedNotification(ctx context.Context, job *ObjectExtractionJob, cause error) {
	if w.notificationsSvc == nil || job == nil {
		return
	}

	documentID := ""
	filename := ""
	if job.DocumentID != nil {
		documentID = *job.DocumentID
		if w.docService != nil {
			if doc, err := w.docService.GetByID(ctx, job.ProjectID, documentID); err == nil && doc != nil && doc.Filename != nil {
				filename = *doc.Filename
			}
		}
	}
	if filename == "" {
		filename = "Document " + shortID(job.ID)
	}

	notifyProcessingFailed(ctx, w.notificationsSvc, w.log,
		job.ProjectID, documentID, job.ID, "object_extraction_job", stageExtraction, filename, cause.Error())
}

// shortID keeps an id readable in a notification message without pasting a full
// UUID into the inbox.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// withVarianceWarning folds a run-to-run variance warning into an outcome
// notification: the message names both counts, the severity escalates to
// "warning" so the inbox row is not another neutral "finished" note, and the
// numbers travel structurally in details. With no warning the inputs are
// unchanged, so callers keep one code path.
func withVarianceWarning(message, severity string, detail map[string]any, warning *RunVarianceWarning) (string, string) {
	if warning == nil {
		return message, severity
	}
	detail["previous_objects"] = warning.PreviousObjects
	detail["variance_ratio"] = warning.Ratio
	detail["variance_direction"] = warning.Direction
	detail["previous_job_id"] = warning.PreviousJobID
	detail["variance_warning"] = warning.Message
	return message + " — " + warning.Message, "warning"
}
