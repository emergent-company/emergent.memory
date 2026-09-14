---
name: pr-review
description: Review a GitHub PR against the repo's formal review contract, or prepare a PR for review. Use when reviewing a pull request, opening a PR that must pass the review gate, or responding to requested changes.
---

# PR Review

Load the full contract from `docs/pr-review-policy.md` — it is the source of truth. This skill is
the operational shortcut both sides use: coding lanes opening PRs and reviewer agents approving,
commenting, or merging.

## 1. When to use

- A coding lane finished a branch and must open a PR that will pass the review gate.
- A reviewer agent reviews an open PR (approve / request-changes / inline comments).
- A PR got "request changes" and the author must fix and re-request.

## 2. Merge gate

Merge requires BOTH: CI green (`build`, `lint`, `templ`, `test`, `vet`) AND an approving review
from the review bot. Bot identity: the GitHub App `emergent-code-reviewer` (App id `4884315`,
installation `160306576`); mint its token with `tools/gh-app-token.sh`. Authors do not merge — the
bot is the merge actor. A human admin retains override.

## 3. Opening a PR (author side)

1. Verify: `go build ./...`, `templ generate -check`, `go vet`, golangci-lint, full test suite —
   run every available suite, capture pass counts.
2. Spec Rule: behavior changed ⇒ same PR updates `docs/spec/`, `openspec/`, or the relevant
   delta. Spec drift is a bug.
3. Write the PR body with a **verification summary**: suites run + counts, lint/build status,
   spec/docs changed and why.
4. `gh pr create --base master --head <branch> --title ... --body ...`
5. If CI is not already green on the branch, wait for checks. Do NOT merge yourself (unless
   bot not yet configured — see gate).

## 4. Reviewing (reviewer side)

1. `gh pr list --state open` → find PRs with no approving review; skip drafts/WIP.
2. Inspect: `gh pr view <n>` (state, CI checks, prior review decision), `gh pr diff <n>`.
3. If a prior review requested changes, confirm the fix is on the head before re-reviewing.
4. Work in an isolated worktree — never the shared `/root/alfred` checkout:
   `gh pr checkout <n>` in a throwaway worktree, or review via `gh pr diff` + targeted file reads.
5. Judge the diff against the formal criteria (scope, spec sync, tests, generated code,
   verification, secrets, copy/tone, design). Label blockers vs nits explicitly.
6. Post the decision:

   ```
   GH_TOKEN="$(tools/gh-app-token.sh)" gh pr review <n> --approve
   GH_TOKEN="$(tools/gh-app-token.sh)" gh pr review <n> --request-changes --body "…"
   GH_TOKEN="$(tools/gh-app-token.sh)" gh pr review <n> --comment -b "nit: …"
   ```

   Inline: `gh api repos/emergent-company/memory.web-ui/pulls/<n>/comments` with a position/line
   (use `gh pr diff` output to resolve the correct diff hunk).
7. Merge approved green PRs: `GH_TOKEN="$(tools/gh-app-token.sh)" gh pr merge <n> --squash --delete-branch`.

## 5. Comment standards

Inline, actionable, one issue per comment; state the fix. Blockers vs nits labeled. Grounded:
`file:line`, exact error output. UI copy: normal, no emojis, no fluff.

## 6. Re-review loop

Requested changes → author pushes fixes → bot re-reviews the new head. Approve only when every
criterion holds at the reviewed commit.
