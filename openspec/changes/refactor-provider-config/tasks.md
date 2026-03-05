## 1. Database Migration

- [x] 1.1 Create migration `apps/server-go/migrations/00042_refactor_provider_configs.sql` that drops `kb.organization_provider_model_selections`, `kb.organization_provider_credentials`, `kb.project_provider_policies`, and the `kb.provider_policy` enum (in dependency order), then creates `kb.org_provider_configs` (id, org_id FK→kb.orgs CASCADE, provider VARCHAR(50), encrypted_credential BYTEA NOT NULL, encryption_nonce BYTEA NOT NULL, gcp_project VARCHAR(255), location VARCHAR(100), generative_model VARCHAR(255), embedding_model VARCHAR(255), created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ, UNIQUE(org_id, provider)) and `kb.project_provider_configs` (same columns with project_id FK→kb.projects CASCADE, UNIQUE(project_id, provider)). Include a `-- +goose Down` that drops the two new tables (old tables are not restored).
- [x] 1.2 Run the migration locally: `PGPASSWORD=local-test-password goose -dir apps/server-go/migrations postgres "host=127.0.0.1 port=5436 user=emergent password=local-test-password dbname=emergent sslmode=disable" up` and verify the two new tables exist and the three old tables are gone.

## 2. Go Entity Layer

- [x] 2.1 In `apps/server-go/domain/provider/entity.go`, add `OrgProviderConfig` and `ProjectProviderConfig` structs (with `bun` tags for `kb.org_provider_configs` and `kb.project_provider_configs`). Keep `ResolvedCredential` as-is. Remove `OrganizationProviderCredential`, `OrganizationProviderModelSelection`, `ProjectProviderPolicy`, and the `ProviderPolicy` type/constants.
- [x] 2.2 In `entity.go`, add request/response types: `UpsertProviderConfigRequest` (fields: `APIKey`, `ServiceAccountJSON`, `GCPProject`, `Location`, `GenerativeModel`, `EmbeddingModel`) and `ProviderConfigResponse` (same fields except credential fields are masked/omitted).

## 3. Go Repository Layer

- [x] 3.1 In `apps/server-go/domain/provider/repository.go`, add `UpsertOrgProviderConfig(ctx, cfg *OrgProviderConfig) error`, `GetOrgProviderConfig(ctx, orgID, provider string) (*OrgProviderConfig, error)`, and `DeleteOrgProviderConfig(ctx, orgID, provider string) error`.
- [x] 3.2 Add `UpsertProjectProviderConfig(ctx, cfg *ProjectProviderConfig) error`, `GetProjectProviderConfig(ctx, projectID, provider string) (*ProjectProviderConfig, error)`, and `DeleteProjectProviderConfig(ctx, projectID, provider string) error`.
- [x] 3.3 Remove old repository methods: `SaveOrgCredential`, `GetOrgCredential`, `DeleteOrgCredential`, `ListOrgCredentials`, `SaveOrgModelSelection`, `GetOrgModelSelection`, `SaveProjectPolicy`, `GetProjectPolicy`, `ListProjectPolicies`.

## 4. Go Service Layer

- [x] 4.1 In `apps/server-go/domain/provider/service.go`, rewrite `Resolve(ctx, provider)`: (1) if `projectID` in context → call `GetProjectProviderConfig`; if found, decrypt and return; (2) derive `orgID` from project, call `GetOrgProviderConfig`; if found, decrypt and return; (3) if project or org present in context but no config found → return descriptive error; (4) if both absent → return `nil, nil`.
- [x] 4.2 Rewrite `ResolveAny(ctx)` to use the same two-step logic without requiring a `provider` argument (try configured provider types in order, or use `google-ai` as default).
- [x] 4.3 Add `UpsertOrgConfig(ctx, orgID, provider string, req UpsertProviderConfigRequest) (*ProviderConfigResponse, error)`: encrypt credential, call live test, sync catalog (with 5s timeout + static fallback), auto-select models if not provided in req, upsert row.
- [x] 4.4 Add `GetOrgConfig(ctx, orgID, provider string) (*ProviderConfigResponse, error)` that returns config metadata (no decrypted creds).
- [x] 4.5 Add `DeleteOrgConfig(ctx, orgID, provider string) error`.
- [x] 4.6 Add `UpsertProjectConfig(ctx, projectID, provider string, req UpsertProviderConfigRequest) (*ProviderConfigResponse, error)` (same flow as 4.3 but for project table).
- [x] 4.7 Add `DeleteProjectConfig(ctx, projectID, provider string) error`.
- [x] 4.8 Remove old service methods: `SaveOrgCredential`, `SetOrgModelSelection`, `SetProjectPolicy`, `GetProjectPolicy`, `ListProjectPolicies`, `resolveForProject`, `resolveForOrg`, `resolveFromEnv`, `decryptOrgCredential`, `decryptProjectCredential`.

## 5. Go Handler and Routes

- [x] 5.1 In `apps/server-go/domain/provider/handler.go`, add `SaveOrgConfig(c echo.Context) error` for `PUT /api/v1/organizations/:orgId/providers/:provider` — parse `UpsertProviderConfigRequest`, call `service.UpsertOrgConfig`, return `ProviderConfigResponse`.
- [x] 5.2 Add `GetOrgConfig(c echo.Context) error` for `GET /api/v1/organizations/:orgId/providers/:provider`.
- [x] 5.3 Add `DeleteOrgConfig(c echo.Context) error` for `DELETE /api/v1/organizations/:orgId/providers/:provider`.
- [x] 5.4 Add `SaveProjectConfig(c echo.Context) error` for `PUT /api/v1/projects/:projectId/providers/:provider`.
- [x] 5.5 Add `GetProjectConfig(c echo.Context) error` for `GET /api/v1/projects/:projectId/providers/:provider`.
- [x] 5.6 Add `DeleteProjectConfig(c echo.Context) error` for `DELETE /api/v1/projects/:projectId/providers/:provider`.
- [x] 5.7 Remove old handlers: `SaveOrgCredential`, `DeleteOrgCredential`, `ListOrgCredentials`, `SetOrgModelSelection`, `SetProjectPolicy`, `GetProjectPolicy`, `ListProjectPolicies`.
- [x] 5.8 In `apps/server-go/domain/provider/routes.go`, replace old routes with the 6 new routes from tasks 5.1–5.6. Remove old route registrations.

## 6. Fix Model Name Precedence

- [x] 6.1 In `apps/server-go/pkg/adk/model.go` `CreateModelWithName`, change the precedence so `cred.GenerativeModel` wins over the caller's `modelName`: if `cred.GenerativeModel != ""`, use it; else if `modelName != ""`, use it; else use `f.cfg.Model`.
- [x] 6.2 Audit all `CreateModelWithName` callers and update them to pass `""` where the user's DB-configured model should apply: `domain/chat/handler.go` (probe call), `domain/agents/executor.go`.

## 7. Build and Deploy Server

- [x] 7.1 Build the server: `cd /root/emergent && task build` — fix any compile errors.
- [x] 7.2 Deploy to container: `docker cp apps/server-go/dist/server emergent-server:/usr/local/bin/emergent-server && docker exec emergent-server pkill -f emergent-server` and wait for restart.
- [x] 7.3 Verify server is healthy: `curl -s http://localhost:3012/health | jq .` — check for `ok` status.

## 8. CLI Commands

- [x] 8.1 In `tools/emergent-cli/internal/cmd/provider.go`, add `configure` subcommand: flags `--api-key`, `--gcp-project`, `--location`, `--key-file` (for Vertex AI service account JSON path), `--generative-model`, `--embedding-model`. Calls `PUT /api/v1/organizations/:orgId/providers/:provider`. Prints effective config (provider, generative model, embedding model) on success.
- [x] 8.2 Add `configure-project` subcommand: same flags as `configure` plus `--remove` flag. Without `--remove`: calls `PUT /api/v1/projects/:projectId/providers/:provider`. With `--remove`: calls `DELETE /api/v1/projects/:projectId/providers/:provider`.
- [x] 8.3 Remove old CLI commands: `set-credentials`, `set-models`, `set-project-policy`, `list-project-policies`.
- [x] 8.4 Build and install CLI: `cd /root/emergent && task cli:install`.

## 9. End-to-End Test

- [x] 9.1 From `/root/specmcp`, run `emergent provider configure vertex-ai --key-file ... --gcp-project mcj-emergent --location us-central1` and verify it prints the effective config with auto-selected generative and embedding models. ✅ gemini-2.5-flash / embeddinggemma
- [x] 9.2 Run `emergent query "What is this project?"` and verify it returns a valid response (not 503). ✅
- [x] 9.3 Verify `cred.GenerativeModel` wins over hardcoded name: confirmed model used is from DB config, not a hardcoded fallback. ✅
- [x] 9.x Run `emergent provider configure-project vertex-ai ...` and verify project-level config works. ✅ (required handler fix for project-token auth → org context injection)

## 10. Update Skills

- [x] 10.1 Update `tools/emergent-cli/internal/skillsfs/skills/emergent-providers/SKILL.md` to document `configure` and `configure-project` commands; remove `set-credentials`, `set-models`, `set-project-policy`.
- [x] 10.2 Update `tools/emergent-cli/internal/skillsfs/skills/emergent-onboard/SKILL.md` — Step 2.5/2.6 (provider setup) now references `provider configure` (single atomic command) instead of `set-credentials` + `set-models`.
- [x] 10.3 Sync both skills to `.agents/skills/` and `/root/meta-project/.agents/skills/`.
- [x] 10.4 Rebuild CLI to embed updated skills: `cd /root/emergent && task cli:install`. ✅
