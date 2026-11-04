# Tasks

Tasks are background operations that require your attention or acknowledgment — things like import jobs that completed with warnings, data quality issues flagged by an agent, or operations that need manual resolution.

Tasks are distinct from [Notifications](notifications.md): a notification informs you, a task requires you to **act**.

---

## Task fields

| Field | Description |
|---|---|
| `title` | What the task is about |
| `description` | Optional details |
| `type` | Category of task (set by the system) |
| `status` | `pending` · `resolved` · `cancelled` |
| `sourceType` | What created this task (e.g. `extraction_job`, `agent_run`) |
| `sourceId` | ID of the originating resource |
| `metadata` | Additional context as JSON |
| `resolvedAt` | When it was resolved |
| `resolutionNotes` | Notes you added when resolving |

---

## Listing Tasks

```http
GET /api/tasks
```

List all tasks across all projects (requires elevated access):

```http
GET /api/tasks/all
```

---

## Task Counts

Get counts for the sidebar badge:

```http
GET /api/tasks/counts
GET /api/tasks/all/counts
```

Response:

```json
{
  "pending": 4,
  "resolved": 12
}
```

---

## Getting a Task

```http
GET /api/tasks/{id}
```

---

## Resolving a Task

Mark a task as done — optionally add resolution notes:

```http
POST /api/tasks/{id}/resolve
Content-Type: application/json

{
  "notes": "Reviewed and confirmed the extracted data is correct."
}
```

---

## Cancelling a Task

If a task is no longer relevant:

```http
POST /api/tasks/{id}/cancel
```

---

## Common task types

| Type | When it appears |
|---|---|
| Data review | Extraction produced objects flagged for review (`needs_review: true`) |
| Import warning | A data source sync completed with partially failed items |
| Agent escalation | An agent determined a human decision is required |
| Schema conflict | A type schema change conflicts with existing data |
