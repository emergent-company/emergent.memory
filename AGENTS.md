# Emergent

Go monorepo for the Emergent knowledge graph platform. React admin UI lives in a **separate repo** at `/root/emergent.memory.ui`.

## Build, Lint, Test

```bash
# Backend (repo root or apps/server-go)
task build          # build Go server binary
task test           # unit tests
task test:e2e       # API e2e tests
task lint           # Go linter

# Frontend (/root/emergent.memory.ui)
pnpm run lint
pnpm run test
```

## Hot Reload — DO NOT restart after code changes

The Go server uses `air`. Changes are picked up in 1-2 seconds automatically.

- **Just save the file** — hot reload handles Go handler/service/store changes
- **Restart only for**: new fx modules in `cmd/server/main.go`, env var changes, after `go mod tidy`, server down

```bash
task status      # check server health first
task dev         # start with hot reload (foreground)
task start       # build + start in background
task stop        # stop background server
```

## Environment URLs

| | Admin | Server |
|---|---|---|
| Domain (preferred) | `https://admin.dev.emergent-company.ai` | `https://api.dev.emergent-company.ai` |
| Localhost | `http://localhost:5176` | `http://localhost:5300` |
| mcj-emergent (remote test) | — | `http://localhost:3002` (via SSH tunnel) |

## Before Writing Code — Check These First

| Creating… | Read first… |
|---|---|
| React component | `/root/emergent.memory.ui/src/components/AGENT.md` — 50+ components |
| React hook | `/root/emergent.memory.ui/src/hooks/AGENT.md` — use `useApi` for ALL API calls |
| Go endpoint | `apps/server-go/AGENT.md` — fx modules, Echo, Bun ORM |
| Database entity | `apps/server-go/AGENT.md` — Bun models, kb/core schemas |

Common mistakes: raw `fetch()` calls (use `useApi`), creating components that already exist.

## Code Style

- **Go**: `gofmt`, no unused imports, wrap errors: `fmt.Errorf("context: %w", err)`
- **TypeScript**: strict, no `any`
- **Database**: always schema-qualified — `kb.documents`, `core.user_profiles`

## Observability

Tracing is opt-in. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable (no-op when unset).

```bash
docker compose --profile observability up tempo -d  # start Tempo
emergent traces list --since 30m                    # query traces
emergent traces get <traceID>                       # full span tree
```

## Logs

```
logs/server/server.log        logs/server/server.error.log
logs/admin/admin.out.log      logs/admin/admin.error.log
```

## Gotchas

- `docs/site/` is tracked in git — do NOT add to `.gitignore`
- `search/client_test.go` and `health/client_test.go` have pre-existing compile errors; ignore unless working on those packages

## Detail Docs

| File | Contents |
|------|----------|
| `apps/server-go/AGENT.md` | fx modules, Echo handlers, Bun ORM, job queues |
| `apps/server-go/migrations/README.md` | Goose migration workflow |
| `/root/emergent.memory.ui/src/components/AGENT.md` | 50+ components, atomic design, DaisyUI |
| `/root/emergent.memory.ui/src/components/organisms/DataTable/AGENT.md` | DataTable config, columns, sorting |
| `/root/emergent.memory.ui/src/contexts/AGENT.md` | Auth, Theme, Toast, Modal contexts |
| `/root/emergent.memory.ui/src/hooks/AGENT.md` | 33+ hooks, `useApi` patterns |
| `/root/emergent.memory.ui/src/pages/AGENT.md` | route structure, page layouts |
| `docs/testing/AI_AGENT_GUIDE.md` | full testing guide |
| `docs/database/schema-context.md` | DB schema reference |
