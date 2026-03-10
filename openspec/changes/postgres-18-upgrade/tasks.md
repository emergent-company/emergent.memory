## 1. CLI Installer Constants

- [ ] 1.1 In `tools/cli/internal/installer/templates.go`, update `PostgresImage` from `pgvector/pgvector:pg17` to `pgvector/pgvector:pg18`
- [ ] 1.2 In `tools/cli/internal/installer/templates.go`, update `PostgresMajorVersion` from `17` to `18`
- [ ] 1.3 In `tools/cli/internal/installer/pg_upgrade.go`, update `pgUpgradeImage` constant from `pgautoupgrade/pgautoupgrade:17-bookworm` to `pgautoupgrade/pgautoupgrade:18-bookworm`
- [ ] 1.4 In `tools/cli/internal/installer/pg_upgrade.go`, update the success message in `RunPostgresUpgrade` from "upgraded to PostgreSQL 17" to "upgraded to PostgreSQL 18"
- [ ] 1.5 In `tools/cli/internal/installer/pg_upgrade.go`, update the comment on `RunPostgresUpgrade` that says "pg16 to pg17" to "pg17 to pg18"

## 2. Server Dockerfile

- [ ] 2.1 In `deploy/self-hosted/Dockerfile.server`, update the runtime stage base image from `alpine:3.21` to `alpine:3.23`
- [ ] 2.2 In `deploy/self-hosted/Dockerfile.server`, update `postgresql16-client` to `postgresql18-client`

## 3. Static Docker Compose Files

- [ ] 3.1 In `deploy/self-hosted/docker-compose.yml`, update `image: pgvector/pgvector:pg17` to `pgvector/pgvector:pg18`
- [ ] 3.2 In `deploy/self-hosted/docker-compose.local.yml`, update `image: pgvector/pgvector:pg17` to `pgvector/pgvector:pg18`
- [ ] 3.3 In `docker/Dockerfile.postgres`, update `FROM pgvector/pgvector:pg17` to `FROM pgvector/pgvector:pg18`
- [ ] 3.4 In `docker/docker-compose.e2e.yml`, update `image: pgvector/pgvector:pg17` to `pgvector/pgvector:pg18`
- [ ] 3.5 In `docker/e2e/docker-compose.yml`, update `image: pgvector/pgvector:pg17` to `pgvector/pgvector:pg18`

## 4. Inline Template in install-online.sh

- [ ] 4.1 In `deploy/self-hosted/install-online.sh`, update the hardcoded `image: pgvector/pgvector:pg17` inside the heredoc compose template to `pgvector/pgvector:pg18`

## 5. Test Fixtures

- [ ] 5.1 In `tools/cli/internal/installer/docker_test.go`, update the expected image string `pgvector/pgvector:pg17` to `pgvector/pgvector:pg18`
- [ ] 5.2 In `tools/cli/internal/installer/installer_test.go`, update the expected image string `pgvector/pgvector:pg17` to `pgvector/pgvector:pg18`

## 6. Verification

- [ ] 6.1 Run `task cli:install` and confirm the binary builds without errors
- [ ] 6.2 Run `go test ./...` inside `tools/cli` and confirm all tests pass (especially installer and pg_upgrade tests)
- [ ] 6.3 Search the entire repo for remaining `pg17` references and confirm only archive/log files remain (not active deployment code)
- [ ] 6.4 Confirm `pgautoupgrade/pgautoupgrade:18-bookworm` can be pulled: `docker pull pgautoupgrade/pgautoupgrade:18-bookworm`
- [ ] 6.5 Confirm `pgvector/pgvector:pg18` can be pulled: `docker pull pgvector/pgvector:pg18`
