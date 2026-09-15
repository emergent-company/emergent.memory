# PR review policy

Formal review contract for pull requests on `emergent-company/emergent.memory` (the web UI lives
in `apps/web-ui/`, its Go gateway module in `apps/web-ui/gateway/`). Scope: merges to `main`, which
are gated by real GitHub branch protection on the required status check `ci` plus one approving
review. This file is the source of truth; reviewers and coding lanes load it through the
`pr-review` skill.

## Merge gate

A PR merges only when BOTH hold:

1. **CI green** — the required `ci` status check reports success, plus the build/lint/test jobs it
   gates (see CI coverage below).
2. **Approving review** — a reviewer agent (bot identity, see below) approved the PR.

The review bot is a GitHub App (`emergent-code-reviewer`, App id `4884315`, installation
`160306576`) — a distinct identity, so its review/merge is a real gate rather than self-approval.
Authors do not merge: the bot is the merge actor. A human admin (`mkucharz`) retains override.

### CI coverage (path-scoped workflows)

The required `ci` context comes from whichever workflow covers the changed paths — a PR whose
paths no workflow matches never produces `ci` and is **unmergeable** until coverage is added:

| Paths | Workflow | Jobs |
|---|---|---|
| `apps/server/**` | `.github/workflows/server.yml` (`Server Go CI`) | `Lint`, `Build`, `Test`, `Migration guard` → gate job `ci` |
| `apps/web-ui/**` | `.github/workflows/web-ui.yml` (`Web UI CI`) | `Gateway` (`templ generate`, `go build`, `go vet`, `go test`, `golangci-lint run`) → gate job `ci` |
| `.github/**` | `.github/workflows/ci.yml` (`Meta CI`) | `py_compile` of `.github/scripts/*.py` → gate job `ci` |

These workflows name their gate job `ci` deliberately. A PR touching multiple trees runs all
matching workflows and must pass each `ci` gate.

### Auto-merge and stale reviews

`.github/workflows/auto-merge.yml` enables squash auto-merge when a non-draft PR is opened or
synchronized, so an approving review is normally the last thing needed. Branch protection is
strict with `dismiss_stale_reviews`: **any push to a reviewed head dismisses the approval**, so a
fix commit requires a fresh approving review.

`.github/workflows/review.yml` runs the in-repo AI reviewer (`ai_review.py` + `ai_fix.py`) on
every `pull_request` (opened/synchronize/reopened). It posts a `REQUEST_CHANGES` review with
line-anchored `suggestion` comments (one-click "Commit suggestion"), and an `ai_fix.py` job
auto-applies `must_fix`/`should_fix` edits (commit + push) capped by the `ai-auto-fixed` label
(one pass). It runs as `github-actions[bot]` — it does **not** satisfy the approving-review
requirement; the App bot (or a human admin) is the approving identity.

## Pipeline

1. Coding lane opens a PR with a verification summary in the body (see Required evidence).
2. Reviewer agent (Paseo schedule, polls open PRs) reviews the head.
3. Actions:
   - **Approve** — criteria met; bot merges the PR and deletes the branch.
   - **Request changes** — actionable inline comments; author fixes, bot re-reviews.
   - **Skip** — draft, WIP, or blocked PR; leave a short reason comment.
4. Human (`mkucharz`) retains admin override at all times.

## Formal criteria

Reviewer approves only when every item holds. Comment (do not approve) on any miss.

### Required evidence in the PR body

Author must state, in the PR body:

- Which test suites ran and pass counts (e.g. `go test ./...`, playwright scenario name).
- Lint/build/templ status (or the CI check URLs).
- Which spec/docs changed and why (Spec Rule: behavior change ⇒ spec updated same change).

### Review checklist

- **Scope** — diff touches only what the PR claims. No unrelated refactors, no drive-by edits.
- **Spec sync** — behavior change updates `docs/spec/`, `openspec/`, or the relevant delta in the
  same PR. Spec drift is a bug.
- **Tests** — new/changed behavior covered; suites run and pass. Mandatory, not optional.
- **Generated code** — `.templ` changes have regenerated output (`templ generate -check` clean);
  vendored/generated artifacts consistent with the repo's gitignore convention.
- **Verification** — CI green at the reviewed commit; lint/build/vet/templ all pass.
- **Secrets** — no keys/tokens in diff (gitleaks gate).
- **Copy/tone** — UI copy grounded, no emojis, no fluff; consistent with surrounding files.
- **Design** — change reuses existing primitives/patterns; no redesign dressed as a fix.

### Comment standards

- Inline, actionable, one issue per comment. State the fix, not just the problem.
- Blocking findings (must fix before merge) vs nits (non-blocking) labeled explicitly.
- Grounded in code: cite `file:line`, quote exact error output.

## Reviewer identity (GitHub App — wired)

The reviewer is a GitHub App so its review/merge is a distinct identity:

- App: `emergent-code-reviewer`, App id `4884315`
- Installation on `emergent-company`, covering the `emergent.memory` repo: `160306576`
- Repository permissions: **Pull requests: Read & write**, **Contents: Read & write**
  (Metadata read auto).
- Private key on the dev server (not committed):
  `/root/Emergent Code Reviewer Private Key Sept 9 2026.pem`

Mint a short-lived installation token with the helper (run it from the repo root):

```
export GH_TOKEN="$(apps/web-ui/tools/gh-app-token.sh)"   # 1-hour installation token
apps/web-ui/tools/gh-app-token.sh -- gh pr view <n>      # or run a command with GH_TOKEN set
```

The helper accepts `GH_APP_ID` / `GH_APP_KEY` / `GH_APP_INSTALLATION_ID` overrides. Rotate the
key by replacing the PEM and re-running; no PAT/`.env` secret is stored.

Verified capability: the App approves (`gh pr review --approve`, review → APPROVED) and merges
(`gh pr merge`, `mergedBy: app/emergent-code-reviewer`).

## Reviewer runbook (Paseo schedule)

Poll cadence: hourly (Paseo schedule `pr-review-bot`). For each open PR lacking a review:

1. `gh pr view <n>` — state, CI checks, review decision. A **missing** `ci` check means no workflow
   covers the changed paths (see CI coverage) — block, do not approve.
2. Skip drafts/WIP. If a prior review requested changes, confirm the fix landed on the head.
3. Fetch the head into an isolated worktree — never the shared checkout.
4. Review the diff against this policy + load `apps/web-ui/.opencode/skills/pr-review/SKILL.md`.
5. Post review (App identity):
   - approve: `GH_TOKEN="$(apps/web-ui/tools/gh-app-token.sh)" gh pr review <n> --approve`
   - changes: `gh pr review <n> --request-changes --body "…"` (+ inline `gh pr review <n> --comment`)
6. Merge approved green PRs (App identity), if auto-merge has not already done it:
   `GH_TOKEN="$(apps/web-ui/tools/gh-app-token.sh)" gh pr merge <n> --squash --delete-branch`.

## Multiple reviewers (future)

The bot identity is reusable; to add a second reviewer lane, create a second bot account and run
a second schedule lane with its own PAT. Any approved review satisfies the gate. Roles can be
split (e.g. code review vs spec/test review) per lane prompt.
