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

Create a trigger with event `issues`. The agent picker is the same one used by
pull-request triggers. The trigger can also carry:

- model and max-step overrides
- a static context object
- allowlisted, non-secret environment variables
- a request for a source credential with `contents`/`pull_requests`/`issues`
  write permission

The credential is not stored on the trigger. The server mints a short-lived
installation token at run start and injects it as `GH_TOKEN` into the sandbox.

Automatic issue triggers default to opening/reopening work. Edits update the
board item; closing cancels a live run and closes the item.

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

## Independent review

The review workflow remains unchanged: a separate review agent reads the PR
diff and submits its verdict, which posts the trusted `Reviewed-SHA` marker.
Auto-merge only completes once the required checks and that marker are present.

## Invocation environment allowlist

Only non-secret keys are accepted: `CI`, `LANG`, `LC_ALL`, `TERM`, `TZ`, and
`GOMAXPROCS`. `GH_TOKEN`/`GITHUB_TOKEN` are server-injected and cannot be set by
a trigger. Use a source credential for GitHub access instead of storing a token.
