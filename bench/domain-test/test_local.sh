#!/bin/bash
set -euo pipefail

API="http://localhost:5300"
KEY="emt_fb6300cd4ec86e87e56ba55db9ad62edc407a681a9957f70b922a734bf37d94d"
PROJECT_ID="54be6136-8af3-47a1-9b36-7dbea67627ce"

green() { echo -e "\033[32m$1\033[0m"; }
red()   { echo -e "\033[31m$1\033[0m"; }

# 1. Configure OpenAI-compatible provider
echo "=== Configuring provider ==="
curl -sf -X POST "$API/api/v1/projects/$PROJECT_ID/providers/openai-compatible" \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"apiKey":"sk-bfa5b8465aad4a1e907474714936b0ff","baseUrl":"https://api.deepseek.com/v1","generativeModel":"deepseek-v4-flash"}' || true

# 2. Test provider
echo "=== Testing provider ==="
curl -sf -X POST "$API/api/v1/projects/$PROJECT_ID/providers/openai-compatible/test" \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"Say hello in one word"}]}' && green "Provider OK" || red "Provider FAIL"

# 3. Upload a document
echo "=== Uploading document ==="
DOC_ID=$(curl -sf -X POST "$API/api/v1/projects/$PROJECT_ID/documents" \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"test","content":"I had a great conversation with Alice about the project roadmap. She suggested we use memory graph for tracking decisions."}' | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))")
echo "Document ID: $DOC_ID"

# 4. Call /remember
echo "=== Calling /remember ==="
REMEMBER_RESP=$(curl -sf -X POST "$API/api/v1/projects/$PROJECT_ID/remember" \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d "{\"document_id\":\"$DOC_ID\"}")
echo "$REMEMBER_RESP" | python3 -m json.tool

RUN_ID=$(echo "$REMEMBER_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('run_id',''))")
echo "Run ID: $RUN_ID"

# 5. Poll for agent completion
echo "=== Polling agent run ==="
for i in $(seq 1 30); do
  STATUS=$(curl -sf "$API/api/v1/projects/$PROJECT_ID/agents/runs/$RUN_ID" \
    -H "X-API-Key: $KEY" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "pending")
  echo "  Attempt $i: status=$STATUS"
  if [ "$STATUS" = "completed" ] || [ "$STATUS" = "input-required" ]; then
    green "Agent finished ($STATUS)"
    break
  fi
  sleep 5
done

# 6. Check schemas
echo "=== Checking schemas ==="
curl -sf "$API/api/v1/projects/$PROJECT_ID/schemas" \
  -H "X-API-Key: $KEY" | python3 -c "
import sys,json
schemas = json.load(sys.stdin)
print(f'Schemas found: {len(schemas)}')
for s in schemas:
    print(f'  - {s.get(\"name\",\"?\")}: domainContext={s.get(\"domainContext\",\"\")[:50]}')
"
