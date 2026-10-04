## MODIFIED Requirements

### Requirement: Server supports xberg OCR backends

The server SHALL accept extraction configuration: an OCR backend from `tesseract`, `paddleocr`, `sceptre`, and `vlm`; one or more OCR languages; a force-OCR flag; and an extraction engine selected from those the extractor exposes. Settings SHALL resolve from project-level defaults, overridden by per-request or per-upload values, and SHALL default to `tesseract`, language `eng`, force-OCR disabled, and the extractor's default engine when unset. The server SHALL expose the available OCR backends and engines. The removed `easyocr` backend SHALL NOT be offered.

#### Scenario: Configured backend is sent to the extractor

- **WHEN** extraction runs for a document whose resolved configuration specifies backend `paddleocr` and language `eng+deu`
- **THEN** the server sends those settings to the extractor instead of the default `tesseract`/`eng`

#### Scenario: Defaults preserve current behavior

- **WHEN** neither project nor request supplies extraction settings
- **THEN** the server sends backend `tesseract`, language `eng`, force-OCR disabled, and the extractor's default engine

#### Scenario: Invalid backend or engine is rejected

- **WHEN** an OCR backend or engine outside the supported set is requested
- **THEN** the server rejects the configuration with a validation error and does not call the extractor

#### Scenario: EasyOCR is not an accepted backend

- **WHEN** OCR backend options are enumerated
- **THEN** `easyocr` is absent and the supported set is `tesseract`, `paddleocr`, `sceptre`, `vlm`

## ADDED Requirements

### Requirement: Extraction reports the engine used

The extraction contract SHALL accept a selected engine and SHALL report which engine produced the result. The server SHALL record the engine that produced a document's content.

#### Scenario: Engine recorded

- **WHEN** a document is parsed
- **THEN** the document records the engine that produced its content

#### Scenario: Engine selection is passed to the extractor

- **WHEN** an extraction engine is selected for a document
- **THEN** the server sends that engine selection to the extractor
