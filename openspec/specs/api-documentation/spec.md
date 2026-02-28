# api-documentation Specification

## Purpose
TBD - created by archiving change adopt-huma-openapi-generation. Update Purpose after archive.
## Requirements
### Requirement: OpenAPI Specification Generation

The Go server SHALL generate an OpenAPI 3.1 specification from code annotations.

#### Scenario: OpenAPI spec served at standard endpoint

- **WHEN** a client requests `GET /openapi.json`
- **THEN** the server returns a valid OpenAPI 3.1 JSON document
- **AND** the document includes all registered API endpoints

#### Scenario: OpenAPI spec includes authentication

- **WHEN** the OpenAPI spec is generated
- **THEN** it includes a Bearer JWT security scheme
- **AND** protected endpoints reference this security scheme

### Requirement: Interactive API Documentation

The Go server SHALL serve interactive API documentation.

#### Scenario: Swagger UI available

- **WHEN** a client requests `GET /docs`
- **THEN** the server returns an interactive Swagger UI page
- **AND** the UI loads the OpenAPI spec from `/openapi.json`

### Requirement: Declarative Request Validation

API endpoints SHALL use declarative struct tags for request validation.

#### Scenario: Query parameter validation

- **WHEN** a request includes a query parameter outside defined bounds
- **THEN** the server returns a 400 Bad Request
- **AND** the error message indicates which parameter failed validation

#### Scenario: Required field validation

- **WHEN** a request body is missing a required field
- **THEN** the server returns a 422 Unprocessable Entity
- **AND** the error message indicates which field is missing

### Requirement: Typed Handler Signatures

API handlers SHALL use typed request and response structs.

#### Scenario: Request struct binding

- **WHEN** a request is received for a Huma-registered endpoint
- **THEN** path, query, header, and body parameters are bound to the request struct
- **AND** the handler receives a fully populated, validated request object

#### Scenario: Response struct serialization

- **WHEN** a handler returns a response struct
- **THEN** the Body field is serialized as JSON
- **AND** header fields are set as response headers

### Requirement: Functional Equivalence with NestJS Spec

The Go-generated OpenAPI spec SHALL be functionally equivalent to the NestJS-generated spec.

#### Scenario: Endpoint coverage

- **WHEN** comparing Go and NestJS OpenAPI specs
- **THEN** all endpoints present in NestJS spec are present in Go spec
- **AND** HTTP methods match for each endpoint

#### Scenario: Parameter compatibility

- **WHEN** comparing endpoint parameters between specs
- **THEN** parameter names, locations (path/query/header), and types match
- **AND** required/optional status matches

