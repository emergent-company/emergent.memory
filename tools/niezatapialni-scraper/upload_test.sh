#!/bin/bash

# Niezatapialni Podcast - TEST Upload Script (5 files only)
# Tests the upload process with smallest files first

set -euo pipefail

# Configuration
API="http://mcj-emergent:3002"
TOKEN="e2e-test-user"
PROJECT="8b5ec6f0-663f-4e78-81cd-6f8e13cee6aa"
MP3_DIR="/root/emergent/tools/niezatapialni-scraper/all_mp3s"
LOG_FILE="/tmp/niezatapialni_upload_test.log"
MAX_FILES=5

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Niezatapialni Podcast TEST Upload (${MAX_FILES} files) ===${NC}"
echo "API: $API"
echo "Project ID: $PROJECT"
echo "MP3 Directory: $MP3_DIR"
echo "Log file: $LOG_FILE"
echo ""

# Get smallest files for faster testing
SORTED_FILES=$(ls -S "$MP3_DIR"/*.mp3 | tac | head -n $MAX_FILES)

# Convert to array
FILES_ARRAY=()
while IFS= read -r file; do
    FILES_ARRAY+=("$file")
done <<< "$SORTED_FILES"

TOTAL_FILES=${#FILES_ARRAY[@]}
echo "Files to upload: $TOTAL_FILES"
echo ""

read -p "Continue with test upload? (y/n): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Upload cancelled."
    exit 0
fi

echo -e "${GREEN}Starting test upload...${NC}"
echo "$(date -Iseconds) - Test upload started" > "$LOG_FILE"

CURRENT_INDEX=0
SUCCESS_COUNT=0
FAIL_COUNT=0
START_TIME=$(date +%s)

for file in "${FILES_ARRAY[@]}"; do
    CURRENT_INDEX=$((CURRENT_INDEX + 1))
    
    filename=$(basename "$file")
    filesize=$(du -h "$file" | cut -f1)
    
    echo -ne "${YELLOW}[$CURRENT_INDEX/$TOTAL_FILES]${NC} Uploading $filename ($filesize)... "
    
    # Upload with timeout and error handling
    if response=$(curl -s -w "\n%{http_code}" --max-time 300 \
        -X POST "$API/api/document-parsing-jobs/upload" \
        -H "Authorization: Bearer $TOKEN" \
        -H "X-Project-Id: $PROJECT" \
        -F "file=@$file;type=audio/mpeg" 2>&1); then
        
        http_code=$(echo "$response" | tail -n1)
        body=$(echo "$response" | head -n-1)
        
        if [ "$http_code" -eq 200 ] || [ "$http_code" -eq 201 ]; then
            DOC_ID=$(echo "$body" | jq -r '.documentId // .id // "unknown"')
            echo -e "${GREEN}✓ Success (Doc ID: $DOC_ID)${NC}"
            echo "$(date -Iseconds) - SUCCESS [$CURRENT_INDEX/$TOTAL_FILES] $filename - Doc ID: $DOC_ID" >> "$LOG_FILE"
            SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
            
            # Rate limiting - wait 2 seconds between uploads
            sleep 2
        else
            echo -e "${RED}✗ Failed (HTTP $http_code)${NC}"
            echo "Response: $body"
            echo "$(date -Iseconds) - FAILED [$CURRENT_INDEX/$TOTAL_FILES] $filename - HTTP $http_code - $body" >> "$LOG_FILE"
            FAIL_COUNT=$((FAIL_COUNT + 1))
            
            # If we get rate limited or server error, wait longer
            if [ "$http_code" -eq 429 ] || [ "$http_code" -eq 503 ]; then
                echo -e "${YELLOW}Server busy, waiting 30 seconds...${NC}"
                sleep 30
            fi
        fi
    else
        echo -e "${RED}✗ Failed (network error)${NC}"
        echo "$(date -Iseconds) - ERROR [$CURRENT_INDEX/$TOTAL_FILES] $filename - Network error: $response" >> "$LOG_FILE"
        FAIL_COUNT=$((FAIL_COUNT + 1))
        
        # Wait before continuing
        sleep 5
    fi
done

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

echo ""
echo -e "${GREEN}=== Test Upload Complete ===${NC}"
echo "Total uploaded: $SUCCESS_COUNT"
echo "Failed: $FAIL_COUNT"
echo "Time elapsed: ${ELAPSED} seconds"
echo "$(date -Iseconds) - Test upload complete - $SUCCESS_COUNT succeeded, $FAIL_COUNT failed" >> "$LOG_FILE"

if [ "$SUCCESS_COUNT" -gt 0 ]; then
    echo ""
    echo -e "${GREEN}✓ Test successful!${NC}"
    echo ""
    echo "Monitor transcription progress with:"
    echo "  watch -n 10 /root/emergent/tools/niezatapialni-scraper/monitor_progress.sh"
    echo ""
    echo "Or check manually:"
    echo "  ssh mcj-emergent \"docker exec emergent-db psql -U emergent -d emergent -c 'SELECT status, COUNT(*) FROM kb.document_parsing_jobs WHERE project_id = \\\"$PROJECT\\\" GROUP BY status;'\""
    echo ""
    echo "When ready to upload all 610 files, run:"
    echo "  /root/emergent/tools/niezatapialni-scraper/upload_all.sh"
else
    echo ""
    echo -e "${RED}✗ Test failed! Check logs at $LOG_FILE${NC}"
    exit 1
fi
