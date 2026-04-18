# code-memory-blueprint

An [Emergent](https://emergent.company) blueprint that installs the **code-structure** schema pack into a Memory knowledge graph project. It maps the structural and architectural layer of any software codebase — what the code *is*, not the workflow of building it. Works for any language or framework (Go, TypeScript, Python, Swift, Rust, etc.).

---

## What it installs

### Schema pack: `code-structure` (v1.7.1)

**24 object types across 6 layers:**

| Layer | Types |
|-------|-------|
| Project structure | `App`, `Module`, `SourceFile` |
| UI surfaces | `Context`, `UIComponent`, `Action` |
| Behavior | `Actor`, `Domain`, `Scenario`, `ScenarioStep` |
| Backend | `Service`, `Method`, `DataModel`, `Database`, `SQLQuery` |
| API & async | `APIEndpoint`, `APIContract`, `Event`, `Middleware`, `Job` |
| Supporting | `ExternalDependency`, `ConfigVar`, `Pattern`, `TestSuite` |

**36 relationship types** across all layers — see [Relationship reference](#relationship-reference) below.

No agents. No seed data. You populate the graph yourself, or let an agent do it by reading your codebase.

---

## Object type reference

### App
A deployable application unit — frontend, backend, mobile, desktop, CLI, or library. Top-level container in the project hierarchy.

| Property | Type | Description |
|---|---|---|
| `app_type` | string | `frontend`, `backend`, `mobile`, `desktop`, `cli`, `library` |
| `platform` | string[] | Target platforms. E.g. `[web]`, `[ios, android]` |
| `root_path` | string | Root directory relative to repo. E.g. `apps/web` |
| `tech_stack` | string[] | Key technologies. E.g. `[react, typescript, vite]` |
| `deployment_target` | string | Where it deploys. E.g. `vercel`, `kubernetes` |
| `port` | number | Default local development port |

### Module
A sub-package or module within an app — Go package, npm package, Python module, feature folder, or any named unit of code organization below the app level.

| Property | Type | Description |
|---|---|---|
| `path` | string | Path relative to app root. E.g. `internal/auth` |
| `purpose` | string | What this module does |

### SourceFile
A tracked source file within a module. All code-level definitions link back to their source file via `defines` relationships rather than storing a string path.

| Property | Type | Description |
|---|---|---|
| `path` | string | File path relative to repo root |
| `language` | string | Programming language. E.g. `go`, `typescript` |

### Context
A screen, modal, panel, or interaction surface — a React page, Next.js route, SwiftUI View, modal dialog, drawer, or any named surface where a user interacts.

| Property | Type | Description |
|---|---|---|
| `context_type` | string | Medium: `web-view`, `mobile-screen`, `desktop-window`, `cli`, `notification`, `email`, `watch-face` |
| `type` | string | Surface kind: `screen`, `modal`, `panel`, `drawer`, `bottom-sheet`, `toast` |
| `scope` | string | `internal`, `external` |
| `route` | string | URL route or navigation path. E.g. `/dashboard` |
| `platform` | string[] | Target platforms |

### UIComponent
A reusable UI component — React component, SwiftUI View, widget, form field, layout primitive, or any named self-contained piece of UI.

| Property | Type | Description |
|---|---|---|
| `type` | string | `primitive`, `composite`, `layout`, `container` |

### Action
A user action or system operation — navigation, data mutation, toggle, form submission, external call. Available within Contexts; performed during Scenario steps.

| Property | Type | Description |
|---|---|---|
| `type` | string | `navigation`, `mutation`, `trigger`, `toggle`, `external` |
| `display_label` | string | Human-readable label. E.g. `Save Changes` |

### Actor
A user role, persona, or system actor that executes scenarios — guest, member, admin, anonymous user, external system.

| Property | Type | Description |
|---|---|---|
| `display_name` | string | Human-readable name. E.g. `Guest User` |

### Domain
A functional or business domain grouping related scenarios, services, and modules — e.g. Authentication, Cases, Compliance, Documents. Domains answer "what area of the product does this belong to?" independently of code packaging.

| Property | Type | Description |
|---|---|---|
| `slug` | string | Short machine-readable identifier. E.g. `auth`, `cases` |

### Scenario
A concrete, testable example of behavior — a user story expressed as given/when/then.

| Property | Type | Description |
|---|---|---|
| `title` | string | E.g. `Valid Credentials Login` |
| `given` | string | Precondition |
| `when` | string | Triggering action |
| `then` | string | Expected outcome |
| `and_also` | string[] | Additional outcomes or side effects |

### ScenarioStep
An ordered step within a complex scenario. Steps pin to a specific Context and Action, forming the transition chain of the flow.

| Property | Type | Description |
|---|---|---|
| `sequence` | number | Step order: 1, 2, 3, … |

### Service
A business logic service — `UserService`, `AuthService`, `PaymentService`, etc. Encapsulates domain operations and orchestrates data access.

| Property | Type | Description |
|---|---|---|
| `struct` | string | Language-specific type name. E.g. `CasesService` |

### Method
A public method on a service — a named operation the service exposes to callers.

| Property | Type | Description |
|---|---|---|
| `signature` | string | Full signature. E.g. `Create(ctx context.Context, input CreateCaseInput) (*Case, error)` |

### DataModel
A domain data type, schema, or DTO shared across the system — Go struct, TypeScript interface, Pydantic model, Protobuf message, database table schema.

| Property | Type | Description |
|---|---|---|
| `language_type` | string | Language-specific type name if different from object name |
| `fields` | string[] | Key field names |

### Database / Store
A persistence or state store — Postgres, Redis, SQLite, S3, Zustand, Redux slice.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `relational`, `key-value`, `document`, `object-storage`, `client-state` |
| `technology` | string | E.g. `postgres`, `redis`, `sqlite`, `zustand` |
| `host` | string | Hostname or connection hint |

### APIEndpoint
A single HTTP, gRPC, GraphQL, or WebSocket endpoint.

| Property | Type | Description |
|---|---|---|
| `method` | string | E.g. `GET`, `POST`, `PUT`, `DELETE`, `rpc` |
| `path` | string | URL path or RPC method. E.g. `/api/v1/users` |
| `auth_required` | boolean | Whether authentication is required |
| `min_role` | string | Minimum RBAC role required. E.g. `employee`, `owner` |
| `handler` | string | Handler function name. E.g. `HandleList` |
| `query_params` | string[] | Accepted query parameters |

### APIContract
A machine-readable API definition grouping multiple endpoints — OpenAPI/Swagger file, Protobuf file, GraphQL schema. Use `SourceFile --defines→ APIContract` to link to the spec file.

| Property | Type | Description |
|---|---|---|
| `format` | string | `openapi`, `protobuf`, `graphql` |
| `version` | string | API version. E.g. `v1` |
| `base_url` | string | Base URL for the API |

### Event / Message
A pub/sub event, message queue message type, WebSocket event name, or domain event.

| Property | Type | Description |
|---|---|---|
| `channel` | string | Queue name, topic, or event channel. E.g. `user.created` |
| `transport` | string | `kafka`, `rabbitmq`, `websocket`, `sns`, `in-process` |

### Middleware
A request pipeline handler — auth, logging, rate limiting, CORS, tracing, error handling. Use `applies_to` relationships to link to the endpoints it covers.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `auth`, `logging`, `rate-limiting`, `cors`, `tracing`, `error-handling` |

### Job / Worker
A background job, cron task, or queue worker.

| Property | Type | Description |
|---|---|---|
| `kind` | string | Job kind string. E.g. `timesheet_sync`, `invoice_sync` |
| `schedule` | string | Cron expression. E.g. `0 * * * *` |

### ExternalDependency
A third-party library, SDK, or external service — npm package, Go module, Python package, SaaS API.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `library`, `sdk`, `saas-api`, `infrastructure` |
| `version` | string | Version constraint. E.g. `^18.0.0` |
| `registry` | string | `npm`, `go-modules`, `pypi`, `cargo` |

### ConfigVar
An environment variable, feature flag, or configuration key the app reads at runtime.

| Property | Type | Description |
|---|---|---|
| `key` | string | Variable name. E.g. `DATABASE_URL` |
| `required` | boolean | Whether the app fails to start without this |
| `default_value` | string | Default if not set |
| `secret` | boolean | Whether this holds a secret and should not be logged |

### Pattern
A recurring implementation pattern observed or mandated in the codebase — repository pattern, optimistic UI update, retry with backoff. Use `exemplifies` and `counter_exemplifies` to link canonical source files.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `architectural`, `ui`, `data-access`, `error-handling`, `concurrency` |
| `scope` | string | `backend`, `frontend`, `data-layer`, `global` |
| `usage_guidance` | string | When and how to apply this pattern |

### TestSuite
A test file, test group, or spec file — unit tests, integration tests, e2e specs.

| Property | Type | Description |
|---|---|---|
| `kind` | string | `unit`, `integration`, `e2e`, `snapshot` |
| `framework` | string | `jest`, `vitest`, `go-test`, `pytest`, `xctest` |
| `coverage_percent` | number | Approximate coverage percentage |

### SQLQuery
A named typed database query — a SQLC query or equivalent. Captures the operation kind, parameters, and return type so agents can find the right query without reading generated files.

| Property | Type | Description |
|---|---|---|
| `operation` | string | `select`, `insert`, `update`, `delete` |
| `input_params` | string[] | Parameter names and types. E.g. `[org_id int64, status CaseStatusEnum]` |
| `return_type` | string | SQLC tokens (`one`, `many`, `exec`) or Go types |

---

## Relationship reference

### Containment & grouping

| Relationship | From → To | Meaning |
|---|---|---|
| `contains` | App → Module | An app is composed of modules |
| `belongs_to` | Scenario, Module, SourceFile, Service, Job, APIEndpoint → Domain | A node belongs to a functional domain |
| `nested_in` | Context → Context | A context is nested inside another (modal in screen) |
| `grouped_in` | APIEndpoint → APIContract | An endpoint belongs to an API contract |

### Definition / implementation

| Relationship | From → To | Meaning |
|---|---|---|
| `defines` | SourceFile → Service, Job, Middleware, DataModel, Context, UIComponent, Action, TestSuite, APIEndpoint, SQLQuery, APIContract | A source file defines or implements a node |
| `entry_point_of` | SourceFile → App | A file is the main entry point for an app |

### Exposure & routing

| Relationship | From → To | Meaning |
|---|---|---|
| `exposes` | Service → APIEndpoint | A service exposes an API endpoint |
| `handles` | SourceFile → APIEndpoint | A source file contains the handler function for an endpoint |
| `applies_to` | Middleware → APIEndpoint | A middleware applies to an endpoint |

### Dependencies & configuration

| Relationship | From → To | Meaning |
|---|---|---|
| `depends_on` | App, Module, Service → App, Module, Service | Import or runtime dependency |
| `uses` | App, Module, Service, Context, Action, UIComponent → ExternalDependency, Middleware, APIEndpoint, DataModel | A node uses another node |
| `configured_by` | App, Module, Service → ConfigVar | A node reads a config variable at runtime |

### Data

| Relationship | From → To | Meaning |
|---|---|---|
| `reads_from` | Service, Job → Database | Reads data from a store |
| `writes_to` | Service, Job → Database | Writes data to a store |
| `stores` | Database → DataModel | A store persists a data model |
| `provides` | Service → DataModel | A service owns and defines a data model |
| `implements` | Service, Job → SQLQuery | A service or job uses a specific query to access the database |

### Scenario / actor layer

| Relationship | From → To | Meaning |
|---|---|---|
| `executed_by` | Scenario → Actor | A scenario is executed by a user role |
| `has` | Scenario → ScenarioStep, Service → Method | A parent node directly owns a child node |
| `variant_of` | Scenario → Scenario | A scenario is an alternative path of another |
| `occurs_in` | ScenarioStep → Context | A step takes place in a specific context |
| `performs` | ScenarioStep → Action | A step performs a specific action |
| `inherits_from` | Actor → Actor | An actor inherits permissions from another |
| `navigates_to` | Action → Context | An action navigates to a target context |
| `available_in` | Action → Context | An action is available within a context |

### Events & jobs

| Relationship | From → To | Meaning |
|---|---|---|
| `publishes` | Service, Job → Event | Emits an event or message |
| `subscribes_to` | Service, Job → Event | Consumes an event or message |
| `triggers` | Service, Action, Job → Job | Enqueues or schedules a background job |

### Patterns & tests

| Relationship | From → To | Meaning |
|---|---|---|
| `follows` | Module, Service → Pattern | A node follows a named pattern |
| `extends` | Pattern → Pattern | One pattern specializes another |
| `exemplifies` | SourceFile → Pattern | A source file is a canonical positive example of a pattern |
| `counter_exemplifies` | SourceFile → Pattern | A source file is a canonical example of what NOT to do |
| `tested_by` | Service, Context, UIComponent, Action, Module, Scenario → TestSuite | A node is covered by a test suite |

---

## The dependency traceability chain

A key capability this schema enables is tracing the full complexity path of any feature:

```
Context
  → uses → APIEndpoint
      ← exposes ← Service
          → reads_from / writes_to → Database
              ← stores ← DataModel

Context
  → uses → UIComponent
      → uses → APIEndpoint

Action
  → uses → APIEndpoint
  → triggers → Job
  → publishes → Event
      ← subscribes_to ← Service
```

This lets you answer: "If I change this endpoint, what UI surfaces are affected?" or "What does this screen actually depend on all the way to the database?"

---

## Installation

```bash
memory blueprints install https://github.com/mkucharz/code-memory-blueprint --project <project-slug>
```

Or from a local clone:

```bash
git clone https://github.com/mkucharz/code-memory-blueprint
memory blueprints install ./code-memory-blueprint --project <project-slug>
```

This installs the `code-structure` schema pack (24 object types, 36 relationship types) into your Emergent project. No agents or seed data are included — this is a schema-only blueprint.

---

## Design decisions

- **SourceFile as first-class object** — all code artifacts (services, models, components, actions, jobs, middleware, test suites, SQL queries, API contracts) link to their implementing file via `defines` relationships. No string `file_path` properties on domain types; the graph itself is the index.
- **Context replaces Page** — `Context` is medium-agnostic: it covers web views, mobile screens, desktop windows, CLI prompts, email templates, and more. The `context_type` property captures the medium; `type` captures the surface kind.
- **Action is a first-class type** — not just a relationship. Actions are independently queryable, can have their own test suites, can publish events, trigger jobs, and call endpoints.
- **Domain as first-class type** — business domains (Auth, Cases, Compliance…) are nodes in the graph, not just string tags. Services, modules, jobs, and endpoints all `belongs_to` a domain.
- **Method as first-class type** — service methods are nodes linked via `has`, not a string array property on Service. This enables per-method relationships, test coverage, and query-by-signature.
- **Clean relationship verbs** — all relationship names are clean verbs without type names embedded (`handles` not `handles_route`, `exposes` not `exposes_endpoint`, `has` not `has_step`).
- **Scenario layer is structural, not workflow** — Scenarios and ScenarioSteps describe observable behavior anchored to Contexts and Actions. They are part of the code's structure, not a sprint planning tool.
- **No workflow types** — this pack contains no `Task`, `Spec`, `Requirement`, `Change`, or `WorkPackage` types. For those, see the `product-memory-blueprint` or a SpecMCP-based pack.

---

## Directory layout

```
code-memory-blueprint/
  schemas/
    code-structure.yaml    # 24 object types, 36 relationship types
  README.md
  project.yaml
```

---

## Prerequisites

- An Emergent project with the `memory` CLI installed
- No agents, no seed data, no external MCP servers required

---

## License

MIT
