## Purpose

Extend the embedded curated MCP registry catalog's refresh tool to source
entries from the official MCP registry in addition to GitHub's registry, so
servers absent from GitHub's registry (such as Home Assistant MCP) can be
curated into the embedded catalog.

## ADDED Requirements

### Requirement: Catalog refresh supports GitHub and official-registry sources

The catalog refresh tool SHALL build the embedded catalog from a curated source
list that can mix two source kinds: a GitHub MCP-registry slug (resolved at
`{github-base}/{slug}?format=json`) and an official MCP registry server name
(line prefixed `official:`, resolved at
`{registry-base}/v0.1/servers/{name}/versions/latest`). Each source SHALL yield
a full official `server.json` server object with its `_meta` removed and a
non-empty `name`, and the tool SHALL preserve source-list order. The tool SHALL
fail without writing on any source that 404s or lacks a name.

#### Scenario: Official-registry source is included

- **WHEN** the source list contains `official:io.github.homeassistant-ai/ha-mcp`
- **THEN** the refresh tool fetches that server from the official MCP registry and adds its server object (minus `_meta`) to the catalog

#### Scenario: Mixed sources preserve order

- **WHEN** the source list mixes GitHub slugs and `official:` names
- **THEN** the resulting catalog contains every entry in source-list order

#### Scenario: Failing source aborts the refresh

- **WHEN** any GitHub slug or official name cannot be fetched or has no name
- **THEN** the tool exits non-zero and leaves the existing catalog unchanged
