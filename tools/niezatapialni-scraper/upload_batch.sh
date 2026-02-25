#!/bin/bash

# Niezatapialni Podcast - Streamlined Batch Upload Script
# Uploads all MP3 files to production server

set -euo pipefail

API="http://mcj-emergent:3002"
TOKEN="e2e-test-user"
PROJECT="8b5ec6f0-663f-4e78-81cd-6f8e13cee6aa"
MP3_DIR="/root/emergent/tools/niezatapialni-scraper/all_mp3s"
LOG_FILE="/tmp/niezatapialni_upload.log"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${GREEN}=== Niezatapialni Podcast Batch Upload ===${NC}"
echo "Start time: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# Get list of already uploaded files from database
echo "Checking already uploaded files..."
UPLOADED_FILES=$(ssh mcj-emergent "docker exec emergent-db psql -U emergent -d emergent -t -c \"SELECT filename FROM kb.documents WHERE project_id = '$PROJECT';\"" | tr -d ' ')

# Count total files and already uploaded
TOTAL_FILES=$(ls -1 "$MP3_DIR"/*.mp3 | wc -l)
ALREADY_UPLOADED=$(echo "$UPLOADED_FILES" | grep -c "^Niezatapialni" || echo 0)

echo "Total MP3 files: $TOTAL_FILES"
echo "Already uploaded: $ALREADY_UPLOADED"
echo "Remaining: $((TOTAL_FILES - ALREADY_UPLOADED))"
echo ""

if [ "$ALREADY_UPLOADED" -eq "$TOTAL_FILES" ]; then
    echo -e "${GREEN}All files already uploaded!${NC}"
    exit 0
fi

# Estimate time
AVG_MINUTES=90
SEQUENTIAL_HOURS=$(( (TOTAL_FILES - ALREADY_UPLOADED) * AVG_MINUTES / 60 ))
echo -e "${BLUE}Estimated transcription time: ~${SEQUENTIAL_HOURS} hours (assuming 90min avg, sequential processing)${NC}"
echo ""

# Sort files smallest to largest for faster initial results
echo "Starting upload (smallest files first)..."
echo ""

SUCCESS=0
FAILED=0
SKIPPED=0
START_TIME=$(date +%s)

# Process each file
ls -S "$MP3_DIR"/*.mp3 | tac | while read -r file; do
    filename=$(basename "$file")
    
    # Skip if already uploaded
    if echo "$UPLOADED_FILES" | grep -q "^$filename$"; then
        echo -e "${BLUE}[SKIP]${NC} $filename (already uploaded)"
        ((SKIPPED++)) || true
        continue
    fi
    
    filesize=$(du -h "$file" | cut -f1)
    echo -ne "${YELLOW}[UPLOAD]${NC} $filename ($filesize)... "
    
    # Upload
    response=$(curl -s -w "\n%{http_code}" --max-time 300 \
        -X POST "$API/api/document-parsing-jobs/upload" \
        -H "Authorization: Bearer $TOKEN" \
        -H "X-Project-Id: $PROJECT" \
        -F "file=@$file;type=audio/mpeg" 2>&1) || true
    
    http_code=$(echo "$response" | tail -n1)
    
    if [ "$http_code" = "200" ] || [ "$http_code" = "201" ]; then
        echo -e "${GREEN}✓${NC}"
        ((SUCCESS++)) || true
        sleep 2  # Rate limiting
    else
        echo -e "${RED}✗ (HTTP $http_code)${NC}"
        ((FAILED++)) || true
        echo "$(date -Iseconds) - FAILED: $filename - HTTP $http_code" >> "$LOG_FILE"
        
        # Back off on errors
        if [ "$http_code" = "429" ] || [ "$http_code" = "503" ]; then
            echo "Server busy, waiting 60s..."
            sleep 60
        else
            sleep 5
        fi
    fi
    
    # Status update every 50 files
    if [ $(( (SUCCESS + FAILED) % 50 )) -eq 0 ] && [ $((SUCCESS + FAILED)) -gt 0 ]; then
        ELAPSED=$(($(date +%s) - START_TIME))
        RATE=$(echo "scale=2; $SUCCESS * 3600 / $ELAPSED" | bc -l 2>/dev/null || echo "0")
        echo ""
        echo -e "${GREEN}Progress: $SUCCESS uploaded, $FAILED failed, $SKIPPED skipped | Rate: ${RATE}/hr${NC}"
        echo ""
    fi
done

END_TIME=$(date +%s)
ELAPSED=$(( (END_TIME - START_TIME) / 60 ))

echo ""
echo -e "${GREEN}=== Upload Complete ===${NC}"
echo "End time: $(date '+%Y-%m-%d %H:%M:%S')"
echo "Elapsed: ${ELAPSED} minutes"
echo "Success: $SUCCESS"
echo "Failed: $FAILED"
echo "Skipped: $SKIPPED"
echo ""
echo "Monitor progress: /root/emergent/tools/niezatapialni-scraper/monitor_progress.sh"
