# Webpage Feedback

A blueprint for triaging webpage feedback captured by
[emergent.feedback](https://github.com/emergent-company/emergent.feedback).

## What it installs

- **Schema pack `webpage-feedback`**
  - `Issue` — one captured report. Board-enabled with statuses
    `new / triaged / needs-info / wont-fix`. Carries the page URL, repo,
    description, capture lifecycle, app version, environment, branch, and
    viewport. `capture_status` is owned by the capture source; the triager owns
    `status`, `severity`, `category`, `confidence`, and `triage_notes`.
  - `IssueGroup` — a recurring problem linking two or more similar Issues with
    a `BELONGS_TO_GROUP` relationship.
- **Agent `issue-triager`** — a reaction agent that wakes on `Issue` created
  events, classifies the report, and groups duplicates.
- **Skill `triage-issues`** — the ownership, status, and grouping rules the
  agent follows.

There is no seed object: installing the blueprint does not create a fake report
in the project.

## Applying it

```bash
memory blueprints install blueprints/webpage-feedback
```

Or install it from the web UI's blueprint gallery. The apply is idempotent for
the pack and the agent.

The blueprint ships a **global skill**, and only a superadmin may create one
through `POST /api/skills`. The CLI applies the blueprint locally and calls that
endpoint, so a non-superadmin token installs the pack and the agent and then
fails with `403 forbidden: Access denied` on `triage-issues`. Apply from the web
UI's blueprint gallery instead — it applies the blueprint server-side, which
writes global skills directly and is not superadmin-gated — or run the CLI with
a superadmin token.

## Connecting emergent.feedback

In the feedback panel, open **Destinations**, choose a repository and the Memory
**API** URL (the origin that serves `/.well-known/oauth-authorization-server`),
then **Connect with Memory**. The OAuth consent page—served by the Memory
gateway—lets the user pick the project and approve the narrow scopes
(`graph:read`, `graph:write`, `schema:read`, `projects:read`, `chat:use`). A
project token pasted under **Advanced** is a permanent fallback when OAuth is
unavailable.

`chat:use` is only needed for the overlay's **Chat** tab (`data-chat`), which
composes a report with an agent from the connected project; the capture path
itself never calls chat. It is part of the scope string emergent.feedback
requests, so a destination connected before the Chat tab existed can still lack
it — reconnect the destination to grant it.

Per-repository modes:

- **Ask** (default) — the feedback dialog shows a “Send to Memory” checkbox.
- **Auto** — every captured report is sent.
- **Off** — the connection is kept but nothing is sent.

## What the capture sends

The feedback app upserts an `Issue` keyed `ef-<repo>-<feedback id>` with
`type: Issue`, then patches `capture_status` when the report is resolved or
dismissed. The initial write uses `PUT /api/graph/objects/upsert`; retries and
status writeback use `PATCH /api/graph/objects/{id}` so the triager's
classification is preserved.

## Grouping

A single report is triaged but not grouped. When the triager finds a second
similar report, it creates one `IssueGroup` and links both Issues. Later similar
reports join the same group. This keeps the graph small and avoids one group per
report.

## Analysts and digests

Not included in v1. An analyst or digest agent can be run ad hoc over the
`Issue` / `IssueGroup` graph later without changing the capture contract.
