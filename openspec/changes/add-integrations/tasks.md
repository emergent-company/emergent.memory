## 1. Memory client — general integrations

- [ ] 1.1 Write `MemoryClient` unit tests (fake backend) for `ListAvailableIntegrations` and `ListIntegrations`, asserting the exact `/api/integrations/available` and `/api/integrations` URLs and the `X-Project-ID`/`X-Org-ID` headers; verify `go test ./...` in `gateway/` fails on the missing methods (TDD)
- [ ] 1.2 Implement `ListAvailableIntegrations` and `ListIntegrations` on `MemoryClient` and add them to the `MemoryBackend` interface; verify the tests from 1.1 pass
- [ ] 1.3 Write unit tests for `GetIntegration`, `CreateIntegration`, `UpdateIntegration`, `DeleteIntegration` covering the name-scoped URLs, the 404 and 409 error paths, and `Delete` returning `204`; verify tests fail before implementation
- [ ] 1.4 Implement `GetIntegration`, `CreateIntegration`, `UpdateIntegration`, `DeleteIntegration` on `MemoryClient` + interface; verify the tests from 1.3 pass
- [ ] 1.5 Write unit tests for `TestIntegration` (`POST /{name}/test`) and `SyncIntegration` (`POST /{name}/sync`) covering success and the "does not support import" error path; verify tests fail before implementation
- [ ] 1.6 Implement `TestIntegration` and `SyncIntegration` on `MemoryClient` + interface; verify the tests from 1.5 pass

## 2. Memory client — GitHub App

- [ ] 2.1 Write unit tests for `GitHubConnect`, `GitHubStatus`, `GitHubDisconnect` (and `GitHubCLIToken`), asserting the `/api/v1/settings/github/*` URLs and method verbs; verify tests fail before implementation
- [ ] 2.2 Implement `GitHubConnect` (returns authorization URL), `GitHubStatus`, `GitHubDisconnect` (and `GitHubCLIToken`) on `MemoryClient` + interface; verify the tests from 2.1 pass

## 3. Handlers + routes

- [ ] 3.1 Write handler tests for the integrations screen handler, covering the available/configured lists, the empty configured state, and the error state; verify tests fail before implementation
- [ ] 3.2 Implement the integrations page handler that loads available + configured lists and renders through the existing `page` shell; verify the tests from 3.1 pass
- [ ] 3.3 Write handler tests for the connect/disconnect and configure/test/sync/delete PRG actions, covering success redirect + flash and the `?err=` failure path; verify tests fail before implementation
- [ ] 3.4 Implement the GitHub connect (redirect to authorization URL) and disconnect handlers, and the integration create/update/test/sync/delete handlers using the PRG convention; verify the tests from 3.3 pass
- [ ] 3.5 Register the routes (`GET /integrations`, `POST /integrations/...`, `POST /integrations/github/...`) in `main.go`; verify a route-registration test asserts the endpoints are mounted

## 4. Web UI

- [ ] 4.1 Write a templ render test for the integrations screen asserting it renders the GitHub status, the available-provider list, and the configured-instance list; verify the test fails before the template exists
- [ ] 4.2 Implement `integrations.templ` + generate, and add a "Settings → Integrations" sidebar entry in `ui.go`; verify the render test from 4.1 passes and `templ generate` produces no diff

## 5. Verification

- [ ] 5.1 `go build ./...` + `go vet ./...` + `go test ./...` in `gateway/` pass
- [ ] 5.2 `templ generate` produces no diff and `task lint` passes
- [ ] 5.3 Manual browser test: open Integrations, connect GitHub (observe redirect), see status flip to connected, configure/test/sync/delete a general integration, and disconnect GitHub — each step observable in the DevTools browser
