## ADDED Requirements

### Requirement: Go SDK README links to GitHub Pages documentation site
The `apps/server-go/pkg/sdk/README.md` SHALL contain a prominent link to the published GitHub Pages documentation site so developers can navigate from the package to the full reference.

#### Scenario: README contains a docs badge or link near the top
- **WHEN** a developer views the Go SDK README on GitHub
- **THEN** they see a link or badge to the GitHub Pages documentation site within the first 20 lines
- **AND** the link is labelled clearly (e.g., "Documentation" or "Full Reference")

#### Scenario: README defers detailed reference to the docs site
- **WHEN** a developer reads the README
- **THEN** the existing quickstart content is retained as-is
- **AND** a note directs readers to the docs site for per-package API reference, guides, and the changelog
