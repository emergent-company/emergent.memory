## Purpose

Makes document parsing robust to real-world file defects by detecting the effective format at the routing boundary, falling back between extraction engines, and tolerating repairable package and cell defects.

## ADDED Requirements

### Requirement: Detect the effective format at the routing boundary

The server SHALL determine a document's effective format from its content signature before choosing the parse path, and SHALL route by that effective format rather than the declared extension. It SHALL include an explicit mapping for legacy binary formats (for example an OLE container SHALL be treated as legacy Word rather than OOXML). When the signature is ambiguous or unknown, the declared extension SHALL win.

#### Scenario: PDF mislabeled as text is not read as plain text

- **WHEN** a PDF file is uploaded with a `.txt` extension
- **THEN** the server detects the PDF content and routes it to the binary/document parser instead of the plain-text reader

#### Scenario: Legacy Word mislabeled as OOXML is routed correctly

- **WHEN** a legacy binary Word file is uploaded with a `.docx` extension
- **THEN** the server detects the OLE container and parses it as a legacy Word document rather than failing as OOXML

#### Scenario: Ambiguous signature falls back to the extension

- **WHEN** the content signature is unknown or ambiguous
- **THEN** the server routes by the declared extension

### Requirement: Parser engine selection with fallback

The server SHALL select an extraction engine from a project default overridden by a per-upload or per-request value, and SHALL fall back to the next engine in a configured order when the selected engine fails to produce content. When a later engine succeeds, the server SHALL record the failed attempts and the final engine. When every engine fails, the server SHALL fail the parse with an error naming the engines attempted.

#### Scenario: Override selects an engine

- **WHEN** an upload specifies an engine override
- **THEN** the server uses that engine rather than the project default

#### Scenario: Fallback records attempts and final engine

- **WHEN** the selected engine fails and a later engine produces content
- **THEN** the document records the failed attempt(s) and the engine that produced the content

#### Scenario: All engines fail

- **WHEN** every engine in the chain fails to produce content
- **THEN** the server fails the parse with an error naming each engine attempted

### Requirement: Tolerate repairable file defects

For office formats, the server SHALL repair repairable packaging defects and SHALL account for merged cells so that values are not lost, rather than failing the document. Only defects that cannot be repaired SHALL fail the parse.

#### Scenario: Repairable package defect parsed

- **WHEN** an office document has a repairable packaging defect
- **THEN** the server repairs it and parses the content successfully

#### Scenario: Merged cells preserve values

- **WHEN** a spreadsheet contains merged cells
- **THEN** the parsed content reflects the merged value for the cells it covers rather than losing it

#### Scenario: Unrepairable defect fails cleanly

- **WHEN** a document has a defect that cannot be repaired
- **THEN** the server fails the parse with a clear error and does not store partial content

### Requirement: Detect text encoding

For plain-text inputs, the server SHALL detect the text encoding and decode accordingly, including BOM-prefixed and UTF-16/32 inputs, so non-UTF-8 text is parsed without mojibake. When detection is ambiguous, the server SHALL decode as UTF-8 and record that detection was ambiguous.

#### Scenario: Non-UTF-8 text decoded

- **WHEN** a plain-text document encoded in a legacy single- or multi-byte encoding is ingested
- **THEN** the server decodes it correctly and stores readable text

#### Scenario: BOM-prefixed input decoded

- **WHEN** a plain-text document begins with a byte-order mark
- **THEN** the server honors the BOM and decodes the text without including the BOM in the stored text

#### Scenario: Ambiguous encoding recorded

- **WHEN** the encoding cannot be determined confidently
- **THEN** the server decodes as UTF-8 and records that detection was ambiguous
