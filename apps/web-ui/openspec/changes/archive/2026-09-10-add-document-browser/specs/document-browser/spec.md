## Purpose

Provides the gateway web UI surface for browsing ingested documents, uploading new ones, previewing chunks, and triggering extraction.

## ADDED Requirements

### Requirement: Navigate to the documents page

The app SHALL provide a navigation entry that opens the documents page.

#### Scenario: Open documents from navigation

- **WHEN** a user selects the documents entry in the app navigation
- **THEN** the documents page opens

### Requirement: List documents

The documents page SHALL list ingested documents, each showing its name, chunk count, and extraction status.

#### Scenario: Documents present

- **WHEN** the documents page loads and documents exist
- **THEN** the documents are listed, each showing name, chunk count, and extraction status

#### Scenario: No documents

- **WHEN** the documents page loads and no documents exist
- **THEN** a clear "no documents yet" empty state is shown

### Requirement: Upload a document

The documents page SHALL let the user select a file and upload it as a new document.

#### Scenario: Successful upload

- **WHEN** the user selects a valid file and uploads it
- **THEN** the document is ingested and appears in the documents list

#### Scenario: Upload fails

- **WHEN** an upload fails
- **THEN** a clear error message is shown and the page remains usable

### Requirement: Preview a document's chunks

Selecting a document SHALL show its chunks with their text.

#### Scenario: Open a document

- **WHEN** the user selects a document in the list
- **THEN** the document's chunks are shown, each with its text

#### Scenario: Document has no chunks

- **WHEN** the user opens a document that has no chunks
- **THEN** a clear "no chunks yet" state is shown

### Requirement: Trigger extraction

The document view SHALL let the user trigger extraction for a document and show the resulting status.

#### Scenario: Trigger extraction

- **WHEN** the user triggers extraction for a document
- **THEN** extraction starts and the view reflects the updated status

### Requirement: Show extraction results

The document view SHALL list the objects and relationships produced by the document's most recent completed extraction.

#### Scenario: Extraction has results

- **WHEN** the user opens a document whose extraction has completed
- **THEN** the extracted objects are listed with their type and name, and the extracted relationships are listed with their type and source/target names

#### Scenario: No extraction yet

- **WHEN** the user opens a document with no completed extraction
- **THEN** no extraction results section is shown

### Requirement: Surface load failures without crashing

The documents page SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails
- **THEN** the affected section shows an error message while the rest of the page remains usable
