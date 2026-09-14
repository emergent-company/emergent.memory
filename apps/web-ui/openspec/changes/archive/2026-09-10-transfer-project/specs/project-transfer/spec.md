## Purpose

Lets an organization admin reparent one of their projects to another organization they belong to, so a project's history and identity move with it instead of being recreated.

## ADDED Requirements

### Requirement: Offer a transfer action for each project in the organization view

The organization view SHALL expose a Transfer action on every project row the acting user is allowed to transfer, alongside the existing Open and Delete actions. When the acting user has no organization available to transfer a project into, the action SHALL NOT be shown.

#### Scenario: Project can be moved to another organization

- **WHEN** the organization view lists projects and the acting user belongs to at least one other organization
- **THEN** each listed project's action menu includes a Transfer action

#### Scenario: No destination organization available

- **WHEN** the organization view lists projects and the acting user belongs to no organization other than the current one
- **THEN** project action menus do not include a Transfer action

### Requirement: Limit who can transfer a project

A project SHALL be transferable only by a user with org_admin role in the project's current organization who also belongs to the destination organization. The UI SHALL hide the Transfer action for users without org_admin role in the current organization, and the backend SHALL remain authoritative: any rejected attempt is reported back as an error.

#### Scenario: Non-admin does not see the action

- **WHEN** a user without org_admin role in the current organization views its project list
- **THEN** no Transfer action is shown for the listed projects

#### Scenario: Backend rejects an unauthorized attempt

- **WHEN** a transfer request is submitted for a user the backend does not authorize
- **THEN** the transfer does not happen and the user is shown an error explaining the transfer was rejected

### Requirement: Choose a destination organization

The transfer dialog SHALL present the organizations the acting user belongs to, excluding the project's current organization, and SHALL require selecting exactly one before the transfer can be submitted. Canceling the dialog SHALL NOT change anything.

#### Scenario: Destination options exclude the source organization

- **WHEN** the transfer dialog opens for a project in organization A
- **THEN** organization A is not listed among the selectable destinations, and only organizations the acting user belongs to are listed

#### Scenario: Cancel without transferring

- **WHEN** the user opens the transfer dialog and cancels
- **THEN** the project remains in its current organization and no request is sent

### Requirement: Transfer moves the project to the destination organization

Submitting a transfer SHALL reparent the project to the selected destination organization in a single operation that does not delete or recreate the project. On success the user SHALL be returned to the source organization view with a confirmation naming the project and its new organization, and the project SHALL appear under the destination organization thereafter.

#### Scenario: Successful transfer

- **WHEN** an authorized user submits a transfer of project P from organization A to organization B
- **THEN** project P's organization becomes B, and the user is returned to organization A's view with a confirmation that P was moved to B

#### Scenario: Project appears in the destination organization

- **WHEN** the user opens organization B's view after a successful transfer of project P
- **THEN** project P is listed there

#### Scenario: Source organization view no longer lists the project

- **WHEN** a user views organization A after project P was transferred out of it
- **THEN** project P is no longer listed in A's project list

### Requirement: Surface transfer failures without side effects

A failed transfer SHALL leave the project in its current organization and SHALL be reported to the user as an error. Transfers to the project's current organization SHALL be rejected without contacting the backend.

#### Scenario: Transfer to the current organization is rejected

- **WHEN** a transfer request names the project's current organization as the destination
- **THEN** the transfer is rejected locally and the user sees an error, with no backend request sent

#### Scenario: Backend rejects the transfer

- **WHEN** the backend refuses a transfer request (for example, project not found, insufficient permission, or an invalid destination)
- **THEN** the project stays in its current organization and the user is shown an error describing the failure
