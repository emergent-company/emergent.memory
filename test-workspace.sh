#!/bin/bash
set -e

# Workspace API Test Script
# Tests workspace creation with the Emergent public repository

API_BASE="http://localhost:5300"
TEST_USER_EMAIL="test@example.com"
TEST_USER_PASSWORD="TestPassword123!"

echo "🔐 Step 1: Authenticating with Zitadel..."

# Get access token from Zitadel
TOKEN_RESPONSE=$(curl -s -X POST "http://localhost:8200/oauth/v2/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=password" \
  -d "username=$TEST_USER_EMAIL" \
  -d "password=$TEST_USER_PASSWORD" \
  -d "client_id=emergent-dev" \
  -d "scope=openid profile email")

ACCESS_TOKEN=$(echo "$TOKEN_RESPONSE" | jq -r '.access_token')

if [ "$ACCESS_TOKEN" == "null" ] || [ -z "$ACCESS_TOKEN" ]; then
  echo "❌ Authentication failed!"
  echo "Response: $TOKEN_RESPONSE"
  exit 1
fi

echo "✅ Authenticated successfully"
echo "Access token: ${ACCESS_TOKEN:0:50}..."

echo ""
echo "📋 Step 2: Listing available providers..."

PROVIDERS_RESPONSE=$(curl -s -X GET "$API_BASE/api/v1/agent/workspaces/providers" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json")

echo "Available providers:"
echo "$PROVIDERS_RESPONSE" | jq '.'

echo ""
echo "🚀 Step 3: Creating workspace with Emergent repository..."

# Create workspace with public Emergent repository
CREATE_REQUEST='{
  "container_type": "agent_workspace",
  "provider": "auto",
  "repository_url": "https://github.com/emergent-company/emergent.git",
  "branch": "main",
  "deployment_mode": "self-hosted",
  "resource_limits": {
    "memory_mb": 2048,
    "cpu_count": 2,
    "disk_mb": 4096
  }
}'

WORKSPACE_RESPONSE=$(curl -s -w "\nHTTP_STATUS:%{http_code}" -X POST "$API_BASE/api/v1/agent/workspaces" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$CREATE_REQUEST")

HTTP_STATUS=$(echo "$WORKSPACE_RESPONSE" | grep "HTTP_STATUS:" | cut -d: -f2)
WORKSPACE_BODY=$(echo "$WORKSPACE_RESPONSE" | sed '/HTTP_STATUS:/d')

echo "HTTP Status: $HTTP_STATUS"
echo "Response:"
echo "$WORKSPACE_BODY" | jq '.'

if [ "$HTTP_STATUS" != "201" ]; then
  echo "❌ Workspace creation failed!"
  exit 1
fi

WORKSPACE_ID=$(echo "$WORKSPACE_BODY" | jq -r '.id')
echo "✅ Workspace created: $WORKSPACE_ID"

echo ""
echo "📁 Step 4: Testing bash command - list repository contents..."

BASH_REQUEST='{
  "command": "ls -la",
  "workdir": "/workspace",
  "timeout_ms": 10000
}'

BASH_RESPONSE=$(curl -s -X POST "$API_BASE/api/v1/agent/workspaces/$WORKSPACE_ID/bash" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$BASH_REQUEST")

echo "Bash command output:"
echo "$BASH_RESPONSE" | jq '.'

echo ""
echo "📄 Step 5: Testing read tool - read README.md..."

READ_REQUEST='{
  "path": "/workspace/README.md",
  "offset": 0,
  "limit": 50
}'

READ_RESPONSE=$(curl -s -X POST "$API_BASE/api/v1/agent/workspaces/$WORKSPACE_ID/read" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$READ_REQUEST")

echo "README.md (first 50 lines):"
echo "$READ_RESPONSE" | jq -r '.content' | head -20

echo ""
echo "🔍 Step 6: Testing grep tool - find TypeScript files..."

GREP_REQUEST='{
  "pattern": "interface.*{",
  "path": "/workspace",
  "include_pattern": "*.ts",
  "max_results": 10
}'

GREP_RESPONSE=$(curl -s -X POST "$API_BASE/api/v1/agent/workspaces/$WORKSPACE_ID/grep" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$GREP_REQUEST")

echo "TypeScript interfaces found:"
echo "$GREP_RESPONSE" | jq '.matches[:5]'

echo ""
echo "📊 Step 7: Getting workspace status..."

STATUS_RESPONSE=$(curl -s -X GET "$API_BASE/api/v1/agent/workspaces/$WORKSPACE_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json")

echo "Workspace status:"
echo "$STATUS_RESPONSE" | jq '{id, status, lifecycle, provider, created_at}'

echo ""
echo "🧹 Step 8: Cleanup - deleting workspace..."

DELETE_RESPONSE=$(curl -s -w "\nHTTP_STATUS:%{http_code}" -X DELETE "$API_BASE/api/v1/agent/workspaces/$WORKSPACE_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN")

DELETE_STATUS=$(echo "$DELETE_RESPONSE" | grep "HTTP_STATUS:" | cut -d: -f2)

if [ "$DELETE_STATUS" == "204" ] || [ "$DELETE_STATUS" == "200" ]; then
  echo "✅ Workspace deleted successfully"
else
  echo "⚠️  Delete returned status: $DELETE_STATUS"
  echo "$DELETE_RESPONSE" | sed '/HTTP_STATUS:/d'
fi

echo ""
echo "✨ Test completed!"
