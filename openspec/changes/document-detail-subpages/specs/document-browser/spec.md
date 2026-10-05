## ADDED Requirements

### Requirement: Navigate within a document's sub-pages

The document detail view SHALL present a sub-navigation rail linking three views — Overview, Chunks, and Extraction history — and SHALL mark the active view.

#### Scenario: Sub-navigation available on every document view

- **WHEN** any of the document detail views is open
- **THEN** the rail lists Overview, Chunks, and Extraction history
- **AND** the current view is marked active

#### Scenario: Overview is the document entry point

- **WHEN** the user opens a document
- **THEN** the Overview view is shown

### Requirement: Preview the original document

The Overview view SHALL preview the document's original content by media type: PDFs inline, images inline, text/markdown as a capped text preview, and other media types as a download link.

#### Scenario: PDF preview

- **WHEN** the document's media type is `application/pdf`
- **THEN** the Overview embeds the original file inline

#### Scenario: Image preview

- **WHEN** the document's media type is an image type
- **THEN** the Overview shows the original image inline

#### Scenario: Text preview

- **WHEN** the document is text or markdown
- **THEN** the Overview shows a capped, escaped text preview of its content

#### Scenario: Preview bytes are authenticated

- **WHEN** the browser requests a document's preview bytes
- **THEN** the gateway fetches them with the caller's credentials and streams them inline

### Requirement: Review extraction history

The Extraction history view SHALL list every extraction job for the document, newest first, each showing when it ran, its status, the number of objects and relationships it created, and the types it discovered.

#### Scenario: Document with multiple extractions

- **WHEN** the user opens a document that has been extracted more than once
- **THEN** every extraction job is listed, newest first, with its timestamp, status, and counts

#### Scenario: No extractions yet

- **WHEN** the user opens a document that has never been extracted
- **THEN** a clear "no extractions yet" state is shown

### Requirement: Inspect and merge a job's extraction branch

Each extraction-history entry SHALL expose the objects and relationships that run produced on its staging branch, and SHALL offer to merge that branch into the main graph when it is not already merged.

#### Scenario: View a job's extracted objects

- **WHEN** the user expands an extraction-history entry
- **THEN** the objects and relationships that job created on its staging branch are shown

#### Scenario: Merge an unmerged branch

- **WHEN** the user merges an extraction job's staging branch
- **THEN** the branch is merged into the main graph with the partial policy, and the view reports the outcome

#### Scenario: Merge action hidden when unavailable

- **WHEN** an extraction job has no staging branch, or its branch was already merged
- **THEN** no merge action is offered for that entry

## MODIFIED Requirements

### Requirement: Preview a document's chunks

The chunks view of a document SHALL show its chunks with their text.

#### Scenario: Open a document

- **WHEN** the user opens a document's Chunks view
- **THEN** the document's chunks are shown, each with its text

#### Scenario: Document has no chunks

- **WHEN** the user opens a document that has no chunks
- **THEN** a clear "no chunks yet" state is shown

### Requirement: Show extraction results

Extraction results SHALL be organised as per-job history: the objects and relationships produced by each extraction job are shown on that job's entry in the Extraction history view, rather than a single most-recent-results section on the overview.

#### Scenario: Extraction has results

- **WHEN** the user opens a document whose extraction has completed
- **THEN** the produced objects and relationships appear under that job in the Extraction history view

#### Scenario: No extraction yet

- **WHEN** the user opens a document with no completed extraction
- **THEN** the Extraction history view shows the empty state and no results
