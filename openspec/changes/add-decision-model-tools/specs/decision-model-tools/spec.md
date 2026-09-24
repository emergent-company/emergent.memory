## ADDED Requirements

### Requirement: A project-scoped decision model can be registered and discovered

Memory SHALL allow a decision model to be registered for a project through the existing external MCP server surface, using a local process transport or a remote transport, and SHALL discover that model's tools and their declared input and output schemas through the existing tool sync. Registration SHALL require administrative scope for the project, and SHALL be per project. Enabling or disabling the server or an individual tool SHALL take effect for that project's agents without requiring a code change or a server restart.

#### Scenario: Local decision model registered

- **WHEN** an administrator registers a decision model as a local-process server for a project and triggers tool discovery
- **THEN** the model's tools SHALL appear in the project's tool list with their declared input and output schemas
- **THEN** the tools SHALL be enabled at the server level but SHALL reach no agent until that agent's whitelist references them

#### Scenario: Remote decision model registered

- **WHEN** a decision model is exposed over a remote HTTP or SSE transport and registered for a project
- **THEN** the same tool discovery, schema declaration, and per-project scoping SHALL apply

#### Scenario: Registration is scoped to one project

- **WHEN** a decision model is registered for project A
- **THEN** project B SHALL NOT see or be able to call that model's tools unless it registers its own

### Requirement: The decision model speaks a typed-decision contract and never generates text

A decision tool call SHALL take a state (text or structured document) plus a set of typed questions, and SHALL return, for every question, a typed answer together with a confidence value and, where the primitive defines one, a probability distribution over that question's options. The response SHALL additionally report which model or checkpoint answered and why it was selected.

Memory SHALL send only typed questions to the decision model, and SHALL NOT rely on it for generated text, summaries, rewrites, code, or multi-step reasoning. Clauses that constrain the external model itself — for example that it does not generate text — are contract terms on that model and are not testable by Memory; the requirements here bind Memory-side behavior: what Memory sends and what Memory relies on.

#### Scenario: Typed answers with probabilities are returned

- **WHEN** a caller sends a state and a set of typed questions
- **THEN** each question SHALL be answered with its typed result, a confidence value, and, for a finite-label question, a probability distribution over the declared labels

#### Scenario: Answering model is reported

- **WHEN** any decision call completes
- **THEN** the response SHALL identify the answering model or checkpoint and the selection reason

#### Scenario: Generative use is out of scope for Memory

- **WHEN** a Memory flow needs generation, summarisation, rewriting, code, or multi-step reasoning
- **THEN** it SHALL NOT use the decision model for that, and SHALL NOT treat decision-model output as generated content

### Requirement: Decision questions use three primitives with validated shapes

The decision model SHALL accept exactly three question primitives: a finite-label choice, an ordinal score, and a boolean question returning the probability that the statement is true (the reference model's vocabulary calls these `choice`, `score`, and `noul`). Each primitive SHALL declare its instructions and its criteria. A malformed question SHALL be rejected with an error that names the offending question and states what must change; it SHALL NOT be silently reinterpreted.

#### Scenario: Choice question with labelled options

- **WHEN** a `choice` question declares a set of labelled options
- **THEN** the answer SHALL be one of the declared labels, with a probability distribution over all declared labels

#### Scenario: Boolean question returns a probability

- **WHEN** a boolean question is asked
- **THEN** the answer SHALL be a raw probability in the range 0 to 1 that the statement is true
- **THEN** that probability SHALL NOT gate a skip until calibration is recorded for the applicable question type and option count

#### Scenario: Malformed question is rejected

- **WHEN** a question omits a required element or supplies invalid criteria
- **THEN** the call SHALL fail with an error identifying that question and the required correction
- **THEN** no partial or reinterpreted answer SHALL be returned

### Requirement: High-cardinality questions are constrained

Because decision quality degrades sharply as the number of options in a single question grows, a `choice` question SHALL NOT exceed the model's supported option ceiling unless the server is explicitly configured to allocate a larger option budget. A question above the ceiling SHALL be rejected with an error naming the question and the ceiling, rather than answered unreliably.

#### Scenario: Question within the ceiling is answered

- **WHEN** a `choice` question declares at most the supported number of options
- **THEN** the question SHALL be answered normally

#### Scenario: Question above the ceiling is refused

- **WHEN** a `choice` question declares more options than the supported ceiling and the larger option budget is not configured
- **THEN** the call SHALL be rejected with an error naming the question and the ceiling

#### Scenario: Larger option sets are decomposed

- **WHEN** a caller needs to choose among more options than the ceiling allows
- **THEN** the supported approach SHALL be to narrow in stages or raise the configured option budget, and the result of an unconfigured oversized question SHALL NOT be presented as reliable

### Requirement: Language routing prevents confident answers from an incapable checkpoint

The decision model SHALL route each request to a checkpoint capable of reading the state's script and language. A state written in a non-Latin script SHALL NOT be answered by a Latin-script-only checkpoint. The selected checkpoint and the reason for its selection SHALL be reported with the answer.

#### Scenario: Non-Latin state is routed to a multilingual checkpoint

- **WHEN** a state contains predominantly non-Latin script
- **THEN** the request SHALL be answered by a checkpoint that supports that script
- **THEN** the reported selection reason SHALL indicate the script or language basis for the routing

#### Scenario: Unsupported language is not answered confidently by default

- **WHEN** a state's language is not supported by any available checkpoint
- **THEN** the result SHALL NOT be presented as reliable, and the low-support condition SHALL be observable to the caller

### Requirement: Confidence gates autonomous behavior only when calibrated

Raw confidence SHALL NOT be treated as calibrated. In this change the only autonomous action gated by a decision is skipping a chunk during extraction triage; gating agent steps or denying actions is explicitly out of scope here. Before a decision's confidence gates a skip, a calibration fitted on held-out project data SHALL have been recorded and persisted for the applicable question type and option count, against the model identity that produced the decision. Where no calibration is recorded, the decision MAY be used for ranking, display, or observation but SHALL NOT gate a skip.

#### Scenario: Uncalibrated confidence cannot gate an action

- **WHEN** no calibration is recorded for a question type and option count
- **THEN** that decision's confidence SHALL NOT be used to gate an autonomous action

#### Scenario: Calibrated confidence is recorded and reused

- **WHEN** a temperature fit on held-out project data has been applied and recorded for a question type and option count
- **THEN** subsequent decisions of that shape MAY gate autonomous behavior against a configured threshold

#### Scenario: Threshold is explicit

- **WHEN** a decision is used to gate a skip
- **THEN** the threshold applied SHALL be explicit and configurable, and the decision plus its confidence and threshold SHALL be recorded for audit

### Requirement: Decision tools are exposed to agents only by explicit opt-in

Decision tools SHALL become available to an agent only when that agent's tool whitelist references them. Default and canonical agent definitions SHALL NOT include decision tools. Existing agent behavior SHALL be unchanged for any agent that does not opt in. Where a bare tool name is ambiguous across more than one registered decision server in the same project, the whitelist entry SHALL be qualified per server so that only the intended tool is granted; an unqualified ambiguous name SHALL NOT be relied on to grant a single tool.

#### Scenario: Default agents are unaffected

- **WHEN** a project registers a decision model but changes no agent definitions
- **THEN** no agent SHALL gain decision tools and existing agent runs SHALL behave as before

#### Scenario: An agent opts in

- **WHEN** an agent's tool whitelist is updated to reference a decision tool
- **THEN** that agent MAY call the decision tool, and only that agent SHALL gain access

### Requirement: Extraction triage is optional and never supplies content

Memory SHALL support an optional pre-extraction triage step in which the decision model judges whether an extraction batch is worth sending to LLM extraction. The unit of triage SHALL be the extraction batch handed to the extraction pipeline, not the embedding chunk. Triage SHALL be disabled by default. When enabled, a batch SHALL be skipped only when the decision is calibrated and its confidence exceeds the configured threshold; otherwise the batch SHALL proceed to LLM extraction. A re-extraction request SHALL bypass triage, so a batch skipped by triage is recoverable on request. The decision model SHALL NOT produce entity or relationship content, and the LLM extraction path SHALL remain authoritative for all graph content.

#### Scenario: Triage disabled preserves existing behavior

- **WHEN** triage is not enabled for a project
- **THEN** every extraction batch eligible for extraction today SHALL still be sent to LLM extraction

#### Scenario: Only high-confidence calibrated skips are honoured

- **WHEN** triage is enabled and the decision model reports an extraction batch as not worth extracting
- **THEN** the batch SHALL be skipped only if the decision is calibrated and its confidence exceeds the configured threshold
- **THEN** otherwise the batch SHALL proceed to LLM extraction

#### Scenario: Triage never becomes the content source

- **WHEN** triage has been applied to an extraction batch
- **THEN** entities and relationships for that batch SHALL be produced only by the LLM extraction path

#### Scenario: Re-extraction bypasses triage

- **WHEN** a re-extraction is requested for a document whose batches were previously skipped by triage
- **THEN** triage SHALL be bypassed for that request so those batches reach LLM extraction

### Requirement: Decision model failure degrades safely and is observable

If the decision model is unavailable, errors, exceeds its timeout, or returns a malformed response, the affected flow SHALL fall back to existing behavior — the extraction batch SHALL proceed to LLM extraction, and the agent step SHALL proceed without the decision. A decision-model failure SHALL NOT block or fail extraction or an agent run. Decisions, failures, and fallbacks SHALL be recorded so the decision tier can be audited and its impact measured.

#### Scenario: Unavailable model does not block extraction

- **WHEN** the decision model is unreachable while triage is enabled
- **THEN** affected extraction batches SHALL proceed to LLM extraction
- **THEN** the flow SHALL complete without error

#### Scenario: Timeout degrades to existing behavior

- **WHEN** a decision call exceeds its configured timeout
- **THEN** the caller SHALL proceed as if no decision was available

#### Scenario: Failures are recorded

- **WHEN** a decision call fails or a fallback is taken
- **THEN** the failure and the fallback SHALL be recorded for later audit
