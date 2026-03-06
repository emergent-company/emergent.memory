#!/bin/bash
# Pre-flight checks before Docker deployment

set -e

echo "🔍 Running pre-flight checks for Docker deployment..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

EXIT_CODE=0

# Check 1: Required environment variables
echo "1️⃣  Checking environment variables..."
REQUIRED_VARS=(
    "POSTGRES_PASSWORD"
    "GOOGLE_API_KEY"
    "ZITADEL_DOMAIN"
    "ZITADEL_CLIENT_ID"
    "ZITADEL_CLIENT_SECRET"
    "VITE_API_URL"
    "VITE_ZITADEL_ISSUER"
    "VITE_ZITADEL_CLIENT_ID"
    "CORS_ORIGIN"
)

if [[ -f .env.production ]]; then
    source .env.production
    MISSING_VARS=()
    PLACEHOLDER_VARS=()
    
    for var in "${REQUIRED_VARS[@]}"; do
        if [[ -z "${!var}" ]]; then
            echo "   ❌ Missing: $var"
            MISSING_VARS+=("$var")
            EXIT_CODE=1
        elif [[ "${!var}" =~ ^\<.*\>$ ]]; then
            echo "   ⚠️  Placeholder: $var"
            PLACEHOLDER_VARS+=("$var")
            EXIT_CODE=1
        else
            echo "   ✅ Set: $var"
        fi
    done
    
    if [[ ${#MISSING_VARS[@]} -gt 0 || ${#PLACEHOLDER_VARS[@]} -gt 0 ]]; then
        echo ""
        echo "   ⚠️  Please configure these variables in .env.production"
    fi
else
    echo "   ❌ No .env.production file found"
    echo "   Create it: cp .env.production.example .env.production"
    EXIT_CODE=1
fi

echo ""

# Check 2: Docker BuildKit
echo "2️⃣  Checking Docker BuildKit..."
if command -v docker &> /dev/null; then
    echo "   ✅ Docker installed"
    if docker buildx version > /dev/null 2>&1; then
        echo "   ✅ BuildKit available"
    else
        echo "   ⚠️  BuildKit not available (optional, but recommended)"
    fi
else
    echo "   ❌ Docker not installed"
    EXIT_CODE=1
fi

echo ""

# Check 3: Git status
echo "3️⃣  Checking Git status..."
if command -v git &> /dev/null; then
    if git rev-parse --git-dir > /dev/null 2>&1; then
        if git diff-index --quiet HEAD -- 2>/dev/null; then
            echo "   ✅ Working directory clean"
        else
            echo "   ⚠️  Uncommitted changes present"
            echo "   Consider committing before deployment"
        fi
        
        # Check for untracked files
        if [[ -n $(git ls-files --others --exclude-standard) ]]; then
            echo "   ⚠️  Untracked files present"
        fi
    else
        echo "   ⚠️  Not a git repository"
    fi
else
    echo "   ⚠️  Git not installed"
fi

echo ""

# Check 4: Build test
echo "4️⃣  Testing builds..."
echo "   Building server (Go)..."
if cd apps/server && go build ./... > /dev/null 2>&1; then
    echo "   ✅ Server build successful"
    cd ../..
else
    echo "   ❌ Server build failed"
    cd ../..
    EXIT_CODE=1
fi

echo "   Building admin..."
if cd apps/admin && npm run build > /dev/null 2>&1; then
    echo "   ✅ Admin build successful"
    cd ../..
else
    echo "   ❌ Admin build failed"
    cd ../..
    EXIT_CODE=1
fi

echo ""

# Check 5: Tests
echo "5️⃣  Running tests..."
echo "   Server tests (Go)..."
if cd apps/server && go test ./... > /dev/null 2>&1; then
    echo "   ✅ Server tests passed"
    cd ../..
else
    echo "   ⚠️  Server tests failed (check manually)"
    cd ../..
fi

echo "   Admin tests..."
if cd apps/admin && npm run test > /dev/null 2>&1; then
    echo "   ✅ Admin tests passed"
    cd ../..
else
    echo "   ⚠️  Admin tests failed (check manually)"
    cd ../..
fi

echo ""

# Check 6: Required files
echo "6️⃣  Checking required files..."
REQUIRED_FILES=(
    "docker-compose.yml"
    "deploy/self-hosted/Dockerfile.server"
    "apps/admin/Dockerfile"
    ".dockerignore"
    ".env.production.example"
)

for file in "${REQUIRED_FILES[@]}"; do
    if [[ -f "$file" ]]; then
        echo "   ✅ Found: $file"
    else
        echo "   ❌ Missing: $file"
        EXIT_CODE=1
    fi
done

echo ""

# Summary
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if [[ $EXIT_CODE -eq 0 ]]; then
    echo "✅ All pre-flight checks passed!"
    echo "🚀 Ready for deployment"
    echo ""
    echo "Next steps:"
    echo "   1. Review .env.production configuration"
    echo "   2. Deploy: docker compose up -d"
else
    echo "❌ Some pre-flight checks failed"
    echo "🔧 Please fix issues before deploying"
    echo ""
    echo "Common fixes:"
    echo "   - Configure .env.production with actual values"
    echo "   - Run 'npm install' in both apps"
    echo "   - Fix any build or test errors"
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

exit $EXIT_CODE
