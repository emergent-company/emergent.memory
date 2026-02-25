---
name: commit
description: Stage, commit, and push changes. In default mode commits only files related to the current working feature (inferred from conversation context). Pass "all" to commit everything. Updates .gitignore before staging if untracked files look like they should be ignored.
metadata:
  author: emergent
  version: "1.0"
---

Stage relevant changes, update .gitignore if needed, commit with a conventional message, and push.

**Input**: Optional modifier — `all` to commit everything, or a short description of scope (e.g. `whisper timeout changes`). Without input, infer scope from conversation context.

---

## Steps

### 1. Capture working tree state

Run these in parallel:
```bash
git status --short
git diff --stat HEAD
git log --oneline -5
```

Parse the output into three buckets:
- **Modified/staged** — files with `M`, `A`, `D`, `R` status
- **Untracked** — files with `??` status
- **Branch** — current branch name (`git rev-parse --abbrev-ref HEAD`)

### 2. Determine scope: feature mode vs. all mode

**All mode** — use when:
- User passed `all` as argument
- User said "commit everything", "commit all", "commit all changes"

**Feature mode** (default) — use when:
- No argument given, or user gave a feature description
- Infer which files belong to the current feature by:
  1. Looking at files the agent touched in this conversation
  2. Looking at files in the same directory/package as recent edits
  3. Looking at the branch name for a theme (e.g. `whisper-timeout` → files under `pkg/whisper/`, `domain/extraction/`)
  4. If still ambiguous, show the modified files grouped by directory and use **AskUserQuestion** to let the user select which groups to include

### 3. Triage untracked files for .gitignore

For every `??` entry in `git status`, classify it:

**Should be ignored** (offer to add to .gitignore):
- Build outputs: `dist/`, `build/`, `*.o`, `*.a`, `*.so`, `*.exe`, `*.bin`, binaries with no extension in `dist/`
- Dependency directories: `node_modules/`, `vendor/` (if not intentionally committed)
- Generated files: `coverage/`, `*.out`, `*.prof`, `*.test`, `lcov.info`, `__pycache__/`, `*.pyc`
- Secrets & env: `*.env`, `.env.*` (except `.env.example`), `*.key`, `*.pem`, `credentials.json`, `secrets/`
- Editor/OS: `.DS_Store`, `Thumbs.db`, `.idea/`, `.vscode/` (if not already partially tracked)
- Logs: `*.log`, `logs/`
- Temp: `*.tmp`, `*.swp`, `*.bak`, `~*`

**Should be committed** (stage normally):
- Source files the user intentionally created (`.go`, `.ts`, `.md`, config files, etc.)
- New skill files, scripts, docs

**Ambiguous** — ask the user:
- Any untracked file not matching above patterns
- Directories with mixed content

If any "should be ignored" files are found:
1. Show the user what would be added to `.gitignore`
2. Use **AskUserQuestion**: "These untracked files look like they should be ignored. Add them to .gitignore?" (Yes / No / Let me decide each one)
3. If Yes: append the patterns to `.gitignore` (group them with a comment `# auto-added by commit skill — <date>`)
4. Stage `.gitignore` as part of this commit

### 4. Stage files

**All mode**: `git add -A` — but first warn about any secrets patterns found in step 3 and require explicit confirmation before including them.

**Feature mode**: Stage only the inferred feature files explicitly by path:
```bash
git add <file1> <file2> <dir/>...
```
Do NOT use `git add -A` in feature mode — be surgical.

If `.gitignore` was updated in step 3, always include it: `git add .gitignore`

After staging, run `git diff --cached --stat` and show the user what's about to be committed.

### 5. Generate commit message

Analyse the staged diff to write a **conventional commit** message:

**Format**: `<type>(<scope>): <description>`

Types:
- `feat` — new functionality
- `fix` — bug fix
- `chore` — tooling, config, version bumps, non-functional changes
- `refactor` — restructuring without behavior change
- `docs` — documentation only
- `test` — tests only
- `ci` — CI/CD workflows

Scope: the domain or package being changed (e.g. `whisper`, `scheduler`, `cli`, `extraction`) — omit if changes span many unrelated areas.

Description: imperative, lowercase, no period, max 72 chars.

If the diff is large or spans multiple unrelated areas, consider splitting into a short subject + body:
```
feat(whisper): add size-based timeout and single-job mode for large files

- TimeoutForSize() computes max(default, size/bytesPerSec × safety)
- poll() switches to batch=1 when large files are pending
- StaleJobCleanupTask uses 8h threshold for document_parsing_jobs
```

Show the proposed message and ask for confirmation or edits via **AskUserQuestion** before committing.

### 6. Commit

```bash
git commit -m "<message>"
```

The pre-commit hook validates Swagger annotations on `apps/server-go/domain/*/handler.go` files. If the hook fails:
- Read the error carefully
- Fix the missing `// @Router` annotations
- Re-stage the fixed files
- Retry the commit (do NOT use `--no-verify` unless the user explicitly asks)

### 7. Push

```bash
git push origin <branch>
```

If the branch has no upstream yet:
```bash
git push -u origin <branch>
```

If push is rejected (non-fast-forward), do NOT force-push. Instead:
- Run `git pull --rebase origin <branch>`
- Then retry the push
- If there are conflicts, surface them to the user

### 8. Report

```
Committed and pushed.

Commit: <hash> (<branch>)
Message: <commit message>

Files committed (<N>):
  <list of files with + / - line counts>
```

---

## Guardrails

- **Never commit secrets** — if a staged file matches `.env*`, `*.key`, `*.pem`, `credentials*`, `secrets/`, stop and warn the user before proceeding
- **Never force-push** to `main`/`master` — warn the user if they ask for it
- **Never use `--no-verify`** unless the user explicitly says to skip hooks
- **Feature mode is the default** — don't use `git add -A` unless the user said "all"
- **Show what's staged before committing** — always confirm the `git diff --cached --stat` output with the user if it's more than 10 files or includes unexpected directories
- **One commit per invocation** — don't silently create multiple commits; if the changes clearly belong to two unrelated areas, ask the user which to commit first
- If the working tree is already clean (nothing to commit), say so and stop
