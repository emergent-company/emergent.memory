## ADDED Requirements

### Requirement: Server consumes extracted images and metadata

The server SHALL consume both the images and the metadata returned by extraction, not only the content and tables. It SHALL persist the returned metadata fields (page count, title, author, creation date, modification date, producer) and the count of extracted images on the document, and SHALL hand the extracted images to the image pipeline.

#### Scenario: Extracted images handed to the image pipeline

- **WHEN** a document is parsed and the extractor returns one or more images
- **THEN** the server passes those images to the image pipeline instead of discarding them

#### Scenario: Extracted metadata persisted

- **WHEN** a document is parsed and the extractor returns page count, title, and author
- **THEN** the document records those values and the count of extracted images

#### Scenario: Absent metadata tolerated

- **WHEN** the extractor returns content without metadata fields
- **THEN** the document is still parsed successfully and the missing fields are left unset

### Requirement: Extraction reports image origin and dimensions

The extraction contract SHALL report, for each extracted image, whether it is a full-page image from a scanned page or an embedded illustration, together with its pixel dimensions. When an image's origin is not reported, the server SHALL treat it as an embedded illustration.

#### Scenario: Reported origin is used

- **WHEN** the extractor reports an image as a full page from a scanned document
- **THEN** the server classifies that image as a scanned page

#### Scenario: Missing origin defaults to illustration

- **WHEN** an extracted image has no reported origin
- **THEN** the server classifies it as an embedded illustration rather than a scanned page

#### Scenario: Dimensions available

- **WHEN** an image is extracted
- **THEN** its pixel dimensions are available to the server without re-decoding the image where the contract supplies them
