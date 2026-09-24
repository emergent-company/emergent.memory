# Tasks

Spec-only change. No implementation is performed in this change as submitted; the tasks below are the implementation plan to be executed on the same branch before the change is archived.

## 1. Deployment prerequisite (operator-owned; not part of the code change)

- [ ] 1.1 Pin an exact decision-model package version and record it in the change
- [ ] 1.2 Provision the model runtime + a checkpoint on the server host (local-process), or a reachable service (remote transport)
- [ ] 1.3 Smoke-test the model standalone: status, routing, and one typed prediction, capturing raw output as evidence
- [ ] 1.4 Register the model for a project through the external MCP server API and sync tools; confirm the tools appear enabled at the server level and reach no agent until a whitelist references them
- [ ] 1.5 Record the registration payload and the resulting tool names in the change

## 2. Decision-model client (server)

- [ ] 2.1 Define a decision-model client interface: typed questions in, typed answers + confidence + probabilities + answering model out
- [ ] 2.2 Implement the interface against the registered model's tool surface, with a configurable timeout
- [ ] 2.3 Unit-test: missing required elements or invalid criteria produce an error naming the offending question and never a partial answer
- [ ] 2.4 Unit-test: an oversized `choice` question is refused with an error naming the question and the ceiling unless the larger option budget is configured
- [ ] 2.5 Unit-test: a non-Latin-script state is routed to a script-capable checkpoint, and the reported selection reason names the basis
- [ ] 2.6 Unit-test: an unsupported language is surfaced as low-support rather than returned as a confident result
- [ ] 2.7 Unit-test: an unavailable model, a timeout, and a malformed response each degrade to "no decision available" without propagating an error

## 3. Calibration record (server)

- [ ] 3.1 Persist calibration records with a minimal migration: model identity, question type, option count, fitted temperature, sample size, fit date
- [ ] 3.2 Implement lookup of an applicable calibration record for a decision shape
- [ ] 3.3 Unit-test: with no recorded calibration, a decision cannot gate a skip
- [ ] 3.4 Unit-test: with a recorded calibration, a decision may gate a skip against a configured threshold, and the decision + confidence + threshold are recorded
- [ ] 3.5 Unit-test: a changed model identity invalidates the prior calibration record

## 4. Extraction triage (server)

- [ ] 4.1 Add per-project triage configuration (enablement, threshold, decision timeout) with an explicit default of disabled
- [ ] 4.2 Identify the triage unit as the extraction batch handed to the extraction pipeline, not the embedding chunk
- [ ] 4.3 Unit-test: with triage disabled, every batch eligible for extraction today still reaches LLM extraction
- [ ] 4.4 Unit-test: with triage enabled, a batch is skipped only when the decision is calibrated AND its confidence exceeds the threshold
- [ ] 4.5 Unit-test: with triage enabled and the decision below threshold, or uncalibrated, the batch reaches LLM extraction
- [ ] 4.6 Unit-test: triage never emits entity or relationship content
- [ ] 4.7 Unit-test: a re-extraction request bypasses triage so a previously skipped batch reaches LLM extraction
- [ ] 4.8 Unit-test: model failure during triage does not fail the extraction flow; affected batches reach LLM extraction

## 5. Agent exposure (server)

- [ ] 5.1 Unit-test: a project with a registered decision model and unchanged agent definitions exposes no decision tools to any agent
- [ ] 5.2 Unit-test: whitelisting a decision tool grants it to that agent only
- [ ] 5.3 Unit-test: agent routing decisions (tool selection) succeed when the model is available and fall back to existing selection when it is not

## 6. Observability and audit

- [ ] 6.1 Persist decision audit rows with a minimal migration: model identity, question shape, answer, confidence, threshold (if any), latency
- [ ] 6.2 Persist every failure and every fallback with the reason (unavailable, timeout, malformed)
- [ ] 6.3 Unit-test: a successful decision, a failure, and a fallback are each recorded once and are attributable to the calling flow

## 7. Documentation

- [ ] 7.1 Add an operator runbook: deployment, registration payload, tool sync, calibration procedure, threshold configuration, rollback
- [ ] 7.2 Document the option ceiling, the three primitives, and the calibration precondition in the runbook
- [ ] 7.3 Cross-link the runbook from the research note in `docs/research/system-one-models/`

## 8. Verification

- [ ] 8.1 `go build ./...` from `apps/server`
- [ ] 8.2 `go test ./domain/...` for the touched domains (pass)
- [ ] 8.3 `task lint` (server)
- [ ] 8.4 `openspec validate add-decision-model-tools --strict`
- [ ] 8.5 End-to-end check against a running model: triage enabled, one batch skipped, one batch extracted, both visible in audit records
- [ ] 8.6 Confirm no behavioral change for a project with triage disabled and no agent opted in
