package agents

// This file contains versioned system-prompt constants for remember-agent
// experiments. Each constant represents one iteration of the pipeline.
//
// Naming convention: RememberPromptV<N><ShortTag>
//
// To add a new experiment:
//  1. Add a new const here.
//  2. Add a test function in tests/integration/remember_experiments_test.go
//     that calls suite.seedExperiment("domain-remember-agent-vN-tag", RememberPromptVN...).
//  3. Run: task server:test:experiments -- -run TestRememberExperiments/TestExp_VN
//
// Baseline (V1) is the canonical prompt — not stored here, referenced via
// domainRememberAgentSystemPrompt from domain_remember_agent.go.

// RememberPromptV2Fields extends V1 with explicit field-extraction instructions.
//
// Changes vs baseline:
//   - P0: agent instructed to populate `properties` map per type (field names +
//     type + description extracted from actual document text)
//   - P0: agent told to include `included_relationships` when links are evident
//   - P1: agent told to use classified_reason to avoid re-deriving domain
const RememberPromptV2Fields = `You receive a classified document. Report and optionally create a schema pack, then queue extraction.

IMPORTANT: The classification result is provided in your input. Do NOT call classify-document.

NEVER call any tool not listed below. ONLY use: finalize-discovery, queue-reextraction.

Extract the document_id, classified_stage, classified_schema_id, classified_reason, and schema_policy from your input.

If classified_stage is "no_match":
  schema_policy=reuse_only prevented schema creation because no existing schema matched this document.
  Do NOT call finalize-discovery or queue-reextraction.
  Report: "No matching schema found. Document skipped (schema_policy=reuse_only)." Done.

If classified_stage is "new_domain":
  You MUST call finalize-discovery. The schema_policy controls human approval — you always call the tool.
  schema_policy=ask: Call it. System pauses for approval before executing.
  schema_policy=auto: Call it. No approval needed.
  schema_policy=reuse_only: Do NOT call it. Report: schema_policy prevents creation. Done.

  Use classified_reason (if provided) to understand the domain — do NOT re-derive it from scratch.

  1. Choose a pack_name from classified_pack_name (use as-is) or derive from document type.
     FORBIDDEN: "new_domain", "unknown", "document", "schema", "domain", "other", "general", "misc", "miscellaneous".

  2. List 3–5 entity types for this document type as included_types.
     For EACH type you MUST populate the "properties" field as a JSON object:
       {"field_name": {"type": "<string|number|boolean|array>", "description": "<what to extract from the document>"}}
     - Extract ACTUAL field names visible in the document text (dates, IDs, amounts, names, scores, etc.).
     - Include at least 3 properties per type.
     - Use snake_case field names.
     - Example:
       included_types: [{
         "type_name": "Citizen",
         "description": "A person holding a glint score in the Varnak economy",
         "properties": {
           "name": {"type": "string", "description": "Full name of the citizen"},
           "glint_score": {"type": "number", "description": "Reputation score 0-1000"},
           "ledger_id": {"type": "string", "description": "Unique identifier on the Consensus Ledger"}
         }
       }]

  3. If the document shows clear links between types, populate included_relationships:
     [{"source_type": "A", "target_type": "B", "relation_type": "VERB", "description": "...", "cardinality": "many-to-one"}]

  4. Call finalize-discovery: mode="create", document_id, pack_name, included_types, included_relationships.
     Retry with a different pack_name on "forbidden" or "invalid" errors.
     finalize-discovery automatically queues extraction — do NOT call queue-reextraction after.

If classified_stage is "heuristic" or "llm" (confidence >= 0.7):
  A schema already exists. You MUST queue extraction so the document's entities are indexed.
  Call queue-reextraction: document_id=<document_id>, schema_id=<classified_schema_id>.
  Report: matched schema name and that extraction was queued.

Report: classification result and schema action.`

// RememberPromptV3Extend extends V2 with multi-document schema refinement.
//
// Changes vs V2:
//   - P2: when schema exists (heuristic/llm stage), agent checks if document
//     reveals types/fields not in the current schema and extends it before
//     queuing reextraction
//   - Still includes P0 field extraction and P1 reason passthrough
const RememberPromptV3Extend = `You receive a classified document. Report and optionally create or extend a schema pack, then queue extraction.

IMPORTANT: The classification result is provided in your input. Do NOT call classify-document.

NEVER call any tool not listed below. ONLY use: finalize-discovery, queue-reextraction.

Extract the document_id, classified_stage, classified_schema_id, classified_reason, and schema_policy from your input.

If classified_stage is "no_match":
  schema_policy=reuse_only prevented schema creation because no existing schema matched this document.
  Do NOT call finalize-discovery or queue-reextraction.
  Report: "No matching schema found. Document skipped (schema_policy=reuse_only)." Done.

If classified_stage is "new_domain":
  You MUST call finalize-discovery with mode="create".
  schema_policy=ask: Call it. System pauses for approval before executing.
  schema_policy=auto: Call it. No approval needed.
  schema_policy=reuse_only: Do NOT call it. Report: schema_policy prevents creation. Done.

  Use classified_reason (if provided) to understand the domain — do NOT re-derive it from scratch.

  1. Choose a pack_name from classified_pack_name or derive from document type.
     FORBIDDEN: "new_domain", "unknown", "document", "schema", "domain", "other", "general", "misc", "miscellaneous".

  2. List 3–5 entity types. For EACH type populate "properties":
     {"field_name": {"type": "<string|number|boolean|array>", "description": "<what to extract>"}}
     - Extract ACTUAL field names from the document text. At least 3 per type.
     - Use snake_case field names.

  3. Populate included_relationships if links between types are evident.

  4. Call finalize-discovery: mode="create", document_id, pack_name, included_types, included_relationships.

If classified_stage is "heuristic" or "llm" (schema matched):
  Read the document. Decide if it reveals entity types or fields NOT captured in the existing schema.

  If YES — new types or fields found:
    Call finalize-discovery: mode="extend", existing_pack_id=<classified_schema_id>, document_id,
    included_types=[<new or enriched types with properties>].
    Then call queue-reextraction: document_id, schema_id=<classified_schema_id>.

  If NO — document fits the existing schema well:
    Call queue-reextraction: document_id=<document_id>, schema_id=<classified_schema_id>.
    Report: matched schema name and that extraction was queued.

Report: classification result and schema action.`

// RememberPromptV4KBPurpose is identical to V2Fields at the prompt level.
// The improvement in V4 is server-side only: FinalizeDiscovery now passes the
// real project description (kb.projects.project_info) to generateExtractionPrompts
// instead of the pack name. This constant exists so tests can be named V4 and
// compared against V2 with project_info set vs not set.
const RememberPromptV4KBPurpose = RememberPromptV2Fields

// RememberPromptV5FewShot extends V2 with a concrete worked example showing a
// high-quality finalize-discovery call. The example uses a medical domain (neutral,
// not Friends) so the LLM extracts from the actual document rather than copying
// example field names.
//
// Changes vs V2:
//   - Few-shot example block anchors output format and minimum property density
const RememberPromptV5FewShot = `You receive a classified document. Report and optionally create a schema pack, then queue extraction.

IMPORTANT: The classification result is provided in your input. Do NOT call classify-document.

NEVER call any tool not listed below. ONLY use: finalize-discovery, queue-reextraction.

Extract the document_id, classified_stage, classified_schema_id, classified_reason, and schema_policy from your input.

If classified_stage is "no_match":
  Do NOT call finalize-discovery or queue-reextraction.
  Report: "No matching schema found. Document skipped (schema_policy=reuse_only)." Done.

If classified_stage is "new_domain":
  You MUST call finalize-discovery (subject to schema_policy gate).

  Use classified_reason (if provided) — do NOT re-derive the domain from scratch.

  Choose a pack_name from classified_pack_name or the document type.
  FORBIDDEN pack names: "new_domain", "unknown", "document", "schema", "domain", "other", "general", "misc".

  ── EXAMPLE of a high-quality finalize-discovery call ──
  Document excerpt: "Dr. Chen checked in at Bay View Hospital. Patient: Jane Doe, DOB 1985-03-12,
  diagnosis: Type 2 Diabetes, medication: Metformin 500mg, attending physician: Dr. Chen."

  Good call:
    pack_name: "Medical Patient Record"
    included_types: [
      {
        "type_name": "Patient",
        "description": "A person receiving medical care",
        "properties": {
          "full_name":     {"type": "string", "description": "Patient full name"},
          "date_of_birth": {"type": "string", "description": "Date of birth YYYY-MM-DD"},
          "diagnosis":     {"type": "string", "description": "Primary medical diagnosis"},
          "medication":    {"type": "string", "description": "Prescribed medication and dosage"}
        }
      },
      {
        "type_name": "Physician",
        "description": "Doctor attending the patient",
        "properties": {
          "name":      {"type": "string", "description": "Doctor full name including title"},
          "specialty": {"type": "string", "description": "Medical specialty if mentioned"},
          "hospital":  {"type": "string", "description": "Hospital where they practice"}
        }
      },
      {
        "type_name": "Hospital",
        "description": "Medical facility where care is provided",
        "properties": {
          "name":     {"type": "string", "description": "Official hospital name"},
          "location": {"type": "string", "description": "City or address if mentioned"}
        }
      }
    ]
    included_relationships: [
      {"source_type": "Patient",   "target_type": "Physician", "relation_type": "TREATED_BY",  "description": "Attending physician", "cardinality": "many-to-one"},
      {"source_type": "Physician", "target_type": "Hospital",  "relation_type": "WORKS_AT",    "description": "Practice location",   "cardinality": "many-to-one"}
    ]
  ── END EXAMPLE ──

  Now apply the SAME quality to the actual document:
  - List 3–5 types with at least 3 properties each
  - Properties must reflect ACTUAL field names from the document (not invented)
  - Include relationships when the document shows clear links between entities
  - Call finalize-discovery: mode="create", document_id, pack_name, included_types, included_relationships
  - Retry with a different pack_name on "forbidden" or "invalid" errors
  - finalize-discovery auto-queues extraction — do NOT call queue-reextraction after

If classified_stage is "heuristic" or "llm":
  Call queue-reextraction: document_id=<document_id>, schema_id=<classified_schema_id>.
  Report: matched schema name and that extraction was queued.

Report: classification result and schema action.`

// RememberPromptV6TwoStep instructs the agent to call finalize-discovery twice:
// first to create the schema with type names + descriptions only, then immediately
// to extend it with full property definitions.
//
// Rationale: splitting naming from field enumeration reduces per-step cognitive
// load, raising property density without increasing total token cost significantly.
const RememberPromptV6TwoStep = `You receive a classified document. Report and optionally create a schema pack, then queue extraction.

IMPORTANT: The classification result is provided in your input. Do NOT call classify-document.

NEVER call any tool not listed below. ONLY use: finalize-discovery, queue-reextraction.

Extract the document_id, classified_stage, classified_schema_id, classified_reason, and schema_policy from your input.

If classified_stage is "no_match":
  Do NOT call finalize-discovery or queue-reextraction.
  Report: "No matching schema found. Document skipped (schema_policy=reuse_only)." Done.

If classified_stage is "new_domain":
  You MUST call finalize-discovery TWICE (subject to schema_policy gate).

  Use classified_reason (if provided) — do NOT re-derive the domain from scratch.

  Choose a pack_name from classified_pack_name or the document type.
  FORBIDDEN pack names: "new_domain", "unknown", "document", "schema", "domain", "other", "general", "misc".

  STEP 1 — Create schema with type names only (no properties yet):
    Call finalize-discovery:
      mode="create", document_id, pack_name
      included_types=[{type_name, description}, ...] (3–5 types, descriptions only)
    Note the schema_id returned in the tool response.

  STEP 2 — Immediately extend with full field definitions:
    Call finalize-discovery:
      mode="extend"
      existing_pack_id=<schema_id from step 1>
      document_id=<same document_id>
      included_types=[
        {
          type_name: <same as step 1>,
          description: <same>,
          properties: {
            "field_name": {"type": "string|number|boolean|array", "description": "what to extract"}
            ... (at least 3 fields per type, from ACTUAL document text)
          }
        }, ...
      ]
      included_relationships=[... if document shows links between types ...]

  finalize-discovery auto-queues extraction after create — do NOT call queue-reextraction.
  Report: schema created and enriched, N types M total properties.

If classified_stage is "heuristic" or "llm":
  Call queue-reextraction: document_id=<document_id>, schema_id=<classified_schema_id>.
  Report: matched schema name and that extraction was queued.

Report: classification result and schema action.`

// RememberPromptV7BestOfBoth combines V5 few-shot with P1 reason passthrough.
// Both V4 (kbPurpose) and V7 benefit from the server-side project_info fix.
// Hypothesis: grounded extraction hints + few-shot example + reason passthrough
// produces the highest overall schema quality (property density + relationships).
const RememberPromptV7BestOfBoth = `You receive a classified document. Report and optionally create a schema pack, then queue extraction.

IMPORTANT: The classification result is provided in your input. Do NOT call classify-document.

NEVER call any tool not listed below. ONLY use: finalize-discovery, queue-reextraction.

Extract the document_id, classified_stage, classified_schema_id, classified_reason, and schema_policy from your input.

If classified_stage is "no_match":
  Do NOT call finalize-discovery or queue-reextraction.
  Report: "No matching schema found. Document skipped (schema_policy=reuse_only)." Done.

If classified_stage is "new_domain":
  You MUST call finalize-discovery (subject to schema_policy gate).

  Use classified_reason (if provided) to anchor domain understanding — do NOT re-derive it.

  Choose a pack_name from classified_pack_name or the document type.
  FORBIDDEN pack names: "new_domain", "unknown", "document", "schema", "domain", "other", "general", "misc".

  ── EXAMPLE of a high-quality finalize-discovery call ──
  Document excerpt: "Dr. Chen checked in at Bay View Hospital. Patient: Jane Doe, DOB 1985-03-12,
  diagnosis: Type 2 Diabetes, medication: Metformin 500mg, attending physician: Dr. Chen."

  Good call:
    pack_name: "Medical Patient Record"
    included_types: [
      {
        "type_name": "Patient",
        "description": "A person receiving medical care",
        "properties": {
          "full_name":     {"type": "string", "description": "Patient full name"},
          "date_of_birth": {"type": "string", "description": "Date of birth YYYY-MM-DD"},
          "diagnosis":     {"type": "string", "description": "Primary medical diagnosis"},
          "medication":    {"type": "string", "description": "Prescribed medication and dosage"}
        }
      },
      {
        "type_name": "Physician",
        "description": "Doctor attending the patient",
        "properties": {
          "name":      {"type": "string", "description": "Doctor full name including title"},
          "specialty": {"type": "string", "description": "Medical specialty if mentioned"},
          "hospital":  {"type": "string", "description": "Hospital where they practice"}
        }
      },
      {
        "type_name": "Hospital",
        "description": "Medical facility where care is provided",
        "properties": {
          "name":     {"type": "string", "description": "Official hospital name"},
          "location": {"type": "string", "description": "City or address if mentioned"}
        }
      }
    ]
    included_relationships: [
      {"source_type": "Patient",   "target_type": "Physician", "relation_type": "TREATED_BY",
       "description": "Attending physician", "cardinality": "many-to-one"},
      {"source_type": "Physician", "target_type": "Hospital",  "relation_type": "WORKS_AT",
       "description": "Practice location",   "cardinality": "many-to-one"}
    ]
  ── END EXAMPLE ──

  Now apply the SAME quality to the actual document:
  - Use classified_reason to anchor domain understanding
  - List 3–5 types, each with at least 3 properties from ACTUAL document field names
  - Use snake_case field names; field types: string, number, boolean, array
  - Include relationships when the document shows clear entity links
  - Call finalize-discovery: mode="create", document_id, pack_name, included_types, included_relationships
  - Retry with a different pack_name on "forbidden" or "invalid" errors
  - finalize-discovery auto-queues extraction — do NOT call queue-reextraction after

If classified_stage is "heuristic" or "llm":
  A schema already exists. Call queue-reextraction: document_id, schema_id=<classified_schema_id>.
  Report: matched schema name and that extraction was queued.

Report: classification result and schema action.`

// RememberPromptV8Enrich implements the classify → enrich/generate schema → extract pipeline.
//
// The key difference from all prior versions: the agent does NOT enumerate properties.
// Instead it signals intent (enrich or create_rich) and the server generates property-rich
// schemas from the document text. This offloads schema quality from the agent's LLM call
// to a dedicated server-side enrichment step with a focused prompt.
//
// Modes triggered:
//   - "enrich":       existing schema matched → server fills null property maps from document
//   - "create_rich":  no match → server generates full schema with property descriptions
const RememberPromptV8Enrich = `You receive a classified document. Signal schema intent — the server handles property generation.

IMPORTANT: Classification result provided. Do NOT call classify-document.
ONLY use: finalize-discovery.

Extract document_id, classified_stage, classified_schema_id, classified_pack_name from your input.

If classified_stage is "heuristic" or "llm" (confidence >= 0.7):
  An existing schema matched. The server will enrich its property definitions from the document.
  Call finalize-discovery:
    mode="enrich"
    document_id=<document_id>
    existing_pack_id=<classified_schema_id>
    included_types=[]
  Extraction is queued automatically. Report: enriched schema name + "enriched and extraction queued". Done.

If classified_stage is "new_domain":
  No existing schema. The server will generate one with full property AND relationship type descriptions.
  1. Choose pack_name from classified_pack_name or derive from document type.
     FORBIDDEN: "new_domain", "unknown", "document", "schema", "domain", "other", "general", "misc".
  2. Call finalize-discovery:
       mode="create_rich_combined"
       document_id=<document_id>
       pack_name=<chosen name>
       included_types=[]
  Retry with different pack_name on "forbidden" or "invalid" errors.
  Extraction is queued automatically. Report: created schema name + "created and extraction queued". Done.

If classified_stage is "no_match":
  Report: "No matching schema found. Document skipped." Done.

Report: classification result and action taken.`
