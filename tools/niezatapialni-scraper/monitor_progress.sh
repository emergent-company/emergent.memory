#!/bin/bash

# Niezatapialni Podcast - Progress Monitoring Script
# Monitors transcription and extraction progress

set -euo pipefail

API="http://mcj-emergent:3002"
TOKEN="e2e-test-user"
PROJECT="8b5ec6f0-663f-4e78-81cd-6f8e13cee6aa"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

clear
echo -e "${GREEN}=== Niezatapialni Podcast - Progress Monitor ===${NC}"
echo "Project ID: $PROJECT"
echo "Last updated: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# 1. Document Parsing Jobs Status
echo -e "${BLUE}📝 Document Parsing Jobs (Whisper Transcription)${NC}"
ssh mcj-emergent "docker exec emergent-db psql -U emergent -d emergent -t -c \"
SELECT 
  status,
  COUNT(*) as count,
  ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 1) as percentage
FROM kb.document_parsing_jobs 
WHERE project_id = '$PROJECT'
GROUP BY status
ORDER BY 
  CASE status
    WHEN 'completed' THEN 1
    WHEN 'processing' THEN 2
    WHEN 'pending' THEN 3
    WHEN 'failed' THEN 4
    ELSE 5
  END;
\""
echo ""

# 2. Document Conversion Status
echo -e "${BLUE}📄 Document Conversion Status${NC}"
ssh mcj-emergent "docker exec emergent-db psql -U emergent -d emergent -t -c \"
SELECT 
  conversion_status,
  COUNT(*) as count,
  ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 1) as percentage
FROM kb.documents 
WHERE project_id = '$PROJECT'
GROUP BY conversion_status
ORDER BY 
  CASE conversion_status
    WHEN 'completed' THEN 1
    WHEN 'processing' THEN 2
    WHEN 'pending' THEN 3
    WHEN 'failed' THEN 4
    ELSE 5
  END;
\""
echo ""

# 3. Extraction Jobs Status
echo -e "${BLUE}🔍 Extraction Jobs (Knowledge Graph)${NC}"
EXTRACTION_STATS=$(curl -s "$API/api/admin/extraction-jobs/projects/$PROJECT?limit=1000" \
  -H "Authorization: Bearer $TOKEN" 2>/dev/null || echo '{"jobs":[]}')

EXTRACTION_TOTAL=$(echo "$EXTRACTION_STATS" | jq -r '.jobs | length')
EXTRACTION_COMPLETED=$(echo "$EXTRACTION_STATS" | jq -r '[.jobs[] | select(.status == "completed")] | length')
EXTRACTION_PROCESSING=$(echo "$EXTRACTION_STATS" | jq -r '[.jobs[] | select(.status == "processing")] | length')
EXTRACTION_PENDING=$(echo "$EXTRACTION_STATS" | jq -r '[.jobs[] | select(.status == "pending")] | length')
EXTRACTION_FAILED=$(echo "$EXTRACTION_STATS" | jq -r '[.jobs[] | select(.status == "failed")] | length')

echo "  Total: $EXTRACTION_TOTAL"
echo "  Completed: $EXTRACTION_COMPLETED"
echo "  Processing: $EXTRACTION_PROCESSING"
echo "  Pending: $EXTRACTION_PENDING"
echo "  Failed: $EXTRACTION_FAILED"

if [ "$EXTRACTION_TOTAL" -gt 0 ]; then
    EXTRACTION_PCT=$(echo "scale=1; 100.0 * $EXTRACTION_COMPLETED / $EXTRACTION_TOTAL" | bc -l)
    echo "  Progress: ${EXTRACTION_PCT}%"
fi
echo ""

# 4. Extracted Objects Count
echo -e "${BLUE}📊 Extracted Knowledge Graph Objects${NC}"
OBJECT_STATS=$(ssh mcj-emergent "docker exec emergent-db psql -U emergent -d emergent -t -c \"
SELECT 
  entity_type,
  COUNT(*) as count
FROM kb.extracted_objects 
WHERE project_id = '$PROJECT'
GROUP BY entity_type
ORDER BY COUNT(*) DESC;
\"" 2>/dev/null || echo "")

if [ -n "$OBJECT_STATS" ]; then
    echo "$OBJECT_STATS"
    
    TOTAL_OBJECTS=$(ssh mcj-emergent "docker exec emergent-db psql -U emergent -d emergent -t -c \"
    SELECT COUNT(*) FROM kb.extracted_objects WHERE project_id = '$PROJECT';
    \"" | xargs)
    
    TOTAL_RELATIONSHIPS=$(ssh mcj-emergent "docker exec emergent-db psql -U emergent -d emergent -t -c \"
    SELECT COUNT(*) FROM kb.extracted_relationships WHERE project_id = '$PROJECT';
    \"" | xargs)
    
    echo ""
    echo "  Total Objects: $TOTAL_OBJECTS"
    echo "  Total Relationships: $TOTAL_RELATIONSHIPS"
else
    echo "  No objects extracted yet"
fi
echo ""

# 5. Recent Activity
echo -e "${BLUE}⏱️  Recent Completed Transcriptions (Last 5)${NC}"
ssh mcj-emergent "docker exec emergent-db psql -U emergent -d emergent -t -c \"
SELECT 
  filename,
  LENGTH(content) as chars,
  LEFT(content, 80) as preview
FROM kb.documents 
WHERE project_id = '$PROJECT' 
  AND conversion_status = 'completed'
  AND content IS NOT NULL
ORDER BY updated_at DESC
LIMIT 5;
\""
echo ""

# 6. System Health
echo -e "${BLUE}🏥 System Health${NC}"
HEALTH=$(curl -s "$API/api/health" 2>/dev/null || echo '{}')
VERSION=$(echo "$HEALTH" | jq -r '.version // "unknown"')
UPTIME=$(echo "$HEALTH" | jq -r '.uptime // "unknown"')

echo "  Server Version: $VERSION"
echo "  Uptime: $UPTIME"

# Check Whisper container
WHISPER_STATUS=$(ssh mcj-emergent "docker ps --filter name=emergent-whisper --format '{{.Status}}'" 2>/dev/null || echo "unknown")
echo "  Whisper Container: $WHISPER_STATUS"

echo ""
echo -e "${YELLOW}Press Ctrl+C to stop monitoring${NC}"
echo "Refresh in 30 seconds..."
