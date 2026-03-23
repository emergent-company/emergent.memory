#!/usr/bin/env bash
# issue-agent/run.sh
#
# Polls GitHub for new issues and triggers an OpenCode session for each one.
# Designed to run as a crontab entry, e.g.:
#   */5 * * * * /root/emergent.memory/tools/issue-agent/run.sh >> /var/log/issue-agent.log 2>&1
#
# State is tracked in STATE_FILE (one issue number per line = already processed).
# OpenCode sessions are created via: opencode github run --event <json>
#
# How opencode github run works (from source):
#   - --event must be a @actions/github Context object with an "eventName" field
#   - For "issues" events, PROMPT env var is required (no comment body to extract)
#   - MODEL env var is required (format: "provider/model")
#   - GITHUB_RUN_ID env var is required
#   - USE_GITHUB_TOKEN=true skips the OIDC token exchange and uses GITHUB_TOKEN directly

set -euo pipefail

# ---------------------------------------------------------------------------
# Config (override via environment or edit here)
# ---------------------------------------------------------------------------
REPO="${REPO:-emergent-company/emergent.memory}"
STATE_FILE="${STATE_FILE:-/root/emergent.memory/tools/issue-agent/.processed-issues}"
OPENCODE_BIN="${OPENCODE_BIN:-/root/.opencode/bin/opencode}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"          # gh CLI auth used as fallback
MODEL="${MODEL:-github-copilot/claude-sonnet-4.6}"
LOG_PREFIX="[issue-agent]"
# Optional: only process issues with this label (leave empty to process all)
LABEL_FILTER="${LABEL_FILTER:-}"
# ---------------------------------------------------------------------------

log() { echo "$(date -u +"%Y-%m-%dT%H:%M:%SZ") $LOG_PREFIX $*"; }

# Ensure state file exists
touch "$STATE_FILE"

# Resolve GitHub token: prefer explicit env var, fall back to gh CLI token
if [[ -z "$GITHUB_TOKEN" ]]; then
  GITHUB_TOKEN=$(gh auth token 2>/dev/null || true)
fi
if [[ -z "$GITHUB_TOKEN" ]]; then
  log "ERROR: no GitHub token available. Set GITHUB_TOKEN or run 'gh auth login'."
  exit 1
fi

# Resolve the authenticated GitHub username (used as actor for permission checks)
GH_ACTOR=$(gh api user --jq '.login' 2>/dev/null || true)
if [[ -z "$GH_ACTOR" ]]; then
  log "ERROR: could not resolve GitHub username. Is 'gh auth login' done?"
  exit 1
fi
log "Authenticated as: $GH_ACTOR"

# Ensure opencode is available
if [[ ! -x "$OPENCODE_BIN" ]]; then
  log "ERROR: opencode not found at $OPENCODE_BIN"
  exit 1
fi

# ---------------------------------------------------------------------------
# Fetch open issues
# ---------------------------------------------------------------------------
LABEL_ARG=""
if [[ -n "$LABEL_FILTER" ]]; then
  LABEL_ARG="--label $LABEL_FILTER"
fi

# shellcheck disable=SC2086
ISSUES=$(gh issue list \
  --repo "$REPO" \
  --state open \
  --limit 50 \
  --json number,title,body,url,createdAt,labels \
  $LABEL_ARG 2>/dev/null)

ISSUE_COUNT=$(echo "$ISSUES" | jq 'length')
log "Fetched $ISSUE_COUNT open issues from $REPO"

if [[ "$ISSUE_COUNT" -eq 0 ]]; then
  log "No open issues. Exiting."
  exit 0
fi

# ---------------------------------------------------------------------------
# Process each new issue
# ---------------------------------------------------------------------------
NEW_COUNT=0

while IFS= read -r issue; do
  NUMBER=$(echo "$issue" | jq -r '.number')
  TITLE=$(echo "$issue"  | jq -r '.title')
  BODY=$(echo "$issue"   | jq -r '.body // ""')
  URL=$(echo "$issue"    | jq -r '.url')
  CREATED=$(echo "$issue" | jq -r '.createdAt')

  # Skip already-processed issues
  if grep -qxF "$NUMBER" "$STATE_FILE"; then
    log "Skipping #$NUMBER (already processed)"
    continue
  fi

  log "New issue #$NUMBER: $TITLE"
  NEW_COUNT=$((NEW_COUNT + 1))

  # Build a @actions/github Context object — this is what opencode github run expects.
  # Key fields:
  #   eventName  — the GitHub event name (read by opencode to route logic)
  #   payload    — the webhook payload (issue data lives here)
  #   repo       — { owner, repo } parsed from the repository full_name
  OWNER=$(echo "$REPO" | cut -d'/' -f1)
  REPO_NAME=$(echo "$REPO" | cut -d'/' -f2)

  EVENT_JSON=$(jq -n \
    --arg eventName "issues" \
    --argjson number "$NUMBER" \
    --arg title "$TITLE" \
    --arg body "$BODY" \
    --arg url "$URL" \
    --arg created_at "$CREATED" \
    --arg owner "$OWNER" \
    --arg repo_name "$REPO_NAME" \
    --arg full_name "$REPO" \
    --arg actor "$GH_ACTOR" \
    '{
      eventName: $eventName,
      action: "opened",
      payload: {
        action: "opened",
        issue: {
          number: $number,
          title: $title,
          body: $body,
          html_url: $url,
          created_at: $created_at,
          state: "open"
        },
        repository: {
          full_name: $full_name,
          name: $repo_name,
          owner: { login: $owner }
        }
      },
      repo: { owner: $owner, repo: $repo_name },
      actor: $actor
    }')

  # PROMPT is required for "issues" events (no comment body to extract from)
  ISSUE_PROMPT="Investigate GitHub issue #${NUMBER}: ${TITLE}

${BODY}

Analyse the codebase, identify the root cause, and either fix it or provide a detailed explanation with relevant file paths and suggested next steps."

  log "Running opencode for issue #$NUMBER..."

  # Required env vars for opencode github run:
  #   MODEL          — provider/model string
  #   GITHUB_RUN_ID  — any unique ID (used for run URL construction)
  #   GITHUB_TOKEN   — the token to use for GitHub API calls
  #   USE_GITHUB_TOKEN=true — skip OIDC exchange, use GITHUB_TOKEN directly
  #   PROMPT         — the task prompt (required for issues events)
  if MODEL="$MODEL" \
     GITHUB_RUN_ID="issue-agent-${NUMBER}-$(date +%s)" \
     GITHUB_TOKEN="$GITHUB_TOKEN" \
     USE_GITHUB_TOKEN=true \
     PROMPT="$ISSUE_PROMPT" \
     "$OPENCODE_BIN" github run \
       --event "$EVENT_JSON" \
       --print-logs 2>>"${STATE_FILE%.processed-issues}.opencode.log"; then
    log "OpenCode session completed for #$NUMBER"
    echo "$NUMBER" >> "$STATE_FILE"
  else
    EXIT_CODE=$?
    log "ERROR: OpenCode failed for #$NUMBER (exit $EXIT_CODE). Will retry next run."
  fi

done < <(echo "$ISSUES" | jq -c '.[]')

log "Done. Processed $NEW_COUNT new issue(s)."
