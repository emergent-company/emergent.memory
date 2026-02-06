#!/bin/bash
set -e

CONCURRENT=${BENCH_CONCURRENT:-10}
ITERATIONS=${BENCH_ITERATIONS:-50}

GO_PORT=3002
NESTJS_PORT=3003

echo "============================================"
echo "NestJS vs Go Performance Comparison"
echo "============================================"
echo "Concurrent workers: $CONCURRENT"
echo "Iterations/worker:  $ITERATIONS"
echo "Total requests:     $((CONCURRENT * ITERATIONS)) per endpoint"
echo "============================================"
echo ""

check_server() {
    local url=$1
    local name=$2
    if curl -sf "$url/health" > /dev/null 2>&1; then
        echo "[OK] $name is running at $url"
        return 0
    else
        echo "[FAIL] $name not responding at $url"
        return 1
    fi
}

cd "$(dirname "$0")/.."

echo "Phase 1: Checking servers..."
echo ""

GO_URL="http://localhost:$GO_PORT"
NESTJS_URL="http://localhost:$NESTJS_PORT"

GO_READY=false
NESTJS_READY=false

if check_server "$GO_URL" "Go server"; then
    GO_READY=true
fi

if check_server "$NESTJS_URL" "NestJS server"; then
    NESTJS_READY=true
fi

if [ "$GO_READY" = false ] && [ "$NESTJS_READY" = false ]; then
    echo ""
    echo "ERROR: No servers running. Start them first:"
    echo ""
    echo "  Terminal 1 (Go):     cd apps/server-go && go run ./cmd/server"
    echo "  Terminal 2 (NestJS): cd apps/server && SERVER_PORT=3003 npm run start:dev"
    echo ""
    exit 1
fi

echo ""
echo "Phase 2: Running benchmarks..."
echo ""

RESULTS_DIR="benchmark_results_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$RESULTS_DIR"

if [ "$GO_READY" = true ]; then
    echo "============================================"
    echo "Benchmarking GO server ($GO_URL)"
    echo "============================================"
    API_BASE_URL="$GO_URL" \
    SERVER_TYPE=go \
    BENCH_CONCURRENT=$CONCURRENT \
    BENCH_ITERATIONS=$ITERATIONS \
    go test -v -count=1 ./tests/api/benchmark/... 2>&1 | tee "$RESULTS_DIR/go_output.txt"
    
    mv benchmark_go_*.json "$RESULTS_DIR/" 2>/dev/null || true
    echo ""
fi

if [ "$NESTJS_READY" = true ]; then
    echo "============================================"
    echo "Benchmarking NESTJS server ($NESTJS_URL)"
    echo "============================================"
    API_BASE_URL="$NESTJS_URL" \
    SERVER_TYPE=nestjs \
    BENCH_CONCURRENT=$CONCURRENT \
    BENCH_ITERATIONS=$ITERATIONS \
    go test -v -count=1 ./tests/api/benchmark/... 2>&1 | tee "$RESULTS_DIR/nestjs_output.txt"
    
    mv benchmark_nestjs_*.json "$RESULTS_DIR/" 2>/dev/null || true
    echo ""
fi

echo "============================================"
echo "Phase 3: Results"
echo "============================================"
echo ""
echo "Results saved to: $RESULTS_DIR/"
ls -la "$RESULTS_DIR/"
echo ""

if [ "$GO_READY" = true ] && [ "$NESTJS_READY" = true ]; then
    echo "============================================"
    echo "COMPARISON SUMMARY"
    echo "============================================"
    echo ""
    
    GO_FILE=$(ls "$RESULTS_DIR"/benchmark_go_*.json 2>/dev/null | head -1)
    NESTJS_FILE=$(ls "$RESULTS_DIR"/benchmark_nestjs_*.json 2>/dev/null | head -1)
    
    if [ -n "$GO_FILE" ] && [ -n "$NESTJS_FILE" ]; then
        echo "Endpoint                     | Go p95    | NestJS p95 | Winner"
        echo "-----------------------------|-----------|------------|--------"
        
        for endpoint in "/health" "/healthz" "/ready" "/api/v2/documents" "/api/v2/projects"; do
            go_p95=$(jq -r ".endpoints[] | select(.endpoint == \"$endpoint\") | .p95_ms" "$GO_FILE" 2>/dev/null || echo "N/A")
            nest_p95=$(jq -r ".endpoints[] | select(.endpoint == \"$endpoint\") | .p95_ms" "$NESTJS_FILE" 2>/dev/null || echo "N/A")
            
            if [ "$go_p95" != "N/A" ] && [ "$nest_p95" != "N/A" ]; then
                if (( $(echo "$go_p95 < $nest_p95" | bc -l) )); then
                    winner="Go"
                else
                    winner="NestJS"
                fi
                printf "%-28s | %8.2fms | %9.2fms | %s\n" "$endpoint" "$go_p95" "$nest_p95" "$winner"
            fi
        done
        echo ""
    fi
fi

echo "Done!"
