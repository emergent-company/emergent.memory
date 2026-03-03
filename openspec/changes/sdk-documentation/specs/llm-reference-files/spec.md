## ADDED Requirements

### Requirement: Combined LLM reference file exists
The repository SHALL contain a `docs/llms.md` file that provides a machine-readable, LLM-consumable reference combining the Go SDK and Swift SDK in a single document.

#### Scenario: Combined file covers both SDKs
- **WHEN** an LLM or developer reads `docs/llms.md`
- **THEN** they find structured sections for both the Go SDK and the Swift SDK
- **AND** the document begins with a brief description of the Emergent platform and its two SDKs

#### Scenario: Combined file is structured for context injection
- **WHEN** the combined file is loaded into an LLM context window
- **THEN** each section uses consistent `##` and `###` headings for SDK, package, type, and method groupings
- **AND** code examples use fenced code blocks with language tags
- **AND** the file does not contain navigation markup, HTML, or MkDocs-specific directives

#### Scenario: Combined file is kept concise
- **WHEN** a developer checks the combined file
- **THEN** it is a summary-level reference (types, method signatures, brief descriptions) rather than a full prose guide
- **AND** it is suitable for inclusion in a prompt or AGENTS.md-style context file

### Requirement: Per-SDK LLM reference files exist
The repository SHALL contain separate LLM reference files for each SDK: `docs/llms-go-sdk.md` and `docs/llms-swift-sdk.md`.

#### Scenario: Go SDK LLM file covers the full client surface
- **WHEN** an LLM reads `docs/llms-go-sdk.md`
- **THEN** they find: module path, installation command, `Client` struct with all 30 service client fields, `Config` and `AuthConfig` structs, all three auth modes (apikey, apitoken, oauth), `SetContext` signature, `Do` and `Close` methods, the `errors` package predicates, the `auth` package types, and for each of the 30 service clients — its name, field, and a list of exported method names with brief one-line descriptions

#### Scenario: Go SDK LLM file includes the dual-ID graph model explanation
- **WHEN** an LLM reads `docs/llms-go-sdk.md`
- **THEN** they find a compact explanation of the dual-ID model: `ID`/`VersionID` (mutable, changes on update) vs `CanonicalID`/`EntityID` (stable across versions)
- **AND** they find a note that `EntityID` and `CanonicalID` were introduced in v0.8.0 and that old names are deprecated

#### Scenario: Swift SDK LLM file covers the Core layer
- **WHEN** an LLM reads `docs/llms-swift-sdk.md`
- **THEN** they find: the repository (`emergent-company/emergent.memory.mac`), source files (`Emergent/Core/EmergentAPIClient.swift`, `Emergent/Core/Models.swift`), all 15 `EmergentAPIClient` method signatures with parameter names and return types, all 16 public model types with their property lists, all `EmergentAPIError` cases, and the `ConnectionState` enum cases
- **AND** they find the status note about `EmergentSwiftSDK/` being a planned stub

### Requirement: LLM reference files follow the llms.txt convention
The LLM reference files SHALL be structured following the `llms.txt` convention so that AI tools can automatically discover and use them.

#### Scenario: Files are plain markdown with no site-generator directives
- **WHEN** any of the three LLM reference files is read as raw text
- **THEN** it contains only standard markdown (headings, paragraphs, fenced code blocks, bullet lists)
- **AND** it contains no YAML front matter, MkDocs admonitions, Jinja templates, or HTML tags

#### Scenario: docs/llms.md is the canonical discovery entry point
- **WHEN** a tool or developer looks for the LLM reference entry point
- **THEN** `docs/llms.md` serves as the top-level combined file
- **AND** it contains references to the two per-SDK files (`docs/llms-go-sdk.md`, `docs/llms-swift-sdk.md`) for consumers that want a focused subset
