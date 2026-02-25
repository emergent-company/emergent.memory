#!/bin/bash

# Niezatapialni Podcast - Batch Upload Script
# Uploads all MP3 files to production server for Whisper transcription

set -euo pipefail

# Configuration
API="http://mcj-emergent:3002"
TOKEN="e2e-test-user"
PROJECT="8b5ec6f0-663f-4e78-81cd-6f8e13cee6aa"
MP3_DIR="/root/emergent/tools/niezatapialni-scraper/all_mp3s"
LOG_FILE="/tmp/niezatapialni_upload.log"
STATE_FILE="/tmp/niezatapialni_upload_state.txt"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Initialize state file if it doesn't exist
if [ ! -f "$STATE_FILE" ]; then
    echo "0" > "$STATE_FILE"
fi

# Get last uploaded count
UPLOADED_COUNT=$(cat "$STATE_FILE")

echo -e "${GREEN}=== Niezatapialni Podcast Upload ===${NC}"
echo "API: $API"
echo "Project ID: $PROJECT"
echo "MP3 Directory: $MP3_DIR"
echo "Log file: $LOG_FILE"
echo "Previously uploaded: $UPLOADED_COUNT files"
echo ""

# Count total files
TOTAL_FILES=$(ls -1 "$MP3_DIR"/*.mp3 2>/dev/null | wc -l)
if [ "$TOTAL_FILES" -eq 0 ]; then
    echo -e "${RED}ERROR: No MP3 files found in $MP3_DIR${NC}"
    exit 1
fi

echo "Total files to process: $TOTAL_FILES"
echo "Remaining: $((TOTAL_FILES - UPLOADED_COUNT))"
echo ""

# Ask for confirmation if starting fresh
if [ "$UPLOADED_COUNT" -eq 0 ]; then
    echo -e "${YELLOW}This will upload $TOTAL_FILES files to the production server.${NC}"
    echo -e "${YELLOW}Estimated transcription time: ~$(($TOTAL_FILES * 90 / 60)) hours (assuming 90min avg per episode)${NC}"
    read -p "Continue? (y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Upload cancelled."
        exit 0
    fi
fi

# Sort files by size (smallest first for faster initial results)
# This way we get some completed transcriptions quickly to verify everything works
SORTED_FILES=$(ls -S "$MP3_DIR"/*.mp3 | tac)

# Convert to array and skip already uploaded files
FILES_ARRAY=()
while IFS= read -r file; do
    FILES_ARRAY+=("$file")
done <<< "$SORTED_FILES"

echo -e "${GREEN}Starting upload...${NC}"
echo "$(date -Iseconds) - Upload started" >> "$LOG_FILE"

CURRENT_INDEX=0
SUCCESS_COUNT=0
FAIL_COUNT=0
START_TIME=$(date +%s)

for file in "${FILES_ARRAY[@]}"; do
    CURRENT_INDEX=$((CURRENT_INDEX + 1))
    
    # Skip already uploaded files
    if [ "$CURRENT_INDEX" -le "$UPLOADED_COUNT" ]; then
        continue
    fi
    
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
            echo -e "${GREEN}✓ Success${NC}"
            echo "$(date -Iseconds) - SUCCESS [$CURRENT_INDEX/$TOTAL_FILES] $filename" >> "$LOG_FILE"
            SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
            
            # Update state file
            echo "$CURRENT_INDEX" > "$STATE_FILE"
            
            # Rate limiting - wait 2 seconds between uploads
            sleep 2
        else
            echo -e "${RED}✗ Failed (HTTP $http_code)${NC}"
            echo "$(date -Iseconds) - FAILED [$CURRENT_INDEX/$TOTAL_FILES] $filename - HTTP $http_code - $body" >> "$LOG_FILE"
            FAIL_COUNT=$((FAIL_COUNT + 1))
            
            # If we get rate limited or server error, wait longer
            if [ "$http_code" -eq 429 ] || [ "$http_code" -eq 503 ]; then
                echo -e "${YELLOW}Server busy, waiting 60 seconds...${NC}"
                sleep 60
            fi
        fi
    else
        echo -e "${RED}✗ Failed (network error)${NC}"
        echo "$(date -Iseconds) - ERROR [$CURRENT_INDEX/$TOTAL_FILES] $filename - Network error" >> "$LOG_FILE"
        FAIL_COUNT=$((FAIL_COUNT + 1))
        
        # Wait before retrying
        sleep 5
    fi
    
    # Print progress summary every 10 files
    if [ $((CURRENT_INDEX % 10)) -eq 0 ]; then
        ELAPSED=$(($(date +%s) - START_TIME))
        RATE=$(echo "scale=2; $SUCCESS_COUNT / ($ELAPSED / 3600)" | bc -l)
        echo -e "\n${GREEN}Progress: $SUCCESS_COUNT succeeded, $FAIL_COUNT failed, Rate: $RATE files/hour${NC}\n"
    fi
done

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))
ELAPSED_MIN=$((ELAPSED / 60))

echo ""
echo -e "${GREEN}=== Upload Complete ===${NC}"
echo "Total uploaded: $SUCCESS_COUNT"
echo "Failed: $FAIL_COUNT"
echo "Time elapsed: ${ELAPSED_MIN} minutes"
echo "$(date -Iseconds) - Upload complete - $SUCCESS_COUNT succeeded, $FAIL_COUNT failed" >> "$LOG_FILE"

# Show summary and next steps
echo ""
echo -e "${GREEN}Next steps:${NC}"
echo "1. Monitor transcription progress:"
echo "   curl -s '$API/api/admin/parsing-jobs/projects/$PROJECT?limit=50' -H 'Authorization: Bearer $TOKEN' | jq '.jobs[] | {status, filename}'"
echo ""
echo "2. Check job status distribution:"
echo "   ssh mcj-emergent \"docker exec emergent-db psql -U emergent -d emergent -c 'SELECT status, COUNT(*) FROM kb.document_parsing_jobs WHERE project_id = \\\"$PROJECT\\\" GROUP BY status;'\""
echo ""
echo "3. Monitor extraction jobs:"
echo "   curl -s '$API/api/admin/extraction-jobs/projects/$PROJECT?limit=50' -H 'Authorization: Bearer $TOKEN' | jq '.jobs[] | {status, objectCount}'"
echo ""
echo "4. View completed transcriptions:"
echo "   ssh mcj-emergent \"docker exec emergent-db psql -U emergent -d emergent -c 'SELECT filename, conversion_status, LENGTH(content) as chars FROM kb.documents WHERE project_id = \\\"$PROJECT\\\" AND conversion_status = \\\"completed\\\" ORDER BY created_at DESC LIMIT 10;'\""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ""
    echo -e "${YELLOW}Warning: $FAIL_COUNT files failed to upload. Check $LOG_FILE for details.${NC}"
    echo "You can re-run this script to retry failed uploads (it will skip already uploaded files)."
fi
