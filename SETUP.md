# Setup Guide

This guide gets you from zero to a locally running stack with the Go API server and its infrastructure.

## Prerequisites

- Go 1.25+
- Docker and Docker Compose
- Taskfile (`task`)

## 1) Configure environment

Create a root `.env` for the server:

```bash
cp .env.example .env
# Edit .env and set at least:
# - GOOGLE_API_KEY=...         # for Gemini embeddings/chat
# - Database connection variables
```

## 2) Start infrastructure

The project includes essential services like PostgreSQL and transcription/extraction tools in a Docker Compose file.

```bash
# Start PostgreSQL, Whisper, and Kreuzberg
docker compose -f docker/docker-compose.dev.yml up -d
```

Key endpoints:
- PostgreSQL: `localhost:5432` (User: `spec`, Pass: `spec`, DB: `spec`)

## 3) Run migrations

Ensure the database schema is up to date:

```bash
task migrate:up
```

## 4) Run the server

You can start the server in development mode with hot-reloading (requires [air](https://github.com/air-verse/air)):

```bash
task dev
```

The server will be available at `http://localhost:5300` (or the port specified in your `.env`).

## 5) Verification

Check the health endpoint to ensure the server is running correctly:

```bash
curl http://localhost:5300/health
```

## 6) Observability (Optional)

### Langfuse
To enable LLM trace debugging for extraction jobs:
1. Configure `LANGFUSE_ENABLED=true` in your `.env`.
2. Provide your Langfuse host, public key, and secret key.

### OpenTelemetry
To enable tracing with Grafana Tempo:
1. Start the observability profile: `docker compose -f docker/docker-compose.dev.yml --profile observability up -d`.
2. Set `OTEL_ENABLED=true` in your `.env`.

## References

- **[QUICK_START_DEV.md](QUICK_START_DEV.md)** - Fast-track developer commands.
- **[RUNBOOK.md](RUNBOOK.md)** - Operational details.
- **[CONTRIBUTING.md](CONTRIBUTING.md)** - Development guidelines.
