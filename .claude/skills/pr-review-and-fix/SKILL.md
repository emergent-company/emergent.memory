---
name: pr-review-and-fix
description: Address PR review comments from all bots and reviewers (CodeRabbit, Copilot, Gemini, etc.), implement fixes, reply to threads with commit references, and resolve threads. Use when the user wants to work through open PR comments.
license: MIT
metadata:
  author: opencode
  version: "1.1"
---

# Skill: pr-review-and-fix

Address all open PR review comments: read them, triage them, implement fixes, commit,
reply to each thread with the commit reference, and resolve threads.

**Input**: PR number (e.g. `51`) or a GitHub PR URL. If omitted, infer from conversation
context (current branch → `gh pr view`) or ask.

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

**Option A — use the bundled script (recommended, saves tokens):**
```bash
bash .agents/skills/pr-review-and-fix/scripts/extract_threads.sh <OWNER> <REPO> <PR_NUMBER>
```
This outputs `threads.tsv` with columns: `node_id`, `comment_db_id`, `is_resolved`,
`is_outdated`, `author`, `path`, `line`, `body`. Use this file for all triage and
reply/resolve steps.

**Option B — raw GraphQL (if script unavailable):**
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
| `isResolved: true` | Already resolved | Skip entirely |
| `isOutdated: true, isResolved: false` | Code changed under comment | Resolve without fix (reply optional — see below) |
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
references a Go struct, read the actual struct definition to verify correctness).

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
  spurious change — instead just reply explaining why no change is needed
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
   | `**/*.go` | `go build ./...` and `go test ./...` |
   | `**/*.ts`, `**/*.tsx` | `tsc --noEmit` or `npm run typecheck` |
   | `**/*.swift` | `xcodebuild build` (signing disabled) |
   | `**/*.py` | `python -m py_compile <file>` |
   | `**/*.rb` | `ruby -c <file>` |
   | `**/*.sh` | `bash -n <file>` |
   | `.github/workflows/*.yml` | YAML syntax check (`python -c "import yaml; yaml.safe_load(open('file'))"`) |
   | `package.json`, `go.mod` | verify no broken deps (`npm install --dry-run` / `go mod tidy`) |

3. If a check fails:
   - Fix the issue before committing
   - Re-run the check to confirm it passes
   - Do NOT commit with `--no-verify` unless the user explicitly requests it

---

### 7. Commit fixes

After checks pass, commit:

```bash
git add <specific files>
git commit -m "Address PR review comments: <summary>

<bullet list of what was fixed and which reviewer raised it>"
```

Push to the PR branch:
```bash
git push
```

Note the commit SHA (`git rev-parse --short HEAD`) — you will use it in replies.

---

### 8. Prepare reply text for each thread

Before posting any replies, write out the reply body for every thread using the
templates below. This makes the reply/resolve step mechanical.

#### Reply templates

**Thread was fixed by a commit:**
```
Fixed in <sha>: <one-line description of what changed>.
```

**Thread was already correct — comment was wrong:**
```
No change needed: <explanation of why the existing code/docs are correct>.
```

**Thread was already handled in a prior commit (before this session):**
```
Already addressed in <sha>: <one-line description>. No further change needed.
```

**Outdated thread — code changed and concern no longer applies:**
```
Resolved — this section was updated in <sha>.
```
> Note: for outdated threads, a reply is **optional**. If the thread is purely
> mechanical (e.g. a formatting nit that got swept up in a larger change),
> skip the reply and just resolve. Only reply if the context is useful.

**Nitpick / informational — acknowledged but not acted on:**
```
Acknowledged. Leaving as-is for now — <brief reason>.
```

---

### 9. Reply to threads and resolve them

**Option A — use the bundled script (recommended, saves tokens):**

1. Fill in `threads.tsv` (output of step 2 script) with a `reply_body` column
   for each thread that needs a reply. Leave blank to skip reply and only resolve.

2. Run:
   ```bash
   bash .agents/skills/pr-review-and-fix/scripts/reply_and_resolve.sh <OWNER>/<REPO> <PR_NUMBER> threads.tsv
   ```

   The script will:
   - POST a reply for every row that has a non-empty `reply_body`
   - Resolve every thread whose `node_id` is in the file
   - Skip rows where `is_resolved` is already `true`
   - Print a summary of what was done

**Option B — manual (if script unavailable):**

Post a reply using the REST API:
```bash
gh api --method POST \
  repos/<OWNER>/<REPO>/pulls/<PR_NUMBER>/comments/<COMMENT_DATABASE_ID>/replies \
  --field body="<reply text>"
```

> **IMPORTANT**: The correct endpoint includes the PR number:
> `pulls/<PR_NUMBER>/comments/<ID>/replies`
> NOT `pulls/comments/<ID>/replies` — that returns 404.

Use the `databaseId` of the **first comment** in the thread as `<COMMENT_DATABASE_ID>`.

Then resolve the thread via GraphQL:
```bash
gh api graphql -f query='
mutation {
  resolveReviewThread(input: {threadId: "<THREAD_NODE_ID>"}) {
    thread { id isResolved }
  }
}'
```

#### What to resolve

| Thread type | Resolve? |
|---|---|
| Fixed by commit | Yes |
| Already correct (no change) | Yes |
| Outdated | Yes |
| Nitpick / deferred by user | No — leave open |

---

### 10. Final status report

```
## PR #<N> — Review Complete

### Fixed & Resolved
- [x] [coderabbitai] .github/workflows/docs.yml — separate deploy job (commit abc1234)
- [x] [copilot] docs/site/go-sdk/reference/chat.md:86 — fix StreamEvent fields (commit abc1234)
...

### Resolved (outdated, no fix needed)
- [x] [coderabbitai] apps/server/pkg/sdk/... — outdated after prior commit

### Skipped (nitpick, deferred per user)
- [ ] [gemini] docs/llms.md:42 — minor suggestion

### Remaining Open
<list any threads intentionally left open>

**Commits pushed:** <list SHAs>
```

---

## How to find the PR repo owner and name

Always derive `<OWNER>` and `<REPO>` from the git remote, not from hardcoded values:

```bash
gh repo view --json owner,name -q '"\(.owner.login)/\(.name)"'
```

---

## Reading CodeRabbit comments correctly

CodeRabbit writes structured Markdown in comment bodies. The actionable fix is
usually after a `**<description>**` heading or a `_⚠️ Potential issue_` badge.
Ignore the `<details>` script blocks (those are CodeRabbit's internal analysis
chains) — focus on the plain text above them. Example pattern:

```
_⚠️ Potential issue_ | _🟠 Major_

**Do not print full API token in docs examples.**

Logging `resp.Token` leaks a credential...
```

The fix instruction is the bold paragraph. The severity badge (`🟠 Major`,
`🟡 Minor`) maps to Must Fix and Should Fix respectively.

---

## Handling outdated threads

Outdated threads (`isOutdated: true`) mean the code under the comment has
already changed (e.g. due to a prior fix commit). They should be resolved
without further code changes:

1. Confirm the old concern is no longer present in the current file
2. Reply (optional): `"Resolved — this section was updated in <commit-sha>."`
   Skip the reply entirely if the thread is purely mechanical — no one will
   read it and it wastes tokens.
3. Resolve the thread via GraphQL

---

## Guardrails

- Always read the live file before making a fix — never guess from the diff hunk alone
- Never commit files unrelated to the PR review comments in scope
- Do not squash or amend commits that have already been pushed
- If a reviewer's comment contradicts another reviewer's comment, pause and ask the user
- Prefer one commit per logical group of fixes over one commit per comment
- Always run pre-commit checks before committing (step 6) — load the
  `pre-commit-check` skill if available
- Never use `git commit --no-verify` unless the user explicitly says to skip hooks
- The reply endpoint MUST include the PR number: `pulls/<PR_NUMBER>/comments/<ID>/replies`
