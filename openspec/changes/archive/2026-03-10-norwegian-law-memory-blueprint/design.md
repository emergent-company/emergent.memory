## Context

The IMDb memory blueprint at `/root/imdb-memory-blueprint` is the reference implementation of a "standalone domain blueprint" pattern for the Memory platform: a YAML template pack defining the knowledge graph ontology, paired with a Go seeder that populates the graph with real data. The importer for Norwegian law already exists as `memory db lovdata` inside the CLI at `tools/cli/internal/cmd/lovdata.go` (2,131 lines). That CLI command is tightly coupled to the monorepo (uses internal `config`, `benchReport`, and other shared helpers). The goal here is to extract the data model and seeder logic into a clean, standalone repository that mirrors the IMDb blueprint structure and can be published independently.

Key facts from the existing implementation:
- Object types (7): `Law`, `Regulation`, `Ministry`, `LegalArea`, `LegalParagraph`, `EUDirective`, `EuroVocConcept`
- Relationship types (13): `ADMINISTERED_BY`, `IN_LEGAL_AREA`, `AMENDED_BY`, `AMENDS`, `SEE_ALSO`, `HAS_LANGUAGE_VARIANT`, `REFERENCES`, `IMPLEMENTS_EEA`, `HAS_PARAGRAPH`, `CITES_EU_LAW`, `EU_CITES`, `EU_MODIFIED_BY`, `HAS_EUROVOC_DESCRIPTOR`
- Data sources: Lovdata public archives (NLOD 2.0), EUR-Lex HTML scrape, EuroVoc SPARQL
- Scale: ~4,461 documents, ~99,563 objects, ~117,158 relationships
- Cached data already at `/tmp/lovdata_data/` (196 MB)
- SDK used: `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk` with `replace` directive

## Goals / Non-Goals

**Goals:**
- Create `/root/norwegian-law-memory-blueprint/` as a standalone Go repository
- Write `packs/norwegian-law.yaml` with all 7 object types and 13 relationship types, matching exactly what the CLI ingests
- Write `cmd/seeder/main.go` as a self-contained seeder that does not import any monorepo-internal packages (only the public SDK)
- Follow the IMDb blueprint structure verbatim: same directory layout, same flag/env var pattern, same two-phase checkpointing (objects → relationships), same batch size (100) and worker concurrency (20)
- Write `README.md` documenting the blueprint schema, data sources, and quick-start
- Include `go.mod` (module `github.com/emergent-company/norwegian-law-memory-blueprint`) and `.gitignore`

**Non-Goals:**
- No changes to the monorepo `lovdata.go` CLI command
- No API server, no HTTP handlers, no database migrations
- No frontend changes
- No production deployment — this is a local/publishable seeder tool
- Not extracting EUR-Lex HTML scraping into a reusable library (inline in seeder is fine)

## Decisions

**1. Standalone repository, not a monorepo package**

The IMDb blueprint is a standalone repo. Norwegian law should follow the same pattern for consistency and publishability. Avoids coupling the blueprint to internal CLI helpers (`benchReport`, `config.DiscoverPath`, etc.).

*Alternative considered*: Move the logic to `apps/server/domain/lovdata/` as a domain module. Rejected — blueprints are external tools, not server domains.

**2. Copy-and-adapt from `lovdata.go`, not import**

The seeder will re-implement the parsing and ingestion logic inline. The existing CLI command uses internal packages (`config`, `benchReport`) that should not be part of a standalone blueprint. Only the public `sdk` package is imported.

*Alternative considered*: Import `tools/cli/internal/cmd` as a package. Rejected — `internal` packages are not importable outside the module.

**3. Template pack YAML reflects actual SDK ingestion, not just the CLI doc comment**

The CLI doc comment lists 11 relationship types; the actual ingestion code uses 13 (`HAS_PARAGRAPH` and `CITES_EU_LAW` are also created). The YAML pack should match the actual behavior.

**4. Two-phase checkpointing matches IMDb blueprint exactly**

Phase 1: ingest all objects, save `idmap.json` (key → Memory entity ID). Phase 2: ingest all relationships using the ID map. If interrupted, resume from the saved phase. This mirrors the IMDb seeder pattern and is proven at scale.

**5. seeder.go is a single file in `cmd/seeder/main.go`**

The IMDb seeder is 1,133 lines in one file. Norwegian law will be similarly structured — one `main.go` covering config, parsing, ingestion, and checkpointing. No sub-packages needed for a standalone tool.

**6. Object key format**

Follow the `lovdata.go` convention:
- Laws/Regulations: `d.RefID` (e.g., `lov/1902-05-22-10`)
- Ministries: `ministry_<name>`
- Legal areas: `area_<name>`, sub-areas: `subarea_<name>`
- Paragraphs: `<refID>#<sectionID>` (e.g., `lov/1902-05-22-10#kapittel-1-paragraf-3`)
- EU directives: `eu_<celexID>`
- EuroVoc concepts: `eurovoc_<id>`

## Risks / Trade-offs

- **Lovdata archive format may change** → Mitigation: pin the parser to the current HTML structure; add a version note in README. The archive format has been stable for years.
- **EUR-Lex HTML scraping may break** → Mitigation: `--skip-eu` flag allows running without EU enrichment. EUR-Lex scraping is best-effort.
- **EuroVoc SPARQL timeout** → Mitigation: batch SPARQL queries; graceful degradation (concepts with no label get an empty `label_en`).
- **SDK `replace` directive requires local monorepo** → This is inherent to the local development pattern; the README will note that `go mod download` won't work without the monorepo present. For a real public release, the SDK would need to be published to pkg.go.dev.
- **Scale**: 99k objects + 117k relationships at 100-item batches = ~1,000 + ~1,200 API calls. At 20 concurrent workers this takes ~5–15 minutes on a local server. No mitigation needed; this matches IMDb performance.

## Migration Plan

No migrations needed. This is a net-new standalone repository. Steps:

1. Create `/root/norwegian-law-memory-blueprint/` directory
2. Create `go.mod`, `go.sum`, `.gitignore`
3. Create `packs/norwegian-law.yaml`
4. Create `cmd/seeder/main.go`
5. Create `README.md`
6. `go build ./cmd/seeder/` to verify compilation
7. Run `./seeder --ingest-only` against a local server using cached data to verify end-to-end

Rollback: simply delete the directory. No impact on the monorepo.

## Open Questions

- Should the seeder auto-register the template pack (call the template pack install API) before ingesting, or leave that as a manual step? IMDb blueprint leaves it manual. Follow the same convention.
- Should the `go.sum` be committed or generated on first `go mod tidy`? Follow IMDb blueprint: commit both `go.mod` and `go.sum`.
