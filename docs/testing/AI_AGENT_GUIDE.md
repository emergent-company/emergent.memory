# AI Agent Testing Guide

**Purpose**: Quick reference for AI coding agents to write correct tests. Optimized for copy-paste and pattern matching.

## Quick Decision Tree

```
What are you testing?
├─ Single function/class/component → UNIT TEST
├─ API endpoint workflow → API E2E TEST (server)
├─ UI interaction/workflow → BROWSER E2E TEST (admin)
└─ Multiple services (no external deps) → INTEGRATION TEST
```

## Rules

### Rule 1: File Location

```
Server unit:        apps/server/tests/unit/**/*.spec.ts
Server e2e:         apps/server/tests/e2e/**/*.e2e-spec.ts
Server integration: apps/server/tests/integration/**/*.integration.spec.ts
Admin unit:         apps/admin/tests/unit/**/*.test.tsx
Admin e2e:          apps/admin/tests/e2e/**/*.spec.ts
```

### Rule 2: Mock Everything in Unit Tests

```typescript
import { vi } from 'vitest';

// Mock dependencies
const mockService = {
  method: vi.fn().mockResolvedValue('result'),
};

// Spy on methods
const spy = vi.spyOn(obj, 'method').mockReturnValue('value');
```

### Rule 3: Use Real Infrastructure in E2E Tests

- API E2E: Real PostgreSQL, real auth tokens, real HTTP
- Browser E2E: Real browser, real UI, real API backend

### Rule 4: Always Clean Up

```typescript
afterEach(() => {
  vi.clearAllMocks(); // Unit tests
});

afterAll(async () => {
  await ctx.cleanup(); // E2E tests
});
```

### Rule 5: Test File Header

```typescript
/**
 * Tests [feature name].
 *
 * Mocked: [list mocked dependencies]
 * Real: [list real dependencies]
 * Auth: [describe auth setup]
 */
```

## Templates

### Unit Test (Server)

```typescript
// apps/server/tests/unit/my-module/my.service.spec.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MyService } from '../../../src/modules/my-module/my.service';

/**
 * Tests MyService business logic.
 *
 * Mocked: DatabaseService, ExternalAPI
 * Real: MyService logic
 * Auth: Not applicable (unit test)
 */
describe('MyService', () => {
  let service: MyService;
  let mockDb: any;

  beforeEach(() => {
    mockDb = {
      query: vi.fn().mockResolvedValue([]),
      transaction: vi.fn(),
    };
    service = new MyService(mockDb);
  });

  describe('methodName', () => {
    it('should do something', async () => {
      // Arrange
      mockDb.query.mockResolvedValue([{ id: '1', name: 'Test' }]);

      // Act
      const result = await service.methodName('input');

      // Assert
      expect(result).toHaveLength(1);
      expect(mockDb.query).toHaveBeenCalledWith(expect.any(String));
    });
  });
});
```

### Unit Test (Admin React)

```typescript
// apps/admin/tests/unit/components/MyComponent.test.tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MyComponent } from '../../../src/components/MyComponent';

/**
 * Tests MyComponent rendering and interactions.
 *
 * Mocked: None (pure component test)
 * Real: Component rendering
 * Auth: Not applicable
 */
describe('MyComponent', () => {
  it('should render with correct props', () => {
    render(<MyComponent title="Test" />);
    expect(screen.getByText('Test')).toBeInTheDocument();
  });

  it('should handle click events', async () => {
    const handleClick = vi.fn();
    render(<MyComponent onClick={handleClick} />);

    await userEvent.click(screen.getByRole('button'));
    expect(handleClick).toHaveBeenCalledTimes(1);
  });
});
```

### API E2E Test (Server)

```typescript
// apps/server/tests/e2e/my-feature.e2e-spec.ts
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import request from 'supertest';
import { createE2EContext } from './e2e-context';
import { authHeader } from './auth-helpers';

/**
 * Tests My Feature API endpoints.
 *
 * Mocked: None
 * Real: Full NestJS app, PostgreSQL, Auth
 * Auth: Zitadel test tokens with scopes
 */
describe('My Feature API (e2e)', () => {
  let ctx: any;

  beforeAll(async () => {
    ctx = await createE2EContext();
  });

  afterAll(async () => {
    await ctx.cleanup();
  });

  it('should create resource', async () => {
    const res = await request(ctx.app.getHttpServer())
      .post('/api/my-resource')
      .set('Authorization', authHeader(['write:resource']))
      .set('X-Organization-ID', ctx.testOrgId)
      .send({ name: 'Test Resource' });

    expect(res.status).toBe(201);
    expect(res.body).toMatchObject({
      id: expect.any(String),
      name: 'Test Resource',
    });
  });
});
```

### Browser E2E Test (Admin)

```typescript
// apps/admin/tests/e2e/specs/my-workflow.spec.ts
import { test, expect } from '@playwright/test';

/**
 * Tests My Workflow user journey.
 *
 * Mocked: None
 * Real: Full application stack
 * Auth: Real Zitadel login via Playwright
 */
test('user can complete my workflow', async ({ page }) => {
  // Login
  await page.goto('/login');
  await page.fill('[name="email"]', process.env.E2E_TEST_USER_EMAIL!);
  await page.fill('[name="password"]', process.env.E2E_TEST_USER_PASSWORD!);
  await page.click('button[type="submit"]');
  await expect(page).toHaveURL(/\/dashboard/);

  // Perform workflow
  await page.goto('/my-feature');
  await page.click('button:has-text("Create New")');
  await page.fill('[name="title"]', 'Test Item');
  await page.click('button:has-text("Save")');

  // Verify
  await expect(page.getByText('Test Item')).toBeVisible();
});
```

## Authentication Patterns

### Unit Tests: Mock ExecutionContext

```typescript
import { ExecutionContext } from '@nestjs/common';

const mockContext = {
  switchToHttp: () => ({
    getRequest: () => ({
      user: { sub: 'user-123', scopes: ['read:documents'] },
    }),
  }),
} as ExecutionContext;
```

### API E2E: authHeader Helper

```typescript
import { authHeader } from './auth-helpers';

// No auth
await request(app).get('/api/public');

// Basic auth (default scopes)
await request(app).get('/api/documents').set('Authorization', authHeader());

// Custom scopes
await request(app)
  .post('/api/documents')
  .set('Authorization', authHeader(['write:documents', 'read:projects']));
```

### Browser E2E: Playwright Login

```typescript
// Use in beforeEach or test
await page.goto('/login');
await page.fill('[name="email"]', process.env.E2E_TEST_USER_EMAIL!);
await page.fill('[name="password"]', process.env.E2E_TEST_USER_PASSWORD!);
await page.click('button[type="submit"]');
await page.waitForURL(/\/dashboard/);
```

## Database Patterns

### Unit Tests: Mock Database

```typescript
const mockDb = {
  query: vi.fn().mockResolvedValue([{ id: '1' }]),
  transaction: vi.fn(async (cb) => cb(mockDb)),
  getRepository: vi.fn().mockReturnValue({
    find: vi.fn().mockResolvedValue([]),
    save: vi.fn().mockResolvedValue({ id: '1' }),
  }),
};
```

### API E2E: createE2EContext

```typescript
import { createE2EContext } from './e2e-context';

beforeAll(async () => {
  ctx = await createE2EContext();
  // Provides: app, db, testOrgId, testProjectId, cleanup()
});

afterAll(async () => {
  await ctx.cleanup(); // Automatic cleanup via RLS
});

// Database automatically scoped to test org via RLS
```

## Mocking Patterns

### Simple Function Mock

```typescript
const mockFn = vi.fn().mockReturnValue('result');
const asyncMock = vi.fn().mockResolvedValue({ data: 'result' });
const errorMock = vi.fn().mockRejectedValue(new Error('Failed'));
```

### Spy on Method

```typescript
const spy = vi.spyOn(service, 'method');
spy.mockReturnValue('mocked');
spy.mockResolvedValue('async mocked');
spy.mockImplementation((arg) => `processed ${arg}`);
```

### Module Mock

```typescript
vi.mock('../../../src/modules/external/external.service', () => ({
  ExternalService: vi.fn().mockImplementation(() => ({
    fetchData: vi.fn().mockResolvedValue({ success: true }),
    processData: vi.fn().mockReturnValue('processed'),
  })),
}));
```

### NestJS Module Mock

```typescript
import { Test } from '@nestjs/testing';

const module = await Test.createTestingModule({
  providers: [
    MyService,
    {
      provide: DatabaseService,
      useValue: mockDb,
    },
    {
      provide: ExternalService,
      useValue: mockExternal,
    },
  ],
}).compile();

const service = module.get(MyService);
```

## Quality Checklist

Before submitting a test, verify:

- [ ] Test is in correct location (see Rule 1)
- [ ] Test has descriptive name (not "should work")
- [ ] Header comment explains what/why/how
- [ ] All dependencies mocked (unit) or real (e2e)
- [ ] Cleanup in afterEach/afterAll
- [ ] Follows Arrange-Act-Assert pattern
- [ ] No console.log() statements
- [ ] No hardcoded IDs or credentials
- [ ] No timing-dependent assertions (use waitFor)
- [ ] Test is independent (no shared state)

## Commands

```bash
# Server (NestJS - deprecated)
nx test server              # Unit tests
nx test server --watch      # Watch mode
nx test-e2e server          # E2E tests
nx test server --coverage   # With coverage

# Server (Go - primary)
nx run server-go:test            # Unit tests
nx run server-go:test-e2e        # E2E tests (HTTP API, 23 suites)
nx run server-go:test-integration # Integration tests (Service + DB, 8 suites)
task test:e2e                    # Alternative: run from apps/server-go/

# Admin
nx test admin               # Unit tests
nx test-e2e admin           # Browser E2E
nx test-e2e admin --ui      # E2E UI mode
nx test-e2e admin --headed  # See browser
```

## Go Server Testing

The Go server (`apps/server-go/`) is the **primary backend**. Tests are organized into two categories:

| Directory            | Purpose                       | Can Run Against External Server |
| -------------------- | ----------------------------- | ------------------------------- |
| `tests/e2e/`         | HTTP API tests (23 suites)    | ✅ Yes                          |
| `tests/integration/` | Service + DB tests (8 suites) | ❌ No (always in-process)       |

**E2E tests** validate HTTP API behavior through the full request/response cycle. They can run against an in-process test server or an external running server.

**Integration tests** test internal services requiring direct database access (job queues, workers, schedulers).

### Go E2E Test Template

```go
// apps/server-go/tests/e2e/my_feature_test.go
package e2e

import (
    "net/http"
    "testing"

    "github.com/anomalyco/emergent/apps/server-go/internal/testutil"
    "github.com/stretchr/testify/suite"
)

type MyFeatureSuite struct {
    suite.Suite
    db        *testutil.TestDB
    server    *testutil.TestServer
    projectID string
    token     string
}

func (s *MyFeatureSuite) SetupSuite() {
    s.db = testutil.NewTestDB(s.T())
    s.server = testutil.NewTestServer(s.db)

    // Create test fixtures
    s.projectID = s.db.CreateProject()
    s.token = testutil.NewTestTokenBuilder().
        WithUserID("test-user").
        WithScopes("documents:read", "documents:write").
        Build()
}

func (s *MyFeatureSuite) TearDownSuite() {
    s.db.Close()
}

func (s *MyFeatureSuite) TestCreateResource_Success() {
    // Arrange
    body := map[string]any{"name": "Test Resource"}

    // Act
    rec := s.server.POST("/api/v2/my-resource",
        testutil.WithAuth(s.token),
        testutil.WithProjectID(s.projectID),
        testutil.WithBody(body),
    )

    // Assert
    s.Equal(http.StatusCreated, rec.Code)
    s.Contains(rec.Body.String(), "Test Resource")
}

func (s *MyFeatureSuite) TestCreateResource_RequiresAuth() {
    rec := s.server.POST("/api/v2/my-resource")  // No auth
    s.Equal(http.StatusUnauthorized, rec.Code)
}

func (s *MyFeatureSuite) TestCreateResource_RequiresScope() {
    token := testutil.NewTestTokenBuilder().
        WithScopes("chat:use").  // Wrong scope
        Build()

    rec := s.server.POST("/api/v2/my-resource",
        testutil.WithAuth(token),
        testutil.WithProjectID(s.projectID),
    )
    s.Equal(http.StatusForbidden, rec.Code)
}

func TestMyFeatureSuite(t *testing.T) {
    suite.Run(t, new(MyFeatureSuite))
}
```

### Go Test Utilities

Located in `apps/server-go/internal/testutil/`:

```go
// TestDB - Isolated database with transaction rollback
db := testutil.NewTestDB(t)
defer db.Close()

// TestServer - Echo server with all routes
server := testutil.NewTestServer(db)

// TestTokenBuilder - Create JWT tokens
token := testutil.NewTestTokenBuilder().
    WithUserID("user-123").
    WithOrgID("org-456").
    WithScopes("documents:read", "documents:write").
    Build()

// Request helpers
rec := server.GET("/api/v2/documents",
    testutil.WithAuth(token),
    testutil.WithProjectID(projectID),
    testutil.WithQuery("limit", "10"),
)

rec := server.POST("/api/v2/documents",
    testutil.WithAuth(token),
    testutil.WithProjectID(projectID),
    testutil.WithBody(map[string]any{"title": "Test"}),
)
```

### Go Test Patterns

**Standard auth/scope/project tests:**

```go
func (s *MySuite) TestEndpoint_RequiresAuth() {
    rec := s.server.GET("/api/v2/resource")
    s.Equal(http.StatusUnauthorized, rec.Code)
    s.Contains(rec.Body.String(), "unauthorized")
}

func (s *MySuite) TestEndpoint_RequiresScope() {
    token := testutil.NewTestTokenBuilder().
        WithScopes("wrong:scope").
        Build()
    rec := s.server.GET("/api/v2/resource",
        testutil.WithAuth(token),
        testutil.WithProjectID(s.projectID),
    )
    s.Equal(http.StatusForbidden, rec.Code)
}

func (s *MySuite) TestEndpoint_RequiresProjectID() {
    rec := s.server.GET("/api/v2/resource",
        testutil.WithAuth(s.token),
        // Missing WithProjectID
    )
    s.Equal(http.StatusBadRequest, rec.Code)
    s.Contains(rec.Body.String(), "project_id")
}
```

**Testing SSE streaming:**

```go
func (s *ChatSuite) TestStreamChat_Success() {
    body := map[string]any{"query": "Hello"}

    rec := s.server.POST("/api/v2/chat/stream",
        testutil.WithAuth(s.token),
        testutil.WithProjectID(s.projectID),
        testutil.WithBody(body),
    )

    s.Equal(http.StatusOK, rec.Code)
    s.Equal("text/event-stream", rec.Header().Get("Content-Type"))

    // Parse SSE events
    events := testutil.ParseSSE(rec.Body.String())
    s.NotEmpty(events)
}
```

### Running Go Tests

```bash
cd apps/server-go

# Run all E2E tests (HTTP API)
nx run server-go:test-e2e
# Or directly:
POSTGRES_PASSWORD=emergent-dev-password go test ./tests/e2e/... -v -count=1

# Run E2E against external server
TEST_SERVER_URL=http://localhost:3002 go test ./tests/e2e/... -v -count=1

# Run integration tests (service + DB)
nx run server-go:test-integration
# Or directly:
POSTGRES_PASSWORD=emergent-dev-password go test ./tests/integration/... -v -count=1

# Run specific suite
go test ./tests/e2e/... -v -run TestDocumentsSuite

# Run specific test
go test ./tests/e2e/... -v -run TestDocumentsSuite/TestCreateDocument_Success

# Run with race detection
go test ./tests/e2e/... -v -race

# Run with coverage
go test ./tests/e2e/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Go Test File Reference

**E2E Tests (tests/e2e/)** - HTTP API tests:

| File                         | Coverage                                   |
| ---------------------------- | ------------------------------------------ |
| `auth_test.go`               | JWT/API token validation                   |
| `security_scopes_test.go`    | Scope enforcement matrix                   |
| `tenant_isolation_test.go`   | RLS, cross-project isolation               |
| `documents_test.go`          | Document CRUD, pagination                  |
| `documents_upload_test.go`   | Upload validation                          |
| `chunks_test.go`             | Chunk listing, bulk delete                 |
| `graph_test.go`              | Objects, relationships, search             |
| `graph_search_test.go`       | Graph search with debug mode               |
| `search_test.go`             | Unified search, fusion, debug mode         |
| `chat_test.go`               | Conversations, SSE streaming               |
| `mcp_test.go`                | MCP protocol auth                          |
| `orgs_test.go`               | Organization CRUD                          |
| `projects_test.go`           | Project CRUD, members                      |
| `invites_test.go`            | Invite CRUD                                |
| `email_jobs_test.go`         | Email queue (HTTP endpoints)               |
| `embedding_policies_test.go` | Embedding policy CRUD                      |
| `branches_test.go`           | Branch CRUD for graph versioning           |
| `superadmin_test.go`         | Superadmin user/org/project/job management |
| `templatepacks_test.go`      | Template pack management                   |
| `tasks_test.go`              | Task management                            |
| `notifications_test.go`      | Notification management                    |
| `useractivity_test.go`       | User activity tracking                     |
| `apitoken_test.go`           | API token CRUD                             |
| `users_test.go`              | User search                                |
| `userprofile_test.go`        | Profile get/update                         |
| `useraccess_test.go`         | Access tree                                |
| `events_test.go`             | Event listing                              |
| `health_test.go`             | Health/ready/debug endpoints               |

**Integration Tests (tests/integration/)** - Service + DB tests:

| File                             | Coverage                     |
| -------------------------------- | ---------------------------- |
| `scheduler_test.go`              | Cron task execution, cleanup |
| `datasource_deadletter_test.go`  | Dead letter handling         |
| `document_parsing_jobs_test.go`  | Document parsing job queue   |
| `chunk_embedding_jobs_test.go`   | Embedding job queue          |
| `chunk_embedding_worker_test.go` | Embedding worker processing  |
| `graph_embedding_jobs_test.go`   | Graph embedding job queue    |
| `graph_embedding_worker_test.go` | Graph embedding worker       |
| `object_extraction_jobs_test.go` | Object extraction job queue  |

See `apps/server-go/AGENT.md` for the complete list.

## Common Patterns

### Test Isolation

```typescript
describe('MyService', () => {
  let service: MyService;
  let mockDep: any;

  beforeEach(() => {
    // Fresh instances per test
    mockDep = { method: vi.fn() };
    service = new MyService(mockDep);
  });

  afterEach(() => {
    vi.clearAllMocks();
  });
});
```

### Async Operations

```typescript
it('should handle async operation', async () => {
  mockService.fetch.mockResolvedValue({ data: 'result' });

  const result = await service.processData();

  expect(result).toBe('result');
});
```

### Error Handling

```typescript
it('should handle errors', async () => {
  mockService.fetch.mockRejectedValue(new Error('Network error'));

  await expect(service.processData()).rejects.toThrow('Network error');
});
```

### Multiple Assertions

```typescript
it('should process data correctly', async () => {
  const input = { id: '123', name: 'Test' };

  const result = await service.process(input);

  expect(result).toMatchObject({
    id: '123',
    name: 'Test',
    processed: true,
  });
  expect(mockDb.save).toHaveBeenCalledWith(
    expect.objectContaining({
      id: '123',
    })
  );
});
```

## Anti-Patterns (DON'T)

```typescript
// ❌ DON'T: Test implementation details
it('should call private method', () => {
  expect(service['_privateMethod']).toHaveBeenCalled();
});

// ✅ DO: Test public behavior
it('should process data', () => {
  const result = service.processData(input);
  expect(result).toBe(expected);
});

// ❌ DON'T: Mock everything in e2e tests
const mockDb = { query: vi.fn() }; // in e2e test

// ✅ DO: Use real infrastructure
const ctx = await createE2EContext(); // real DB

// ❌ DON'T: Shared state between tests
let sharedData = [];
it('test 1', () => {
  sharedData.push(1);
});
it('test 2', () => {
  expect(sharedData).toHaveLength(1);
}); // Fragile!

// ✅ DO: Independent tests
let data: any[];
beforeEach(() => {
  data = [];
});
it('test 1', () => {
  data.push(1);
  expect(data).toHaveLength(1);
});
it('test 2', () => {
  data.push(2);
  expect(data).toHaveLength(1);
});
```

## Troubleshooting

### Test Fails: "Cannot find module"

→ Check import paths after migration, use correct relative paths

### Test Fails: "Database connection error"

→ Verify Docker containers running: `docker ps`
→ Check `.env` has correct DB connection string

### Test Timeout

→ Increase timeout: `it('test', async () => {...}, 10000)` (10s)
→ Check for missing `await` on async operations

### Flaky Test

→ Add explicit waits in browser tests: `await page.waitForSelector()`
→ Check for race conditions
→ Ensure cleanup runs properly

### Mock Not Working

→ Verify mock is set up before test runs
→ Check mock is reset between tests: `vi.clearAllMocks()`
→ Use `vi.spyOn()` for existing methods

## Reference

Full documentation: `docs/testing/TESTING_GUIDE.md`
