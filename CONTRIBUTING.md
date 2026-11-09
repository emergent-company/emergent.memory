# Contributing

## Prerequisites

| Tool | Needed for |
|---|---|
| [Go](https://go.dev/dl/) 1.26+ | server, CLI, SDKs |
| [Task](https://taskfile.dev/installation/) | every build and test entry point |
| [lefthook](https://lefthook.dev/) | git hooks (`task hooks:install`) |
| Docker with Compose v2 | the local dependency stack, the web UI e2e suite |
| Node 20+ | `apps/web-ui` (CSS build, Playwright) |

### Local dependencies

`task dev` needs a **PostgreSQL 18 + pgvector** database with a migrated schema,
an **S3-compatible object store**, and something to authenticate against: the
`/health` endpoint reports `database`, `storage` and `auth` as critical, so it
returns 503 until all three are healthy.

The database and object store ship in the self-hosted stack in this repo. Bring
up just those services and let `air` run the server from source:

```bash
# SeaweedFS takes the same credential pair under the compose file's names. The
# server's tracked .env uses emergent/emergent, so give the container the same
# values (its own default secret is "changeme", which would be rejected).
export OBJECT_STORE_ACCESS_KEY=emergent OBJECT_STORE_SECRET_KEY=emergent

# Bucket bootstrap runs from the server image, so build it once.
docker compose -f deploy/self-hosted/docker-compose.local.yml build server
docker compose -f deploy/self-hosted/docker-compose.local.yml up -d db seaweedfs storage-init
```

Then put the values that have no safe default in `.env.local` (gitignored, and
where every secret belongs — the server re-reads it over `.env`):

```bash
POSTGRES_PASSWORD=changeme   # what the compose file creates the database with
STANDALONE_MODE=true         # authenticate with an API key instead of Zitadel
STANDALONE_API_KEY=<random>  # e.g. $(openssl rand -hex 32)
```

`STANDALONE_MODE` is what self-hosted installs use: with it set, the server
validates API keys directly instead of doing OIDC discovery against
`ZITADEL_ISSUER`, and the `auth` health component reports healthy. If you want
OIDC locally, run a Zitadel deployment and set `ZITADEL_ISSUER` to a URL it can
actually reach instead.

Migrate once, then start the server:

```bash
task migrate:up
task dev
```

Two things that catch people out:

- Task interpolation is **first-file-wins**, so values a task passes to a command
  (`POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB` for `task migrate:*`) are read
  from the tracked `.env`, not from `.env.local` — those defaults are already
  there, and copying them into `.env.local` has no effect on the task. The
  password is different: `task` exports `.env.local`-only keys to the commands it
  runs, so `POSTGRES_PASSWORD` reaches both `task migrate:up` and `task dev`. Run
  the migrate binary directly and it reads no env files at all — pass
  `POSTGRES_PASSWORD=…` inline (as `apps/server/migrations/README.md` shows).
- `storage` is critical and the server pings the documents bucket, so `/health`
  fails until SeaweedFS is up *and* the bucket exists — that is what the one-shot
  `storage-init` service creates.

To run the whole stack in containers instead of from source, follow
[`deploy/self-hosted/README.md`](deploy/self-hosted/README.md).

`task test:integration` creates a throwaway database per run from a migrated
template, so the Postgres role needs `CREATEDB`.

## Building and Testing

The repo uses [Taskfile](https://taskfile.dev) for its build and test entry
points. Run tasks from the repository root:

```bash
task build              # build the Go server binary
task test               # run backend unit tests
task test:integration   # run backend integration tests (needs the database above)
task test:e2e           # run the API e2e suites
task lint               # run all linters via lefthook
task hooks:install      # install git hooks (once per checkout)
```

Sub-projects carry their own `Taskfile.yml` (`apps/server/`, `apps/web-ui/`,
`apps/connector.mac/`, `e2e/`); run them with `task -d <dir> <task>`.

## Pull Requests

- `dev` is the default branch and receives feature work. `main` is protected;
  do not push directly to either.
- Use conventional commits (`feat:`, `fix:`, `docs:`, `chore:`, etc.).
- Open a feature/docs/chore pull request against **`dev`**
  (`gh pr create --base dev`). Run the test suite locally first.
- Hotfixes (`fix/*`) may target `main` directly. `dev -> main` is the promotion
  PR and carries the full suite; it merges with a merge commit (not squash).
- An independent review is required before merge. Feature/hotfix PRs are
  squashed on merge; promotions keep their history.

## CI

CI runs on GitHub-hosted runners for jobs that execute pull-request-controlled
code (the migration-order and route-authority guards). External (fork) pull
requests run the required `ci` check as a no-op on the self-hosted runner until
a maintainer re-runs it from a trusted ref; the guards that execute
PR-controlled Go still run on GitHub-hosted runners.
