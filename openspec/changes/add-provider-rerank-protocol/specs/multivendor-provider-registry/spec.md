## ADDED Requirements

### Requirement: Rerank API is a registry dimension

The vendor registry SHALL describe reranking as an orthogonal dimension on each vendor definition, separate from the chat wire protocol. A `ProviderDefinition` SHALL carry a `RerankAPI` value naming the vendor's rerank wire shape, and the registry SHALL expose a `RerankAPIFor` lookup. An empty `RerankAPI` SHALL mean the vendor does not support reranking.

#### Scenario: Vendor declares rerank support

- **WHEN** a vendor supports reranking
- **THEN** its registry definition declares a non-empty `RerankAPI`, and `RerankAPIFor` returns it

#### Scenario: Vendor without rerank support

- **WHEN** a vendor does not support reranking
- **THEN** its `RerankAPI` is empty and `RerankAPIFor` returns the empty value

#### Scenario: Rerank shape is independent of chat protocol

- **WHEN** a vendor speaks one chat protocol and a different rerank shape (for example OpenAI-compatible chat plus TC3-signed rerank)
- **THEN** the chat `Protocol` and the `RerankAPI` are declared independently and neither constrains the other

### Requirement: Rerank adapters are selected from the registry definition

The system SHALL select a rerank adapter from the vendor's registry definition (`RerankAPI`) rather than from a hardcoded per-vendor switch, and SHALL reuse the existing credential plumbing — the bearer credential, the default rerank base URL, per-vendor extra fields, and extra headers — when constructing the adapter. A vendor whose `RerankAPI` is empty SHALL be rejected with an error.

#### Scenario: Vendor maps to its adapter

- **WHEN** a reranker is built for a vendor declaring a `RerankAPI`
- **THEN** the adapter matching that `RerankAPI` is used and the request hits that vendor's wire path

#### Scenario: Unsupported vendor rejected

- **WHEN** a reranker is requested for a vendor with an empty `RerankAPI`
- **THEN** construction fails with an actionable error

### Requirement: Rerank adapters normalize to best-first scored results

Each rerank adapter SHALL accept a query and a list of documents and return, for each input document, its relevance score keyed by the document's original index, ordered best-first and truncated to the configured top-N. Vendor-specific response shapes (nested results, positional score lists) SHALL be normalized inside the adapter.

#### Scenario: Positional scores are ordered

- **WHEN** a vendor returns one score per input document in input order
- **THEN** the adapter returns the documents ordered best-first with their original indexes preserved

#### Scenario: Top-N bounds the result

- **WHEN** the configured top-N is smaller than the number of input documents
- **THEN** the adapter returns exactly top-N results, best-first
