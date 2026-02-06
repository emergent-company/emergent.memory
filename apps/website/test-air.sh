#!/bin/bash
echo "Testing Air hot reload..."
echo ""
echo "1. Starting server with Air..."
timeout 5 air > /tmp/air-test.log 2>&1 &
AIR_PID=$!
sleep 3

echo "2. Testing initial server response..."
curl -s http://localhost:4002/health | head -1

echo ""
echo "3. Air is running. Check /tmp/air-test.log for output"
echo ""
echo "Air PID: $AIR_PID"
echo "Kill with: kill $AIR_PID"

kill $AIR_PID 2>/dev/null
wait $AIR_PID 2>/dev/null

echo ""
echo "✅ Air test complete. Check /tmp/air-test.log for full output"
tail -20 /tmp/air-test.log
