package main

import (
	"context"
	"io"
	"strconv"
	"strings"
)

// --- fakeMemory notification methods (inbox subsystem) ---
//
// The fake is the MemoryBackend stand-in for handler tests: it records what the
// gateway forwarded and serves canned notification data/errors.

func (f *fakeMemory) ListNotifications(_ context.Context, p NotificationListParams) ([]Notification, error) {
	f.lastListParams = p
	if f.notificationListErr != nil {
		return nil, f.notificationListErr
	}
	return f.notifications, nil
}

func (f *fakeMemory) NotificationCounts(_ context.Context, scope, projectID string) (*NotificationCounts, error) {
	f.lastCountsScope, f.lastCountsProject = scope, projectID
	if f.notificationCountErr != nil {
		return nil, f.notificationCountErr
	}
	if f.notificationCounts != nil {
		return f.notificationCounts, nil
	}
	return &NotificationCounts{}, nil
}

func (f *fakeMemory) ListNotificationPreferences(_ context.Context, projectID string) ([]NotificationPreference, error) {
	f.lastPrefsProject = projectID
	if f.notificationPrefsErr != nil {
		return nil, f.notificationPrefsErr
	}
	return f.notificationPrefs, nil
}

func (f *fakeMemory) SetNotificationPreference(_ context.Context, projectID, eventKey, channel string, enabled bool) error {
	if f.notificationMutErr != nil {
		return f.notificationMutErr
	}
	f.notificationMutations = append(f.notificationMutations,
		"pref:"+projectID+":"+eventKey+":"+channel+":"+strconv.FormatBool(enabled))
	return nil
}

func (f *fakeMemory) MarkNotificationRead(_ context.Context, id string) error {
	return f.recordNotificationMutation("read", id, "")
}

func (f *fakeMemory) MarkNotificationUnread(_ context.Context, id string) error {
	return f.recordNotificationMutation("unread", id, "")
}

func (f *fakeMemory) DismissNotification(_ context.Context, id string) error {
	return f.recordNotificationMutation("dismiss", id, "")
}

func (f *fakeMemory) SnoozeNotification(_ context.Context, id, until string) error {
	return f.recordNotificationMutation("snooze", id, until)
}

func (f *fakeMemory) UnsnoozeNotification(_ context.Context, id string) error {
	return f.recordNotificationMutation("unsnooze", id, "")
}

func (f *fakeMemory) ClearNotification(_ context.Context, id string) error {
	return f.recordNotificationMutation("clear", id, "")
}

func (f *fakeMemory) RestoreNotification(_ context.Context, id string) error {
	return f.recordNotificationMutation("restore", id, "")
}

func (f *fakeMemory) ResolveNotification(_ context.Context, id, action string) error {
	return f.recordNotificationMutation("resolve", id, action)
}

func (f *fakeMemory) MarkAllNotificationsRead(_ context.Context, scope, projectID string) error {
	return f.recordNotificationMutation("mark-all-read", scope, projectID)
}

func (f *fakeMemory) recordNotificationMutation(kind, id, verb string) error {
	if f.notificationMutErr != nil {
		return f.notificationMutErr
	}
	f.notificationMutations = append(f.notificationMutations, kind+":"+id+":"+verb)
	return nil
}

func (f *fakeMemory) NotificationEventStream(_ context.Context, projectID string) (io.ReadCloser, error) {
	f.lastEventStreamProject = projectID
	if f.eventStreamErr != nil {
		return nil, f.eventStreamErr
	}
	if f.eventStreamBody != nil {
		return f.eventStreamBody, nil
	}
	return io.NopCloser(strings.NewReader("")), nil
}
