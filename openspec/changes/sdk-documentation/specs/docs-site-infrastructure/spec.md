## ADDED Requirements

### Requirement: MkDocs Material site is configured
The repository SHALL contain a `mkdocs.yml` configuration at the repo root that defines the documentation site structure using MkDocs Material theme.

#### Scenario: mkdocs.yml is present and valid
- **WHEN** a developer runs `mkdocs build` from the repo root
- **THEN** the command succeeds without errors
- **AND** a `site/` output directory is produced containing static HTML

#### Scenario: Material theme is applied
- **WHEN** the MkDocs site is rendered
- **THEN** it uses the `material` theme
- **AND** features enabled include: navigation tabs, navigation sections, navigation expand, content tabs, search suggest, search highlight, and copy code buttons

#### Scenario: Navigation tree covers both SDKs
- **WHEN** a developer opens the site
- **THEN** the top navigation includes at minimum: "Home", "Go SDK", and "Swift SDK" tabs
- **AND** the Go SDK tab expands to: Quickstart, Authentication, Multi-tenancy, Error Handling, Streaming, Graph ID Model, and a "Reference" section containing one page per package
- **AND** the Swift SDK tab expands to: Overview, API Reference (EmergentAPIClient, Models, Errors), and a Roadmap page

#### Scenario: Python dependencies are declared
- **WHEN** a developer checks `docs/requirements.txt`
- **THEN** they find `mkdocs-material` pinned to a specific version
- **AND** any additional required plugins (e.g., `mkdocs-autorefs`) are also declared

### Requirement: GitHub Actions deploys docs to GitHub Pages
The repository SHALL contain a `.github/workflows/docs.yml` workflow that automatically builds and deploys the MkDocs site to GitHub Pages.

#### Scenario: Docs deploy on push to main
- **WHEN** a commit is pushed to the `main` branch
- **THEN** the `docs.yml` workflow triggers
- **AND** it installs Python and MkDocs Material dependencies
- **AND** it runs `mkdocs gh-deploy --force`
- **AND** the updated site is available on the `gh-pages` branch

#### Scenario: Docs build is validated on pull requests
- **WHEN** a pull request is opened or updated that modifies files under `docs/` or `mkdocs.yml`
- **THEN** the workflow runs `mkdocs build --strict` without deploying
- **AND** the check must pass before the PR can be merged

#### Scenario: Workflow does not deploy on PRs
- **WHEN** the workflow runs on a pull request event
- **THEN** it only builds (does not call `mkdocs gh-deploy`)
- **AND** no changes are made to the `gh-pages` branch

### Requirement: Docs source files live under docs/site/
The documentation source markdown files SHALL be organized under `docs/site/` to separate them from the existing internal `docs/` content.

#### Scenario: Go SDK docs are under docs/site/go-sdk/
- **WHEN** a developer lists `docs/site/go-sdk/`
- **THEN** they find: `index.md` (overview/quickstart), `authentication.md`, `multi-tenancy.md`, `error-handling.md`, `streaming.md`, `graph-id-model.md`, `changelog.md`, and a `reference/` subdirectory with one `.md` file per package

#### Scenario: Swift SDK docs are under docs/site/swift-sdk/
- **WHEN** a developer lists `docs/site/swift-sdk/`
- **THEN** they find: `index.md` (overview + status note), `api-client.md`, `models.md`, `errors.md`, `roadmap.md`

#### Scenario: Site root has an index page
- **WHEN** a developer opens the docs site root
- **THEN** they see a landing page that introduces both SDKs, links to the quickstart for each, and includes a brief description of the Emergent platform

### Requirement: Local development of docs is supported
A developer SHALL be able to preview the documentation site locally with live reload.

#### Scenario: Local preview works with a single command
- **WHEN** a developer runs `mkdocs serve` from the repo root (with MkDocs Material installed)
- **THEN** a local HTTP server starts on port 8000
- **AND** changes to markdown files are reflected in the browser without restarting
