#!/usr/bin/env bash
# extract_threads.sh — Fetch all review threads for a PR and emit a TSV.
#
# Usage:
#   bash extract_threads.sh <OWNER> <REPO> <PR_NUMBER> [output_file]
#
# Output columns (tab-separated):
#   node_id  comment_db_id  is_resolved  is_outdated  author  path  line  body
#
# The output file defaults to threads.tsv in the current directory.
# The body field has newlines replaced with \n so the file stays single-line per thread.
#
# After running this script, fill in reply text for each thread that needs a reply,
# then pass the file to reply_and_resolve.sh.

set -euo pipefail

OWNER="${1:?Usage: extract_threads.sh <OWNER> <REPO> <PR_NUMBER> [output_file]}"
REPO="${2:?}"
PR_NUMBER="${3:?}"
OUTPUT="${4:-threads.tsv}"

echo "Fetching review threads for $OWNER/$REPO PR #$PR_NUMBER ..."

RESULT=$(gh api graphql -f query="
{
  repository(owner: \"$OWNER\", name: \"$REPO\") {
    pullRequest(number: $PR_NUMBER) {
      reviewThreads(first: 100) {
        totalCount
        nodes {
          id
          isResolved
          isOutdated
          comments(first: 1) {
            nodes {
              databaseId
              author { login }
              path
              line
              body
            }
          }
        }
      }
    }
  }
}")

TOTAL=$(echo "$RESULT" | jq -r '.data.repository.pullRequest.reviewThreads.totalCount')
echo "Total threads: $TOTAL"

# Write TSV header
printf 'node_id\tcomment_db_id\tis_resolved\tis_outdated\tauthor\tpath\tline\tbody\treply_body\n' > "$OUTPUT"

# Write one row per thread
echo "$RESULT" | jq -r '
  .data.repository.pullRequest.reviewThreads.nodes[] |
  [
    .id,
    (.comments.nodes[0].databaseId | tostring),
    (.isResolved | tostring),
    (.isOutdated | tostring),
    (.comments.nodes[0].author.login // "unknown"),
    (.comments.nodes[0].path // ""),
    (.comments.nodes[0].line | tostring),
    # Collapse newlines so body stays on one TSV line
    (.comments.nodes[0].body | gsub("\n"; "\\n") | gsub("\t"; " ")),
    # reply_body column — empty, to be filled in by the agent
    ""
  ] | @tsv
' >> "$OUTPUT"

echo "Written to $OUTPUT"
echo ""
echo "Next steps:"
echo "  1. Open $OUTPUT and fill in the 'reply_body' column for each thread"
echo "     that needs a reply. Leave blank to skip reply (resolve only)."
echo "  2. Run: bash reply_and_resolve.sh $OWNER/$REPO $PR_NUMBER $OUTPUT"
