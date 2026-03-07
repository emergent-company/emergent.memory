## ADDED Requirements

### Requirement: Production tests skip when token absent
All production smoke tests SHALL be skipped (not failed) when the `MEMORY_PROD_TEST_TOKEN` environment variable is not set.

#### Scenario: No token, tests skipped
- **WHEN** `MEMORY_PROD_TEST_TOKEN` is not set in the environment
- **THEN** all tests in `production_test.go` are reported as skipped with a message indicating the missing variable

---

### Requirement: Production tests skip when server unreachable
All production smoke tests SHALL be skipped (not failed) when the production server at `https://memory.emergent-company.ai` is unreachable or returns a non-2xx response from `/health`.

#### Scenario: Server down, tests skipped
- **WHEN** `https://memory.emergent-company.ai/health` returns a network error or HTTP 4xx/5xx
- **THEN** all production tests are skipped

---

### Requirement: set-token with production token writes credentials
The suite SHALL verify that `memory set-token <prod-token> --server https://memory.emergent-company.ai` creates `~/.memory/credentials.json`.

#### Scenario: Production credentials file created
- **WHEN** `TestProduction_SetToken` runs with a valid `MEMORY_PROD_TEST_TOKEN`
- **THEN** `memory set-token` exits 0, output contains `"token"`, and `~/.memory/credentials.json` exists

---

### Requirement: Full authenticated round-trip against production
The suite SHALL verify: set-token → `memory status` shows "Connected" → `memory projects list` exits 0.

#### Scenario: Status shows connected
- **WHEN** `TestProduction_AuthAndList` runs with a valid token
- **THEN** `memory status` output contains `"Connected"` or `"✓"`

#### Scenario: Projects list exits zero
- **WHEN** `TestProduction_AuthAndList` runs after authentication
- **THEN** `memory projects list` exits 0 (content not asserted)

---

### Requirement: Production server health endpoint returns valid JSON
The suite SHALL verify that `GET /health` on the production server returns HTTP 200 and a JSON body with a non-empty `status` field.

#### Scenario: Health endpoint responds
- **WHEN** `TestProduction_ServerHealth` runs
- **THEN** the response is HTTP 200 and the decoded `status` field is non-empty

---

### Requirement: Production issuer endpoint returns valid OIDC configuration
The suite SHALL verify that `GET /api/auth/issuer` returns HTTP 200 with a JSON body where `issuer` is a non-empty HTTPS URL and `standalone` is `false`.

#### Scenario: Issuer URL is HTTPS and non-empty
- **WHEN** `TestProduction_IssuerEndpoint` runs
- **THEN** the response `issuer` field starts with `"https://"` and is non-empty

#### Scenario: Server is not in standalone mode
- **WHEN** `TestProduction_IssuerEndpoint` runs
- **THEN** the `standalone` field in the response is `false`
