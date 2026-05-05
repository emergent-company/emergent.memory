## ADDED Requirements

### Requirement: RequireProject middleware in pkg/auth
The `pkg/auth` package SHALL provide a `RequireProject() echo.MiddlewareFunc` that:
1. Verifies a non-nil authenticated user exists on the context
2. Verifies the user's `ProjectID` field is non-empty
3. Returns `apperror.ErrUnauthorized` if the user is nil
4. Returns `apperror.NewBadRequest("x-project-id header required")` if ProjectID is empty

#### Scenario: Unauthenticated request reaches protected route
- **WHEN** a request arrives at a route protected by `RequireProject()` with no authenticated user on the context
- **THEN** the middleware returns HTTP 401 and the handler is never called

#### Scenario: Authenticated request missing project ID
- **WHEN** a request arrives with a valid user but no `x-project-id` header
- **THEN** the middleware returns HTTP 400 with message "x-project-id header required" and the handler is never called

#### Scenario: Fully authenticated request with project passes through
- **WHEN** a request arrives with a valid user and a non-empty `ProjectID`
- **THEN** the middleware calls `next(c)` and the handler executes normally

### Requirement: MustGetUser helper
The `pkg/auth` package SHALL provide `MustGetUser(c echo.Context) *auth.User` that returns the authenticated user from context. If called on a context guaranteed by `RequireProject()` middleware, it SHALL never return nil.

#### Scenario: Handler retrieves user without nil check
- **WHEN** a handler behind `RequireProject()` calls `auth.MustGetUser(c)`
- **THEN** it receives a non-nil `*auth.User` without any additional nil guard

### Requirement: GetProjectUUID helper
The `pkg/auth` package SHALL provide `GetProjectUUID(c echo.Context) (uuid.UUID, error)` that extracts and UUID-parses the project ID from the authenticated user on the context, replacing all local `getProjectID()` helper copies in domain handlers.

#### Scenario: Handler gets project UUID without local parsing
- **WHEN** a handler calls `auth.GetProjectUUID(c)`
- **THEN** it receives a parsed `uuid.UUID` and any parse error, with no local parsing logic required

#### Scenario: Local getProjectID copies removed
- **WHEN** the codebase is compiled after migration
- **THEN** no local `getProjectID` function exists in `domain/chunks/handler.go`, `domain/journal/handler.go`, or `domain/graph/handler.go`

### Requirement: Inline auth boilerplate eliminated from handlers
All domain handlers that previously contained the inline pattern:
```go
user := auth.GetUser(c)
if user == nil { return apperror.ErrUnauthorized }
```
SHALL have that block removed. The equivalent protection SHALL be provided by `RequireProject()` middleware applied at route group registration time.

#### Scenario: Handler body contains no inline auth guard
- **WHEN** a handler method is protected by the RequireProject middleware
- **THEN** the handler body does not contain a `user == nil` check for authorization purposes
