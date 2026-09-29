# chat-citations Specification

## Purpose
Defines grounded citations for knowledge-base-backed chat answers: how the server
derives references to graph objects and relationships from data a run actually
retrieved, validates them, transports them on the live stream and the session
timeline, and how the web client renders them as validated inline links and a
Sources block (with an A2UI reference component).

## Requirements

### Requirement: Citations are grounded in retrieved data

The server SHALL derive an answer's citations from the identifiers the run's
tools actually returned and the answer actually referenced. It SHALL collect
candidate object and relationship references by walking the run's tool-call
outputs, and SHALL include a citation only when the answer references that
identifier **and** the identifier appears among the candidates. An identifier
referenced by the answer but absent from the candidates SHALL NOT become a
citation.

#### Scenario: Retrieved and referenced object is cited

- **WHEN** a run's `search-hybrid` output contains object id `X` and the answer links to `/objects/X`
- **THEN** the answer's citations SHALL include object `X` with its type and label

#### Scenario: Hallucinated identifier is not cited

- **WHEN** the answer links to `/objects/Y` and no tool output of the run contains object id `Y`
- **THEN** `Y` SHALL NOT appear in the citations

#### Scenario: Retrieved but unreferenced object is not cited

- **WHEN** a tool output of the run contains object `Z` and the answer never references `Z`
- **THEN** `Z` SHALL NOT appear in the citations

#### Scenario: Derivation covers every graph tool

- **WHEN** a run retrieves objects through `search-hybrid`, `entity-query`, `graph-traverse`, `search-similar`, or `entity-edges-get`, or relationships through `relationship-list`
- **THEN** the ids in those outputs SHALL be candidates for citation

### Requirement: Answer reference format

An answer SHALL reference an object with a markdown link whose target is
`/objects/<canonical-object-id>`, and a relationship with a markdown link whose
target is `/objects/<source-object-id>` carrying a `#relationship-<relationship-id>`
fragment. The identifiers SHALL be the canonical identifiers returned by the
tools.

#### Scenario: Object reference

- **WHEN** the answer contains `[Acme Corp](/objects/<id>)` and the run retrieved `<id>`
- **THEN** the citations SHALL include an object citation for `<id>` labelled `Acme Corp`

#### Scenario: Relationship reference

- **WHEN** the answer contains `[A —works_at→ B](/objects/<src>#relationship-<rel>)` and the run retrieved relationship `<rel>`
- **THEN** the citations SHALL include a relationship citation for `<rel>` with its type, source `A`, and target `B`

### Requirement: Key-based references are resolved

An answer MAY reference an object by its `key` (for example `lov/2005-06-17-90`)
instead of its canonical identifier. When a `/objects/<ref>` reference matches
the `key` of a retrieved object rather than its `id`, the server SHALL resolve it
to that object's canonical id and a citation SHALL be produced for that object.
The rendered link SHALL target `/objects/<canonical-id>` so it resolves, not the
key. A reference that matches neither a retrieved id nor a retrieved key SHALL be
treated as unvalidated.

#### Scenario: Key reference resolves to the object

- **WHEN** the answer links `/objects/lov/2005-06-17-90` and a retrieved object of the run carries that `key`
- **THEN** the citations SHALL include that object and the rendered link SHALL target `/objects/<canonical-id>`

#### Scenario: Unknown key is unvalidated

- **WHEN** the answer links a key that no retrieved object of the run carries
- **THEN** it SHALL NOT be cited and the link SHALL follow the neutralization rule

### Requirement: Citation payload shape

Each citation SHALL carry `kind` (`object` or `relationship`), `id`, `type`,
`label`, and `url`. An object citation's `url` SHALL be `/objects/<id>`; a
relationship citation's `url` SHALL resolve to the object page of its source. A
citation MAY additionally carry `key` — the object's human key — when the
reference was made by key, so a renderer can keep and re-target such a link.

#### Scenario: Shape is stable across transports

- **WHEN** a citation reaches the client on the live stream or in a reloaded transcript
- **THEN** it SHALL carry `kind`, `id`, `type`, `label`, and `url`

### Requirement: Citations are transported on live turns and history

The chat SSE stream SHALL carry a turn's citations in a dedicated terminal
`citations` event emitted before `done`, and the session timeline and run-history
responses SHALL carry the same citations on assistant message items. The live
turn and a reloaded transcript SHALL therefore render the same citations.

#### Scenario: Live turn carries citations

- **WHEN** a run produces citations and finishes
- **THEN** the client SHALL receive a `citations` event before `done` carrying them

#### Scenario: Reloaded transcript carries citations

- **WHEN** a conversation's history is loaded for an answer that has citations
- **THEN** that answer's timeline item SHALL carry the same citations

#### Scenario: Turn without citations is unchanged

- **WHEN** a run produces no citations
- **THEN** the stream and timeline SHALL behave exactly as before this capability

### Requirement: Unvalidated object links are neutralized

A link to `/objects/<id>` whose `id` is not a validated citation SHALL render as
its label text without a hyperlink. A `#relationship-<rel>` fragment whose `rel`
is not a validated citation SHALL be dropped. A link whose target is a validated
citation SHALL render as a working link.

#### Scenario: Unknown object link is demoted

- **WHEN** an answer links to `/objects/<id>` but `<id>` is not a citation
- **THEN** the rendered answer SHALL show the link label as plain text and SHALL NOT emit an anchor for that id

#### Scenario: Valid citation link is preserved

- **WHEN** an answer links to `/objects/<id>` and `<id>` is a citation
- **THEN** the rendered answer SHALL contain a link to that object page

#### Scenario: Unknown relationship fragment is dropped

- **WHEN** a link carries `#relationship-<rel>` and `<rel>` is not a citation
- **THEN** the fragment SHALL be removed and the remaining object link SHALL follow the object-link rule

#### Scenario: Key-based link is kept and re-targeted

- **WHEN** an answer links `/objects/<key>` and `<key>` is the key of a citation
- **THEN** the link SHALL be kept with its target rewritten to `/objects/<canonical-id>`

### Requirement: Sources are surfaced to the user

The web chat SHALL render a Sources block beneath an assistant answer that has
citations, listing each citation with its label and type and linking to its
`url`. The block SHALL be produced without interpreting answer text as markup.

#### Scenario: Sources block renders

- **WHEN** an answer has citations
- **THEN** a Sources block SHALL appear beneath it, each entry linking to its object page

#### Scenario: No citations, no block

- **WHEN** an answer has no citations
- **THEN** no Sources block SHALL be rendered

### Requirement: A2UI reference components

The A2UI catalog SHALL add a `sources` component (carrying an `items` list), and
the web renderer SHALL render each item as a link to its object page. The `entity`
component SHALL render its `id` prop as a link to `/objects/<id>` when present.
Both additions SHALL be additive and SHALL NOT change the envelope or existing
components.

#### Scenario: Sources card renders links

- **WHEN** an agent emits a `sources` component whose items carry ids
- **THEN** the client SHALL render each item as a link to its object page

#### Scenario: Entity card links its object

- **WHEN** an `entity` component carries an `id` prop
- **THEN** the client SHALL render a link to `/objects/<id>`

#### Scenario: Older clients degrade

- **WHEN** a client without a `sources` renderer receives a `sources` component
- **THEN** it SHALL fall back to the existing text/summary render and SHALL NOT error

### Requirement: Agents are instructed to cite retrieved sources

An agent that answers from the knowledge base SHALL be instructed to cite the
objects and relationships it uses, using the exact identifiers from tool results
and the reference format above, and to never invent an identifier.

#### Scenario: Instruction is present

- **WHEN** a knowledge-base-backed agent's instruction is assembled
- **THEN** it SHALL include the citation instruction and the reference format
