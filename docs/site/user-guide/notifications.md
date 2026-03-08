# Notifications

Notifications inform you about activity in your projects — agent questions awaiting your response, completed background tasks, errors, and other system events.

---

## Notification fields

| Field | Description |
|---|---|
| `title` | Short heading |
| `message` | Full notification body |
| `severity` | `info` · `warning` · `error` · `success` |
| `importance` | `important` · `other` |
| `category` | Optional grouping label |
| `relatedResourceType` | The type of resource this notification relates to |
| `relatedResourceId` | UUID of the related resource |
| `actionUrl` | Optional deep link to the relevant UI page |
| `actionLabel` | Button label for the action link |
| `read` | Whether you have read the notification |
| `dismissed` | Whether you have dismissed it |
| `expiresAt` | Optional auto-expiry timestamp |

---

## Listing Notifications

```http
GET /api/notifications
```

### Filter by tab

| Tab | What it shows |
|---|---|
| `all` | Every non-dismissed notification |
| `important` | High-importance notifications only |
| `other` | Normal-importance notifications |
| `snoozed` | Notifications snoozed until a future time |
| `cleared` | Previously dismissed notifications |

```http
GET /api/notifications?tab=important
```

---

## Notification Counts

Get the unread badge counts for the UI:

```http
GET /api/notifications/counts
GET /api/notifications/stats
```

Response:

```json
{
  "unread": 3,
  "important": 1,
  "pendingQuestions": 1
}
```

---

## Marking as Read

Mark a single notification as read:

```http
PATCH /api/notifications/{id}/read
```

Mark all as read:

```http
POST /api/notifications/mark-all-read
```

---

## Dismissing a Notification

```http
DELETE /api/notifications/{id}/dismiss
```

Dismissed notifications move to the `cleared` tab and are hidden from the default view.

---

## Real-Time Updates (SSE)

Subscribe to a server-sent event stream to receive notifications in real time without polling:

```http
GET /api/events/stream
Accept: text/event-stream
```

Events are emitted for:

- New notifications
- Agent run status changes
- Agent questions (pause/resume)
- Background task completions
- Document conversion completions

The stream stays open; reconnect automatically if it drops.

```javascript
const source = new EventSource('/api/events/stream', {
  headers: { 'Authorization': 'Bearer emt_...' }
});

source.onmessage = (event) => {
  const payload = JSON.parse(event.data);
  if (payload.type === 'notification') {
    // update your notification badge
  }
};
```
