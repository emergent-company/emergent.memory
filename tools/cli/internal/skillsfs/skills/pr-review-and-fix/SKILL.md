---
name: pr-review-and-fix
description: Address PR review comments from all bots and reviewers (CodeRabbit, Copilot, Gemini, etc.), implement fixes, run pre-commit checks, commit, reply to threads with commit references, and resolve threads. Use when the user wants to work through open PR comments.
license: MIT
metadata:
  author: opencode
  version: "1.0"
---

# Skill: pr-review-and-fix

Address all open PR review comments: read them, triage them, implement fixes,
verify with the project's pre-commit checks, commit, reply to each thread with
the commit reference, and resolve threads.

**Input**: PR number (e.g. `51`) or a GitHub PR URL. If omitted, infer from
conversation context (current branch → `gh pr view`) or ask.

---

## Steps

### 1. Resolve the PR

If no PR number is given, run:
```bash
gh pr view --json number,url,headRefName
```
Use the returned number for all subsequent steps. Announce: "Working on PR #<N>".

---

### 2. Fetch all review threads

Use the GraphQL API — it is the authoritative source because it exposes both
`isResolved` and `isOutdated` per thread, and gives you the node ID needed to
resolve threads later.

```bash
gh api graphql -f query='
{
  repository(owner: "<OWNER>", name: "<REPO>") {
    pullRequest(number: <N>) {
      reviewThreads(first: 100) {
        totalCount
        nodes {
          id
          isResolved
          isOutdated
          comments(first: 50) {
            nodes {
              databaseId
              author { login }
              createdAt
              body
              path
              line
              outdated
            }
          }
        }
      }
    }
  }
}'
```

Parse `owner` and `repo` from the git remote:
```bash
gh repo view --json owner,name
```

---

### 3. Triage threads

Build a triage table from the thread list. For each thread:

| State | Meaning | Action |
|---|---|---|
| `isResolved: true` | Already resolved | Skip |
| `isOutdated: true, isResolved: false` | Code changed under comment | Resolve without fix (see step 8) |
| `isResolved: false, isOutdated: false` | Open, actionable | Read and triage |

For open, non-outdated threads, classify each by severity using the comment body:
- **Must fix** — correctness errors, crashes, security issues, broken examples
- **Should fix** — logic improvements, missing error handling, style issues called out explicitly
- **Nitpick / informational** — optional suggestions, praise, questions already answered

Show the triage table to the user before proceeding:

```
## PR #<N> — Review Thread Triage

### Must Fix (<N> threads)
- [ ] [<reviewer>] <path>:<line> — <one-line summary>

### Should Fix (<N> threads)
- [ ] [<reviewer>] <path>:<line> — <one-line summary>

### Nitpick / Informational (<N> threads)
- [ ] [<reviewer>] <path>:<line> — <one-line summary>

### Outdated — will auto-resolve (<N> threads)
### Already Resolved (<N> threads)
```

Ask the user:
- "Proceed with all Must Fix + Should Fix? Or select specific threads?"

Wait for confirmation before implementing fixes. Default is to fix all Must Fix
and Should Fix unless the user says otherwise.

---

### 4. Read source files for each open thread

Before writing any code, read the actual current state of every file referenced
by open threads. Do not rely solely on the diff hunk in the comment — always
read the live file to understand current context.

For each thread, also read any related source files (e.g. if a docs example
references a struct, read the actual struct definition to verify correctness).

---

### 5. Implement fixes

Work through threads in priority order (Must Fix first, then Should Fix).

For each thread:
1. Read the comment body fully — understand what is being asked
2. Read the current file content at the affected location
3. Make the minimal targeted fix
4. Note the thread ID and path for the reply step

**Guardrails:**
- Keep each fix minimal and scoped — don't refactor unrelated code
- If a comment is factually wrong (the code is already correct), do not make a
  spurious change — instead prepare to reply explaining why no change is needed
- If a fix is ambiguous or has multiple valid approaches, pause and ask the user
- Do not fix nitpick threads unless the user explicitly requested it

Group related fixes when they touch the same file — commit by logical unit.

---

### 6. Run pre-commit checks before committing

Before staging and committing, run the project's validation checks to make sure
the fixes don't break anything.

**How to determine which checks to run:**

1. Check if a `pre-commit-check` skill exists in `.agents/skills/pre-commit-check/SKILL.md`.
   If it does, load and follow it — it contains the authoritative checks for this project.

2. If no project-specific skill exists, auto-detect based on what changed:

   | Changed files | Check to run |
   |---|---|
   | `*.go` files | `go build ./...` and `go test ./...` in the relevant module |
   | `*.go` handler files | also check for `// @Router` Swagger annotations |
   | `*.md` docs + `mkdocs.yml` | `mkdocs build --strict` |
   | `*.ts` / `*.tsx` | `tsc --noEmit` |
   | `*.yml` workflows | `python3 -c "import yaml; yaml.safe_load(open('<file>'))"` |
   | Swift / Xcode | `xcodebuild build` (project-specific) |

3. If a check fails:
   - Fix the issue before committing
   - Re-run the check to confirm it passes
   - Do NOT commit with `--no-verify` unless the user explicitly requests it

---

### 7. Commit fixes

After checks pass, commit:

```bash
git add <specific files only — not git add -A>
git commit -m "Address PR review comments: <summary>

- <what was fixed> (raised by <reviewer>)
- <what was fixed> (raised by <reviewer>)"
```

Push to the PR branch:
```bash
git push
```

Note the commit SHA (`git rev-parse --short HEAD`) — you will use it in replies.

---

### 8. Reply to threads

For every thread that was fixed (or where no fix was needed with explanation),
post a reply using the REST API:

```bash
gh api --method POST \
  repos/<OWNER>/<REPO>/pulls/comments/<COMMENT_DATABASE_ID>/replies \
  --field body="Fixed in <commit-sha>: <one-line description of the fix>"
```

Use the `databaseId` of the **first comment** in the thread as `<COMMENT_DATABASE_ID>`.

For threads where no code change was made (comment was incorrect or already handled):
```bash
--field body="No change needed: <explanation>"
```

---

### 9. Resolve threads

After replying, resolve each thread using the GraphQL node `id`:

```bash
gh api graphql -f query='
mutation {
  resolveReviewThread(input: {threadId: "<THREAD_NODE_ID>"}) {
    thread { id isResolved }
  }
}'
```

Resolve:
- All threads that were fixed
- All outdated threads (they no longer apply to current code)
- Threads where you explained why no change was needed

Do **not** resolve threads that the user asked you to skip or defer.

---

### 10. Final status report

```
## PR #<N> — Review Complete

### Fixed & Resolved (<N>)
- [x] [<reviewer>] <path>:<line> — <summary> (commit <sha>)
...

### Resolved — outdated (<N>)
- [x] [<reviewer>] <path> — outdated after <prior-sha>
...

### Skipped — nitpick / deferred (<N>)
- [ ] [<reviewer>] <path>:<line> — <summary>

### Remaining Open
<list any threads intentionally left open, or "none">

**Commits pushed:** <sha1> <sha2> ...
```

---

## Reference: How to find repo owner and name

Always derive from the git remote — never hardcode:
```bash
gh repo view --json owner,name -q '"\(.owner.login)/\(.name)"'
```

---

## Reference: Reading CodeRabbit comments correctly

CodeRabbit writes structured Markdown. The actionable instruction is the bold
paragraph immediately below the severity badge. Ignore everything inside
`<details>` blocks — those are CodeRabbit's internal analysis chains, not
instructions.

Pattern:
```
_⚠️ Potential issue_ | _🟠 Major_

**Do not print full API token in docs examples.**

Logging `resp.Token` leaks a credential...
```

Severity mapping:
- `🟠 Major` → Must Fix
- `🟡 Minor` → Should Fix
- `🔵 Nitpick` → Nitpick

---

## Reference: Handling outdated threads

Outdated threads (`isOutdated: true`) mean the line referenced by the comment
no longer exists in the current diff — the code was already changed. Handle them
without making any new code changes:

1. Confirm the old concern is no longer present in the current file
2. Reply: `"Resolved — this section was updated in <prior-commit-sha>."`
3. Resolve the thread via GraphQL

---

## Guardrails

- Always read the live file before making a fix — never rely solely on the diff hunk
- Never commit files unrelated to the PR review comments in scope
- Never amend or squash commits that have already been pushed
- If two reviewers contradict each other, pause and ask the user which to follow
- Prefer one commit per logical group of fixes over one commit per comment
- Always run pre-commit checks before committing (step 6)
- Never use `git commit --no-verify` unless the user explicitly says to skip hooks
