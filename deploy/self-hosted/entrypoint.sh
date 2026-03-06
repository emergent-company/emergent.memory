#!/bin/bash
set -e

echo "=== Emergent Server Startup ==="

export DB_HOST="${POSTGRES_HOST:-db}"
export DB_PORT="${POSTGRES_PORT:-5432}"

echo "Waiting for database at ${DB_HOST}:${DB_PORT}..."
max_retries=30
retry_count=0

while [ $retry_count -lt $max_retries ]; do
    if pg_isready -h "${DB_HOST}" -p "${DB_PORT}" -U "${POSTGRES_USER:-emergent}" >/dev/null 2>&1; then
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

echo "Running database migrations..."
/usr/local/bin/emergent-migrate -c up

echo "Migrations complete!"

if [ -n "${STANDALONE_API_KEY:-}" ]; then
    echo "Configuring CLI..."
    cat > /root/.emergent/config.yaml <<EOF
server_url: http://localhost:3002
api_key: ${STANDALONE_API_KEY}
EOF
    echo "CLI configured!"
fi

echo "Starting Emergent server..."
exec /usr/local/bin/emergent-server "$@"
