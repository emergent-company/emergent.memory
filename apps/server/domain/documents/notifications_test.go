package documents

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
)

// fakeNotifier records the lifecycle notices a producer emits. It is the seam
// that lets these tests assert the payload without a database or the real
// notifications domain.
type fakeNotifier struct {
	notices  []DocumentEventNotice
	projects []string
	err      error
}

func (f *fakeNotifier) NotifyDocumentEvent(_ context.Context, projectID string, notice DocumentEventNotice) error {
	f.projects = append(f.projects, projectID)
	f.notices = append(f.notices, notice)
	return f.err
}

func testService(n DocumentEventNotifier) *Service {
	svc := NewService(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc.WithDocumentEventNotifier(n)
}

// --- Notice builders ---

func TestUploadNotice(t *testing.T) {
	n := uploadNotice("doc-1", "spec.pdf", "user-a", "Ada Lovelace")

	require.Equal(t, taxonomy.EventDocumentUploaded, n.EventKey)
	require.Equal(t, "New document uploaded", n.Title)
	require.Equal(t, "spec.pdf by Ada Lovelace", n.Message)
	require.Equal(t, "doc-1", n.DocumentID)
	require.Equal(t, 1, n.Version)
	require.Equal(t, "/documents/doc-1", n.ActionURL)
	require.Equal(t, "Open document", n.ActionLabel)
	// The uploader is the actor, so the fan-out excludes them.
	require.Equal(t, "user-a", n.ActorUserID)
	require.Equal(t, "Ada Lovelace", n.ActorName)
	require.Equal(t, "document.uploaded:doc-1", n.GroupKey)
	require.Equal(t, "spec.pdf", n.Details["filename"])
}

func TestUploadNotice_OmitsUnknownActorName(t *testing.T) {
	n := uploadNotice("doc-1", "spec.pdf", "user-a", "")
	require.Equal(t, "spec.pdf", n.Message, "an unresolvable name must not leave a dangling 'by'")
	require.Equal(t, "user-a", n.ActorUserID, "the id still drives actor exclusion")
}

func TestVersionSyncedNotice(t *testing.T) {
	n := versionSyncedNotice("doc-2", "notes.md", 4, "src-9")

	require.Equal(t, taxonomy.EventDocumentVersionSynced, n.EventKey)
	require.Equal(t, "notes.md was updated to version 4", n.Message)
	require.Equal(t, 4, n.Version)
	require.Equal(t, "/documents/doc-2/versions", n.ActionURL)
	require.Empty(t, n.ActorUserID, "a scheduled sync has no actor to exclude")
	require.Equal(t, "document.version_synced:doc-2", n.GroupKey)
	require.Equal(t, "src-9", n.Details["source_id"])
}

func TestDocumentGroupKeys(t *testing.T) {
	require.Equal(t, "document.uploaded:abc", UploadGroupKey("abc"))
	require.Equal(t, "document.version_synced:abc", VersionSyncedGroupKey("abc"))
}

// --- Emit seam ---

func TestNotifyDocumentEvent_DeliversToTheNotifier(t *testing.T) {
	f := &fakeNotifier{}
	testService(f).notifyDocumentEvent(context.Background(), "prj", uploadNotice("doc-1", "a.pdf", "user-a", ""))

	require.Len(t, f.notices, 1)
	require.Equal(t, []string{"prj"}, f.projects)
}

// TestNotifyDocumentEvent_NoNotifierIsSilent guards the many services built
// outside a fully wired container (CLI, dev MCP, tests): no notifier means no
// announcement and no panic.
func TestNotifyDocumentEvent_NoNotifierIsSilent(t *testing.T) {
	svc := testService(nil)
	require.NotPanics(t, func() {
		svc.notifyDocumentEvent(context.Background(), "prj", uploadNotice("doc-1", "a.pdf", "", ""))
	})
}

func TestNotifyDocumentEvent_NoProjectIsSilent(t *testing.T) {
	f := &fakeNotifier{}
	testService(f).notifyDocumentEvent(context.Background(), "", uploadNotice("doc-1", "a.pdf", "", ""))
	require.Empty(t, f.notices)
}

// TestNotifyDocumentEvent_SwallowsNotifierError is the contract that a
// notification failure never fails the document operation that caused it.
func TestNotifyDocumentEvent_SwallowsNotifierError(t *testing.T) {
	f := &fakeNotifier{err: errors.New("memory unavailable")}
	require.NotPanics(t, func() {
		testService(f).notifyDocumentEvent(context.Background(), "prj", uploadNotice("doc-1", "a.pdf", "", ""))
	})
	require.Len(t, f.notices, 1)
}

// --- Batch digest ---

func TestNotifyBatchUpload_SendsOneDigestForTheWholeBatch(t *testing.T) {
	f := &fakeNotifier{}
	testService(f).notifyBatchUpload(context.Background(), "prj", "", []string{"a.pdf", "b.pdf", "c.pdf", "d.pdf", "e.pdf"}, 2, 1)

	require.Len(t, f.notices, 1, "a batch must produce one row, not one per file")
	n := f.notices[0]
	require.Equal(t, taxonomy.EventDocumentUploaded, n.EventKey)
	require.Equal(t, "5 documents uploaded", n.Title)
	require.Equal(t, "a.pdf, b.pdf, c.pdf and 2 more (2 duplicate, 1 failed)", n.Message)
	require.Equal(t, "/documents", n.ActionURL)
	require.Empty(t, n.GroupKey, "separate batches are separate events")
	require.Equal(t, 5, n.Details["count"])
	require.Equal(t, 2, n.Details["duplicates"])
	require.Equal(t, 1, n.Details["failed"])
	require.Len(t, n.Details["filenames"], 3, "the message names three files at most")
}

func TestNotifyBatchUpload_SingleFileTitle(t *testing.T) {
	f := &fakeNotifier{}
	testService(f).notifyBatchUpload(context.Background(), "prj", "", []string{"only.pdf"}, 0, 0)

	require.Len(t, f.notices, 1)
	require.Equal(t, "1 document uploaded", f.notices[0].Title)
	require.Equal(t, "only.pdf", f.notices[0].Message)
}

func TestNotifyBatchUpload_NothingUploadedIsSilent(t *testing.T) {
	f := &fakeNotifier{}
	testService(f).notifyBatchUpload(context.Background(), "prj", "", nil, 3, 2)
	require.Empty(t, f.notices, "an all-duplicate/failed batch changed nothing worth announcing")
}
