package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
)

// fakeProjectNotifier records the fan-outs a producer performs, so the lifecycle
// payloads can be asserted without a database or the notifications domain.
type fakeProjectNotifier struct {
	calls []fanoutCall
	err   error
}

type fanoutCall struct {
	projectID string
	input     notifications.CreateInput
	opts      []notifications.FanoutOptions
}

func (f *fakeProjectNotifier) CreateForProjectMembers(_ context.Context, projectID string, in notifications.CreateInput, opts ...notifications.FanoutOptions) error {
	f.calls = append(f.calls, fanoutCall{projectID: projectID, input: in, opts: opts})
	return f.err
}

func testWorker(n projectNotifier) *ObjectExtractionWorker {
	return &ObjectExtractionWorker{
		log:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		notificationsSvc: n,
	}
}

func decodeDetails(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	require.NotEmpty(t, raw)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// --- Failure notification ---

// TestNotifyProcessingFailed_DeadLetterPayload pins the failure row: required
// delivery, error severity, important, deep link, job-scoped coalescing.
func TestNotifyProcessingFailed_DeadLetterPayload(t *testing.T) {
	f := &fakeProjectNotifier{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	notifyProcessingFailed(context.Background(), f, log,
		"prj", "doc-1", "job-1", "document_parsing_job", stageParsing, "broken.pdf", "xberg: unsupported encoding")

	require.Len(t, f.calls, 1)
	call := f.calls[0]
	require.Equal(t, "prj", call.projectID)
	require.Equal(t, taxonomy.EventDocumentProcessingFailed, call.input.EventKey)
	require.Equal(t, "Document processing failed", call.input.Title)
	require.Equal(t, "broken.pdf · Text extraction failed: xberg: unsupported encoding", call.input.Message)
	require.Equal(t, "error", call.input.Severity)
	require.Equal(t, "important", call.input.Importance)
	require.NotNil(t, call.input.ActionURL)
	require.Equal(t, "/documents/doc-1", *call.input.ActionURL)
	require.NotNil(t, call.input.GroupKey)
	require.Equal(t, "document.processing_failed:job-1", *call.input.GroupKey)
	// Failures are never actor-excluded: no FanoutOptions is passed at all.
	require.Empty(t, call.opts, "the member whose run failed must still be notified")

	details := decodeDetails(t, call.input.Details)
	require.Equal(t, "job-1", details["job_id"])
	require.Equal(t, "doc-1", details["document_id"])
	require.Equal(t, stageParsing, details["stage"])
}

// TestNotifyProcessingFailed_TruncatesLongCause keeps a stack-trace-sized error
// out of the inbox row while leaving the beginning (which names the operation).
func TestNotifyProcessingFailed_TruncatesLongCause(t *testing.T) {
	f := &fakeProjectNotifier{}
	long := ""
	for i := 0; i < 40; i++ {
		long += "a very long failure description "
	}
	notifyProcessingFailed(context.Background(), f, slog.New(slog.NewTextHandler(io.Discard, nil)),
		"prj", "doc-1", "job-1", "object_extraction_job", stageExtraction, "big.pdf", long)

	require.Len(t, f.calls, 1)
	msg := f.calls[0].input.Message
	require.Contains(t, msg, "…")
	require.Less(t, len(msg), len(long))
	// The untruncated cause still travels in details.
	require.Equal(t, long, decodeDetails(t, f.calls[0].input.Details)["error"])
}

func TestNotifyProcessingFailed_NilServiceIsSilent(t *testing.T) {
	require.NotPanics(t, func() {
		notifyProcessingFailed(context.Background(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
			"prj", "doc-1", "job-1", "document_parsing_job", stageParsing, "a.pdf", "boom")
	})
}

func TestEmitProcessingFailedNotification_NoDocumentNamesTheJob(t *testing.T) {
	f := &fakeProjectNotifier{}
	w := testWorker(f)

	w.emitProcessingFailedNotification(context.Background(), &ObjectExtractionJob{
		ID:        "0123456789abcdef",
		ProjectID: "prj",
	}, errors.New("model refused"))

	require.Len(t, f.calls, 1)
	require.Contains(t, f.calls[0].input.Message, "Document 01234567")
	require.Nil(t, f.calls[0].input.ActionURL, "no document to link to")
}

// --- Completion notification ---

func TestEmitExtractionCompleted_ActorIsExcluded(t *testing.T) {
	f := &fakeProjectNotifier{}
	w := testWorker(f)
	actor := "user-a"

	w.emitExtractionCompletedNotification(context.Background(), &ObjectExtractionJob{
		ID:         "job-1",
		ProjectID:  "prj",
		CreatedBy:  &actor,
		DocumentID: func() *string { s := "doc-1"; return &s }(),
	}, &ObjectExtractionResults{ObjectsCreated: 3, RelationshipsCreated: 2})

	require.Len(t, f.calls, 1)
	call := f.calls[0]
	require.Equal(t, taxonomy.EventDocumentExtractionCompleted, call.input.EventKey)
	require.Equal(t, "3 objects and 2 relationships added", call.input.Message)
	require.Equal(t, "info", call.input.Severity)
	require.Equal(t, "other", call.input.Importance)
	require.NotNil(t, call.input.ActionURL)
	require.Equal(t, "/documents/doc-1/extractions", *call.input.ActionURL)
	require.Len(t, call.opts, 1)
	require.Equal(t, "user-a", call.opts[0].ActorUserID, "the triggerer already knows")
	require.Equal(t, "job-1", decodeDetails(t, call.input.Details)["job_id"])
}

func TestEmitExtractionCompleted_NoActorNotifiesEveryone(t *testing.T) {
	f := &fakeProjectNotifier{}
	w := testWorker(f)

	w.emitExtractionCompletedNotification(context.Background(), &ObjectExtractionJob{
		ID:        "job-1",
		ProjectID: "prj",
	}, &ObjectExtractionResults{ObjectsCreated: 1})

	require.Len(t, f.calls, 1)
	require.Empty(t, f.calls[0].opts)
}

// TestEmitExtractionCompleted_EmptyRunIsSilent guards the inbox against a row
// that reports nothing changed.
func TestEmitExtractionCompleted_EmptyRunIsSilent(t *testing.T) {
	f := &fakeProjectNotifier{}
	testWorker(f).emitExtractionCompletedNotification(context.Background(), &ObjectExtractionJob{
		ID:        "job-1",
		ProjectID: "prj",
	}, &ObjectExtractionResults{})

	require.Empty(t, f.calls)
}

func TestEmitExtractionCompleted_NilServiceIsSilent(t *testing.T) {
	require.NotPanics(t, func() {
		testWorker(nil).emitExtractionCompletedNotification(context.Background(), &ObjectExtractionJob{
			ID: "job-1", ProjectID: "prj",
		}, &ObjectExtractionResults{ObjectsCreated: 1})
	})
}

// TestEmitReviewReady_RequiresABranch pins the "exactly one notification"
// contract from the other side: without a staging branch the review producer
// emits nothing, which is what lets the completion producer run instead.
func TestEmitReviewReady_RequiresABranch(t *testing.T) {
	f := &fakeProjectNotifier{}
	w := testWorker(f)

	emitted := w.emitReviewReadyNotification(context.Background(), &ObjectExtractionJob{
		ID: "job-1", ProjectID: "prj",
	}, nil, &ObjectExtractionResults{ObjectsCreated: 3})

	require.False(t, emitted)
	require.Empty(t, f.calls)
}

// TestEmitExtractionOutcome_OneRowPerRun is the "one notification per outcome"
// contract: an open staging branch suppresses the completion row, and a run with
// nothing left to review emits it. A regression that emitted both would fail
// here with two calls.
func TestEmitExtractionOutcome_OneRowPerRun(t *testing.T) {
	job := &ObjectExtractionJob{ID: "job-1", ProjectID: "prj"}
	result := &ObjectExtractionResults{ObjectsCreated: 3, RelationshipsCreated: 2}

	branch := uuid.New()
	review := &fakeProjectNotifier{}
	testWorker(review).emitExtractionOutcome(context.Background(), job, &branch, result)
	require.Len(t, review.calls, 1, "a review-branch run must emit exactly one row")
	require.Equal(t, "extraction.review_ready", review.calls[0].input.EventKey)

	merged := &fakeProjectNotifier{}
	testWorker(merged).emitExtractionOutcome(context.Background(), job, nil, result)
	require.Len(t, merged.calls, 1, "a merged run must emit exactly one row")
	require.Equal(t, taxonomy.EventDocumentExtractionCompleted, merged.calls[0].input.EventKey)
}
