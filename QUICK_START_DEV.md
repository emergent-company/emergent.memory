# Quick Start – Local Development

The project uses `task` (Taskfile) for local development automation, providing a streamlined workflow for building, testing, and running the Go-based backend.

## TL;DR

```bash
# Start infrastructure
docker compose -f docker/docker-compose.dev.yml up -d

# Build and run the server with hot reload
task dev

# Run all unit tests
task test

# Stop infrastructure when done
docker compose -f docker/docker-compose.dev.yml down
```

## Infrastructure Setup

The essential infrastructure (PostgreSQL and transcription/extraction services) is managed via Docker Compose:

```bash
# Start PostgreSQL, Whisper, and Kreuzberg
docker compose -f docker/docker-compose.dev.yml up -d
```

Connection: `postgresql://spec:spec@localhost:5432/spec`

## Starting the Server

The server can be run in development mode with hot-reloading (requires [air](https://github.com/air-verse/air)):

```bash
$ task dev

# This will:
# 1. Load environment variables from .env
# 2. Start 'air' to watch for file changes
# 3. Rebuild and restart the server automatically on save
```

The server listens on port `5300` by default (as configured in `.env`).

## Status & Logs

The server logs to stdout. You can also inspect logs in the `apps/server-go/logs/` directory if configured.

## Daily Flow

```bash
# 1. Start infrastructure
docker compose -f docker/docker-compose.dev.yml up -d

# 2. Run migrations
task migrate:up

# 3. Start development server
task dev

# 4. When finished
ctrl+c
docker compose -f docker/docker-compose.dev.yml stop
```

## Building for Production

To create a production-ready binary:

```bash
task build
# Binary will be at apps/server-go/dist/server
```
