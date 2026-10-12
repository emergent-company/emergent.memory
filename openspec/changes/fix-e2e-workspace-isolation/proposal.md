## Why

The `adk-v2-migration` change added a requirement that the `e2e` module not share
the repo workspace build list, and implemented it with `GOWORK=off` on the CI
jobs only. The root `go.work` still listed `./e2e` and its two stubs, so a bare
`go build ./...` / `go test ./...` inside `e2e/` compiled through the root build
list and broke:

```
google.golang.org/adk@v0.6.0/internal/telemetry/converters.go:34: undefined: log.Value
```

`runlog` pins `google.golang.org/adk v0.6.0`, whose `internal/telemetry` was
written against `otel/log v0.16.0`, while the workspace resolves the `otel/log`
that `adk/v2 v2.5.0` selects. CI's `GOWORK=off` masked the coupling; local runs
had no escape (issue #1744).

## What Changes

- **The root `go.work` stops listing `./e2e`, `./e2e/stubs/mailgun` and
  `./e2e/stubs/mcp-registry`.** The `e2e` module gains its own `e2e/go.work`
  claiming itself and both stubs, so the module resolves its own dependency graph
  in workspace mode as well. CI keeps `GOWORK=off`.
- **The `go.work` coverage guard becomes isolation-aware.** A module is covered
  when the root `go.work` lists it, or when the allowlisted isolated root's own
  `go.work` (`isolated_roots`, currently `./e2e`) claims it. Isolation stays
  opt-in by editing that list — nesting a `go.work` elsewhere, or deeper inside
  the isolated subtree, does not let a module escape the root build list, so the
  #923 drift class stays caught. Nested entries are validated for staleness like
  root ones.
- **The e2e Docker/compose paths stay in module mode.** The image now ships
  `e2e/go.work`; the compose test service and `run_tests.sh --tests-only` both set
  `GOWORK=off` explicitly, so container behaviour is unchanged.
- **Documentation and comments updated** (`e2e/AGENTS.md` "Module isolation"
  section; the three `e2e.yml` job comments no longer attribute the breakage to
  "otel bump breaks runlog" but to `adk v0.6.0`'s telemetry versus the `otel/log`
  that `adk/v2` selects).

The requirement itself is restated: isolation is structural (own workspace file,
root `go.work` exclusion, allowlisted guard exemption), not just a CI flag.
