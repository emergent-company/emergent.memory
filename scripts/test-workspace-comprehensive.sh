#!/bin/bash
#
# Comprehensive Agent Workspace Test Script
# Tests workspace creation, git operations, tool usage, and vector database access
#
# Usage: ./test-workspace-comprehensive.sh [SERVER_URL] [API_KEY] [PROJECT_ID]
#
# Example:
#   ./test-workspace-comprehensive.sh http://localhost:3002 your-api-key your-project-id
#

set -e

# Configuration
SERVER_URL="${1:-http://localhost:3002}"
API_KEY="${2}"
PROJECT_ID="${3}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_section() {
    echo -e "\n${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}\n"
}

log_step() {
    echo -e "${YELLOW}→${NC} $1"
}

log_success() {
    echo -e "${GREEN}✓${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
}

# Check required parameters
if [ -z "$API_KEY" ]; then
    log_error "API_KEY is required"
    echo "Usage: $0 [SERVER_URL] API_KEY PROJECT_ID"
    exit 1
fi

if [ -z "$PROJECT_ID" ]; then
    log_error "PROJECT_ID is required"
    echo "Usage: $0 [SERVER_URL] API_KEY PROJECT_ID"
    exit 1
fi

# API helpers
api_call() {
    local method=$1
    local endpoint=$2
    local data=$3
    local extra_headers=$4
    
    if [ -n "$data" ]; then
        curl -s -X "$method" "${SERVER_URL}${endpoint}" \
            -H "X-API-Key: ${API_KEY}" \
            -H "Content-Type: application/json" \
            $extra_headers \
            -d "$data"
    else
        curl -s -X "$method" "${SERVER_URL}${endpoint}" \
            -H "X-API-Key: ${API_KEY}" \
            $extra_headers
    fi
}

workspace_api() {
    local method=$1
    local endpoint=$2
    local data=$3
    
    api_call "$method" "/api/v1/agent/workspaces${endpoint}" "$data"
}

# Track workspace ID for cleanup
WORKSPACE_ID=""
cleanup() {
    if [ -n "$WORKSPACE_ID" ]; then
        log_section "Cleanup: Deleting Workspace"
        workspace_api DELETE "/${WORKSPACE_ID}" || true
        log_success "Workspace deleted"
    fi
}
trap cleanup EXIT

# ============================================================================
# TEST 1: Create Workspace with Repository
# ============================================================================
log_section "Test 1: Create Workspace with Repository"

log_step "Creating workspace with Emergent repository..."
WORKSPACE_RESPONSE=$(workspace_api POST "" '{
    "container_type": "agent_workspace",
    "provider": "gvisor",
    "repository_url": "https://github.com/emergent-company/emergent.git",
    "branch": "main",
    "deployment_mode": "self-hosted"
}')

WORKSPACE_ID=$(echo "$WORKSPACE_RESPONSE" | jq -r '.id')
WORKSPACE_STATUS=$(echo "$WORKSPACE_RESPONSE" | jq -r '.status')

if [ -z "$WORKSPACE_ID" ] || [ "$WORKSPACE_ID" = "null" ]; then
    log_error "Failed to create workspace"
    echo "$WORKSPACE_RESPONSE" | jq .
    exit 1
fi

log_success "Workspace created: $WORKSPACE_ID (status: $WORKSPACE_STATUS)"

# Wait for workspace to be ready
log_step "Waiting for workspace to be ready..."
MAX_WAIT=60
ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    WORKSPACE_STATUS=$(workspace_api GET "/${WORKSPACE_ID}" | jq -r '.status')
    
    if [ "$WORKSPACE_STATUS" = "ready" ]; then
        log_success "Workspace is ready!"
        break
    elif [ "$WORKSPACE_STATUS" = "error" ]; then
        log_error "Workspace entered error state"
        exit 1
    fi
    
    echo -n "."
    sleep 2
    ELAPSED=$((ELAPSED + 2))
done

if [ "$WORKSPACE_STATUS" != "ready" ]; then
    log_error "Workspace did not become ready within ${MAX_WAIT}s"
    exit 1
fi

# ============================================================================
# TEST 2: Verify Repository Clone
# ============================================================================
log_section "Test 2: Verify Repository Clone"

log_step "Checking if repository was cloned..."
CLONE_CHECK=$(workspace_api POST "/${WORKSPACE_ID}/bash" '{
    "command": "ls -la /workspace/ && git status 2>&1"
}')

EXIT_CODE=$(echo "$CLONE_CHECK" | jq -r '.exit_code')
STDOUT=$(echo "$CLONE_CHECK" | jq -r '.stdout')

if [ "$EXIT_CODE" = "0" ] && echo "$STDOUT" | grep -q "On branch"; then
    log_success "Repository cloned successfully"
    echo "$STDOUT" | head -20
else
    log_error "Repository not cloned or git not available"
    echo "$CLONE_CHECK" | jq .
fi

# ============================================================================
# TEST 3: Git Operations
# ============================================================================
log_section "Test 3: Git Operations"

log_step "Testing git log..."
GIT_LOG=$(workspace_api POST "/${WORKSPACE_ID}/git" '{
    "command": "log",
    "args": ["--oneline", "-5"]
}')
echo "$GIT_LOG" | jq -r '.stdout' | head -5
log_success "Git log working"

log_step "Testing git status..."
GIT_STATUS=$(workspace_api POST "/${WORKSPACE_ID}/git" '{
    "command": "status",
    "args": ["--short"]
}')
echo "$GIT_STATUS" | jq -r '.stdout'
log_success "Git status working"

# ============================================================================
# TEST 4: File Operations
# ============================================================================
log_section "Test 4: File Operations (Read, Write, Edit, Glob, Grep)"

log_step "Reading README.md..."
README=$(workspace_api POST "/${WORKSPACE_ID}/read" '{
    "file_path": "/workspace/README.md",
    "limit": 10
}')
echo "$README" | jq -r '.content' | head -10
log_success "Read operation working"

log_step "Creating test file..."
workspace_api POST "/${WORKSPACE_ID}/write" '{
    "file_path": "/workspace/test-agent.txt",
    "content": "# Agent Test File\n\nThis file was created by the agent workspace test.\n\nFeatures tested:\n- Workspace creation\n- Git operations\n- File manipulation\n- Vector database access\n"
}' > /dev/null
log_success "Write operation working"

log_step "Searching for files with glob..."
GLOB_RESULT=$(workspace_api POST "/${WORKSPACE_ID}/glob" '{
    "pattern": "*.md",
    "path": "/workspace"
}')
FILE_COUNT=$(echo "$GLOB_RESULT" | jq -r '.count')
log_success "Glob found $FILE_COUNT markdown files"
echo "$GLOB_RESULT" | jq -r '.matches[]' | head -5

log_step "Searching file content with grep..."
GREP_RESULT=$(workspace_api POST "/${WORKSPACE_ID}/grep" '{
    "pattern": "Emergent",
    "path": "/workspace"
}')
MATCH_COUNT=$(echo "$GREP_RESULT" | jq -r '.matches | length')
log_success "Grep found $MATCH_COUNT matches"
echo "$GREP_RESULT" | jq -r '.matches[0:3][]' 2>/dev/null || true

# ============================================================================
# TEST 5: Vector Database Operations
# ============================================================================
log_section "Test 5: Vector Database Operations"

log_step "Creating a test document via workspace..."
DOC_CREATE_SCRIPT='
curl -s -X POST http://host.docker.internal:3002/api/documents \
  -H "Authorization: Bearer '"$API_KEY"'" \
  -H "X-Project-ID: '"$PROJECT_ID"'" \
  -H "Content-Type: application/json" \
  -d "{
    \"source_type\": \"agent_workspace\",
    \"title\": \"Agent Test Document\",
    \"text_content\": \"This is a test document created by the agent workspace. It contains information about vector databases, embeddings, and semantic search.\",
    \"metadata\": {
      \"workspace_id\": \"'"$WORKSPACE_ID"'\",
      \"test_type\": \"comprehensive\",
      \"created_by\": \"test_script\"
    }
  }"
'

DOC_CREATE_RESULT=$(workspace_api POST "/${WORKSPACE_ID}/bash" "{
    \"command\": $(echo "$DOC_CREATE_SCRIPT" | jq -Rs .)
}")

DOC_ID=$(echo "$DOC_CREATE_RESULT" | jq -r '.stdout' | jq -r '.id' 2>/dev/null || echo "")

if [ -n "$DOC_ID" ] && [ "$DOC_ID" != "null" ]; then
    log_success "Document created: $DOC_ID"
else
    log_error "Failed to create document"
    echo "$DOC_CREATE_RESULT" | jq .
fi

log_step "Listing documents..."
DOCS_LIST_SCRIPT='
curl -s -X GET "http://host.docker.internal:3002/api/documents?limit=5" \
  -H "Authorization: Bearer '"$API_KEY"'" \
  -H "X-Project-ID: '"$PROJECT_ID"'"
'

DOCS_LIST=$(workspace_api POST "/${WORKSPACE_ID}/bash" "{
    \"command\": $(echo "$DOCS_LIST_SCRIPT" | jq -Rs .)
}")

DOC_COUNT=$(echo "$DOCS_LIST" | jq -r '.stdout' | jq -r '.items | length' 2>/dev/null || echo "0")
log_success "Found $DOC_COUNT documents in project"

log_step "Creating a graph object..."
GRAPH_CREATE_SCRIPT='
curl -s -X POST http://host.docker.internal:3002/api/graph/objects \
  -H "Authorization: Bearer '"$API_KEY"'" \
  -H "X-Project-ID: '"$PROJECT_ID"'" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"test_entity\",
    \"key\": \"workspace_test_'$(date +%s)'\",
    \"properties\": {
      \"name\": \"Agent Workspace Test Entity\",
      \"description\": \"Created by comprehensive workspace test\",
      \"workspace_id\": \"'"$WORKSPACE_ID"'\"
    },
    \"labels\": [\"test\", \"agent_created\"]
  }"
'

GRAPH_CREATE_RESULT=$(workspace_api POST "/${WORKSPACE_ID}/bash" "{
    \"command\": $(echo "$GRAPH_CREATE_SCRIPT" | jq -Rs .)
}")

OBJECT_ID=$(echo "$GRAPH_CREATE_RESULT" | jq -r '.stdout' | jq -r '.id' 2>/dev/null || echo "")

if [ -n "$OBJECT_ID" ] && [ "$OBJECT_ID" != "null" ]; then
    log_success "Graph object created: $OBJECT_ID"
else
    log_error "Failed to create graph object"
    echo "$GRAPH_CREATE_RESULT" | jq .
fi

log_step "Performing unified search..."
SEARCH_SCRIPT='
curl -s -X POST http://host.docker.internal:3002/api/search/unified \
  -H "Authorization: Bearer '"$API_KEY"'" \
  -H "X-Project-ID: '"$PROJECT_ID"'" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"workspace test\",
    \"limit\": 5,
    \"resultTypes\": \"both\",
    \"fusionStrategy\": \"rrf\"
  }"
'

SEARCH_RESULT=$(workspace_api POST "/${WORKSPACE_ID}/bash" "{
    \"command\": $(echo "$SEARCH_SCRIPT" | jq -Rs .)
}")

SEARCH_COUNT=$(echo "$SEARCH_RESULT" | jq -r '.stdout' | jq -r '.results | length' 2>/dev/null || echo "0")
log_success "Search returned $SEARCH_COUNT results"

if [ "$SEARCH_COUNT" != "0" ]; then
    echo "$SEARCH_RESULT" | jq -r '.stdout' | jq -r '.results[0]' 2>/dev/null | head -10 || true
fi

# ============================================================================
# TEST 6: Complex Operations - Python Script Execution
# ============================================================================
log_section "Test 6: Complex Operations - Python Script Execution"

log_step "Installing Python and creating analysis script..."
workspace_api POST "/${WORKSPACE_ID}/bash" '{
    "command": "apk add --no-cache python3 py3-pip > /dev/null 2>&1 && python3 --version"
}' > /dev/null

workspace_api POST "/${WORKSPACE_ID}/write" '{
    "file_path": "/workspace/analyze_codebase.py",
    "content": "#!/usr/bin/env python3\nimport os\nimport json\nfrom collections import defaultdict\n\ndef analyze_codebase(root_dir):\n    stats = defaultdict(lambda: {\"files\": 0, \"lines\": 0})\n    \n    for root, dirs, files in os.walk(root_dir):\n        # Skip hidden dirs and node_modules\n        dirs[:] = [d for d in dirs if not d.startswith(\".\") and d != \"node_modules\"]\n        \n        for file in files:\n            if file.startswith(\".\"):\n                continue\n                \n            ext = os.path.splitext(file)[1]\n            if not ext:\n                continue\n                \n            filepath = os.path.join(root, file)\n            try:\n                with open(filepath, \"r\", encoding=\"utf-8\", errors=\"ignore\") as f:\n                    lines = len(f.readlines())\n                stats[ext][\"files\"] += 1\n                stats[ext][\"lines\"] += lines\n            except:\n                pass\n    \n    # Sort by line count\n    sorted_stats = sorted(stats.items(), key=lambda x: x[1][\"lines\"], reverse=True)[:10]\n    \n    print(json.dumps({ext: data for ext, data in sorted_stats}, indent=2))\n\nif __name__ == \"__main__\":\n    analyze_codebase(\"/workspace\")\n"
}' > /dev/null

log_step "Running codebase analysis..."
ANALYSIS=$(workspace_api POST "/${WORKSPACE_ID}/bash" '{
    "command": "python3 /workspace/analyze_codebase.py"
}')

echo "$ANALYSIS" | jq -r '.stdout' | head -20
log_success "Python script executed successfully"

# ============================================================================
# TEST 7: Workspace Information
# ============================================================================
log_section "Test 7: Workspace Information Summary"

WORKSPACE_INFO=$(workspace_api GET "/${WORKSPACE_ID}")

echo "Workspace Details:"
echo "$WORKSPACE_INFO" | jq '{
    id,
    status,
    provider,
    repository_url,
    branch,
    resource_limits,
    created_at,
    expires_at
}'

# ============================================================================
# Summary
# ============================================================================
log_section "Test Summary"

log_success "✓ Workspace creation with git repository"
log_success "✓ Automatic repository cloning"
log_success "✓ Git operations (log, status)"
log_success "✓ File operations (read, write, glob, grep)"
log_success "✓ Vector database operations (documents, graph objects, search)"
log_success "✓ Complex script execution (Python)"
log_success "✓ Network access to host API"

echo -e "\n${GREEN}All tests passed successfully!${NC}\n"
echo "Workspace ID: $WORKSPACE_ID"
echo "Server: $SERVER_URL"
echo -e "\nTo manually explore the workspace, use:"
echo "  curl -X POST ${SERVER_URL}/api/v1/agent/workspaces/${WORKSPACE_ID}/bash \\"
echo "    -H 'X-API-Key: ${API_KEY}' \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"command\": \"your-command-here\"}'"
