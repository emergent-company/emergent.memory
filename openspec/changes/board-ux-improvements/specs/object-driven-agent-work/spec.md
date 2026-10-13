## ADDED Requirements

### Requirement: Board runs are listable as sessions

The platform SHALL expose a project-scoped listing of the agent runs triggered
by the board — the runs whose subject object is set — ordered newest-first and
limited, and SHALL report each run's subject object so a caller can resolve its
work item. The listing SHALL be authorized at the same tier as the board's read
projection.

#### Scenario: Only subject-bound runs are listed

- **WHEN** the listing is requested for a project
- **THEN** it returns only that project's runs whose subject object is set, and no runs without a subject

#### Scenario: Newest first and limited

- **WHEN** more board runs exist than the requested limit
- **THEN** the newest are returned, up to the limit

#### Scenario: The subject is reported

- **WHEN** a board run is returned
- **THEN** its subject object id is present on the run so the caller can join it to the work item

#### Scenario: Authorization matches the board projection

- **WHEN** a caller without read access to the project's board requests the listing
- **THEN** the request is rejected
