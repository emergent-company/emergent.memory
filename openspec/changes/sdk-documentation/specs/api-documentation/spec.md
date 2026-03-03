## ADDED Requirements

### Requirement: OpenAPI spec is cross-referenced from the SDK documentation site
The GitHub Pages documentation site SHALL include a link to the live OpenAPI interactive documentation (`/docs`) and the raw spec (`/openapi.json`) from the Go SDK reference section.

#### Scenario: SDK docs site links to the OpenAPI reference
- **WHEN** a developer reads the Go SDK documentation site
- **THEN** they find a reference to the server's interactive Swagger UI at `/docs`
- **AND** they find a note that the raw OpenAPI 3.1 spec is available at `/openapi.json`
- **AND** the link is contextual (e.g., in the "Architecture" or "Further Reading" section of the Go SDK overview)
