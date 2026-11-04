---
name: memory-issue-report
description: Diagnose a problem with the Memory platform (server, CLI, or agents), gather environment and log diagnostics, and file a GitHub issue with structured context. Use when something is broken, behaving unexpectedly, or producing errors.
metadata:
  author: emergent
  version: "1.0"
---

Create a GitHub issue on the `emergent-company/emergent.memory` repository with structured diagnostics gathered from the local environment.

## When to Use

- A CLI command fails or returns unexpected results
- The server returns errors or behaves incorrectly
- An agent run fails, hangs, or produces wrong output
- Embedding workers are stuck or not processing
- Any other platform misbehavior the user wants to report

## Steps

### 1. Understand the problem

Ask the user to describe the problem if they haven't already. Clarify:

- What they were trying to do
- What happened instead
- Whether it is reproducible

### 2. Gather diagnostics

Run these commands in parallel to collect environment context. Capture the output of each -- some may fail and that is fine (include the failure in the report).

```bash
# Version and config
memory --version
memory config show

# Server health
curl -s http://localhost:3012/health | python3 -m json.tool

# Recent server errors (last 50 lines)
tail -50 logs/server/server.error.log

# Recent server log (last 50 lines)
tail -50 logs/server/server.log

# Git state (to identify exact build)
git log --oneline -1
git diff --stat HEAD

# Embedding worker state
memory embeddings status

# Go version
go version
```

If the problem involves a specific command, re-run it with `--debug` to capture verbose output:

```bash
memory <failing-command> --debug 2>&1
```

If the problem involves an agent run, also gather:

```bash
memory agents get-run <run-id> --json
```

### 3. Check for known causes

Before filing, check whether the problem matches a known pattern:

| Symptom | Likely cause | Fix |
|---|---|---|
| `unknown command "X"` | Old CLI binary | `task cli:install` or `memory upgrade` on remote |
| `401 Unauthorized` | Expired token or missing API key | `memory auth login` or check API token |
| `connection refused` on localhost:3012 | Server not running | `task start` or `task dev` |
| Embedding workers "running" but nothing processed | Provider not configured or API key expired | `memory provider test <provider>` |
| `migration` errors in server.error.log | Missing migration | Check `apps/server/migrations/` for pending migrations |

If the problem matches a known cause, tell the user the fix instead of filing an issue. Only file an issue for genuine bugs or unexpected behavior.

### 4. Determine severity and label

| Severity | Criteria | Label |
|---|---|---|
| Bug | Something that previously worked is now broken, or documented behavior does not match actual behavior | `bug` |
| Enhancement | Feature works but could be improved, or error messages are confusing | `enhancement` |
| Question | Unclear whether it is a bug or expected behavior | `question` |

### 5. Draft the issue

Format the issue body using this template:

```markdown
## Problem

<1-3 sentences describing what is wrong>

## Steps to Reproduce

1. <step>
2. <step>
3. <step>

## Expected Behavior

<what should have happened>

## Actual Behavior

<what actually happened, including error messages>

## Environment

- **Memory CLI**: <output of `memory --version`>
- **Server**: <health endpoint version, or "unreachable">
- **Git commit**: <short hash from `git log --oneline -1`>
- **Go version**: <output of `go version`>
- **Platform**: <OS/arch>

## Diagnostics

<details>
<summary>Server health</summary>

```json
<health endpoint output>
```
</details>

<details>
<summary>Server error log (last 50 lines)</summary>

```
<error log tail>
```
</details>

<details>
<summary>Debug output</summary>

```
<debug output from failing command, if available>
```
</details>
```

Omit any diagnostics section that is empty or not relevant.

### 6. Confirm with the user

Show the drafted issue title and body to the user. Ask for confirmation before creating. The user may want to:

- Edit the title or description
- Add additional context
- Change the label
- Decide not to file after all

### 7. Create the issue

```bash
gh issue create \
  --repo emergent-company/emergent.memory \
  --title "<title>" \
  --label "<label>" \
  --body "$(cat <<'EOF'
<issue body>
EOF
)"
```

### 8. Report back

Print the issue URL so the user can track it.

## Rules

- Never file duplicate issues. Before creating, search existing issues:
  ```bash
  gh issue list --repo emergent-company/emergent.memory --search "<keywords>" --limit 10
  ```
  If a matching open issue exists, tell the user and link to it instead of creating a new one.
- Never include secrets, API keys, tokens, or passwords in the issue body. Redact them from any log output before posting.
- Always get user confirmation before creating the issue.
- Keep the title concise and descriptive (under 80 characters). Use the pattern: `<component>: <what is wrong>` (e.g. `cli: journal command not recognized on v0.35.86`, `embeddings: workers running but no jobs processed`).
- If the problem turns out to be user error or a known fix, help the user resolve it instead of filing an issue.
