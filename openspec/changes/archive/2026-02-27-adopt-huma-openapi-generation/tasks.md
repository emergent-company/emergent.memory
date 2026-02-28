# Tasks: Adopt Huma for OpenAPI Generation

## 1. Setup & Infrastructure

- [ ] 1.1 Add `github.com/danielgtaylor/huma/v2` to go.mod
- [ ] 1.2 Create `internal/server/huma.go` with humaecho adapter setup
- [ ] 1.3 Configure Bearer JWT security scheme
- [ ] 1.4 Configure OpenAPI metadata (title, version, description)
- [ ] 1.5 Wire Huma API into existing Echo router in `server.go`
- [ ] 1.6 Serve OpenAPI spec at `/openapi.json` and docs at `/docs`

## 2. Shared Types & Patterns

- [ ] 2.1 Create `pkg/huma/` with shared request/response helpers
- [ ] 2.2 Define common pagination types (cursor, offset)
- [ ] 2.3 Define common error response types matching RFC 9457
- [ ] 2.4 Create auth middleware adapter for Huma operations

## 3. Pilot Migration (Simple Domains)

- [ ] 3.1 Convert `health` domain to Huma pattern
- [ ] 3.2 Convert `userprofile` domain to Huma pattern
- [ ] 3.3 Convert `users` domain to Huma pattern
- [ ] 3.4 Verify OpenAPI output for pilot domains
- [ ] 3.5 Ensure E2E tests still pass

## 4. Core Domain Migration

- [ ] 4.1 Convert `documents` domain (List, Get, Create, Delete, BulkDelete)
- [ ] 4.2 Convert `chunks` domain
- [ ] 4.3 Convert `search` domain
- [ ] 4.4 Convert `graph` domain (largest - nodes, edges, branches, policies)
- [ ] 4.5 Convert `projects` domain
- [ ] 4.6 Convert `orgs` domain

## 5. Supporting Domain Migration

- [ ] 5.1 Convert `tasks` domain
- [ ] 5.2 Convert `events` domain
- [ ] 5.3 Convert `notifications` domain
- [ ] 5.4 Convert `invites` domain
- [ ] 5.5 Convert `useraccess` domain
- [ ] 5.6 Convert `useractivity` domain
- [ ] 5.7 Convert `templatepacks` domain

## 6. Admin Domain Migration

- [ ] 6.1 Convert `superadmin` domain
- [ ] 6.2 Convert `devtools` domain (keep raw Echo for file serving)

## 7. Special Cases

- [ ] 7.1 Keep `chat` domain SSE endpoints as raw Echo
- [ ] 7.2 Keep `mcp` domain WebSocket/SSE as raw Echo
- [ ] 7.3 Document hybrid approach in AGENT.md

## 8. Validation & Documentation

- [ ] 8.1 Compare Go OpenAPI output with NestJS spec (functional equivalence)
- [ ] 8.2 Run full E2E test suite
- [ ] 8.3 Update `apps/server-go/AGENT.md` with Huma patterns
- [ ] 8.4 Add example of adding new endpoint with Huma
- [ ] 8.5 Update devtools to serve Go-generated spec instead of placeholder

## 9. Cleanup

- [ ] 9.1 Remove placeholder OpenAPI stub from devtools
- [ ] 9.2 Update root README with Go OpenAPI generation info
- [ ] 9.3 Consider CI check for OpenAPI spec drift
