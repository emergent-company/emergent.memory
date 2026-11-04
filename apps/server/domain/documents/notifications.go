package documents

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// DocumentEventNotifier is the slice of the notifications domain the documents
// domain needs to announce lifecycle changes. It is declared here, rather than
// importing the notifications domain, so this package stays usable without it —
// the CLI, the dev MCP and unit tests all build a Service with no notifier.
//
// It is satisfied in production by an adapter over *notifications.Service
// (cmd/server), which is the only place the two domains are joined. The
// documents domain owns the wording because it knows what changed; the adapter
// owns delivery.
type DocumentEventNotifier interface {
	// NotifyDocumentEvent delivers one lifecycle event to the project's members,
	// excluding notice.ActorUserID when set.
	NotifyDocumentEvent(ctx context.Context, projectID string, notice DocumentEventNotice) error
}

// DocumentEventNotice is the payload of a lifecycle announcement. It is plain
// data on purpose: the documents domain must not depend on the notifications
// domain's input type, and the payload must stay JSON-serialisable for the
// notification's details column.
type DocumentEventNotice struct {
	// EventKey is one of the taxonomy.EventDocument* constants. The taxonomy
	// supplies the scope, category and importance class.
	EventKey string
	Title    string
	Message  string
	// Importance and Severity default to "other" and "info" when empty.
	Importance string
	Severity   string

	DocumentID string
	// Version is the document version the event refers to; 0 omits it.
	Version int

	ActionURL   string
	ActionLabel string

	// ActorUserID is the user whose action produced the event. The fan-out skips
	// them entirely — a member is never notified about something they just did.
	// It is empty for events with no actor (a scheduled source sync).
	ActorUserID string
	// ActorName is the actor's display name, already resolved by the producer so
	// the message can name who acted. Empty when unknown.
	ActorName string

	// GroupKey coalesces repeated emissions of the same event.
	GroupKey string
	// Details holds additional structured fields (counts, filenames).
	Details map[string]any
}

// UploadGroupKey is the coalescing key for the upload event of one document. A
// repeat emission for the same document collapses onto the existing row rather
// than stacking a second one.
func UploadGroupKey(documentID string) string {
	return "document.uploaded:" + documentID
}

// VersionSyncedGroupKey is the coalescing key for a document's version-sync
// event.
func VersionSyncedGroupKey(documentID string) string {
	return "document.version_synced:" + documentID
}

// uploadNotice builds the announcement for a freshly created document. actorID
// is the uploader (excluded from delivery) and actorName the already-resolved
// display name, which adds "by <name>" to the message.
func uploadNotice(documentID, filename, actorID, actorName string) DocumentEventNotice {
	message := filename
	if actorName != "" {
		message = fmt.Sprintf("%s by %s", filename, actorName)
	}
	return DocumentEventNotice{
		EventKey:    taxonomy.EventDocumentUploaded,
		Title:       "New document uploaded",
		Message:     message,
		DocumentID:  documentID,
		Version:     1,
		ActionURL:   "/documents/" + documentID,
		ActionLabel: "Open document",
		ActorUserID: actorID,
		ActorName:   actorName,
		GroupKey:    UploadGroupKey(documentID),
		Details:     map[string]any{"filename": filename},
	}
}

// versionSyncedNotice builds the announcement for a new version appended by a
// source sync. There is no actor: the sync is scheduled, not performed by a
// member.
func versionSyncedNotice(documentID, filename string, version int, sourceID string) DocumentEventNotice {
	return DocumentEventNotice{
		EventKey:    taxonomy.EventDocumentVersionSynced,
		Title:       "New document version",
		Message:     fmt.Sprintf("%s was updated to version %d", filename, version),
		DocumentID:  documentID,
		Version:     version,
		ActionURL:   "/documents/" + documentID + "/versions",
		ActionLabel: "View versions",
		GroupKey:    VersionSyncedGroupKey(documentID),
		Details:     map[string]any{"filename": filename, "source_id": sourceID},
	}
}

// WithDocumentEventNotifier installs the lifecycle notifier. A nil notifier
// disables announcements, which is the state of every service built outside a
// fully wired container.
func (s *Service) WithDocumentEventNotifier(n DocumentEventNotifier) *Service {
	s.notifier = n
	return s
}

// notifyDocumentEvent is the best-effort emit every producer funnels through.
// A notification failure must never fail the document operation that caused it:
// the upload or extraction already succeeded, and losing its announcement is
// strictly better than returning an error to a user whose work landed.
func (s *Service) notifyDocumentEvent(ctx context.Context, projectID string, notice DocumentEventNotice) {
	if s.notifier == nil || projectID == "" {
		return
	}
	if err := s.notifier.NotifyDocumentEvent(ctx, projectID, notice); err != nil {
		s.log.Warn("failed to announce document event",
			logger.Error(err),
			slog.String("event_key", notice.EventKey),
			slog.String("document_id", notice.DocumentID),
			slog.String("project_id", projectID))
	}
}

// actorName resolves a user's display name for a notification message. An
// unknown or lookup failure yields "", which the message renders by omitting the
// attribution rather than the whole notification.
func (s *Service) actorName(ctx context.Context, userID string) string {
	if userID == "" {
		return ""
	}
	name, err := s.repo.UserDisplayName(ctx, userID)
	if err != nil {
		s.log.Debug("failed to resolve actor display name",
			logger.Error(err), slog.String("user_id", userID))
		return ""
	}
	return name
}

// notifyBatchUpload emits one digest row for a batch upload instead of one row
// per file. The batch endpoint processes files concurrently, so per-file
// announcements would arrive unordered and bury everything else in the inbox;
// the digest names the count and links to the document list. Duplicates and
// failures are reported in the same row because the uploader's request
// contained them.
func (s *Service) notifyBatchUpload(ctx context.Context, projectID, actorID string, uploaded []string, duplicates, failed int) {
	if len(uploaded) == 0 {
		return
	}

	shown := uploaded
	const shownMax = 3
	suffix := ""
	if len(shown) > shownMax {
		suffix = fmt.Sprintf(" and %d more", len(shown)-shownMax)
		shown = shown[:shownMax]
	}
	name := s.actorName(ctx, actorID)
	message := strings.Join(shown, ", ") + suffix
	if name != "" {
		message = fmt.Sprintf("%s by %s", message, name)
	}
	extra := make([]string, 0, 2)
	if duplicates > 0 {
		extra = append(extra, fmt.Sprintf("%d duplicate", duplicates))
	}
	if failed > 0 {
		extra = append(extra, fmt.Sprintf("%d failed", failed))
	}
	if len(extra) > 0 {
		message = fmt.Sprintf("%s (%s)", message, strings.Join(extra, ", "))
	}

	title := fmt.Sprintf("%d documents uploaded", len(uploaded))
	if len(uploaded) == 1 {
		title = "1 document uploaded"
	}

	s.notifyDocumentEvent(ctx, projectID, DocumentEventNotice{
		EventKey:    taxonomy.EventDocumentUploaded,
		Title:       title,
		Message:     message,
		ActionURL:   "/documents",
		ActionLabel: "View documents",
		ActorUserID: actorID,
		ActorName:   name,
		Details: map[string]any{
			"count":      len(uploaded),
			"filenames":  shown,
			"duplicates": duplicates,
			"failed":     failed,
		},
	})
}
