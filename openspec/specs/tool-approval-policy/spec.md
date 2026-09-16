## Purpose

Defines human-in-the-loop gating of agent tool executions: per-tool approval policy, in-conversation approval prompts, structured rejection results, and a durable audit trail of every decision.

## Requirements

### Requirement: Per-tool approval policy

Each tool an agent may call SHALL carry one of three policies: `allow` (run without asking), `deny` (unavailable), or `ask` (intercept and require approval). An agent SHALL have a default policy that applies to any tool without an explicit entry.

#### Scenario: Default policy applies to unlisted tools
- **WHEN** an agent calls a tool that has no explicit policy entry
- **THEN** the agent's default policy governs that call

#### Scenario: Explicit policy overrides default
- **WHEN** an agent calls a tool that has an explicit policy entry
- **THEN** the explicit entry governs the call, regardless of the default

### Requirement: Deny makes a tool unavailable

A tool under `deny` policy SHALL fail immediately before any side effect, and the agent SHALL receive that the tool is unavailable.

#### Scenario: Denied tool fails before execution
- **WHEN** an agent calls a tool under `deny` policy
- **THEN** the tool does not execute and the agent receives a result indicating the tool is unavailable

### Requirement: Ask policy pauses the run

A tool under `ask` policy SHALL not execute until a human approves it. The run SHALL pause while the approval is pending.

#### Scenario: Intercepted call waits for a decision
- **WHEN** an agent calls a tool under `ask` policy
- **THEN** the tool does not execute immediately and the run remains paused until the human decides

### Requirement: Approval prompt reaches the conversation

An approval request SHALL surface in the conversation UI (chat or side panel) as an approval card showing the tool and a summary of its arguments, not only as an out-of-band notification.

#### Scenario: Approval card appears in conversation
- **WHEN** a tool under `ask` policy is intercepted
- **THEN** the conversation shows an approval card identifying the tool and its argument summary

### Requirement: Approve executes the tool

Approving an approval request SHALL execute the tool and return its real result to the agent.

#### Scenario: Approved tool runs and returns result
- **WHEN** the human approves an approval request
- **THEN** the tool executes and its actual result is delivered to the agent as the tool result

### Requirement: Reject returns a structured result

Rejecting an approval request SHALL return a structured result indicating rejection to the agent, optionally carrying a human-written message. Rejection MUST NOT be delivered as a tool error.

#### Scenario: Reject with a message
- **WHEN** the human rejects an approval request with a message
- **THEN** the agent receives a structured result marking the call rejected and containing the message

#### Scenario: Reject without a message
- **WHEN** the human rejects an approval request without a message
- **THEN** the agent receives a structured result marking the call rejected with no message

### Requirement: Approval waits indefinitely

A pending approval SHALL have no automatic timeout. It SHALL remain pending until the human approves, rejects, or revokes it. An unanswered approval MUST NOT be treated as a rejection.

#### Scenario: Unanswered approval stays pending
- **WHEN** an approval request is not answered for an extended period
- **THEN** the run remains paused and the approval remains pending

### Requirement: Multiple concurrent approvals

When one turn requests multiple tools that each require approval, every request SHALL be surfaced and decided independently. The turn SHALL resume only after every approval in the batch reaches a final decision.

#### Scenario: Independent decisions in a batch
- **WHEN** a turn intercepts multiple tools under `ask` policy
- **THEN** each approval request can be approved or rejected independently, and the turn resumes once all are decided

### Requirement: Revoke a pending approval

The human SHALL be able to revoke a pending approval before deciding it. Revocation SHALL resolve the pause without executing the tool.

#### Scenario: Revoked approval resolves the pause
- **WHEN** the human revokes a pending approval
- **THEN** the tool does not execute and the run resumes with the call treated as not taken

### Requirement: Audit trail of decisions

Every approval decision SHALL be recorded with the agent, tool, an argument summary, the decision (approved, rejected, revoked), any message, the acting user, and a timestamp. The audit trail SHALL be viewable and SHALL redact sensitive argument values.

#### Scenario: Decision is recorded and viewable
- **WHEN** a human decides or revokes an approval request
- **THEN** the decision is recorded and appears in the audit view with the tool, decision, message, user, and timestamp

#### Scenario: Sensitive arguments are redacted
- **WHEN** an approval involves arguments containing secrets
- **THEN** the audit record stores a redacted summary rather than the raw argument values

### Requirement: Audit record links to its session

An audit record SHALL link to the chat conversation (session) that produced it, so a viewer can jump from an approval to its originating conversation. Records whose run has no chat conversation SHALL omit the link.

#### Scenario: Approval links to its conversation
- **WHEN** an approval's run belongs to a chat conversation
- **THEN** the audit view shows a link to that conversation

#### Scenario: Non-chat approval omits the link
- **WHEN** an approval's run has no chat conversation
- **THEN** the audit record shows no session link
