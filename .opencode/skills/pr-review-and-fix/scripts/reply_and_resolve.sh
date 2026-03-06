#!/usr/bin/env bash
# reply_and_resolve.sh — Batch reply to PR review threads and resolve them.
#
# Usage:
#   bash reply_and_resolve.sh <OWNER/REPO> <PR_NUMBER> <threads_tsv>
#
# The TSV must have these columns (as produced by extract_threads.sh):
#   node_id  comment_db_id  is_resolved  is_outdated  author  path  line  body  reply_body
#
# Behaviour per row:
#   - is_resolved=true   → skip entirely (already done)
#   - reply_body non-empty → POST a reply to the thread
#   - node_id non-empty   → resolve the thread via GraphQL mutation
#     (regardless of whether a reply was posted)
#
# Skips the header row automatically.

set -euo pipefail

REPO="${1:?Usage: reply_and_resolve.sh <OWNER/REPO> <PR_NUMBER> <threads_tsv>}"
PR_NUMBER="${2:?}"
TSV="${3:?}"

REPLIED=0
RESOLVED=0
SKIPPED=0
ERRORS=0

echo "Processing threads from $TSV for $REPO PR #$PR_NUMBER ..."
echo ""

# Read TSV, skip header
tail -n +2 "$TSV" | while IFS=$'\t' read -r node_id comment_db_id is_resolved is_outdated author path line body reply_body; do
  label="[$author] $path:$line"

  # Skip already-resolved threads
  if [[ "$is_resolved" == "true" ]]; then
    echo "  SKIP (already resolved)  $label"
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  # Post reply if reply_body is non-empty
  if [[ -n "${reply_body:-}" ]]; then
    echo "  REPLY  $label"
    echo "         → $reply_body"
    if gh api --method POST \
        "repos/$REPO/pulls/$PR_NUMBER/comments/$comment_db_id/replies" \
        --field body="$reply_body" \
        --silent; then
      REPLIED=$((REPLIED + 1))
    else
      echo "  ERROR: reply failed for comment $comment_db_id"
      ERRORS=$((ERRORS + 1))
    fi
  fi

  # Resolve the thread
  if [[ -n "${node_id:-}" ]]; then
    echo "  RESOLVE  $label  ($node_id)"
    if gh api graphql -f query="
mutation {
  resolveReviewThread(input: {threadId: \"$node_id\"}) {
    thread { id isResolved }
  }
}" --silent; then
      RESOLVED=$((RESOLVED + 1))
    else
      echo "  ERROR: resolve failed for thread $node_id"
      ERRORS=$((ERRORS + 1))
    fi
  fi

  echo ""
done

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Done."
echo "  Replied:  $REPLIED"
echo "  Resolved: $RESOLVED"
echo "  Skipped:  $SKIPPED (already resolved)"
if [[ $ERRORS -gt 0 ]]; then
  echo "  ERRORS:   $ERRORS  ← check output above"
fi
