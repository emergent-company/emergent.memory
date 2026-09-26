## Purpose

Provide include/exclude property projection on graph object list/query responses so callers can omit large property values (for example document bodies) and avoid paying to serialize and transfer them.

## ADDED Requirements

### Requirement: Exclude property fields on object list

`GET /api/graph/objects/search` SHALL accept an optional `exclude_fields` query parameter — a comma-separated list of property keys — and SHALL remove each named key from every returned object's `properties`. When the parameter is absent the response SHALL be unchanged.

#### Scenario: Excluded key removed

- **WHEN** a client calls `/api/graph/objects/search?exclude_fields=content`
- **THEN** each returned object's `properties` omits the `content` key
- **THEN** all other property keys are returned unchanged

#### Scenario: No parameter, unchanged response

- **WHEN** a client calls `/api/graph/objects/search` without `exclude_fields`
- **THEN** each object's `properties` is returned exactly as stored

#### Scenario: Include and exclude compose

- **WHEN** a client calls `/api/graph/objects/search?fields=title,content&exclude_fields=content`
- **THEN** each returned object's `properties` contains `title` and does not contain `content`

### Requirement: Objects browser list omits document bodies

The web UI objects browser SHALL request `exclude_fields=content` when listing objects, since the list view renders only key, type, and labels, not properties.

#### Scenario: List request excludes content

- **WHEN** the objects browser list page loads
- **THEN** the object list request includes `exclude_fields=content`
