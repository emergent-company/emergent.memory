#!/bin/bash
set -e

echo "=== Emergent Server Startup ==="

# Wait for database to be ready
echo "Waiting for database..."
max_retries=30
retry_count=0

while [ $retry_count -lt $max_retries ]; do
    if pg_isready -h "${POSTGRES_HOST:-db}" -p "${POSTGRES_PORT:-5432}" -U "${POSTGRES_USER:-emergent}" >/dev/null 2>&1; then
        echo "Database is ready!"
        break
    fi
    retry_count=$((retry_count + 1))
    echo "Waiting for database... ($retry_count/$max_retries)"
    sleep 1
done

if [ $retry_count -eq $max_retries ]; then
    echo "ERROR: Database not ready after ${max_retries} seconds"
    exit 1
fi

# Run database migrations
echo "Running database migrations..."
/usr/local/bin/emergent-migrate -c up

echo "Migrations complete!"

# Start the server
echo "Starting Emergent server..."
exec /usr/local/bin/emergent-server "$@"
