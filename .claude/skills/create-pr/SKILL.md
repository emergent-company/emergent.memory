---
name: create-pr
description: Create a GitHub pull request for the current branch — verify state, push, write the PR body safely, open via `gh pr create`, and confirm the result. Use when the user says "create a PR", "open a PR", "make a PR", "push and PR", or wants finished work turned into a pull request.
metadata:
  author: emergent
  version: "1.0"
---

# Skill: create-pr

Turn a finished branch into a pull request against `main`: verify state, push,
write the body safely, create the PR, and confirm it landed correctly.

**Input**: Optional PR title and/or a one-line body summary. If omitted, infer
from the branch name and recent commits.

---

## Steps

### 1. Confirm branch and clean state

Run in parallel:

```bash
git branch --show-current
git status --short
git log --oneline -5
```

- You must be on a feature branch (`feat/*`, `fix/*`, `docs/*`, `chore/*`), **not** `main`.
- A PR carries **committed** work only. Commit any uncommitted changes first
  (see the `commit` skill).

### 2. Push the branch

```bash
git push -u origin <branch>
```

### 3. Write the PR body

Body conventions:

- Summarize **what** changed and **why**, in plain prose.
- If the work has an OpenSpec change, link it (`openspec/changes/<name>/`) and
  summarize the delta specs.
- List what was **verified** (build / test / lint commands and results).
- Note anything **not** verified (e.g. e2e not run).

**Write the body to a unique temp file first, in its own step — never in the
same tool-call block as the `gh pr create --body-file` that reads it.** Parallel
tool calls race: `gh` can read a stale temp file left by another session and
attach the wrong body (this has happened — a PR shipped with another PR's body
while the title/head ref were correct).

```bash
cat > /tmp/pr-body-$(date +%s).md <<'EOF'
## What

...
EOF
```

Or write the body inline in one atomic shell invocation, with no temp file:

```bash
gh pr create --base main --head <branch> --title "<title>" --body "$(cat <<'EOF'
## What

...
EOF
)"
```

### 4. Create the PR

```bash
gh pr create --base main --head <branch> --title "<title>" --body-file /tmp/pr-body-<unique>.md
```

- Base is `main` (the default branch).
- Pass `--head <branch>` explicitly when the shell is not already on that
  branch (e.g. running from a worktree or the shared checkout). `gh pr create`
  otherwise assumes the *current* checked-out branch — which may be `main` and
  will error "you must first push the current branch".

### 5. Verify the PR

```bash
gh pr view <number> --json title,body,headRefName,baseRefName,state
```

Confirm:

- `title` matches what you wrote.
- `body` is **your** body — read the first few lines; a stale body is a real,
  recurring failure mode.
- `headRefName` is your branch, `baseRefName` is `main`.

If the body is wrong, fix it (write the corrected file first, sequentially, then
edit):

```bash
gh pr edit <number> --body-file <correct-file>
```

---

## Guardrails

- **Authors do not merge their own PRs.** The review bot (`emergent-code-reviewer`)
  reviews and merges once checks pass. Never run `gh pr merge` yourself.
- **Never force-push** to `main`/`master`. If the branch conflicts, merge `main`
  into it (or rebase) instead.
- **Verify the body landed correctly** before reporting done — don't trust that
  `--body-file` read what you think it read.
- **Small changes skip OpenSpec**; non-trivial work ships spec + implementation
  in the same PR (see repo `AGENTS.md`).
- **Work in a worktree**, not the shared checkout, when other sessions may be
  active (see repo `AGENTS.md` / the `worktrees` skill).
- Report the PR URL when done; leave merging to the review bot.
