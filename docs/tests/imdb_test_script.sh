#!/bin/bash

export SERVER_URL="http://mcj-emergent:3002"
export API_KEY="emt_ec70233facfa29385abfef9bff015df72f08f7205be51f3034b42bf1484d0ec1"
export PROJECT_ID="956e3e88-07c5-462b-9076-50ea7e1e7951"

# === DRY RUN MODE ===
# Set DRY_RUN=true to test the seeder with only the first 100 movies.
# This is perfect for verifying the SDK and graph relationships map correctly
# without waiting hours for the entire dataset to ingest.
#
# To run the FULL dataset, set DRY_RUN=false or remove this line.
export DRY_RUN="true"

# Alternatively, set an exact limit of titles to ingest
# export SEED_LIMIT="500"

echo "Using Project ID: $PROJECT_ID"
echo "Using Server: $SERVER_URL"
echo "Dry Run Mode: $DRY_RUN"

# Run the seeder
echo "Starting the IMDb Seeder..."
cd ../apps/server-go && go run ./cmd/seed-imdb/main.go

echo "Seeder finished. You can now run the benchmark script."
