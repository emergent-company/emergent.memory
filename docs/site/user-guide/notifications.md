# Notifications

Notifications inform you about activity in your projects — agent questions awaiting your response, completed background tasks, errors, and other system events.

---

## Document activity in your projects

A project's knowledge base is shared, so a document landing in it is project news.
Four event keys cover the document lifecycle:

| Event key | When it fires | Default | Can you turn it off? |
|---|---|---|---|
| `document.uploaded` | a member uploads a document (`POST /api/documents/upload`, and once per request for `/upload/batch`) | on | yes |
| `document.version_synced` | a data-source sync appends a new version of a document (a source's *first* ingest does not notify) | on | yes |
| `document.extraction_completed` | extraction finished and its objects were merged into the graph | on | yes |
| `document.processing_failed` | an upload's parsing or extraction reached its final retry (dead letter) | on | **no** |

### Who receives them

Every row goes to the project's members **except the member whose action produced
it** — you are never notified about your own upload or your own extraction run.
A data-source sync carries no actor, so a new version it appends reaches every
member, including whoever pressed sync. Consequences worth knowing:

- A solo project sees nothing from the three informational keys; there is nobody
  else to tell.
- A batch upload arrives as one digest row (`12 documents uploaded · a.pdf,
  b.pdf, c.pdf and 9 more`) instead of one row per file.
- An extraction that leaves objects on a staging branch for review sends
  `extraction.review_ready`, not `document.extraction_completed` — one row per
  extraction outcome, never both.

`document.processing_failed` is the exception to the actor rule: it is **not**
suppressed for the person whose run failed, because that person is the one who has
to act. It fires once, on the final attempt (retries do not notify), and names the
stage that failed — `Text extraction` or `Extraction`.

### Turning them off

Project notification preferences are per user and per project:

```http
GET /api/notifications/preferences?project_id=<project>
```

```http
PUT /api/notifications/preferences
Content-Type: application/json

{ "projectId": "<project>", "eventKey": "document.uploaded", "enabled": false }
```

The three informational document keys are **on by default** and are reported with
`enabled: true` before you save anything, so you disable what you do not want
rather than opting into each key. `document.processing_failed` is required
delivery (`required: true`): a stored preference for it is ignored when the row is
created, because a processing run that died needs someone to look at it.

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

## The web inbox

The bell in the top bar counts unread notifications for the scope on screen, and the
inbox offers three scopes:

| Scope | Shows |
|---|---|
| **Everything** (default) | account events and project activity from every project you belong to |
| **Account** | account-scope events only — membership, permissions, invites, budget |
| **Project** | the active project only |

Everything is the default for the inbox *and* for the bell on every other page, so a
teammate's upload raises the bell while you are elsewhere in the app. In Everything
and Account the list can be narrowed to a single project with the project filter, and
each row carries a chip naming its project.

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

Subscribe to a server-sent event stream to receive notifications in real time without
polling. There are two streams; which one carries a notification depends on its scope:

```http
GET /api/events/stream/account        # account-scope notification events
GET /api/events/stream?projectId=<id> # that project's events
Accept: text/event-stream
```

The account stream carries **only** account-scope notifications for the calling user.
Project-scope notifications — including every document lifecycle event — arrive on
that project's stream, so a client that wants both opens both.

Frames are entity events, not notification events: the SSE event name is
`entity.created` (or `entity.updated`) and the payload names the entity. Frames for
documents, graph objects and agent runs travel on the same streams, so filter on
`entity`:

```javascript
const source = new EventSource('/api/events/stream?projectId=' + projectId);

source.addEventListener('entity.created', (event) => {
  const payload = JSON.parse(event.data);
  if (payload.entity !== 'notification') return;
  // payload.data.scope === 'project', payload.data.eventKey === 'document.uploaded'
  refreshUnreadCount();
});
```

The stream stays open; reconnect automatically if it drops.
