# Wire the GitHub App review bot

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-schema-migration-resync](../sessions/2026-09-09-schema-migration-resync.md)

## What

Finish the agent-verified PR review gate on `emergent-company/memory.web-ui`. The GitHub App
`emergent-code-reviewer` exists (App id `4884315`) and is installed on the org
(installation `160306576`) but has **zero repository permissions**, so it cannot review or
merge yet. Private key on the alfred server:
`/root/Emergent Code Reviewer Private Key Sept 9 2026.pem`.

Steps:
1. Set App repository permissions: **Pull requests = Read & write**, **Contents = Read & write**
   (Metadata read auto); ideally restrict the install to `memory.web-ui` only.
2. Write a short-lived-token helper on the server: sign a JWT with the App private key
   (`iss` = App id), `POST /app/installations/{id}/access_tokens`, export as `GH_TOKEN`.
3. Create the Paseo review schedule (poll open PRs lacking an approving review; reviewer agent
   follows `.opencode/skills/pr-review/SKILL.md` + `docs/pr-review-policy.md`).
4. Flip the interim rule in `/root/.config/opencode/AGENTS.md`: authors stop merging; the bot
   approves and merges green PRs.
5. Live-test: open a throwaway PR, confirm the bot reviews, approves, merges.

## Why

PRs should be verified by a dedicated agent (approve / inline comments / request changes) beyond
automated CI. A distinct bot identity is required because a PR author cannot approve or gate its
own PR. Process gate substitutes for branch protection while the repo stays on the free plan.

## Depends on

- `.opencode/skills/pr-review/SKILL.md` and `docs/pr-review-policy.md` (already merged, PR #4).

## Notes

- GitHub App + token creation are web-only; the user provisions, the agent wires.
- Merge gate: CI green (`build`, `lint`, `templ`, `test`, `vet`) AND approving review.
- Branch protection via GitHub Pro remains the alternative hard gate (deferred by choice).
- Future variants: multiple reviewer lanes (role split), optional human signoff — see policy doc.
- Multi-agent review works by giving each lane its own App/bot identity; any approval satisfies
  the gate.
