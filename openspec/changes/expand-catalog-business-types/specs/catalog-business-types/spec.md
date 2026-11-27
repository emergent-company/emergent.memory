## ADDED Requirements

### Requirement: Role type
The catalog SHALL define a `Role` object type derived from schema.org/Role, with parent `Thing`, so a role or position held by a person aligns to a canonical type instead of an invented one.

#### Scenario: Alias alignment
- **WHEN** discovery proposes an entity that is a role, position, or title
- **THEN** it SHALL align to the `Role` type via labels `role`, `position`, and `title`

#### Scenario: Properties
- **WHEN** the `Role` type is projected into a schema pack
- **THEN** it SHALL expose `name`, `description`, `start_date`, and `end_date` properties with `name` required

### Requirement: Device type
The catalog SHALL define a `Device` object type derived from schema.org/Device, with parent `Product`, for hardware and electronic equipment.

#### Scenario: Alias alignment
- **WHEN** discovery proposes a device or machine
- **THEN** it SHALL align to the `Device` type via labels `device` and `machine`

#### Scenario: Properties
- **WHEN** the `Device` type is projected into a schema pack
- **THEN** it SHALL expose `name`, `description`, `manufacturer`, `model`, and `url` properties with `name` required

### Requirement: DecisionRecord type
The catalog SHALL define a `DecisionRecord` object type, with parent `CreativeWork`, for a recorded decision and its rationale.

#### Scenario: Alias alignment
- **WHEN** discovery proposes a decision, decision record, adr, or decision log
- **THEN** it SHALL align to the `DecisionRecord` type via labels `decision`, `decision record`, `adr`, and `decision log`

#### Scenario: Properties
- **WHEN** the `DecisionRecord` type is projected into a schema pack
- **THEN** it SHALL expose `name`, `description`, `decided_on`, `status`, and `rationale` properties with `name` required

### Requirement: Equipment type
The catalog SHALL define an `Equipment` object type, with parent `Device`, for a physical piece of machinery or apparatus.

#### Scenario: Alias alignment
- **WHEN** discovery proposes equipment, a machine, or hardware
- **THEN** it SHALL align to the `Equipment` type via labels `equipment`, `machine`, and `hardware`

#### Scenario: Properties
- **WHEN** the `Equipment` type is projected into a schema pack
- **THEN** it SHALL expose `name`, `description`, `manufacturer`, `model`, and `serial_number` properties with `name` required

### Requirement: StorageSystem type
The catalog SHALL define a `StorageSystem` object type, with parent `Device`, for a system that stores energy.

#### Scenario: Alias alignment
- **WHEN** discovery proposes a storage system, energy storage, battery system, or energy storage technology
- **THEN** it SHALL align to the `StorageSystem` type via labels `storage system`, `storage`, `energy storage`, `battery system`, and `energy storage technology`

#### Scenario: Properties
- **WHEN** the `StorageSystem` type is projected into a schema pack
- **THEN** it SHALL expose `name`, `description`, `capacity`, and `technology` properties with `name` required

### Requirement: Alias-aware discovery matching
A synonymous discovered type SHALL count toward its canonical catalog type during discovery evaluation.

#### Scenario: Synonym counts as a hit
- **WHEN** a discovered type normalizes to an alias of a golden type (for example `Decision` toward `DecisionRecord`, or `EnergyStorageTechnology` toward `StorageSystem`)
- **THEN** it SHALL be scored as a true positive against that golden type, with precision `tp/(tp+fp)` and recall `tp/len(golden)` unchanged
