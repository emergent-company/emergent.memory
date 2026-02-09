# Mac Standalone Installation (Local Build)

This guide helps you test the **Standalone Version** of Emergent using your local repository code. This is different from the standard developer setup and bundles the backend, CLI, database, and extraction services into a single deployment.

**Note:** This method builds the server from your local source code, ensuring you are testing the latest version with security fixes.

## Prerequisites

- ✅ **Docker Desktop** running
- ✅ **Repository** cloned locally
- ✅ **Terminal** open in repository root

## Step 1: Prepare Environment

1. Navigate to the deployment directory:

   ```bash
   cd deploy/minimal
   ```

2. Create the environment configuration file `.env.local`:

   ```bash
   # Create file
   nano .env.local
   ```

3. Paste the following configuration (I've included your new Google API key):

   ```env
   # Secure random passwords
   POSTGRES_USER=emergent
   POSTGRES_PASSWORD=secure_postgres_password_123
   POSTGRES_DB=emergent
   POSTGRES_PORT=15432

   MINIO_ROOT_USER=minioadmin
   MINIO_ROOT_PASSWORD=secure_minio_password_123
   MINIO_API_PORT=19000
   MINIO_CONSOLE_PORT=19001

   # Standalone Configuration
   STANDALONE_MODE=true
   STANDALONE_API_KEY=test-api-key-12345
   STANDALONE_USER_EMAIL=admin@localhost
   STANDALONE_ORG_NAME=My Organization
   STANDALONE_PROJECT_NAME=Default Project

   # Services Ports
   KREUZBERG_PORT=18000
   SERVER_PORT=13002

   # AI Configuration (New Key)
   GOOGLE_API_KEY=YOUR_GOOGLE_API_KEY
   EMBEDDING_DIMENSION=768

   # Logging
   KREUZBERG_LOG_LEVEL=info
   ```

   **Save and exit** (`Ctrl+X`, `Y`, `Enter`).

## Step 2: Build Local Image

Since we want to test the **current code** (not an old public image), we need to build the server image locally.

```bash
# Ensure the build script is executable
chmod +x build-server-with-cli.sh

# Run the build (takes 1-2 minutes)
./build-server-with-cli.sh
```

**Expected output:**

```
Building emergent-server-with-cli:latest...
...
✅ Build complete: emergent-server-with-cli:latest
```

## Step 3: Start Services

Now start the standalone stack using the local Docker Compose configuration.

```bash
docker compose -f docker-compose.local.yml --env-file .env.local up -d
```

**What starts:**

- **Server + CLI**: Port 13002 (mapped to internal 3002)
- **Postgres**: Port 15432
- **MinIO**: Ports 19000/19001
- **Kreuzberg**: Port 18000

## Step 4: Verify Installation

Wait about 30 seconds for everything to initialize, then verify:

1. **Check Health Endpoint:**

   ```bash
   curl http://localhost:13002/health
   ```

   _Expected: `{"status":"ok",...}`_

2. **Test CLI Access:**
   The server container includes the `emergent-cli` tool.

   ```bash
   # List projects (should show "Default Project")
   docker exec emergent-server emergent-cli projects list
   ```

3. **View Logs:**
   ```bash
   docker compose -f docker-compose.local.yml logs -f server
   ```

## Step 5: Test Embeddings (Verification)

To verify the new Google API key is working correctly in this build:

1. **Get an internal shell:**

   ```bash
   docker exec -it emergent-server sh
   ```

2. **Run a test embedding via CLI (inside container):**

   ```bash
   # This uses the configured GOOGLE_API_KEY
   emergent-cli embeddings test --text "Hello world"
   ```

   _(If the CLI command `embeddings test` doesn't exist yet, we can verify via logs when ingesting a document)_

3. **Alternative: Ingest a document**

   ```bash
   # Exit container first
   exit

   # Call ingest endpoint
   curl -X POST http://localhost:13002/api/v1/ingest/text \
     -H "X-API-Key: test-api-key-12345" \
     -H "Content-Type: application/json" \
     -d '{"text": "This is a test document to verify embeddings.", "filename": "test.txt"}'
   ```

## Cleanup

To stop the standalone services:

```bash
docker compose -f docker-compose.local.yml down -v
```

## Summary of Ports (Local Standalone)

| Service   | Local Port | Internal Port |
| --------- | ---------- | ------------- |
| Server    | **13002**  | 3002          |
| Postgres  | **15432**  | 5432          |
| Kreuzberg | **18000**  | 8000          |
| MinIO API | **19000**  | 9000          |
| MinIO UI  | **19001**  | 9001          |

These non-standard ports prevent conflicts with any other development environment you might be running.

## Public Installation Script (For Sharing)

If you need to share a one-line installer with others (once the repo/images are public), we have prepared `deploy/minimal/install-online.sh`.

**Usage:**

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent/main/deploy/minimal/install-online.sh | bash
```

**Prerequisites for this to work publicly:**

1. The repository `emergent-company/emergent` must be **public**.
2. The Docker images (`ghcr.io/emergent-company/emergent-server-go:latest` etc.) must be **publicly accessible**.

**To test this script locally (simulation):**

```bash
# Run the local copy of the script
./deploy/minimal/install-online.sh
```
