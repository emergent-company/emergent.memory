package sources

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// NotificationPublisher is the slice of the notifications domain the sources
// domain needs. It is declared here, rather than importing the notifications
// domain, so this package stays usable without it — the CLI, the dev MCP and
// unit tests all build a Service with no publisher at all.
//
// It is satisfied in production by an adapter over *notifications.Service
// (cmd/server), which is the only place the two domains are joined.
type NotificationPublisher interface {
	// PublishDataSourceNeedsReconnect leaves each project member with an
	// unresolved actionable notification for a source that has just parked.
	PublishDataSourceNeedsReconnect(ctx context.Context, projectID string, notice DataSourceReconnectNotice) error
	// ResolveDataSource clears the unresolved notification for a source that is
	// no longer parked. It is idempotent.
	ResolveDataSource(ctx context.Context, projectID, sourceID string) error
}

// DataSourceReconnectNotice is the payload of a park announcement. It is plain
// data on purpose: the sources domain must not depend on the notifications
// domain's input type.
type DataSourceReconnectNotice struct {
	SourceID    string
	Provider    string
	SourceName  string
	GroupKey    string
	Title       string
	Message     string
	ActionURL   string
	ActionLabel string
}

// NotificationGroupKey is the coalescing key for a source's reconnect item: one
// unresolved item per source at a time. It is exported so tests and the
// publisher adapter name the key the same way.
func NotificationGroupKey(sourceID string) string {
	return "data_source:" + sourceID
}

// WithNotificationPublisher installs the optional publisher. A nil publisher
// means "do not publish": park and resolve become no-ops, which is how the CLI,
// the dev MCP and unit tests avoid the notifications domain.
func WithNotificationPublisher(p NotificationPublisher) ServiceOption {
	return func(s *Service) {
		s.publisher = p
	}
}

// publishSourceNeedsReconnect announces a committed park. It is best-effort: a
// delivery failure is logged and never fails the sync or the park, because the
// source's remediation does not depend on the member being told (spec:
// "Notification delivery never changes the sync outcome").
func (s *Service) publishSourceNeedsReconnect(ctx context.Context, src *Source, remediation string) {
	if s.publisher == nil || src == nil {
		return
	}
	actionURL := "/data-sources/" + src.ID.String()
	notice := DataSourceReconnectNotice{
		SourceID:    src.ID.String(),
		Provider:    src.Provider,
		SourceName:  src.DisplayName,
		GroupKey:    NotificationGroupKey(src.ID.String()),
		Title:       fmt.Sprintf("%s needs reconnecting", s.sourceNotificationName(src)),
		Message:     remediation,
		ActionURL:   actionURL,
		ActionLabel: "Reconnect",
	}
	if err := s.publisher.PublishDataSourceNeedsReconnect(ctx, src.ProjectID.String(), notice); err != nil {
		s.log.Warn("failed to publish source reconnect notification",
			"source_id", src.ID.String(),
			"error", err)
	}
}

// resolveSourceNotification clears a source's unresolved reconnect item. It is
// best-effort and idempotent, and is called on every path that leaves the
// source not parked — a repair, an enable, a disable, a successful sync, a
// deletion — so a stale item cannot outlive the problem it describes.
func (s *Service) resolveSourceNotification(ctx context.Context, projectID, sourceID uuid.UUID) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher.ResolveDataSource(ctx, projectID.String(), sourceID.String()); err != nil {
		s.log.Warn("failed to resolve source notification",
			"source_id", sourceID.String(),
			"error", err)
	}
}

// sourceNotificationName is the display name a member recognises in an inbox:
// the provider and the source, because both are needed to tell two parked
// sources apart.
func (s *Service) sourceNotificationName(src *Source) string {
	provider := src.Provider
	if s.providers != nil {
		if p, ok := s.providers.Get(src.Provider); ok {
			if name := p.Catalog().Name; name != "" {
				provider = name
			}
		}
	}
	if src.DisplayName == "" {
		return provider + " source"
	}
	return fmt.Sprintf("%s (%s)", provider, src.DisplayName)
}
