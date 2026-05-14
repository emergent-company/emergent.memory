# Domain-Aware Extraction — Full Spec & Implementation Plan

## Goal

Extend the Memory extraction pipeline so that:
1. Incoming documents are classified into a domain (e.g. "Medical Records", "AI Chat", "Legal Contract")
2. Extraction is guided by domain-specific prompts and schema types
3. When no domain matches, a human-in-the-loop discovery agent asks the user to create, extend, or map to an existing schema pack
4. The domain-discovery agent is triggered from the `/remember` endpoint (and test agent equivalent)

---

## Background

### Current State

- `ObjectExtractionWorker` extracts entities using general prompts — no domain context
- `GraphMemorySchema` has an opaque `ExtractionPrompts JSONB` field — unused
- `project_info` exists on `Project` but never flows into the extraction pipeline
- `discoveryjobs` induces schema types from documents but uses a hardcoded global LLM (bypasses per-project model config)
- `ask_user` tool exists and works in ADK agent runs (pause/resume, `kb.agent_questions`)
- `/remember` endpoint runs a graph-insert agent with `schema_policy` (auto/reuse_only/ask)

### Gaps

- No document classification before extraction
- No domain guidance injected into extraction prompts
- No automatic discovery trigger when a document fits no known domain
- No human-in-the-loop for schema pack create/extend/map decisions
- `discoveryjobs` bypasses `adk.ModelFactory` / per-project model resolution

---

## Architecture

### Classification Flow

```
Document arrives (via /remember or direct upload)
        │
        ▼
DocumentClassifier.Classify(doc, projectID, projectInfo)
        │
        ├── Stage 1: Heuristic (~0ms, no LLM)
        │     Load installed schemas → score ClassificationSignals
        │     MIME match +0.5, filename pattern +0.3, keyword hits capped +0.4
        │     confidence ≥ 0.7 → matched schema
        │
        ├── Stage 2: LLM (only if stage 1 < 0.7)
        │     Prompt: project_info + domain descriptions + filename/MIME + first 800 chars
        │     confidence ≥ 0.4 → matched schema
        │
        └── confidence < 0.4 → "new_domain"
```

### Extraction Flow (matched domain)

```
ClassificationResult{SchemaID: X, confidence: 0.85}
        │
        ▼
Load SchemaExtractionPrompts from matched schema
        │
        ▼
ExtractionPipelineInput{
    ProjectContext: project.ProjectInfo,
    DomainGuidance: schema.ExtractionPrompts.EntityGuidance + RelationshipGuidance,
    ...existing fields...
}
        │
        ▼
BuildDomainSection() injects PROJECT PURPOSE + DOMAIN CONTEXT blocks into prompts
        │
        ▼
Richer, domain-specific entity extraction
```

### Extraction Flow (new domain)

```
ClassificationResult{Stage: "new_domain"}
        │
        ├── General extraction runs immediately (don't block user)
        ├── Write domain_label="new_domain" to kb.documents
        └── Trigger domain-discovery agent run (async, non-blocking)
```

### Domain Discovery Agent Flow

```
domain-discovery agent run starts
(triggered by new_domain result, or manually by user)
        │
        ├── 1. list-installed-schemas        ← always fresh
        ├── 2. check for pending docs        ← domain_label="new_domain"
        │
        ├── if none pending → "All documents classified. Done."
        │
        └── for the current unclassified document:
              ├── 3. run_discovery_job(docID) → discovered types
              ├── 4. compare vs installed schemas (overlap %)
              ├── 5. ask_user:
              │       "Document: invoice-2026-05.pdf
              │        Detected domain: Supplier Agreement
              │        Discovered types: Party, Contract, Obligation, PaymentTerm
              │
              │        Installed packs:
              │          [AI Chat]         8% overlap
              │          [Medical Records] 3% overlap
              │
              │        Recommendation: Create new pack 'Supplier Agreements'
              │
              │        [Create new pack: Supplier Agreements]
              │        [Extend: AI Chat]
              │        [Extend: Medical Records]
              │        [Skip]"
              │
              ├── 6. user responds
              ├── 7. finalize-discovery(mode, packName, types)
              ├── 8. queue-reextraction(docID, schemaID)
              └── 9. loop → go back to step 1 (re-check schemas, next doc)
```

**Key rule:** step 9 always re-loads `list-installed-schemas` before the next document.
If user just created "AI Chat" pack for doc 1, it is available as an extend option for doc 2.

### schema_policy Behavior

| policy | classifier match | classifier new_domain |
|---|---|---|
| `auto` | extract with domain guidance | general extraction + trigger discovery agent |
| `reuse_only` | extract with domain guidance | general extraction only, no discovery |
| `ask` | ask_user even on match (user reviews) | general extraction + trigger discovery agent |

---

## Data Model Changes

### `SchemaExtractionPrompts` (typed Go struct)

Replaces opaque `ExtractionPrompts JSON` on `GraphMemorySchema`.

```go
type ClassificationSignals struct {
    Keywords         []string `json:"keywords"`
    MimeTypes        []string `json:"mime_types"`
    FilenamePatterns []string `json:"filename_patterns"`
    ConfidenceHint   float32  `json:"confidence_hint"`
}

type SchemaExtractionPrompts struct {
    DomainDescription    string                `json:"domain_description"`
    EntityGuidance       string                `json:"entity_guidance"`
    RelationshipGuidance string                `json:"relationship_guidance"`
    Classification       ClassificationSignals `json:"classification"`
}
```

Field on `GraphMemorySchema`: `ExtractionPrompts SchemaExtractionPrompts`
Backward compatible — old JSONB rows unmarshal to zero value struct.

### Migration `00110_add_document_domain.sql`

```sql
-- +goose Up
ALTER TABLE kb.documents
  ADD COLUMN domain_label      text,
  ADD COLUMN domain_confidence real,
  ADD COLUMN matched_schema_id uuid REFERENCES kb.graph_schemas(id);

-- +goose Down
ALTER TABLE kb.documents
  DROP COLUMN IF EXISTS domain_label,
  DROP COLUMN IF EXISTS domain_confidence,
  DROP COLUMN IF EXISTS matched_schema_id;
```

### `Document` struct additions

```go
DomainLabel      *string    `bun:"domain_label"`
DomainConfidence *float32   `bun:"domain_confidence"`
MatchedSchemaID  *uuid.UUID `bun:"matched_schema_id"`
```

### `ExtractionPipelineInput` additions

```go
ProjectContext string  // from project.ProjectInfo
DomainGuidance string  // from matched schema ExtractionPrompts (EntityGuidance + RelationshipGuidance)
```

---

## Implementation Phases

### Phase 1 — Fix `discoveryjobs` LLM provider

**File:** `apps/server/domain/discoveryjobs/module.go`

**Problem:** `NewLLMProvider` hardcodes Vertex AI from global `cfg.LLM.*`.
Bypasses per-project model resolution via `adk.ModelFactory` + `CredentialResolver`.

**Fix:** Inject `*adk.ModelFactory` (same pattern as `ObjectExtractionWorker`).
Discovery service calls `modelFactory.CreateModel(ctx)` per job, resolving project → org → env hierarchy.

**Touches:** `module.go`, `service.go` (replace `s.llm` usages with `s.modelFactory.CreateModel(ctx)`)

---

### Phase 2 — Typed `SchemaExtractionPrompts`

**File:** `apps/server/domain/extraction/schema_provider.go`

Replace:
```go
ExtractionPrompts JSON `bun:"extraction_prompts,type:jsonb"`
```
With:
```go
ExtractionPrompts SchemaExtractionPrompts `bun:"extraction_prompts,type:jsonb"`
```

Add `ClassificationSignals` and `SchemaExtractionPrompts` structs.

Update `GetProjectSchemas` / `MemorySchemaProvider` to surface `ExtractionPrompts` in its return type
so `ObjectExtractionWorker` and `DocumentClassifier` can access it.

---

### Phase 3 — Migration + Document fields

**New file:** `apps/server/migrations/00110_add_document_domain.sql`
(see Data Model section above for SQL)

**File:** `apps/server/domain/documents/entity.go`
Add 3 fields to `Document` struct (see Data Model above).

---

### Phase 4 — Domain context in extraction pipeline

**File:** `apps/server/domain/extraction/agents/pipeline.go`

Add to `ExtractionPipelineInput`:
```go
ProjectContext string
DomainGuidance string
```

**File:** `apps/server/domain/extraction/agents/prompts.go`

Add function:
```go
func BuildDomainSection(input ExtractionPipelineInput) string
```

Logic:
- If `DomainGuidance` non-empty → prepend `DOMAIN CONTEXT:\n<guidance>` block
- If `ProjectContext` non-empty → prepend `PROJECT PURPOSE:\n<context>` block
- Both empty → return `""` (no-op, preserves current behavior exactly)

Wire into `BuildEntityExtractionPrompt` and `BuildRelationshipPrompt`.

---

### Phase 5 — `DocumentClassifier`

**New file:** `apps/server/domain/extraction/document_classifier.go`

```go
type ClassificationResult struct {
    SchemaID   *uuid.UUID
    Label      string
    Confidence float32
    Stage      string // "heuristic" | "llm" | "new_domain"
}

type DocumentClassifier struct {
    schemaProvider SchemaProvider
    modelFactory   *adk.ModelFactory
    log            *slog.Logger
}

func (c *DocumentClassifier) Classify(
    ctx context.Context,
    doc *documents.Document,
    projectID uuid.UUID,
    projectInfo string,
) (ClassificationResult, error)
```

**Stage 1 — heuristic (~0ms, no LLM):**
- Load installed schemas via `GetProjectSchemas(ctx, projectID)`
- For each schema, score against `ExtractionPrompts.Classification`:
  - MIME exact match → +0.5
  - Filename pattern match (filepath.Match glob) → +0.3
  - Keyword scan of first 500 chars of `doc.Content` → +0.05 per hit, max +0.4
- Best score ≥ 0.7 → return `ClassificationResult{Stage: "heuristic", SchemaID: ..., Confidence: score}`

**Stage 2 — LLM (only if stage 1 best score < 0.7):**

Prompt template:
```
Project purpose: <projectInfo>

Installed domains:
<for each schema>
- "<name>": <domain_description>
  Keywords: <keywords joined by comma>
</for>

Document filename: <doc.Filename>
MIME type: <doc.MimeType>
Content preview (first 800 chars):
<doc.Content[:800]>

Which installed domain best matches this document?
If none match well, suggest a new domain label.

Respond with JSON only:
{"schema_id": "<uuid or null>", "confidence": 0.0, "suggested_label": "<if schema_id null>"}
```

- Parse JSON response
- confidence ≥ 0.4 and schema_id non-null → return `ClassificationResult{Stage: "llm", ...}`
- else → return `ClassificationResult{Stage: "new_domain", Label: suggested_label}`

**Wire via fx:** inject `DocumentClassifier` into `module.go`, provide to `ObjectExtractionWorker`.

---

### Phase 6 — Wire classifier into `ObjectExtractionWorker`

**File:** `apps/server/domain/extraction/object_extraction_worker.go`

In the job processing function, before calling `ExtractionPipeline.Run`:

```
1. Load project by projectID → get ProjectInfo string
2. Call classifier.Classify(ctx, doc, projectID, projectInfo)
3. UPDATE kb.documents SET domain_label=..., domain_confidence=..., matched_schema_id=... WHERE id=doc.ID
4. If result.Stage != "new_domain":
   a. Load SchemaExtractionPrompts from result.SchemaID
   b. input.DomainGuidance = prompts.EntityGuidance + "\n\n" + prompts.RelationshipGuidance
   c. input.ProjectContext = projectInfo
5. Else (new_domain):
   a. input.ProjectContext = projectInfo (general extraction still benefits)
   b. After pipeline.Run completes, if schema_policy != "reuse_only":
      → go enqueueDiscoveryJob(ctx, projectID, doc.ID)  // async, non-blocking
```

`enqueueDiscoveryJob` creates a `DiscoveryJob` with `PendingReview: true` for the document.

---

### Phase 7 — Discovery job writes `ClassificationSignals` back

**File:** `apps/server/domain/discoveryjobs/service.go`

At end of `processDiscoveryJob`, after `CreateMemorySchema`, before `MarkCompleted`:

Add LLM call to generate `SchemaExtractionPrompts`:

Prompt:
```
You discovered these entity types from a document:
<for each type>
- <TypeName>: <description> (confidence: <X>)
</for>

KB Purpose: <job.KBPurpose>

Generate extraction guidance and classification signals for this domain.
Respond with JSON only:
{
  "domain_description": "2-3 sentence summary of what this domain covers",
  "entity_guidance": "specific instructions for extracting these entity types",
  "relationship_guidance": "relationship patterns typical between these entity types",
  "classification": {
    "keywords": ["10-15 keywords that identify documents of this domain"],
    "mime_types": ["application/pdf"],
    "filename_patterns": ["*contract*", "*agreement*"],
    "confidence_hint": 0.8
  }
}
```

Write result to `kb.graph_schemas.extraction_prompts` via:
```go
repo.UpdateExtractionPrompts(ctx, schemaID, prompts)
```

Add `UpdateExtractionPrompts(ctx, schemaID uuid.UUID, prompts SchemaExtractionPrompts) error`
to `discoveryjobs/store.go`.

---

## Part B — MCP Tools

**New file:** `apps/server/domain/mcp/domain_tools.go`

Four tools, following existing `ToolDefinition` + `execute*` pattern in `token_tools.go`:

### `classify-document`

```
Input:  project_id (string, required), document_id (string, required)
Output: {schema_id: string|null, label: string, confidence: float, stage: string}
Action: Calls DocumentClassifier.Classify, returns result
Note:   Does NOT write to document — read-only, for agent introspection
```

### `list-installed-schemas`

```
Input:  project_id (string, required)
Output: [{id, name, domain_description, keywords, mime_types, filename_patterns}]
Action: Calls schemaProvider.GetProjectSchemas, maps ExtractionPrompts.Classification per schema
```

### `finalize-discovery`

```
Input:  job_id (string, required)
        mode (string, required): "create" | "extend" | "map"
        pack_name (string, required if mode="create")
        existing_pack_id (string, required if mode="extend" or "map")
Output: {schema_id: string, message: string}
Action: Calls discoveryjobs.Service.FinalizeDiscovery
```

### `queue-reextraction`

```
Input:  project_id (string, required)
        document_id (string, required)
        schema_id (string, required)
Output: {job_id: string}
Action: Creates ObjectExtractionJob{
          JobType: "reextraction",
          ProjectID: projectID,
          DocumentID: documentID,
        }
        Also updates kb.documents.matched_schema_id = schemaID
```

Wire all four into:
- `domain_tools.go` — definitions func + execute methods
- `service.go` — dispatch switch case
- `entity.go` `AgentToolHandler` interface if needed to break import cycles

---

## Part C — Blueprint: `test-agents`

**New directory:** `blueprints/test-agents/`

```
blueprints/test-agents/
  agents/
    remember-test.yaml
```

**`blueprints/test-agents/agents/remember-test.yaml`:**

```yaml
name: remember-test
description: >
  Test agent mimicking the future built-in /remember endpoint with
  domain-aware extraction and human-in-the-loop schema discovery.
  Used for end-to-end testing of the domain classification pipeline.
systemPrompt: |
  You are a memory ingestion agent. When given content to remember
  (provided as document_id in your run input), follow these steps:

  STEP 1 — Check installed schemas
  Call list-installed-schemas to see what domain packs exist in this project.

  STEP 2 — Classify the document
  Call classify-document with the document_id from your input.

  STEP 3a — If a schema matched (confidence >= 0.7):
  The ObjectExtractionWorker has already run extraction with domain guidance.
  Report the match: schema name, confidence, stage (heuristic or llm).
  Done — no discovery needed.

  STEP 3b — If new_domain:
  General extraction has already run. Now handle domain discovery:

  a. Note the suggested_label from the classification result.

  b. Call finalize-discovery with mode="create" to trigger the discovery job.
     Wait for it to complete. Note the discovered types.

  c. Call list-installed-schemas again (fresh).
     Compare discovered types against installed schemas.
     Calculate rough overlap % for each installed schema.

  d. Formulate a recommendation:
     - overlap > 60% with an installed schema → recommend extend
     - no overlap > 30% → recommend create new pack
     - Suggest a clear pack name (e.g. "Medical Records", "Supplier Agreements")

  e. Call ask_user with a concise message:
     - Document filename and detected domain
     - Top 4-5 discovered entity types with one-line descriptions
     - Overlap scores with existing packs (if any)
     - Your recommendation (1 sentence)
     - Buttons: ["Create new pack: <name>", "Extend: <pack>", ..., "Skip"]

  f. Once user responds, call finalize-discovery with their chosen mode/pack.

  g. Call queue-reextraction so the document gets re-extracted with domain guidance.

  h. Report completion: what pack was created/extended, reextraction job ID.

  Always be concise. Keep ask_user messages under 200 words.

tools:
  - ask_user
  - classify-document
  - list-installed-schemas
  - finalize-discovery
  - queue-reextraction
  - memory_search
  - graph_query
  - graph_write
flowType: single
visibility: project
maxSteps: 50
```

Apply with:
```bash
memory blueprints blueprints/test-agents --project <project_id>
```

---

## Part D — Python Test Script

**New directory:** `bench/domain-test/`

```
bench/domain-test/
  run_domain_test.py
  fixtures/
    ai-chat-1.txt
    personal-notes.txt
    medical-lab-1.txt
    supplier-agreement.txt
    ai-chat-2.txt
    medical-lab-2.txt
```

### Test Fixture Content

#### `fixtures/ai-chat-1.txt`

```
[AI Assistant Session — 2026-05-10 14:32 UTC]

User: I need to book a flight from London Heathrow to JFK on June 3rd, returning June 17th. Economy class. Budget around £600.

See fixture files: `bench/domain-test/fixtures/`
See test script:   `bench/domain-test/run_domain_test.py`

---

## Execution Order

```
Phase 1  discoveryjobs LLM fix        (prerequisite for Phase 7)
Phase 2  typed SchemaExtractionPrompts (prerequisite for 5, 6, 7)
Phase 3  migration + Document fields   (prerequisite for 6)
Phase 4  pipeline domain context       (prerequisite for 6)
Phase 5  DocumentClassifier            (prerequisite for 6)
         ↓ (all above in parallel)
Phase 6  wire classifier into worker   (requires 2, 3, 4, 5)
Phase 7  discovery writes signals back (requires 1, 2)
         ↓
Part B   MCP domain tools              (requires 2, 5)
         ↓
Part C   blueprint test-agents         (requires Part B tools exist)
         ↓
Part D   run_domain_test.py            (requires all above deployed)
```

---

## Key Files Reference

| File | Role |
|---|---|
| `apps/server/domain/extraction/schema_provider.go` | `GraphMemorySchema`, `SchemaExtractionPrompts`, `ClassificationSignals` |
| `apps/server/domain/extraction/document_classifier.go` (new) | `DocumentClassifier`, `ClassificationResult` |
| `apps/server/domain/extraction/agents/pipeline.go` | `ExtractionPipelineInput` + new fields |
| `apps/server/domain/extraction/agents/prompts.go` | `BuildDomainSection()` |
| `apps/server/domain/extraction/object_extraction_worker.go` | classifier wiring, domain field writes |
| `apps/server/domain/documents/entity.go` | `Document` + 3 new fields |
| `apps/server/domain/discoveryjobs/module.go` | `adk.ModelFactory` injection |
| `apps/server/domain/discoveryjobs/service.go` | `ClassificationSignals` write-back |
| `apps/server/migrations/00110_add_document_domain.sql` (new) | DB migration |
| `apps/server/domain/mcp/domain_tools.go` (new) | 4 MCP tools |
| `blueprints/test-agents/agents/remember-test.yaml` (new) | test agent definition |
| `bench/domain-test/run_domain_test.py` (new) | e2e test script |
| `bench/domain-test/fixtures/` (new) | 6 realistic test documents |
