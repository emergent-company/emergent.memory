## MODIFIED Requirements

### Requirement: Upload a document

The documents page SHALL let the user select a file and upload it as a new document. When the user activates the upload control without having chosen a file, the page SHALL open the file picker rather than submit an empty form.

#### Scenario: Successful upload

- **WHEN** the user selects a valid file and uploads it
- **THEN** the document is ingested and appears in the documents list

#### Scenario: Upload pressed with no file chosen

- **WHEN** the user activates the upload control without having chosen a file
- **THEN** the file picker opens and no upload request is sent

#### Scenario: Upload fails

- **WHEN** an upload fails
- **THEN** a clear error message is shown and the page remains usable
