#!/usr/bin/env bash
# issue-agent/run.sh
#
# Polls GitHub for new issues and new comments, triggers one OpenCode session
# per event. Never triggers the same event twice.
#
# State files (append-only, never deleted):
#   .processed-issues   — one issue number per line
#   .processed-comments — one "ISSUE_NUMBER:COMMENT_ID" per line
#
# Crontab (every 5 min):
#   */5 * * * * cd /root/emergent.memory && bash tools/issue-agent/run.sh >> /var/log/issue-agent.log 2>&1

set -euo pipefail

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
REPO="${REPO:-emergent-company/emergent.memory}"
DIR="${DIR:-/root/emergent.memory}"
STATE_DIR="${STATE_DIR:-${DIR}/tools/issue-agent}"
OPENCODE_BIN="${OPENCODE_BIN:-/root/.opencode/bin/opencode}"
MODEL="${MODEL:-github-copilot/claude-sonnet-4.6}"
LABEL_FILTER="${LABEL_FILTER:-}"
LOG_PREFIX="[issue-agent]"
# ---------------------------------------------------------------------------

ISSUES_STATE="${STATE_DIR}/.processed-issues"
COMMENTS_STATE="${STATE_DIR}/.processed-comments"

log() { echo "$(date -u +"%Y-%m-%dT%H:%M:%SZ") $LOG_PREFIX $*"; }

touch "$ISSUES_STATE" "$COMMENTS_STATE"

[[ ! -x "$OPENCODE_BIN" ]] && { log "ERROR: opencode not found at $OPENCODE_BIN"; exit 1; }

# ---------------------------------------------------------------------------
# Helper: fire an opencode session in the background, don't wait for it
# ---------------------------------------------------------------------------
trigger() {
  local prompt="$1"
  local title="$2"
  cd "$DIR" && "$OPENCODE_BIN" run \
    --model "$MODEL" \
    --title "$title" \
    --share \
    "$prompt" \
    > /dev/null 2>&1 &
  log "Triggered session: $title (pid $!)"
}

# ---------------------------------------------------------------------------
# Fetch open issues
# ---------------------------------------------------------------------------
LABEL_ARG=()
[[ -n "$LABEL_FILTER" ]] && LABEL_ARG=(--label "$LABEL_FILTER")

ISSUES=$(gh issue list \
  --repo "$REPO" \
  --state open \
  --limit 50 \
  --json number,title,body,url,comments \
  "${LABEL_ARG[@]}" 2>/dev/null)

ISSUE_COUNT=$(echo "$ISSUES" | jq 'length')
log "Fetched $ISSUE_COUNT open issues from $REPO"

# ---------------------------------------------------------------------------
# Process each issue
# ---------------------------------------------------------------------------
while IFS= read -r issue; do
  NUMBER=$(echo "$issue" | jq -r '.number')
  TITLE=$(echo "$issue"  | jq -r '.title')
  BODY=$(echo "$issue"   | jq -r '.body // "(no description)"')
  URL=$(echo "$issue"    | jq -r '.url')

  # ── New issue ─────────────────────────────────────────────────────────────
  if ! grep -qxF "$NUMBER" "$ISSUES_STATE"; then
    log "New issue #$NUMBER: $TITLE"
    echo "$NUMBER" >> "$ISSUES_STATE"

    trigger \
      "GitHub issue #${NUMBER} has been opened in ${REPO}.

Title: ${TITLE}
URL: ${URL}

${BODY}

Investigate this issue in the codebase. Identify the root cause and either fix it or provide a detailed analysis with relevant file paths and suggested next steps." \
      "issue #${NUMBER}: ${TITLE}"
  fi

  # ── New comments on any issue (open or already seen) ──────────────────────
  while IFS= read -r comment; do
    [[ -z "$comment" ]] && continue
    COMMENT_ID=$(echo "$comment"     | jq -r '.id')
    COMMENT_AUTHOR=$(echo "$comment" | jq -r '.author.login')
    COMMENT_BODY=$(echo "$comment"   | jq -r '.body')
    COMMENT_KEY="${NUMBER}:${COMMENT_ID}"

    if ! grep -qxF "$COMMENT_KEY" "$COMMENTS_STATE"; then
      log "New comment on #$NUMBER by @$COMMENT_AUTHOR ($COMMENT_ID)"
      echo "$COMMENT_KEY" >> "$COMMENTS_STATE"

      trigger \
        "A new comment has been posted on GitHub issue #${NUMBER} (${TITLE}) in ${REPO}.

Comment by @${COMMENT_AUTHOR}:
${COMMENT_BODY}

Issue URL: ${URL}

Review this comment in the context of the issue and respond appropriately — if it requests changes, implement them; if it's a question, answer it; if it's new information, investigate further." \
        "issue #${NUMBER}: comment by @${COMMENT_AUTHOR}"
    fi
  done < <(echo "$issue" | jq -c '.comments[]?' 2>/dev/null)

done < <(echo "$ISSUES" | jq -c '.[]')

log "Done."
