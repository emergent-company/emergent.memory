# org-membership Specification

## Purpose
Defines the web-console behavior for managing organizations, project members, invitations, roles, and the signed-in user's profile, all backed by Emergent Memory's org/member/invite endpoints.

## ADDED Requirements

### Requirement: List organizations

The console SHALL list the organizations the signed-in user can access, each identified by its id and name.

#### Scenario: Organizations listed

- **WHEN** a signed-in user opens the organizations view
- **THEN** the console shows every accessible organization with its name

#### Scenario: No organizations

- **WHEN** the signed-in user belongs to no organization
- **THEN** the console shows an empty state, not an error

### Requirement: Create an organization

The console SHALL allow a signed-in user to create a new organization by submitting a name on a dedicated create-organization page, reached from the organizations list.

#### Scenario: Organization created

- **WHEN** a user submits a valid organization name
- **THEN** the console creates the organization and adds it to the organizations list

#### Scenario: Duplicate or over-limit name

- **WHEN** the submitted name conflicts with an existing organization or exceeds the per-user organization limit
- **THEN** the console surfaces the conflict as a form error and does not create a duplicate organization

### Requirement: Expose the user access tree

The console SHALL present the signed-in user's organizations with their nested projects and the user's role at each level.

#### Scenario: Access tree shown

- **WHEN** a signed-in user views their organizations and projects
- **THEN** the console shows each organization's projects and the user's role in that organization and project

### Requirement: List project members

The console SHALL list the members of the active project, including each member's identity and project role, and SHALL list pending invitations for the project as members in a not-yet-responded state.

#### Scenario: Members listed

- **WHEN** a user opens the members view for a project they can access
- **THEN** the console lists every project member with their email and project role

#### Scenario: Pending invitations shown as members

- **WHEN** a user opens the members view and the project has pending invitations
- **THEN** the console lists those invitations alongside members, marked as not responded

#### Scenario: Empty member list

- **WHEN** a project has no members other than the current user
- **THEN** the console shows the current member and no error

### Requirement: Remove a project member

The console SHALL allow removing a member from the active project.

#### Scenario: Member removed

- **WHEN** an authorized user removes a project member
- **THEN** the console removes the member and refreshes the member list

#### Scenario: Last admin removal blocked

- **WHEN** a user attempts to remove the project's last administrator
- **THEN** the console rejects the removal and shows that another administrator must be assigned first

### Requirement: View member details

The console SHALL allow a user to open a member's details page from the members view and see that member's identity, contact, and role information.

#### Scenario: Member details shown

- **WHEN** a user clicks a project member
- **THEN** the console shows the member's name, email, role, and joined date

#### Scenario: Member not found

- **WHEN** a user opens a member details page for an unknown member
- **THEN** the console shows a not-found state, not an error

### Requirement: Change a project member's role

Because the backing service exposes no member-role update endpoint, the console SHALL change a member's role from the member details page by removing the member and re-inviting their email with the chosen project role. The console SHALL make that remove-and-re-invite consequence explicit before the change is submitted, and SHALL NOT report success when the change did not fully complete.

#### Scenario: Role changed

- **WHEN** a user changes a member's role to a different supported project role
- **THEN** the console removes the member and creates a new invitation for the member's own email with the new role

#### Scenario: Invalid or unchanged role rejected

- **WHEN** a user submits an unsupported project role, or the role the member already holds
- **THEN** the console rejects the change and neither removes nor re-invites the member

#### Scenario: Last admin role change blocked

- **WHEN** a user attempts to change the project's last administrator to a non-admin role
- **THEN** the console rejects the change and shows that another administrator must be assigned first

#### Scenario: Re-invite fails after removal

- **WHEN** the member is removed but the follow-up invitation fails
- **THEN** the console reports the partial failure explicitly and does not report the role change as successful

### Requirement: Invite a member by email

The console SHALL invite a person to an organization or project by email, with a role chosen at invite time.

#### Scenario: Invitation sent

- **WHEN** a user invites an email address with a selected role
- **THEN** the console records the invitation and lists it as pending

#### Scenario: Invalid invitation

- **WHEN** the invitation request lacks a valid email or role
- **THEN** the console reports the validation failure and sends no invitation

### Requirement: Invite a user by email

The console SHALL invite a person to an organization or project by email, whether or not they already have an account, issuing the invitation with a chosen role. A not-yet-registered email is invited the same way and completes account setup through the invitation link.

#### Scenario: New user invited by email

- **WHEN** a user invites a not-yet-registered email address with a role
- **THEN** the console records the invitation with the chosen role and the invitee completes sign-up via the invitation link

### Requirement: Search existing users for invitation

The console SHALL let a user search for already-registered people by email prefix so invitations target existing accounts when possible.

#### Scenario: Matching users returned

- **WHEN** a user searches by an email prefix that matches existing users
- **THEN** the console lists up to ten matching users for selection

#### Scenario: No matches

- **WHEN** a search matches no registered user
- **THEN** the console shows no matches and still permits inviting the email address directly

### Requirement: Manage sent invitations

The console SHALL list invitations sent for the active project and allow revoking one that is still pending.

#### Scenario: Sent invitations listed

- **WHEN** a user opens the invitations view for the active project
- **THEN** the console lists each sent invitation with its email, role, and status

#### Scenario: Invitation revoked

- **WHEN** a user revokes a still-pending invitation
- **THEN** the console marks the invitation revoked and it no longer appears as pending

### Requirement: Accept a pending invitation

The console SHALL allow the signed-in user to accept a pending invitation addressed to them.

#### Scenario: Invitation accepted

- **WHEN** the signed-in user accepts a pending invitation
- **THEN** the console accepts it and the user gains the invited organization or project access

#### Scenario: Invitation no longer pending

- **WHEN** the user attempts to accept an invitation that is no longer pending
- **THEN** the console reports that the invitation is not pending and leaves access unchanged

### Requirement: Decline a pending invitation

The console SHALL allow the signed-in user to decline a pending invitation addressed to them.

#### Scenario: Invitation declined

- **WHEN** the signed-in user declines a pending invitation
- **THEN** the console marks the invitation declined and it leaves the pending list

#### Scenario: Invitation not for the user

- **WHEN** the user attempts to decline an invitation addressed to someone else
- **THEN** the console rejects the action as forbidden

### Requirement: View and update the user profile

The console SHALL display the signed-in user's profile and allow updating their display name and contact fields.

#### Scenario: Profile displayed

- **WHEN** the signed-in user opens their profile
- **THEN** the console shows their name, display name, and contact details

#### Scenario: Profile updated

- **WHEN** the user submits updated profile fields
- **THEN** the console persists the changes and shows the updated profile

### Requirement: Role-scoped access

The console SHALL reflect the user's role at each level and MUST NOT permit actions beyond that role.

#### Scenario: Non-admin action rejected

- **WHEN** a user without administrative rights attempts to remove a member or revoke an invitation
- **THEN** the console rejects the action and shows an authorization error

#### Scenario: Role strings preserved

- **WHEN** the console sends or reads a role
- **THEN** it uses only the supported role values (`org_admin`, `org_member`, `project_admin`, `project_user`)
