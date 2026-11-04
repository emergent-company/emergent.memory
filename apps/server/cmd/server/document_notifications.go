package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
)

// documentResourceType is the related-resource type a document notification
// points at, so a future resolve-by-resource can find these rows.
const documentResourceType = "document"

// documentEventNotifier adapts the notifications service to the slice of it the
// documents domain needs. It lives here because cmd/server is the only place the
// two domains meet, which is what lets the documents domain declare its own
// interface and stay usable without notifications (CLI, dev MCP, tests).
type documentEventNotifier struct {
	svc *notifications.Service
}

func newDocumentEventNotifier(svc *notifications.Service) documents.DocumentEventNotifier {
	return &documentEventNotifier{svc: svc}
}

// NotifyDocumentEvent fans one lifecycle event out to the project's members.
// The actor is passed through to the fan-out, which skips them at write time:
// the uploader (or extraction triggerer) never sees a row about their own
// action. Failure events intentionally carry no actor.
func (n *documentEventNotifier) NotifyDocumentEvent(ctx context.Context, projectID string, notice documents.DocumentEventNotice) error {
	if _, ok := taxonomy.Lookup(notice.EventKey); !ok {
		// A producer bug must not take the document operation down with it, and
		// an unknown key would be rejected by Create for every member anyway.
		return fmt.Errorf("unknown document notification event key %q", notice.EventKey)
	}

	details := make(map[string]any, len(notice.Details)+4)
	for k, v := range notice.Details {
		details[k] = v
	}
	details["document_id"] = notice.DocumentID
	if notice.Version > 0 {
		details["document_version"] = notice.Version
	}
	if notice.ActorUserID != "" {
		details["actor_id"] = notice.ActorUserID
	}
	if notice.ActorName != "" {
		details["actor_name"] = notice.ActorName
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal document notification details: %w", err)
	}

	importance := notice.Importance
	if importance == "" {
		importance = "other"
	}
	severity := notice.Severity
	if severity == "" {
		severity = "info"
	}
	resourceType := documentResourceType
	in := notifications.CreateInput{
		EventKey:            notice.EventKey,
		Title:               notice.Title,
		Message:             notice.Message,
		Severity:            severity,
		Importance:          importance,
		RelatedResourceType: &resourceType,
		RelatedResourceID:   &notice.DocumentID,
		Details:             raw,
	}
	if notice.ActionURL != "" {
		actionURL := notice.ActionURL
		in.ActionURL = &actionURL
	}
	if notice.ActionLabel != "" {
		actionLabel := notice.ActionLabel
		in.ActionLabel = &actionLabel
	}
	if notice.GroupKey != "" {
		groupKey := notice.GroupKey
		in.GroupKey = &groupKey
	}

	return n.svc.CreateForProjectMembers(ctx, projectID, in, notifications.FanoutOptions{ActorUserID: notice.ActorUserID})
}
