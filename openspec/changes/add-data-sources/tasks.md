## 1. Memory client: providers & config

- [ ] 1.1 Add `DataSourceIntegration`, `Provider`, `ProviderSchema`, `SourceType`, `TestConnectionResponse`, `TriggerSyncResponse`, `SyncJob`, `ListSyncJobsResponse` DTOs to `gateway/memory.go`; verify a unit test unmarshals a sample JSON response for each shape
- [ ] 1.2 Add client methods `ListDataSources`, `GetDataSource`, `CreateDataSource`, `UpdateDataSource`, `DeleteDataSource`; verify unit tests against the fake memory backend cover list/create/update/delete and a 404 delete
- [ ] 1.3 Add client methods `ListProviders`, `GetProviderSchema`, `TestProviderConfig`, `TestDataSourceConnection`; verify unit tests cover the provider list, schema fetch, and both connection-test paths

## 2. Memory client: sync

- [ ] 2.1 Add client methods `TriggerDataSourceSync`, `ListSyncJobs`, `GetLatestSyncJob`, `GetSyncJob`, `CancelSyncJob`; verify unit tests cover trigger (with/without jobId), list, latest-null, and cancel paths

## 3. Memory client: discovery

- [ ] 3.1 Add client methods `StartDiscoveryJob`, `GetDiscoveryJob`, `ListDiscoveryJobs`, `FinalizeDiscoveryJob`, `CancelDiscoveryJob` with the `document_ids`-based start payload; verify unit tests cover start/monitor/finalize

## 4. Handlers & routes

- [ ] 4.1 Add JSON API handlers for data-source CRUD, connection testing, sync trigger/monitor/cancel, and discovery start/status/finalize; verify handler tests cover success, validation, and memory-error (502/404) paths
- [ ] 4.2 Register the data-source API routes and verify they require the session-or-`X-API-Key` middleware (a test asserts unauthenticated calls get 401)

## 5. Web UI

- [ ] 5.1 Add the Data Sources sidebar entry + list page (integrations with provider/status/last-sync badges, empty state); verify a templ render test asserts the page renders with and without integrations
- [ ] 5.2 Add the connect form rendered from the provider config schema, with a "test connection" action; verify render tests cover provider selection and the test-result feedback
- [ ] 5.3 Add the integration detail/edit page with sync-mode/interval fields, test-connection, trigger-sync, sync-job history, and delete; verify render tests cover detail, in-progress job, and delete confirm
- [ ] 5.4 Add the schema-discovery flow (select documents → start → review discovered types → finalize); verify render tests cover start and finalize states

## 6. Verification

- [ ] 6.1 `go build ./...` + `go vet ./...` + `go test ./...` in `gateway/` pass
- [ ] 6.2 `templ generate` produces no diff and `task lint` passes
- [ ] 6.3 Manual browser test: list data sources, connect an IMAP source with a connection test, trigger a sync, watch job progress, cancel a job, and run discovery — each step observable in the DevTools browser
