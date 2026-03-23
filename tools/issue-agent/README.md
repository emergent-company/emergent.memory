# Issue Agent

A bash script that polls GitHub for new issues and comments, and triggers an OpenCode session for each one. No LLM is involved in the polling — it's pure shell. OpenCode does the actual work.

## How It Works

```
cron (every 5 min)
    │
    ▼
run.sh
    │
    ├── gh issue list → for each open issue:
    │       │
    │       ├── issue number NOT in .processed-issues?
    │       │       → write to state, fire opencode session (background)
    │       │
    │       └── for each comment on the issue:
    │               "NUMBER:COMMENT_ID" NOT in .processed-comments?
    │                   → write to state, fire opencode session (background)
    │
    └── done (opencode sessions run independently)
```

Each trigger fires `opencode run` in the background and returns immediately. The script never waits for a session to finish. OpenCode picks up the repo context from the working directory and works autonomously.

## State Files

Two append-only plain text files in `tools/issue-agent/`:

| File | Format | Purpose |
|---|---|---|
| `.processed-issues` | one issue number per line | tracks which issues have been triggered |
| `.processed-comments` | `ISSUE_NUMBER:COMMENT_ID` per line | tracks which comments have been triggered |

State is written **before** the session is fired. This means:
- If the script crashes, it won't re-trigger the same event next run
- Each issue and each comment triggers OpenCode **exactly once**

## Setup

### Prerequisites

- `opencode` installed at `~/.opencode/bin/opencode`
- `gh` CLI authenticated (`gh auth login`)
- `jq` installed

### Run manually

```bash
cd /root/emergent.memory
bash tools/issue-agent/run.sh
```

Must be run from the repo root — OpenCode uses the working directory to find the project.

### Run on a schedule (crontab)

```bash
crontab -e
```

Add:
```
*/5 * * * * cd /root/emergent.memory && bash tools/issue-agent/run.sh >> /var/log/issue-agent.log 2>&1
```

## Configuration

All config is at the top of `run.sh` and can be overridden via environment variables:

| Variable | Default | Description |
|---|---|---|
| `REPO` | `emergent-company/emergent.memory` | GitHub repo to poll |
| `DIR` | `/root/emergent.memory` | Repo root — OpenCode runs from here |
| `STATE_DIR` | `${DIR}/tools/issue-agent` | Where state files are stored |
| `OPENCODE_BIN` | `~/.opencode/bin/opencode` | Path to opencode binary |
| `MODEL` | `github-copilot/claude-sonnet-4.6` | Model passed to `opencode run --model` |
| `LABEL_FILTER` | _(empty)_ | Only process issues with this label |

Example — only process issues labelled `opencode`:
```bash
LABEL_FILTER=opencode bash tools/issue-agent/run.sh
```

## What OpenCode Receives

**New issue prompt:**
```
GitHub issue #115 has been opened in emergent-company/emergent.memory.

Title: schemas: multiple relationship entries with same name not registered
URL: https://github.com/emergent-company/emergent.memory/issues/115

<issue body>

Investigate this issue in the codebase. Identify the root cause and either
fix it or provide a detailed analysis with relevant file paths and suggested
next steps.
```

**New comment prompt:**
```
A new comment has been posted on GitHub issue #115 (...) in emergent-company/emergent.memory.

Comment by @mkucharz:
<comment body>

Issue URL: https://github.com/...

Review this comment in the context of the issue and respond appropriately —
if it requests changes, implement them; if it's a question, answer it; if
it's new information, investigate further.
```

Each session is independent — OpenCode reads the full repo, reasons about the prompt, and works autonomously. Sessions are visible in the OpenCode server UI.

## Logs

```bash
tail -f /var/log/issue-agent.log
```

Example output:
```
2026-03-23T13:30:42Z [issue-agent] Fetched 7 open issues from emergent-company/emergent.memory
2026-03-23T13:30:42Z [issue-agent] New issue #116: cli: graph relationships list missing --cursor flag
2026-03-23T13:30:43Z [issue-agent] Triggered session: issue #116: cli: graph relationships list... (pid 3132215)
2026-03-23T13:30:43Z [issue-agent] New comment on #115 by @mkucharz (IC_kwDORKVfDM71AUX-)
2026-03-23T13:30:44Z [issue-agent] Triggered session: issue #115: comment by @mkucharz (pid 3132301)
2026-03-23T13:30:44Z [issue-agent] Done.
```

## Resetting State

To re-process an issue (e.g. for testing):

```bash
# Remove a specific issue from state
sed -i '/^115$/d' tools/issue-agent/.processed-issues

# Remove a specific comment
sed -i '/^115:IC_kwDORKVfDM71AUX-$/d' tools/issue-agent/.processed-comments

# Reset everything
> tools/issue-agent/.processed-issues
> tools/issue-agent/.processed-comments
```
