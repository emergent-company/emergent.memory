## ADDED Requirements

### Requirement: Shared generic response types in pkg/httputil
The system SHALL provide a single `pkg/httputil` package containing `APIResponse[T any]`, `PaginatedResponse[T any]`, and `SuccessResponse[T any]` generic types. All domains MUST import these types from `pkg/httputil` and MUST NOT define their own local copies.

#### Scenario: Domain uses shared APIResponse type
- **WHEN** a domain handler returns a paginated or success response
- **THEN** the response struct is sourced from `pkg/httputil`, not from a local domain DTO

#### Scenario: No duplicate type definitions exist
- **WHEN** the codebase is compiled
- **THEN** `APIResponse[T]`, `PaginatedResponse[T]`, and `SuccessResponse[T]` each appear as a type definition in exactly one location: `pkg/httputil`

### Requirement: SuccessResponse constructor
The `pkg/httputil` package SHALL provide a `NewSuccessResponse[T any](data T) APIResponse[T]` constructor function that domains use to build consistent success payloads.

#### Scenario: Handler builds success response
- **WHEN** a handler calls `httputil.NewSuccessResponse(data)`
- **THEN** it returns an `APIResponse[T]` with `Data` set and no error fields populated

### Requirement: PaginatedResponse includes cursor metadata
The `PaginatedResponse[T any]` type SHALL include `Items []T`, `Total int`, `Limit int`, `Offset int`, and optionally `NextCursor string` fields.

#### Scenario: Paginated list endpoint returns consistent shape
- **WHEN** any list endpoint returns multiple items
- **THEN** the response body matches the `PaginatedResponse[T]` schema with all required metadata fields present
