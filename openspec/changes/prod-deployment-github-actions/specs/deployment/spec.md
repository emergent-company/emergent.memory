## ADDED Requirements

### Requirement: Production host deployment is documented in the deployment spec
The deployment specification SHALL cover the production bare-metal host scenario in addition to the existing local development scenarios. The production deployment SHALL use Docker Compose managed by a GitHub Actions workflow targeting `memory.emergent-company.ai`.

#### Scenario: Production deployment is distinct from local dev
- **WHEN** an operator reads the deployment spec
- **THEN** it SHALL be clear that production deployment uses the infra repo (`emergent-company/emergent.memory.infra`) and NOT the monorepo's local compose setup
- **AND** the local dev Compose setup SHALL remain unchanged

#### Scenario: Reverse proxy handles SSL and virtual hosting
- **WHEN** the production stack is running
- **THEN** the server SHALL only bind to an internal network interface (not `0.0.0.0` on port 443)
- **AND** the operator's external reverse proxy SHALL terminate SSL and route `memory.emergent-company.ai` to the internal server port
