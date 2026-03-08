## 1. Repository Scaffold

- [x] 1.1 Create `/root/norwegian-law-memory-blueprint/` directory with subdirectories `packs/`, `cmd/seeder/`
- [x] 1.2 Create `go.mod` with module `github.com/emergent-company/norwegian-law-memory-blueprint`, Go 1.24, SDK dependency and `replace` directive pointing to `/root/emergent.memory/apps/server/pkg/sdk`
- [x] 1.3 Run `go mod tidy` to generate `go.sum`
- [x] 1.4 Create `.gitignore` covering Go binaries, `/tmp/`, state files (`*.jsonl`, `idmap.json`, `state.json`), and OS files

## 2. Template Pack YAML

- [x] 2.1 Create `packs/norwegian-law.yaml` with pack metadata (`name`, `version`, `description`, `author`, `license`, `repositoryUrl`)
- [x] 2.2 Add `Law` object type with all 15 properties: `name`, `title`, `ref_id`, `doc_id`, `short_title`, `legacy_id`, `language`, `date_in_force`, `year_in_force`, `decade_in_force`, `last_change_in_force`, `date_of_publication`, `applies_to`, `eea_references`, `content`
- [x] 2.3 Add `Regulation` object type (identical schema to `Law`, different label/description)
- [x] 2.4 Add `Ministry` object type with property `name`
- [x] 2.5 Add `LegalArea` object type with properties `name` and `parent_area`
- [x] 2.6 Add `LegalParagraph` object type with properties: `name`, `content`, `section_id`, `paragraph_num`, `law_ref_id`, `position` (number), `chapter_id`, `title`
- [x] 2.7 Add `EUDirective` object type with properties: `name`, `celex_id`, `directive_id`, `full_title`, `form`, `date_of_document`, `date_of_effect`, `author`, `subject_matter`, `oj_reference`, `content`
- [x] 2.8 Add `EuroVocConcept` object type with properties: `name`, `eurovoc_id`, `label_en`
- [x] 2.9 Add all 13 relationship types: `ADMINISTERED_BY`, `IN_LEGAL_AREA`, `AMENDED_BY`, `AMENDS`, `SEE_ALSO`, `HAS_LANGUAGE_VARIANT`, `REFERENCES`, `IMPLEMENTS_EEA`, `HAS_PARAGRAPH`, `CITES_EU_LAW`, `EU_CITES`, `EU_MODIFIED_BY`, `HAS_EUROVOC_DESCRIPTOR` — each with correct `sourceType` and `targetType`

## 3. Seeder — Config and Entry Point

- [x] 3.1 Create `cmd/seeder/main.go` with `package main`, imports for standard library + SDK only (no monorepo internals)
- [x] 3.2 Define `config` struct with all fields: `serverURL`, `token`, `projectID`, `stateDir`, `limit`, `skipEU`, `euLimit`, `workers`, `batchSz`, `ingestOnly`
- [x] 3.3 Implement `parseConfig()` using `flag` package with env var fallbacks (`MEMORY_SERVER`, `MEMORY_PROJECT_TOKEN`, `MEMORY_PROJECT_ID`, `MEMORY_STATE_DIR`, `SEED_LIMIT`)
- [x] 3.4 Implement `(c *config) validate()` returning clear error message for missing required fields
- [x] 3.5 Implement `main()` entry point: parse config, validate, initialize SDK client with `apikey` auth mode, call phase 1 then phase 2

## 4. Seeder — Data Types

- [x] 4.1 Define `LovDoc` struct matching all fields from `lovdata.go`: `RefID`, `DocID`, `LegacyID`, `Title`, `ShortTitle`, `Language`, `Ministry`, `LegalArea`, `LegalSubArea`, `AllLegalAreas`, `AllLegalSubAreas`, `DateInForce`, `LastChangeInForce`, `DateOfPublication`, `AppliesTo`, `LastChangedByRef`, `AmendsRefs`, `SeeAlsoRefs`, `EEAReferences`, `EUDirectiveIDs`, `DocType`, `References`, `Content`, `Paragraphs`, `EUBodyRefs`
- [x] 4.2 Define `LovParagraph` struct: `SectionID`, `ChapterID`, `ParagraphNum`, `Title`, `Content`, `Position`
- [x] 4.3 Define `EUDirective` struct: `DirectiveID`, `CelexID`, `FullTitle`, `ShortTitle`, `Form`, `DateOfDocument`, `DateOfEffect`, `Author`, `ResponsibleDG`, `Content`, `SubjectMatter`, `DirectoryCode`, `LegalBasis`, `ProcedureNum`, `OJReference`, `EuroVocIDs`, `CitedCELEX`, `ModifiedByCELEX`
- [x] 4.4 Define `EuroVocConcept` struct: `ID`, `LabelEN`
- [x] 4.5 Define `SeedState` struct with `Phase` field and phase constants: `objects_pending`, `objects_done`, `rels_pending`, `done`

## 5. Seeder — Archive Download and Parsing

- [x] 5.1 Implement `downloadAndCache(url, destPath string)` that downloads a file if not cached, with progress output
- [x] 5.2 Implement `loadDataset(archiveURL, docType string, limit int) ([]LovDoc, error)` that streams bzip2+tar archive and parses each HTML file
- [x] 5.3 Implement `parseHTMLDoc(htmlBytes []byte, docType string) (LovDoc, error)` extracting all metadata fields from `<meta>` tags and `<dl>` structured data
- [x] 5.4 Implement `extractBody(mainNode *html.Node) (fullMarkdown string, paragraphs []LovParagraph, euBodyRefs []string)` walking `<main id="dokument">` to extract Markdown text and per-article paragraph chunks
- [x] 5.5 Implement HTML helper functions: `attr()`, `text()`, `findAll()`, `findFirst()`, `appendUniq()`, `stripAnchor()`
- [x] 5.6 Implement EU directive ID extraction regexps: `directivePattern` for `YYYY/NNN/EF|EU|EEA` forms, `celexPattern` for `3XXXXXXXAYYY` forms, `eurovocPattern` for EuroVoc URLs

## 6. Seeder — EUR-Lex and EuroVoc Fetching

- [x] 6.1 Implement `fetchAllEUData(ctx, docs []LovDoc, euLimit int) ([]*EUDirective, []*EuroVocConcept)` orchestrating EUR-Lex scrape + EuroVoc SPARQL
- [x] 6.2 Implement `fetchEURLex(celexID string) (*EUDirective, error)` scraping `https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=CELEX:<id>` to extract directive metadata fields
- [x] 6.3 Implement EuroVoc SPARQL query to resolve concept IDs to English labels, batched to avoid query length limits
- [x] 6.4 Implement cache load/save for `parsed_directives.json` and `parsed_concepts.json` — skip EUR-Lex fetch if valid cache exists

## 7. Seeder — Object Ingestion (Phase 1)

- [x] 7.1 Implement `ingestObjects(ctx, client, docs, directives, concepts, batchSz, nWorkers int) map[string]string` building the full list of `CreateObjectRequest` items
- [x] 7.2 Build Ministry and LegalArea (area + subarea) items with deterministic keys (`ministry_<name>`, `area_<name>`, `subarea_<name>`)
- [x] 7.3 Build Law/Regulation items with all properties populated from `LovDoc` fields; key = `d.RefID`
- [x] 7.4 Build LegalParagraph items; key = `<refID>#<sectionID>` — skip paragraphs with empty `Content` or `SectionID`
- [x] 7.5 Build EUDirective items; key = `eu_<celexID>`
- [x] 7.6 Build EuroVocConcept items; key = `eurovoc_<id>`
- [x] 7.7 Implement `bulkUploadObjects(ctx, client, items, batchSz, nWorkers)` using channel-based worker pool, 409-conflict resolution via `ListObjects` by key, return `map[string]string` (key → entityID)
- [x] 7.8 Save `idmap.json` to state directory after phase 1 completes; update `state.json` to `objects_done`

## 8. Seeder — Relationship Ingestion (Phase 2)

- [x] 8.1 Implement `ingestRelationships(ctx, client, docs, directives, idMap, batchSz, nWorkers)` building all `CreateRelationshipRequest` items
- [x] 8.2 Build `ADMINISTERED_BY` rels: Law/Regulation → Ministry (for each doc with non-empty Ministry)
- [x] 8.3 Build `IN_LEGAL_AREA` rels: Law/Regulation → LegalArea (for each area in `AllLegalAreas` and `AllLegalSubAreas`, with `"level": "sub"` property for sub-areas)
- [x] 8.4 Build `AMENDED_BY` and `AMENDS` rels from `LastChangedByRef` and `AmendsRefs`
- [x] 8.5 Build `SEE_ALSO` and `REFERENCES` rels from `SeeAlsoRefs` and `References` (only for known refs in corpus)
- [x] 8.6 Build `IMPLEMENTS_EEA` rels from `EUDirectiveIDs` (resolve directive ID → CELEX → `eu_<celex>` key)
- [x] 8.7 Build `HAS_PARAGRAPH` rels: Law/Regulation → LegalParagraph for each paragraph, with `position` property
- [x] 8.8 Build `CITES_EU_LAW` rels: Law/Regulation → EUDirective from `EUBodyRefs`
- [x] 8.9 Build `HAS_LANGUAGE_VARIANT` rels between docs sharing the same `DocID` in different languages
- [x] 8.10 Build `EU_CITES` and `EU_MODIFIED_BY` rels between `EUDirective` nodes from `CitedCELEX` and `ModifiedByCELEX`
- [x] 8.11 Build `HAS_EUROVOC_DESCRIPTOR` rels: EUDirective → EuroVocConcept from `EuroVocIDs`
- [x] 8.12 Implement `bulkUploadRelationships(ctx, client, items, batchSz, nWorkers)` with worker pool; write failed batches to `rels_failed.jsonl` in state directory
- [x] 8.13 Update `state.json` to `done` after phase 2 completes

## 9. README and Verification

- [x] 9.1 Create `README.md` with: project overview, schema table (all 7 object types + 13 relationship types), data sources + licenses, prerequisites (Go 1.24+, Memory server URL + project token), quick-start commands
- [x] 9.2 Run `go build ./cmd/seeder/` from `/root/norwegian-law-memory-blueprint/` and confirm it exits 0
- [x] 9.3 Run `./seeder --server http://localhost:3012 --token <token> --project <id> --ingest-only --limit 50 --skip-eu` against local server using cached data to verify end-to-end ingestion of a small sample
- [x] 9.4 Verify object types and relationship types in the project match the template pack YAML
