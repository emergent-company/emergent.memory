# GitHub issue coding

A GitHub App source can turn issues into board work. The flow has two entry
points over the same `GitHubIssue` board item:

- **Automatic**: an `issues` trigger on the source assigns new/reopened issues
  to a coding agent.
- **Manual**: backfill imports open issues unassigned; a human assigns an agent
  from the Board.

Both paths use the same run invocation contract, so an agent can run with a
different model, context, environment or source credential depending on the
trigger that started it.

## GitHub App requirements

Create the App (or update an existing one) with:

- **Events**: `issues`, `pull_request`
- **Permissions**: `contents: write`, `issues: write`,
  `pull_requests: write`, `checks: read`

Register the App as a source App, install it, and create a `github_app` source
with the repositories that should be imported.

The review App is separate. Its bot identity is the one that posts the
`Reviewed-SHA` marker; do not reuse the coding App for review.

## Import open issues

On the source detail page, **Import open issues** creates an unassigned
`GitHubIssue` board item for every open issue (pull requests returned by the
issues API are skipped). Importing is idempotent and never starts a run.

## Trigger an agent

For manual work, configure the coding agent's work rule for object type
`GitHubIssue`, pickup status `ready`, with a prompt and an invocation. Assigning
a backfilled issue to that agent then starts the run.

For automatic work, create a trigger with event `issues`. The agent picker is
the same one used by pull-request triggers. The trigger can also carry:

- model and max-step overrides
- a static context object
- allowlisted, non-secret environment variables
- a request for a source credential with `contents`/`pull_requests`/`issues`
  write permission

The credential is not stored on the trigger. The server mints a short-lived
installation token at run start and injects it as `GH_TOKEN` into the sandbox.

Automatic issue triggers default to opening/reopening work. Edits update the
board item; closing cancels a live run and closes the item.

## Issue ownership and claims

Set `assignee_login` in the source config to the GitHub login of the person
responsible for issues this source starts. On opening or reopening an issue,
the integration:

1. reads the live issue and refuses to start if another agent has claimed it
   (any `status: blocked`, any other `manager:` label, or `status: in-progress`
   without `manager: memory-coding`), or if it is assigned to someone else;
2. creates the claim labels when the repository does not have them yet;
3. assigns the configured responsible user;
4. adds `status: in-progress` and `manager: memory-coding`; and
5. posts a receipt comment naming the agent.

The responsible user stays assigned for the life of the issue. The labels are
the claim signal peer agents read; `manager: memory-coding` identifies the
agent, while the graph keeps the precise agent and run identity. A coding PR
must reference the issue with a closing keyword (`Closes #N`, `Fixes #N`, or
`Resolves #N`) so the merge closes it. Label and comment writes from the
integration's own App are ignored, so claiming cannot re-trigger the source.

## Reviewer agents

Reviewers are normal reaction agents, not a source-level reviewer list. The
source emits a board object for each pull request; reviewer agents subscribe by
object type and property predicates.

Example:

```json
{
  "objectTypes": ["GitHubReviewLane"],
  "events": ["created", "updated"],
  "match": [
    {"path": "/status", "operator": "equals", "value": "ready"}
  ],
  "reviewer": true,
  "reviewSubjectType": "GitHubPullRequest",
  "reviewMatch": [
    {"path": "/labels", "operator": "contains", "value": "review:ui"}
  ],
  "concurrencyStrategy": "skip"
}
```

- A reviewer with no `reviewMatch` is the default reviewer.
- `objectTypes`/`events` drive the reviewer's own lane work item.
- `match` on `status=ready` prevents a cancelled lane from waking the reviewer.
- `reviewSubjectType`/`reviewMatch` select the reviewer from the parent review
  subject.
- The source creates one lane work item per required reviewer and head;
  superseded head lanes are cancelled.
- Every matching reviewer is required for the current head.
- Added reviewers are sticky for that head; a new head requires a fresh set.
- Reviewer verdicts are aggregated server-side; a reviewer cannot set the
  aggregate approval or publish the provider check directly.

The source creates one `GitHubReviewLane` work item per required reviewer so
parallel reviewers do not contend for the parent subject's work claim. The
GitHub adapter publishes the aggregate `reviewed` check and the source
discovers the reviewer agents from the project. Do not add a source workflow
reviewer list for new review sources.

## Run inside the sandbox

The coding agent uses the existing workspace tools. A `workspace_git push` and
the `gh` CLI are available:

```bash
gh issue view 737 --repo owner/repo
git checkout -b fix/737
# edit, commit
workspace_git push
gh pr create --base dev --fill
gh pr merge --auto --squash
```

Use the configured base branch; do not push directly to the protected branch.

## Independent review and rework loop

A separate review agent reads the PR diff and submits its verdict, which posts
the trusted `Reviewed-SHA` marker. The PR is linked to its coding issue by
`Closes #N`, `Refs #N`, a `fix/<N>-...` branch, or `(#N)` in the title.

- **MERGE**: the issue item moves to `done`, the issue is closed, and auto-merge
  completes once required checks are green.
- **CHANGES_REQUESTED**: the issue item moves to `revision`, the review text is
  stored as work feedback, and the coding agent is enqueued again with the PR
  number and branch. It must push to the same branch; opening a second PR is a
  failure. The PR `synchronize` event triggers a fresh review.

A pull request authored by a source-trigger run (a GitHub App bot) needs at
least two reviewer agents. The server snapshots that quorum on the review gate
and never lowers it for the head: a project with a single reviewer agent leaves
`reviewed` pending (and logs an under-configured error) instead of letting the
reviewing App's one bot verdict satisfy "all reviewers approved" (#1005). An
unknown author also fails closed to the two-reviewer quorum. The quorum counts
distinct reviewer agents; it does not prove they use a different App credential,
so keep the review App separate from the coding App (above) and configure two
distinct reviewer agents. Human-authored pull requests keep the
single-reviewer default.

The loop is capped by the coding agent's `revisionLimit` (default 3), after which
the item is blocked for a human.

## Invocation environment allowlist

Only non-secret keys are accepted: `CI`, `LANG`, `LC_ALL`, `TERM`, `TZ`, and
`GOMAXPROCS`. `GH_TOKEN`/`GITHUB_TOKEN` are server-injected and cannot be set by
a trigger. Use a source credential for GitHub access instead of storing a token.
