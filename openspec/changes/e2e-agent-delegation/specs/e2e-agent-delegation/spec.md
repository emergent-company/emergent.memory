## Purpose

Playwright coverage of the agent-delegation loop: the delegation toggle on an
agent's Settings page that grows the definition into a delegator, a live chat
turn in which that agent invokes `spawn_agents` on a named target, and
verification that the target really ran as a child agent run linked to its
parent. The capability exists because delegation's parts — gateway toggle,
stored spawn policy, coordination tool, and sub-agent run — are only covered
individually by unit tests; this spec is the end-to-end proof that they line up,
and it confines itself to a disposable project so it never disturbs shared
bootstrap state.

## ADDED Requirements

### Requirement: Delegation is configured on the agent Settings page

The agent Settings page SHALL let the analyst enable delegation for that agent
and select one or more target agents, SHALL refuse to enable delegation with no
target selected, and SHALL persist the delegator's tool set and spawn allow-list
to the stored agent definition.

#### Scenario: Enabling delegation persists the spawn tools and allow-list

- **WHEN** delegation is enabled on an agent's Settings page with another agent selected as the target, and the form is submitted
- **THEN** the stored definition read back through the API lists `spawn_agents` and `list_available_agents` in its tools, and its spawn policy allows exactly the selected target's name

#### Scenario: Delegation without a target is rejected

- **WHEN** delegation is enabled with no target selected and the form is submitted
- **THEN** a readable error is surfaced that delegation requires at least one target, and the agent's stored definition gains neither delegation tool nor a spawn policy

#### Scenario: Disabling delegation removes the configured surface

- **WHEN** delegation is disabled on an agent that had it enabled and the form is submitted
- **THEN** both delegation tools are gone from the stored definition and no spawn policy remains

### Requirement: A delegated agent spawns its target in a live turn

An agent configured as a delegator SHALL be able to invoke the spawn tool on an
allowed target during a live chat turn, and the invocation SHALL be observable
in that turn.

#### Scenario: Forced turn produces a spawn invocation

- **WHEN** a delegator agent is given a chat instruction that requires calling the spawn tool for its allowed target before answering
- **THEN** the turn's stream contains a spawn-tool invocation naming that target, rather than only a final answer

#### Scenario: Turn errors do not masquerade as delegation failures

- **WHEN** the live turn cannot complete for provider or model-access reasons
- **THEN** the spec reports a skip with the reason instead of a delegation failure

### Requirement: A spawn is verifiable as a real child run

The spec SHALL verify a spawn by the run it produced, not only by the tool
invocation: the target agent SHALL have a run linked to the delegator's run, that
run SHALL reach a terminal completed state, and its transcript SHALL carry the
result of the delegated task.

#### Scenario: Child run is linked to the parent run

- **WHEN** the target agent's run is read by the id carried in the completed spawn tool result
- **THEN** that run belongs to the target agent's definition, carries a non-empty parent run id, and the parent run fetched by that id exists and is the run the child points at

#### Scenario: A shared orchestration root is cross-checked, not required

- **WHEN** both the child run and its parent run carry a root run id
- **THEN** the two root run ids match; when the spawn path leaves the root unset, the linkage is carried by the parent run id alone and the spec does not require a root

#### Scenario: Child run completes

- **WHEN** the linked child run is polled until it reaches a terminal state or the bounded timeout expires
- **THEN** the child run's status is completed, and on timeout the spec fails reporting the last observed status

#### Scenario: Child transcript carries the delegated result

- **WHEN** the child run's full bundle is read after completion
- **THEN** its transcript contains the result of the task the delegator passed to the target

#### Scenario: A spawn with no child run fails loudly

- **WHEN** the tool invocation is present but no run exists for the target agent
- **THEN** the spec fails, naming the missing child run as the unmet expectation

### Requirement: The spec confines and cleans up its tenant state

The spec SHALL run against a dedicated, uniquely named project and dedicated
agents, and SHALL remove everything it created so repeated runs are idempotent
and never race the parallel read surface.

#### Scenario: Dedicated project and agents per run

- **WHEN** the spec runs
- **THEN** it seeds its own project, target agent, and delegator agent rather than reusing shared bootstrap entities

#### Scenario: Cleanup happens on completion

- **WHEN** the spec finishes, whether it passed or failed
- **THEN** both agents and the seeded project are deleted and the bootstrap project is reactivated

#### Scenario: Live-model dependence is declared by skipping

- **WHEN** the environment has no scenario LLM credential or no live provider configured
- **THEN** the spec skips with a clear reason instead of failing
