## MODIFIED Requirements

### Requirement: Serve Apple Notes and Reminders tools on macOS

On macOS the connector SHALL register local MCP tools for Apple Notes and Apple Reminders, implemented via the system's AppleScript automation and, where available, an embedded native EventKit helper (no third-party dependencies). For Reminders the tool set SHALL cover listing reminders, enumerating reminder lists, adding a reminder, updating a reminder, and deleting a reminder, with names, descriptions, and input schemas for each.

#### Scenario: Apple tools registered on macOS

- **WHEN** the connector starts on macOS
- **THEN** its registered tool list includes Notes tools and the full Reminders tool set (`reminders_list`, `reminders_lists`, `reminders_add`, `reminders_update`, `reminders_delete`) with names, descriptions, and input schemas

#### Scenario: Apple tools unavailable on other platforms

- **WHEN** the connector starts on a non-macOS platform
- **THEN** it runs without the Apple tools and its status output explains the platform limitation

## ADDED Requirements

### Requirement: Reminders expose identity and list context

The connector's reminder listing SHALL return, for each reminder, an identifier and the name of the list it belongs to, in addition to the reminder's name and optional due date, and SHALL report completion state. The identifier SHALL be usable to address that reminder in subsequent update and delete calls. Reminder note text SHALL be returned only when explicitly requested, so that listing a large library stays compact.

#### Scenario: Listing returns identity and list

- **WHEN** an agent lists reminders
- **THEN** each returned reminder includes an identifier, its list name, whether it is completed, and its name and due date when present

#### Scenario: Notes returned on request

- **WHEN** an agent lists reminders requesting notes
- **THEN** each returned reminder includes its note text when it has one

#### Scenario: Lists can be enumerated

- **WHEN** an agent requests the reminder lists
- **THEN** the connector returns each list's name and identifier, so a list can be named unambiguously

### Requirement: Reminders can be modified by identifier

The connector SHALL let an agent modify an existing reminder addressed by its identifier: change its title, set or clear its due date, set or clear its notes, change its priority, mark it complete or incomplete, and move it to another list. It SHALL also let an agent delete a reminder by identifier. An identifier not resolvable to a reminder SHALL fail with an error that names the identifier, and a move or add targeting a list that does not exist or does not accept reminders SHALL fail with an error identifying the list.

#### Scenario: Update fields

- **WHEN** an agent updates a reminder's title, due date, notes, or priority by identifier
- **THEN** the reminder is changed and the connector reports the updated reminder

#### Scenario: Clear a field

- **WHEN** an agent clears a reminder's due date or notes
- **THEN** the field is removed from the reminder

#### Scenario: Complete or reopen

- **WHEN** an agent marks a reminder complete or incomplete
- **THEN** its completion state changes accordingly

#### Scenario: Move between lists

- **WHEN** an agent moves a reminder to another existing list by identifier
- **THEN** the reminder appears in the target list and no longer in the original list

#### Scenario: Delete

- **WHEN** an agent deletes a reminder by identifier
- **THEN** the reminder no longer appears in any listing

#### Scenario: Unknown identifier

- **WHEN** an agent addresses a reminder with an identifier that resolves to no reminder
- **THEN** the connector returns an error naming that identifier without modifying anything

#### Scenario: Invalid target list

- **WHEN** an agent moves or adds a reminder to a list that does not exist or does not accept reminders
- **THEN** the connector returns an error identifying the list without modifying anything

### Requirement: Reminders can be added to a chosen list

The connector SHALL let an agent add a reminder to a named list, defaulting to the default reminder list when no list is given.

#### Scenario: Add to a named list

- **WHEN** an agent adds a reminder specifying a list name
- **THEN** the reminder is created in that list

#### Scenario: Add without a list

- **WHEN** an agent adds a reminder without specifying a list
- **THEN** the reminder is created in the default reminder list
