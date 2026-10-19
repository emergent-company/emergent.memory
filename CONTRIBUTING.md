# Contributing

## Building and Testing

The repo uses [Taskfile](https://taskfile.dev) for its build and test entry
points. Run tasks from the repository root:

```bash
task build              # build the Go server binary
task test               # run backend unit tests
task test:integration   # run backend integration tests
task test:e2e           # run the API e2e suites
task lint               # run all linters via lefthook
task hooks:install      # install git hooks (once per checkout)
```

Sub-projects carry their own `Taskfile.yml` (`apps/server/`, `apps/web-ui/`,
`apps/connector.mac/`, `e2e/`); run them with `task -d <dir> <task>`.

## Pull Requests

- `main` is protected. Do not push directly.
- Use conventional commits (`feat:`, `fix:`, `docs:`, `chore:`, etc.).
- Open a pull request against `main`. An independent review is required before
  merge; the PR is squashed on merge.

## CI

CI runs on GitHub-hosted runners for jobs that execute pull-request-controlled
code (the migration-order and route-authority guards). External (fork) pull
requests run the required `ci` check as a no-op on the self-hosted runner until
a maintainer re-runs it from a trusted ref; the guards that execute
PR-controlled Go still run on GitHub-hosted runners.
