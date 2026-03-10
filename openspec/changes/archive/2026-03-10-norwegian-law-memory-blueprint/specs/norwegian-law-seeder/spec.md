## ADDED Requirements

### Requirement: Seeder accepts configuration via flags and environment variables
The seeder program SHALL accept configuration through command-line flags, with environment variable fallbacks. Required parameters are `--server` (env: `MEMORY_SERVER`), `--token` (env: `MEMORY_PROJECT_TOKEN`), and `--project` (env: `MEMORY_PROJECT_ID`). Optional parameters are `--state-dir` (checkpoint directory, default `~/.norwegian-law-seed-state`), `--limit` (max documents, default 0 = all), `--skip-eu` (skip EUR-Lex/EuroVoc), `--workers` (parallel workers, default 20), `--batch` (batch size, default 100), and `--ingest-only` (skip download, use cached data).

#### Scenario: Missing required flags
- **WHEN** the seeder is invoked without `--server`, `--token`, or `--project`
- **THEN** it prints a clear error listing the missing parameters and exits non-zero

#### Scenario: Environment variable fallback
- **WHEN** `MEMORY_SERVER`, `MEMORY_PROJECT_TOKEN`, and `MEMORY_PROJECT_ID` are set
- **THEN** the seeder uses those values without requiring flags

### Requirement: Seeder downloads and caches Lovdata archives
The seeder SHALL download `gjeldende-lover.tar.bz2` (laws) and `gjeldende-sentrale-forskrifter.tar.bz2` (regulations) from `api.lovdata.no`, extract the bzip2+tar archives in memory, and cache parsed results as JSON in a local directory (`/tmp/norwegian_law_data/` by default).

#### Scenario: Fresh download
- **WHEN** no cached data exists and `--ingest-only` is not set
- **THEN** archives are downloaded from Lovdata, parsed, and saved to the cache directory

#### Scenario: Cache hit
- **WHEN** `--ingest-only` is set and cached JSON files exist
- **THEN** download is skipped and cached data is loaded directly

#### Scenario: Download limit
- **WHEN** `--limit N` is specified
- **THEN** at most N documents per dataset (laws/regulations) are parsed

### Requirement: Seeder parses Lovdata HTML documents into structured objects
Each document in the Lovdata archive is an HTML file. The seeder SHALL parse each document to extract: `RefID`, `DocID`, `Title`, `ShortTitle`, `Language`, `Ministry`, `LegalArea`, `AllLegalAreas`, `AllLegalSubAreas`, `DateInForce`, `LastChangeInForce`, `DateOfPublication`, `LastChangedByRef`, `AmendsRefs`, `SeeAlsoRefs`, `EUDirectiveIDs`, `References`, and body content including per-paragraph chunks (`LegalParagraph` with `SectionID`, `ChapterID`, `ParagraphNum`, `Title`, `Content`, `Position`).

#### Scenario: Law with paragraphs
- **WHEN** a law HTML document is parsed
- **THEN** one `Law` object is produced plus one `LegalParagraph` object per `<article class="legalArticle">` element that has non-empty content

#### Scenario: Regulation parsing
- **WHEN** a regulation HTML document is parsed
- **THEN** a `Regulation` object is produced (same schema as `Law`)

#### Scenario: EU directive IDs extracted
- **WHEN** a law references EU directives in its metadata or body
- **THEN** directive IDs (e.g., `2001/42/EF`) are captured in `EUDirectiveIDs` and `EUBodyRefs`

### Requirement: Seeder fetches EU directive metadata from EUR-Lex
When `--skip-eu` is not set, the seeder SHALL fetch full metadata for each EU directive referenced by the Norwegian documents by scraping `eur-lex.europa.eu`. For each directive the seeder SHALL capture: `CelexID`, `FullTitle`, `ShortTitle`, `Form`, `DateOfDocument`, `DateOfEffect`, `Author`, `SubjectMatter`, `OJReference`, `EuroVocIDs`, `CitedCELEX`, and `ModifiedByCELEX`. It SHALL also fetch human-readable EuroVoc concept labels via the SPARQL endpoint at `publications.europa.eu`.

#### Scenario: EUR-Lex fetch with cached results
- **WHEN** `parsed_directives.json` already exists in the cache directory with valid content
- **THEN** the seeder skips re-fetching and loads from cache

#### Scenario: EU enrichment skipped
- **WHEN** `--skip-eu` flag is set
- **THEN** no HTTP requests are made to EUR-Lex or EuroVoc

### Requirement: Seeder uses two-phase checkpointing
The seeder SHALL implement a two-phase ingestion pipeline with persistent state in `--state-dir`:
- Phase 1 (objects): ingest all `Ministry`, `LegalArea`, `Law`, `Regulation`, `LegalParagraph`, `EUDirective`, and `EuroVocConcept` objects; save `idmap.json` mapping keys to Memory entity IDs
- Phase 2 (relationships): load `idmap.json` and ingest all relationships

If interrupted, re-running the seeder SHALL resume from the saved phase rather than starting over.

#### Scenario: Resume after interruption in phase 1
- **WHEN** the seeder is interrupted during object ingestion
- **THEN** re-running picks up from the start of phase 1 (objects are idempotent due to conflict resolution)

#### Scenario: Resume after phase 1 complete
- **WHEN** `state.json` contains `phase: "objects_done"` and `idmap.json` exists
- **THEN** the seeder skips phase 1 and proceeds directly to relationship ingestion

### Requirement: Seeder ingests objects and relationships using the Memory SDK
The seeder SHALL use `sdk.BulkCreateObjects` with batch size 100 and 20 concurrent goroutine workers. Conflict errors (HTTP 409) SHALL be resolved by looking up the existing object by key via `sdk.ListObjects`. The seeder SHALL use `sdk.BulkCreateRelationships` with the same batch/worker settings for relationships. Failed relationship batches SHALL be logged to `rels_failed.jsonl` in the state directory.

#### Scenario: Conflict resolution
- **WHEN** a batch of objects returns a conflict error
- **THEN** the seeder resolves the conflict by fetching the existing entity by key and adds it to the ID map

#### Scenario: Relationship ingestion with all types
- **WHEN** all objects are ingested
- **THEN** the seeder creates relationships of all 13 types: `ADMINISTERED_BY`, `IN_LEGAL_AREA`, `AMENDED_BY`, `AMENDS`, `SEE_ALSO`, `HAS_LANGUAGE_VARIANT`, `REFERENCES`, `IMPLEMENTS_EEA`, `HAS_PARAGRAPH`, `CITES_EU_LAW`, `EU_CITES`, `EU_MODIFIED_BY`, `HAS_EUROVOC_DESCRIPTOR`

#### Scenario: Deterministic object keys
- **WHEN** objects are created
- **THEN** each object has a stable, deterministic key so re-runs produce the same graph structure (no duplicates)

### Requirement: Repository compiles and is documented
The blueprint repository SHALL compile with `go build ./cmd/seeder/` and include a `README.md` documenting the schema, data sources, prerequisites, and quick-start instructions.

#### Scenario: Build succeeds
- **WHEN** `go build ./cmd/seeder/` is run from the repository root
- **THEN** it exits with code 0 and produces the seeder binary

#### Scenario: README covers quick-start
- **WHEN** a user reads `README.md`
- **THEN** they can find instructions for installing the template pack, running the seeder, and the full list of object and relationship types
