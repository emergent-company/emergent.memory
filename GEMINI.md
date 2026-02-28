# Gemini Workspace

This document provides instructions and guidelines for interacting with the Spec Server 2 project using Gemini.

## About the Project

Spec Server 2 is a minimal ingestion server that can ingest a URL or uploaded file, extract text, chunk, embed with Google Gemini `text-embedding-004`, and store it in Postgres with pgvector and FTS.

The project uses a hybrid approach to database operations, using `bun` for PostgreSQL-specific features and multi-tenant Row-Level Security (RLS) infrastructure.

## Technologies

- **Backend:** Go (1.25+)
- **Database:** PostgreSQL 16 with pgvector
- **Authentication:** Zitadel (OIDC)
- **CLI:** Go (emergent-cli)
- **Automation:** Taskfile

## Key Commands

- **Build server:** `task build`
- **Run tests:**
  - `task test` (Unit tests)
  - `task test:integration` (Integration tests)
  - `task test:e2e` (E2E tests)
- **Lint:** `task lint`
- **Database migrations:**
  - `task migrate:up`
  - `task migrate:status`
- **Hot reload (dev):** `task dev` (requires `air`)

## Development Workflow

1.  **Start dependencies:** Ensure PostgreSQL and Zitadel are running.
2.  **Make changes:** Modify the code in `apps/server-go/` or `pkg/`.
3.  **Run tests:** Execute `task test` or `task test:integration` to ensure your changes haven't introduced regressions.
4.  **Lint:** Run `task lint`.
5.  **Commit:** Use the conventional commit format (e.g., `feat:`, `fix:`, `docs:`).

## Testing

- **Unit Tests:** Run with `task test`. Aim for 80%+ coverage.
- **Integration Tests:** Used for Strategic SQL patterns and critical paths. These run against a real PostgreSQL database.
- **E2E Tests:** Cover user flows from end to end using `task test:e2e`.

When modifying database-related code, it's crucial to add or update integration tests.

## Code Style

- **Go:** Follow standard Go idioms and `gofmt` conventions.
- **Database:** When making changes to the database, use `bun` models and respect multi-tenant RLS-enforced queries where applicable.

## Commits

This project uses [conventional commits](https://www.conventionalcommits.org/). When committing changes, please follow this format. Gemini should generate commit messages that adhere to this standard.

**Examples:**

- `feat(auth): add OAuth2 password grant support`
- `fix(graph): resolve N+1 query in relationship loading`
- `docs(patterns): add Strategic SQL patterns guide`
