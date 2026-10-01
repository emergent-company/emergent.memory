## Purpose

Specialized review agents that consume a work queue, inspect a typed review subject (pull request, commit, document, or graph object), and record structured findings, with configurable output actions that turn findings into side effects (a human task, a notification, a GitHub review comment, or graph objects). The pilot is a security code review agent.

## ADDED Requirements

### Requirement: Define a review agent

The system SHALL allow a project to define a review agent as an agent definition bound to a queue, with a system prompt that establishes the review contract (the subject it inspects, the severity scale, and the finding shape).

#### Scenario: Security review agent defined

- **WHEN** a project installs a security-review agent definition bound to the `security-review` queue
- **THEN** the definition exists with that binding and, when triggered, its runs execute against the `security-review` queue

#### Scenario: Review agent without a queue

- **WHEN** a review agent definition declares no queue
- **THEN** its runs are routed to the `default` queue

### Requirement: Enqueue a review with a typed subject

The system SHALL enqueue a review as a queued agent run carrying a typed subject — subject type and subject reference — in the run's trigger metadata.

#### Scenario: Pull-request subject

- **WHEN** a review is enqueued for a pull request
- **THEN** the run's trigger metadata identifies the subject type as a pull request and includes the pull request reference

#### Scenario: Document subject

- **WHEN** a review is enqueued for a specification document
- **THEN** the run's trigger metadata identifies the subject type as a document and includes the document reference

### Requirement: Produce structured findings

A completed review run SHALL produce structured findings, each with a severity, a category, a location or subject reference, a title, and detail, associated with the producing run and its queue.

#### Scenario: Findings recorded

- **WHEN** a review run completes having identified issues
- **THEN** each finding is recorded with its severity, category, subject reference, title, and detail, linked to the run

#### Scenario: Clean review

- **WHEN** a review run completes having identified no issues
- **THEN** the run completes successfully with no findings and this is distinguishable from a failed run

### Requirement: Configurable output actions

A review agent definition SHALL declare an ordered list of output actions, and after a review run completes the system SHALL execute those actions against the run's findings. A failing action MUST NOT fail the review run.

#### Scenario: Create a triage task

- **WHEN** a review agent with a `create_task` action completes with findings
- **THEN** a human review task is created referencing the findings

#### Scenario: Post a review comment

- **WHEN** a review agent with a `github_comment` action completes on a pull-request subject
- **THEN** the findings are posted back to the pull request and the run is not failed if the post fails

#### Scenario: No actions configured

- **WHEN** a review agent declares no output actions
- **THEN** findings are still recorded and no side effects are attempted

### Requirement: Read findings

The system SHALL expose a project's review findings, filterable by queue, run, subject, severity, and status.

#### Scenario: Filter by status

- **WHEN** a client requests findings filtered to the open status
- **THEN** only findings whose status is open are returned

#### Scenario: Findings for a run

- **WHEN** a client requests findings for a completed review run
- **THEN** the findings produced by that run are returned, or an empty list if it produced none
