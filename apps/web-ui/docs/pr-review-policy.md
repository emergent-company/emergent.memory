# PR review policy

Formal review contract for pull requests on `memory.web-ui`. Scope: master merges, which use a
process gate instead of GitHub branch protection (private repo, free plan). This file is the
source of truth; reviewers and coding lanes load it through the `pr-review` skill.

## Merge gate

A PR merges only when BOTH hold:

1. **CI green** — GitHub Actions checks pass: `build`, `lint`, `templ`, `test`, `vet`.
2. **Approving review** — a reviewer agent (bot identity, see below) approved the PR.

The review bot is a GitHub App (`emergent-code-reviewer`, App id `4884315`, installation
`160306576`) — a distinct identity, so its review/merge is a real gate rather than self-approval.
Authors do not merge: the bot is the merge actor. A human admin (`mkucharz`) retains override.

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
- Installation on `emergent-company` (repo scope `memory.web-ui`): `160306576`
- Repository permissions: **Pull requests: Read & write**, **Contents: Read & write**
  (Metadata read auto).
- Private key on the alfred server (not committed):
  `/root/Emergent Code Reviewer Private Key Sept 9 2026.pem`

Mint a short-lived installation token with the helper:

```
export GH_TOKEN="$(tools/gh-app-token.sh)"     # 55-min installation token
tools/gh-app-token.sh -- gh pr view <n>        # or run a command with GH_TOKEN set
```

The helper accepts `GH_APP_ID` / `GH_APP_KEY` / `GH_APP_INSTALLATION_ID` overrides. Rotate the
key by replacing the PEM and re-running; no PAT/`.env` secret is stored.

Verified capability: the App approves (`gh pr review --approve`, review → APPROVED) and merges
(`gh pr merge`, `mergedBy: app/emergent-code-reviewer`).

## Reviewer runbook (Paseo schedule)

Poll cadence: every 10 minutes. For each open PR lacking a review:

1. `gh pr view <n>` — state, CI checks, review decision.
2. Skip drafts/WIP. If a prior review requested changes, confirm the fix landed on the head.
3. Fetch the head into an isolated worktree (never the shared checkout).
4. Review the diff against this policy + load `.opencode/skills/pr-review/SKILL.md`.
5. Post review (App identity):
   - approve: `GH_TOKEN="$(tools/gh-app-token.sh)" gh pr review <n> --approve`
   - changes: `gh pr review <n> --request-changes --body "…"` (+ inline `gh pr review <n> --comment`)
6. Merge approved green PRs (App identity):
   `GH_TOKEN="$(tools/gh-app-token.sh)" gh pr merge <n> --squash --delete-branch`.

## Multiple reviewers (future)

The bot identity is reusable; to add a second reviewer lane, create a second bot account and run
a second schedule lane with its own PAT. Any approved review satisfies the gate. Roles can be
split (e.g. code review vs spec/test review) per lane prompt.
