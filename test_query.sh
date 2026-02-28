#!/bin/bash

# Test the unified search query against the remote server
# Uses the mcj-emergent server with IMDB project

PROJECT_ID="dfe2febb-1971-4325-8f97-c816c6609f6d"
ORG_ID="c9bfa6d1-dc9f-4c3b-ac37-7a0411a0beba"
SERVER="https://api.dev.emergent-company.ai"

# Get auth token from environment or config
if [ -f ~/.emergent/credentials.json ]; then
    TOKEN=$(jq -r '.access_token' ~/.emergent/credentials.json 2>/dev/null)
fi

if [ -z "$TOKEN" ]; then
    echo "Error: No authentication token found"
    echo "Please run 'emergent login' first"
    exit 1
fi

# Test query: "who directed fight club and what are the other movies of this director"
QUERY="who directed fight club and what are the other movies of this director"

echo "Testing unified search query..."
echo "Project: IMDB ($PROJECT_ID)"
echo "Query: $QUERY"
echo ""

curl -X POST "$SERVER/api/search/unified" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Org-ID: $ORG_ID" \
  -H "X-Project-ID: $PROJECT_ID" \
  -d "{
    \"query\": \"$QUERY\",
    \"fusionStrategy\": \"weighted\",
    \"resultTypes\": \"both\",
    \"limit\": 5,
    \"includeDebug\": true
  }" | jq '.'
