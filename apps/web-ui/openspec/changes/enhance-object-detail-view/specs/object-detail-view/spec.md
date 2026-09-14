## Purpose

Lets users actively manage a single memory object from its detail view: edit its fields, connect it to other objects, and start an agent conversation about it.

## ADDED Requirements

### Requirement: Editable object properties

The object detail SHALL present the object's editable fields in a single-column layout, with each field's label on its own row and the content beneath it. Users SHALL be able to edit the object's common fields (key, status, labels) and its schema-defined properties, and save the changes back to the object.

#### Scenario: Single-column layout
- **WHEN** the user opens an object with multiple properties
- **THEN** each property is rendered as a label row with its content beneath it
- **AND** all properties stack in a single column rather than a multi-column grid

#### Scenario: Edit a property
- **WHEN** the user edits a property value and saves
- **THEN** the value is persisted to the object
- **AND** the detail view reflects the updated value

#### Scenario: Edit common fields
- **WHEN** the user edits the object's key, status, or labels and saves
- **THEN** the updated key, status, or labels are persisted to the object

#### Scenario: Complex property values
- **WHEN** a property holds a non-scalar value (array or object)
- **THEN** it is shown and edited as JSON text and parsed back to its structured form on save

#### Scenario: Internal properties hidden
- **WHEN** the object has underscore-prefixed internal properties
- **THEN** those properties are not shown or editable in the detail view

### Requirement: Relationship creation from the detail view

When an object has no relationships, the detail view SHALL show a call-to-action to connect the object to another object. The user SHALL be able to choose a relationship type and select source and target objects via search, with the current object pre-selected as the source by default and freely changeable.

#### Scenario: Call-to-action on empty relationships
- **WHEN** the user opens an object with no relationships
- **THEN** the relationships section shows a call-to-action to connect the object rather than only a passive empty-state message

#### Scenario: Choose relationship type
- **WHEN** the user initiates a connection
- **THEN** they can select a relationship type
- **AND** the available types are constrained to those valid for the object

#### Scenario: Select source and target
- **WHEN** the user initiates a connection
- **THEN** the current object is pre-selected as the source
- **AND** the user can search for and change both the source and target objects

#### Scenario: Create the relationship
- **WHEN** the user confirms a valid type, source, and target
- **THEN** a relationship of that type is created between the chosen objects
- **AND** it appears in the object's relationships

### Requirement: Object-linked chat

The object detail SHALL provide an action to start a chat about the object. Initiating SHALL create or resume a refinement conversation linked one-to-one to the object, and SHALL seed the conversation with a pre-prepared prompt containing the object's data. The acting agent SHALL be configurable through a project setting.

#### Scenario: Start a chat
- **WHEN** the user activates the chat-about-object action
- **THEN** a conversation about the object is started
- **AND** the first message is a pre-prepared prompt seeded with the object's data

#### Scenario: Resume the object's conversation
- **WHEN** the user activates the chat-about-object action for an object that already has a linked conversation
- **THEN** the existing conversation is resumed rather than a duplicate created

#### Scenario: Configurable editor agent
- **WHEN** the project has a configured editor agent
- **THEN** the chat-about-object conversation uses that agent
- **AND** the editor agent can be changed through the project settings

#### Scenario: No editor agent configured
- **WHEN** no editor agent is configured
- **THEN** a deterministic fallback agent is used, or the action is surfaced with a clear configuration hint
