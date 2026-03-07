## Why

The IMDb memory blueprint (`/root/imdb-memory-blueprint`) demonstrates a canonical pattern for a domain knowledge graph: a YAML template pack defining the ontology + a Go seeder program that populates the graph. We already have a full Norwegian law importer implemented as a CLI command (`memory db lovdata`) inside the monorepo, along with cached data at `/tmp/lovdata_data/` covering ~4,461 documents, ~99,563 objects, and ~117,158 relationships. We need a standalone, publishable blueprint repository for Norwegian law that mirrors the IMDb blueprint structure so it can be shared, cloned, and run independently.

## What Changes

- Create a new standalone Go repository at `/root/norwegian-law-memory-blueprint/` following the same directory structure as `/root/imdb-memory-blueprint/`
- Create `packs/norwegian-law.yaml` — the template pack YAML defining all object types and relationship types for the Norwegian law knowledge graph
- Create `cmd/seeder/main.go` — a standalone Go seeder that downloads Lovdata public archives (and optionally EUR-Lex / EuroVoc), parses them, and ingests objects + relationships into a Memory project using the SDK
- Create `go.mod` and `go.sum` with module name `github.com/emergent-company/norwegian-law-memory-blueprint` and a `replace` directive for the local SDK
- Create `README.md` documenting the blueprint, its schema, and how to run the seeder
- Create `.gitignore` for standard Go + state file ignores

## Capabilities

### New Capabilities

- `norwegian-law-template-pack`: Template pack YAML defining the Norwegian law knowledge graph ontology — object types (`Law`, `Regulation`, `Ministry`, `LegalArea`, `LegalParagraph`, `EUDirective`, `EuroVocConcept`) and relationship types (`ADMINISTERED_BY`, `IN_LEGAL_AREA`, `AMENDED_BY`, `AMENDS`, `SEE_ALSO`, `HAS_LANGUAGE_VARIANT`, `REFERENCES`, `IMPLEMENTS_EEA`, `HAS_PARAGRAPH`, `CITES_EU_LAW`, `EU_CITES`, `EU_MODIFIED_BY`, `HAS_EUROVOC_DESCRIPTOR`)
- `norwegian-law-seeder`: Standalone Go seeder program that fetches Lovdata public archives, parses HTML law/regulation documents into structured objects with paragraphs, fetches EU directive metadata from EUR-Lex and EuroVoc SPARQL, and ingests everything using the Memory SDK with two-phase checkpointing, batching (100 items), and 20 concurrent workers

### Modified Capabilities

## Impact

- New standalone repository at `/root/norwegian-law-memory-blueprint/` (no changes to the monorepo)
- Uses the Memory SDK from the local monorepo via `replace` directive: `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`
- Data sources: `api.lovdata.no` (NLOD 2.0 license), `eur-lex.europa.eu` (public HTML scrape), EuroVoc SPARQL endpoint
- No database migrations, no monorepo code changes, no API changes
