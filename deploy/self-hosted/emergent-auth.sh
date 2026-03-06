#!/bin/bash
#
# Emergent Google Cloud Authentication Setup
# Guides user through setting up Google API key for embeddings
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${SCRIPT_DIR%/bin}"
CONFIG_DIR="$INSTALL_DIR/config"
DOCKER_DIR="$INSTALL_DIR/docker"
ENV_FILE="$CONFIG_DIR/.env.local"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${BOLD}Emergent - Google Cloud Setup${NC}"
echo "=============================="
echo ""

if ! command -v gcloud &> /dev/null; then
    echo -e "${YELLOW}gcloud CLI not found.${NC}"
    echo ""
    echo "To install gcloud CLI:"
    echo ""
    echo "  ${BOLD}Linux/macOS:${NC}"
    echo "    curl https://sdk.cloud.google.com | bash"
    echo "    exec -l \$SHELL"
    echo "    gcloud init"
    echo ""
    echo "  ${BOLD}Or visit:${NC}"
    echo "    https://cloud.google.com/sdk/docs/install"
    echo ""
    echo -e "${CYAN}After installing, run this command again.${NC}"
    echo ""
    
    echo -e "${BOLD}Manual Setup (alternative):${NC}"
    echo "1. Go to: https://console.cloud.google.com/apis/credentials"
    echo "2. Create API key"
    echo "3. Enable 'Generative Language API'"
    echo "4. Edit ${ENV_FILE}"
    echo "5. Add: GOOGLE_API_KEY=your-key-here"
    echo "6. Run: ~/.emergent/bin/emergent-ctl restart"
    exit 1
fi

CURRENT_ACCOUNT=$(gcloud config get-value account 2>/dev/null || echo "")
if [ -z "$CURRENT_ACCOUNT" ]; then
    echo -e "${YELLOW}Not logged in to Google Cloud.${NC}"
    echo ""
    read -p "Run 'gcloud auth login'? (Y/n): " -r DO_LOGIN
    if [[ ! "$DO_LOGIN" =~ ^[Nn]$ ]]; then
        gcloud auth login
    else
        echo "Please login first: gcloud auth login"
        exit 1
    fi
fi

echo -e "${CYAN}Logged in as:${NC} $(gcloud config get-value account 2>/dev/null)"
echo ""

echo -e "${BOLD}Step 1: Select Google Cloud Project${NC}"
echo ""

PROJECTS=$(gcloud projects list --format="value(projectId)" 2>/dev/null | head -20)
if [ -z "$PROJECTS" ]; then
    echo -e "${YELLOW}No projects found. You need to create a project first.${NC}"
    echo ""
    echo "Visit: https://console.cloud.google.com/projectcreate"
    exit 1
fi

echo "Available projects:"
echo ""
i=1
declare -a PROJECT_ARRAY
while IFS= read -r proj; do
    echo "  $i) $proj"
    PROJECT_ARRAY+=("$proj")
    ((i++))
done <<< "$PROJECTS"
echo ""

CURRENT_PROJECT=$(gcloud config get-value project 2>/dev/null || echo "")
if [ -n "$CURRENT_PROJECT" ]; then
    echo -e "Current project: ${GREEN}$CURRENT_PROJECT${NC}"
    read -p "Use this project? (Y/n): " -r USE_CURRENT
    if [[ ! "$USE_CURRENT" =~ ^[Nn]$ ]]; then
        SELECTED_PROJECT="$CURRENT_PROJECT"
    fi
fi

if [ -z "$SELECTED_PROJECT" ]; then
    read -p "Enter project number (1-${#PROJECT_ARRAY[@]}): " -r PROJECT_NUM
    if [[ "$PROJECT_NUM" =~ ^[0-9]+$ ]] && [ "$PROJECT_NUM" -ge 1 ] && [ "$PROJECT_NUM" -le "${#PROJECT_ARRAY[@]}" ]; then
        SELECTED_PROJECT="${PROJECT_ARRAY[$((PROJECT_NUM-1))]}"
    else
        echo -e "${RED}Invalid selection${NC}"
        exit 1
    fi
fi

echo ""
echo -e "Selected project: ${GREEN}$SELECTED_PROJECT${NC}"
gcloud config set project "$SELECTED_PROJECT" 2>/dev/null

echo ""
echo -e "${BOLD}Step 2: Enable Required APIs${NC}"
echo ""

REQUIRED_API="generativelanguage.googleapis.com"

echo "Checking if Generative Language API is enabled..."
API_ENABLED=$(gcloud services list --enabled --filter="name:$REQUIRED_API" --format="value(name)" 2>/dev/null || echo "")

if [ -z "$API_ENABLED" ]; then
    echo -e "${YELLOW}Generative Language API is not enabled.${NC}"
    read -p "Enable it now? (Y/n): " -r ENABLE_API
    if [[ ! "$ENABLE_API" =~ ^[Nn]$ ]]; then
        echo "Enabling API (this may take a moment)..."
        gcloud services enable "$REQUIRED_API"
        echo -e "${GREEN}API enabled!${NC}"
    else
        echo ""
        echo "You can enable it manually:"
        echo "  gcloud services enable $REQUIRED_API"
        echo "  Or visit: https://console.cloud.google.com/apis/library/generativelanguage.googleapis.com"
        exit 1
    fi
else
    echo -e "${GREEN}Generative Language API is already enabled.${NC}"
fi

echo ""
echo -e "${BOLD}Step 3: Create or Use API Key${NC}"
echo ""

EXISTING_KEYS=$(gcloud services api-keys list --format="value(name,displayName)" 2>/dev/null | head -10)

if [ -n "$EXISTING_KEYS" ]; then
    echo "Existing API keys:"
    echo ""
    i=1
    declare -a KEY_ARRAY
    while IFS=$'\t' read -r key_name key_display; do
        short_name=$(echo "$key_name" | sed 's|.*/||')
        display="${key_display:-$short_name}"
        echo "  $i) $display"
        KEY_ARRAY+=("$key_name")
        ((i++))
    done <<< "$EXISTING_KEYS"
    echo "  $i) Create new key"
    echo ""
    
    read -p "Select key (1-$i): " -r KEY_NUM
    
    if [ "$KEY_NUM" -eq "$i" ] 2>/dev/null; then
        CREATE_NEW=true
    elif [[ "$KEY_NUM" =~ ^[0-9]+$ ]] && [ "$KEY_NUM" -ge 1 ] && [ "$KEY_NUM" -lt "$i" ]; then
        SELECTED_KEY="${KEY_ARRAY[$((KEY_NUM-1))]}"
    else
        echo -e "${RED}Invalid selection${NC}"
        exit 1
    fi
else
    CREATE_NEW=true
fi

if [ "$CREATE_NEW" = true ]; then
    echo "Creating new API key..."
    KEY_NAME="emergent-$(date +%Y%m%d)"
    
    RESULT=$(gcloud services api-keys create --display-name="$KEY_NAME" --format="json" 2>&1)
    
    if echo "$RESULT" | grep -q "keyString"; then
        API_KEY=$(echo "$RESULT" | grep -o '"keyString": "[^"]*"' | cut -d'"' -f4)
    else
        KEY_ID=$(echo "$RESULT" | grep -o '"name": "[^"]*"' | head -1 | cut -d'"' -f4)
        if [ -n "$KEY_ID" ]; then
            API_KEY=$(gcloud services api-keys get-key-string "$KEY_ID" --format="value(keyString)" 2>/dev/null)
        fi
    fi
else
    echo "Retrieving key string..."
    API_KEY=$(gcloud services api-keys get-key-string "$SELECTED_KEY" --format="value(keyString)" 2>/dev/null)
fi

if [ -z "$API_KEY" ]; then
    echo -e "${RED}Failed to retrieve API key.${NC}"
    echo ""
    echo "Please create one manually:"
    echo "  1. Visit: https://console.cloud.google.com/apis/credentials"
    echo "  2. Click 'Create Credentials' > 'API Key'"
    echo "  3. Copy the key and add to .env.local:"
    echo "     GOOGLE_API_KEY=your-key-here"
    exit 1
fi

echo ""
echo -e "${GREEN}API Key obtained!${NC}"
echo ""

echo -e "${BOLD}Step 4: Save Configuration${NC}"
echo ""

if [ -f "$ENV_FILE" ]; then
    if grep -q "^GOOGLE_API_KEY=" "$ENV_FILE"; then
        echo "Updating existing GOOGLE_API_KEY in .env.local..."
        sed -i "s|^GOOGLE_API_KEY=.*|GOOGLE_API_KEY=$API_KEY|" "$ENV_FILE"
    else
        echo "Adding GOOGLE_API_KEY to .env.local..."
        echo "GOOGLE_API_KEY=$API_KEY" >> "$ENV_FILE"
    fi
else
    echo -e "${RED}.env.local not found at: $ENV_FILE${NC}"
    echo "Please add manually: GOOGLE_API_KEY=$API_KEY"
    exit 1
fi

echo -e "${GREEN}Configuration saved!${NC}"
echo ""

echo -e "${BOLD}Step 5: Restart Services${NC}"
echo ""
read -p "Restart Emergent server now to apply changes? (Y/n): " -r DO_RESTART

if [[ ! "$DO_RESTART" =~ ^[Nn]$ ]]; then
    if [ -f "$SCRIPT_DIR/emergent-ctl" ]; then
        "$SCRIPT_DIR/emergent-ctl" restart
    else
        cd "$DOCKER_DIR"
        docker compose -f docker-compose.yml --env-file "$CONFIG_DIR/.env.local" restart server
    fi
    echo ""
    echo -e "${GREEN}Server restarted!${NC}"
fi

echo ""
echo -e "${GREEN}${BOLD}Setup Complete!${NC}"
echo ""
echo "Embeddings are now enabled. Test with:"
echo "  docker exec emergent-server emergent projects list"
echo ""
