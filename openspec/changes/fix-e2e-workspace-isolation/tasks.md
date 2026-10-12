## 1. Structural isolation

- [x] 1.1 Remove `./e2e`, `./e2e/stubs/mailgun`, `./e2e/stubs/mcp-registry` from the
  root `go.work`. Verify: `go build ./...` in `apps/server`, `apps/cli`,
  `apps/web-ui/gateway` (generated assets present) and every `blueprints/**` tool
  still resolves — the root build list loses only e2e-owned entries.
- [x] 1.2 Add `e2e/go.work` claiming `.` and both stub modules, plus the
  `e2e/go.work.sum` it requires. Verify: `cd e2e && go build ./...` exits 0 with no
  `GOWORK` override, and `go env GOWORK` resolves to `e2e/go.work`.
- [x] 1.3 Re-run `go list -m all` to canonicalise `e2e/go.work.sum` (stable
  afterwards; no tree churn on repeated runs).
- [x] 1.4 Refresh the root `go.work.sum` for the new build list (+10/−2; the two
  dropped entries were e2e-only).

## 2. Coverage guard

- [x] 2.1 Teach `.github/scripts/check-go-work-coverage.sh` rule 2 (nested
  workspace coverage) but scope it to an explicit `isolated_roots` allowlist
  (`./e2e`) **and** to that root's own `go.work`, so neither nesting a `go.work`
  elsewhere nor one deeper inside the subtree can drop a module from the root
  build list. Verify: a synthetic `zzz-exploit/` module with its own `go.work`
  fails the guard.
- [x] 2.2 Keep rejecting drift inside an isolated root: dropping an entry from
  `e2e/go.work`, or removing the file, fails the guard.
- [x] 2.3 Validate nested `go.work` entries for staleness (entry with no `go.mod`).
- [x] 2.4 `shellcheck .github/scripts/check-go-work-coverage.sh` clean; guard exits
  0 on the real tree.
- [x] 2.5 Rule 2 accepts only the isolated root's own `go.work`, not a deeper file:
  `e2e/foo/go.work` claiming `.` while `e2e/go.work` omits `./foo` fails the guard
  (a command run at `e2e/` resolves `e2e/go.work`, so it would otherwise stop
  building `e2e/foo` silently).

## 3. Runtime paths

- [x] 3.1 `GOWORK: "off"` for the compose `test-emergent-client` service (the image
  ships `e2e/go.work`), and `-e GOWORK=off` for `run_tests.sh --tests-only`, so
  container runs stay in module mode exactly as before `e2e/go.work` existed.
- [x] 3.2 Verify the CI path is unaffected: `cd e2e && GOWORK=off go build ./...`
  and `go build -mod=readonly ./...` both exit 0.
- [x] 3.3 Verify stub modules build (`cd e2e/stubs/<stub> && go build ./...`).

## 4. Docs and spec

- [x] 4.1 `e2e/AGENTS.md`: "Module isolation" section (root `go.work` exclusion,
  `e2e/go.work`, CI `GOWORK=off`, guard allowlist).
- [x] 4.2 Correct the attribution in the three `e2e.yml` job comments (it is
  `adk v0.6.0`'s `internal/telemetry` versus the `otel/log` that `adk/v2` selects —
  not "otel bump breaks runlog").
- [x] 4.3 Restate the `server-agent-runtime` requirement delta: isolation is
  structural (root exclusion + own workspace + allowlisted guard exemption) rather
  than a CI-only flag. Verify: `openspec validate fix-e2e-workspace-isolation
  --strict` passes.
