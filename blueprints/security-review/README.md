# security-review blueprint

A queue-backed **security code review** agent — the pilot for agent review queues.

## What it applies

- An agent definition `security-review` bound to the `security-review` work queue
  (`defaultQueue: security-review`, `dispatchMode: queued`).

It is an *agent* blueprint only: it does not create the queue or the runtime agent.

## Prerequisite: create the queue

Queues are project resources created via the API (blueprints do not yet manage
queues):

```bash
curl -sS -X POST "$MEMORY_SERVER_URL/api/projects/$MEMORY_PROJECT_ID/agent-queues" \
  -H "Authorization: Bearer $MEMORY_API_KEY" \
  -H "X-Project-ID: $MEMORY_PROJECT_ID" \
  -H "Content-Type: application/json" \
  -d '{"name":"security-review","displayName":"Security Review","concurrency":1,"priority":50}'
```

Use `concurrency: 1` when the review model is expensive or rate-limited; raise it
to review more subjects in parallel.

## Apply

```bash
memory blueprints ./blueprints/security-review
```

## Enable a runtime agent bound to the queue

The definition binds runs to the queue, but a runtime agent must exist to trigger
them. Create one whose name matches the definition (or link it via
`agentDefinitionId`), then set `dispatchMode: queued` so its triggers enqueue:

```bash
memory agents create --name security-review --definition-id <definition-id>
```

## Enqueue a review

Enqueue a work item with a typed subject; the worker claims it from the
`security-review` queue and runs the agent against it:

```bash
curl -sS -X POST "$MEMORY_SERVER_URL/api/projects/$MEMORY_PROJECT_ID/agent-queues/security-review/enqueue" \
  -H "Authorization: Bearer $MEMORY_API_KEY" \
  -H "X-Project-ID: $MEMORY_PROJECT_ID" \
  -H "Content-Type: application/json" \
  -d '{
        "agentName": "security-review",
        "message": "Review this pull request for security issues.",
        "metadata": {"subjectType": "pull_request", "subjectRef": "42", "prNumber": 42, "repo": "org/repo"}
      }'
```

## Producers (any of)

- **Manual/CLI/API** — the enqueue call above.
- **Graph reaction** — bind the runtime agent with a `reaction` trigger on graph
  objects (e.g. a `PullRequest` object) to enqueue automatically.
- **Cron** — a `schedule` trigger for periodic sweeps.
- **GitHub** — planned GitHub webhook producer (see the OpenSpec change
  `add-agent-review-queues`, phase 2).

## Findings

In phase 1 the agent records findings through the graph write tools. A dedicated
`kb.review_findings` store and configurable output actions (GitHub comment,
triage task, notification) are specified in `add-agent-review-queues` phases 2–3.
