package main

import (
	"context"
	"encoding/json"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
	"github.com/emergent-company/emergent.memory/domain/sources"
)

// dataSourceResourceType is the related-resource type a source's notification
// points at. It is also the value ResolveResource matches on, so the publish and
// resolve sides agree on one string.
const dataSourceResourceType = "data_source"

// sourceNotificationPublisher adapts the notifications service to the slice of
// it the sources domain needs. It lives here because cmd/server is the only
// place the two domains meet, which is what lets the sources domain declare its
// own interface and stay usable without notifications (CLI, dev MCP, tests).
type sourceNotificationPublisher struct {
	svc *notifications.Service
}

func newSourceNotificationPublisher(svc *notifications.Service) sources.NotificationPublisher {
	return &sourceNotificationPublisher{svc: svc}
}

// PublishDataSourceNeedsReconnect leaves each project member with an unresolved,
// actionable item for the parked source. It uses CreateOrReopen rather than
// CreateForProjectMembers: a resolved, snoozed or read item for the same group
// key has to be reopened, not coalesced away (#2414).
func (p *sourceNotificationPublisher) PublishDataSourceNeedsReconnect(ctx context.Context, projectID string, notice sources.DataSourceReconnectNotice) error {
	details, err := json.Marshal(map[string]any{
		"source_id":   notice.SourceID,
		"provider":    notice.Provider,
		"source_name": notice.SourceName,
	})
	if err != nil {
		return err
	}
	sourceType := dataSourceResourceType
	severity := "warning"
	importance := "important"
	return p.svc.CreateOrReopenForProjectMembers(ctx, projectID, notifications.CreateInput{
		EventKey:            taxonomy.EventDataSourceReconnectRequired,
		Title:               notice.Title,
		Message:             notice.Message,
		Severity:            severity,
		Importance:          importance,
		SourceType:          &sourceType,
		SourceID:            &notice.SourceID,
		RelatedResourceType: &sourceType,
		RelatedResourceID:   &notice.SourceID,
		ActionURL:           &notice.ActionURL,
		ActionLabel:         &notice.ActionLabel,
		RequiresAction:      true,
		GroupKey:            &notice.GroupKey,
		Details:             details,
	})
}

// ResolveDataSource clears the source's actionable item. ResolveResource matches
// on (project, related resource type, related resource id), which is why the
// publish side names the source as both its source and its related resource.
func (p *sourceNotificationPublisher) ResolveDataSource(ctx context.Context, projectID, sourceID string) error {
	_, err := p.svc.ResolveResource(ctx, projectID, dataSourceResourceType, sourceID)
	return err
}
