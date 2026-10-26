# E2E test stacks

The NestJS-era ephemeral database described in this file (port 5439,
`spec`/`spec_e2e`, `npm run db:e2e:*`, `nx run server-go:test-e2e`) no longer
exists. `docker-compose.e2e.yml` was removed with it.

Current suites:

| Suite | Location | Run |
|---|---|---|
| API e2e (plain `go test`, no Docker) | `e2e/tests/api/` | `task test:e2e` |
| CLI e2e (runlog + Docker server) | `e2e/tests/cli/` | `task -d e2e test:mcj` |
| Web UI e2e (Playwright) | `apps/web-ui/tests/e2e/` | `cd apps/web-ui && task e2e:test` |

`e2e/docker-compose.yml` provisions its own ephemeral Postgres, server and
storage per run — no host-side database setup is required. See
[`../e2e/CONSTITUTION.md`](../e2e/CONSTITUTION.md) and
[`../apps/web-ui/tests/e2e/README.md`](../apps/web-ui/tests/e2e/README.md) for
the suite contracts.
