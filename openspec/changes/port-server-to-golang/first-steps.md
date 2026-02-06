# Go Migration: First Steps Plan

## Executive Summary

This document outlines the first practical steps for migrating the NestJS backend to Go, focusing on:

1. Setting up the Go project structure with fx
2. Implementing auth middleware compatible with existing Zitadel tokens
3. Configuring routing to split traffic between NestJS and Go backends

## Current Architecture Analysis

### Backend (NestJS)

- **49 modules**, **~342 endpoints**, **100+ services**
- Auth via Zitadel (OIDC/JWT) with token introspection
- RLS-based multi-tenancy via `X-Project-ID` header
- Background workers using polling patterns

### Frontend (Admin)

- Uses `useApi()` hook for all API calls
- Vite proxy: `/api/*` → `http://localhost:3001/*` (strips `/api` prefix)
- Auth tokens from Zitadel stored in localStorage
- Headers sent: `Authorization`, `X-Project-ID`, `X-View-As-User-ID`

### Auth Flow

```
Frontend                    Backend                         Zitadel
   │                           │                               │
   │──── Bearer Token ────────>│                               │
   │                           │──── Introspect (cached) ─────>│
   │                           │<──── Token Valid ─────────────│
   │                           │                               │
   │                           │── Upsert user_profiles ──────>│ (DB)
   │                           │<── AuthUser ─────────────────<│
   │<──── Response ────────────│                               │
```

## Routing Strategy Options

### Option A: Vite Proxy Split (Development Only)

```typescript
// vite.config.ts
proxy: {
  '/api/v2': {  // Go endpoints
    target: 'http://localhost:8080',
    rewrite: (path) => path.replace(/^\/api\/v2/, ''),
  },
  '/api': {     // NestJS (existing)
    target: 'http://localhost:3001',
    rewrite: (path) => path.replace(/^\/api/, ''),
  },
}
```

**Pros:** Simple, no backend changes
**Cons:** Dev only, production needs different solution

### Option B: Traefik/Nginx Path-Based Routing (Production)

```yaml
# Traefik example
http:
  routers:
    go-health:
      rule: 'PathPrefix(`/health`)'
      service: server-go
    go-user-profile:
      rule: 'PathPrefix(`/user/profile`)'
      service: server-go
    nestjs-fallback:
      rule: 'PathPrefix(`/`)'
      service: server-nestjs
      priority: 1
```

**Pros:** Works in production, zero frontend changes
**Cons:** Requires infrastructure setup

### Option C: NestJS Proxy Module (Recommended for Start)

```typescript
// apps/server/src/modules/go-proxy/go-proxy.module.ts
@Module({
  imports: [HttpModule],
})
export class GoProxyModule {}

// Forward specific routes to Go server
@Controller('health')
export class GoHealthProxyController {
  @All('*')
  async proxy(@Req() req, @Res() res) {
    // Forward to Go server
  }
}
```

**Pros:** Works immediately in dev and prod, gradual migration
**Cons:** Extra hop through NestJS

### Recommended Approach: Hybrid

1. **Phase 0-1 (Dev):** Use Vite proxy split (`/api/v2/*` → Go)
2. **Phase 2+ (Staging/Prod):** Add Traefik rules progressively
3. **Parallel:** Keep NestJS running as fallback

---

## First Steps: Implementation Plan

### Step 1: Create Go Project Skeleton (Day 1-2)

```bash
# Create directory structure
mkdir -p apps/server-go/{cmd/server,domain,internal/{config,database,server},pkg/{auth,apperror}}
cd apps/server-go
go mod init github.com/anomalyco/emergent/apps/server-go
```

**Files to create:**

```
apps/server-go/
├── cmd/server/main.go           # fx.New() composition
├── internal/
│   ├── config/config.go         # Env loading
│   ├── database/database.go     # Bun + pgx setup
│   └── server/server.go         # Echo setup
├── pkg/
│   └── auth/
│       ├── middleware.go        # zitadel-go middleware
│       └── module.go            # fx.Module
├── domain/
│   └── health/
│       ├── module.go
│       ├── handler.go
│       └── routes.go
├── go.mod
├── go.sum
├── Makefile
└── .air.toml                    # Hot reload
```

### Step 2: Implement Core Infrastructure (Day 2-3)

**main.go:**

```go
package main

import (
    "go.uber.org/fx"
    "github.com/anomalyco/emergent/apps/server-go/internal/config"
    "github.com/anomalyco/emergent/apps/server-go/internal/database"
    "github.com/anomalyco/emergent/apps/server-go/internal/server"
    "github.com/anomalyco/emergent/apps/server-go/pkg/auth"
    "github.com/anomalyco/emergent/apps/server-go/domain/health"
)

func main() {
    fx.New(
        // Infrastructure
        config.Module,
        database.Module,
        server.Module,
        auth.Module,

        // Domain modules
        health.Module,
    ).Run()
}
```

**config/config.go:**

```go
type Config struct {
    ServerPort       string `env:"GO_SERVER_PORT" envDefault:"8080"`
    DatabaseURL      string `env:"DATABASE_URL,required"`
    ZitadelDomain    string `env:"ZITADEL_DOMAIN,required"`
    ZitadelClientID  string `env:"ZITADEL_API_CLIENT_ID,required"`
    ZitadelClientSecret string `env:"ZITADEL_API_CLIENT_SECRET,required"`
}
```

### Step 3: Implement Auth Middleware (Day 3-4)

**Critical:** Must validate the same Zitadel tokens as NestJS.

```go
// pkg/auth/middleware.go
package auth

import (
    "github.com/labstack/echo/v4"
    "github.com/zitadel/zitadel-go/v3/pkg/authorization"
    "github.com/zitadel/zitadel-go/v3/pkg/authorization/oauth"
)

type AuthUser struct {
    ID       string   // Internal user_profiles.id (UUID)
    Sub      string   // Zitadel subject ID
    Email    string
    Scopes   []string
    ProjectID string  // From X-Project-ID header
}

func (m *Middleware) RequireAuth() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            token := extractBearerToken(c.Request())
            if token == "" {
                return echo.NewHTTPError(401, map[string]any{
                    "error": map[string]any{
                        "code": "invalid_token",
                        "message": "Missing or invalid authorization token",
                    },
                })
            }

            // Validate with zitadel-go
            ctx, err := m.authorizer.CheckAuthorization(c.Request().Context(), token)
            if err != nil {
                return echo.NewHTTPError(401, map[string]any{
                    "error": map[string]any{
                        "code": "invalid_token",
                        "message": "Token validation failed",
                    },
                })
            }

            // Ensure user profile exists (upsert to user_profiles table)
            user, err := m.ensureUserProfile(c.Request().Context(), ctx)
            if err != nil {
                return echo.NewHTTPError(500, map[string]any{
                    "error": map[string]any{
                        "code": "internal_error",
                        "message": "Failed to sync user profile",
                    },
                })
            }

            // Extract project ID from header
            user.ProjectID = c.Request().Header.Get("X-Project-ID")

            c.Set("user", user)
            return next(c)
        }
    }
}
```

### Step 4: Implement Health Endpoint (Day 4)

**domain/health/handler.go:**

```go
package health

import (
    "context"
    "net/http"
    "github.com/labstack/echo/v4"
    "github.com/uptrace/bun"
)

type Handler struct {
    db *bun.DB
}

func NewHandler(db *bun.DB) *Handler {
    return &Handler{db: db}
}

func (h *Handler) Health(c echo.Context) error {
    // Check database connectivity
    if err := h.db.PingContext(c.Request().Context()); err != nil {
        return c.JSON(http.StatusServiceUnavailable, map[string]any{
            "status": "unhealthy",
            "database": "disconnected",
        })
    }

    return c.JSON(http.StatusOK, map[string]any{
        "status": "healthy",
        "database": "connected",
    })
}
```

### Step 5: Configure Vite Proxy Split (Day 4-5)

**Update vite.config.ts:**

```typescript
// Add Go server target
const GO_API_TARGET = process.env.GO_API_ORIGIN || 'http://localhost:8080';

proxy: {
  // Go backend endpoints (new)
  '/api/v2': {
    target: GO_API_TARGET,
    changeOrigin: true,
    secure: false,
    rewrite: (path) => path.replace(/^\/api\/v2/, ''),
  },
  // NestJS backend (existing)
  '/api': {
    target: API_TARGET,
    changeOrigin: true,
    secure: false,
    rewrite: (path) => path.replace(/^\/api/, ''),
  },
}
```

### Step 6: Add Go-Specific API Hook (Day 5)

**apps/admin/src/hooks/use-go-api.ts:**

```typescript
// Temporary hook for Go backend endpoints during migration
// Once fully migrated, remove this and update paths in use-api.ts

import { useApi } from './use-api';

export function useGoApi() {
  const { fetchJson, fetchForm, buildHeaders } = useApi();

  // Wrap to prefix /api/v2 instead of /api
  const goFetchJson = async <T, B = unknown>(
    path: string,
    init?: Parameters<typeof fetchJson>[1]
  ): Promise<T> => {
    // Prefix with /api/v2 for Go backend
    const goPath = path.startsWith('/api/v2') ? path : `/api/v2${path}`;
    return fetchJson<T, B>(goPath, init);
  };

  return { fetchJson: goFetchJson, fetchForm, buildHeaders };
}
```

### Step 7: Test Auth End-to-End (Day 5-6)

1. Start Go server: `cd apps/server-go && air`
2. Start NestJS server: `pnpm run workspace:start`
3. Login to admin UI
4. Call Go health endpoint: `GET /api/v2/health`
5. Verify token validation works

---

## Module Migration Priority

Based on the analysis, here's the recommended migration order:

### Tier 1: Foundation (Week 1-2)

| Module   | Endpoints | Complexity | Dependencies     |
| -------- | --------- | ---------- | ---------------- |
| health   | 1         | Very Low   | PostgreSQL only  |
| config   | 0         | Low        | Environment vars |
| database | 0         | Low        | PostgreSQL + Bun |

### Tier 2: Auth & Profile (Week 3-4)

| Module          | Endpoints | Complexity | Dependencies           |
| --------------- | --------- | ---------- | ---------------------- |
| auth middleware | 0         | Medium     | Zitadel, user_profiles |
| user-profile    | 3         | Low        | auth                   |
| settings        | 3         | Low        | auth                   |

### Tier 3: Core CRUD (Week 5-6)

| Module        | Endpoints | Complexity | Dependencies |
| ------------- | --------- | ---------- | ------------ |
| orgs          | 4         | Low        | auth         |
| projects      | 8         | Medium     | auth, RLS    |
| notifications | 12        | Low        | auth         |

### Tier 4: Data (Week 7-10)

| Module    | Endpoints | Complexity | Dependencies          |
| --------- | --------- | ---------- | --------------------- |
| documents | 15        | Medium     | auth, storage         |
| chunks    | 5         | Medium     | auth, embeddings      |
| graph     | 45        | High       | auth, complex queries |

---

## Success Criteria for First Steps

### Week 1 Checkpoint

- [ ] Go project compiles and runs
- [ ] fx.Module pattern established
- [ ] Health endpoint returns 200
- [ ] Database connection works

### Week 2 Checkpoint

- [ ] Auth middleware validates Zitadel tokens
- [ ] User profile upsert works
- [ ] Vite proxy split configured
- [ ] Can call Go endpoints from admin UI

### Week 3 Checkpoint

- [ ] `/api/v2/user/profile` works (GET)
- [ ] `/api/v2/settings` works (GET/PUT)
- [ ] Contract tests pass for migrated endpoints

---

## Environment Variables Required

```bash
# Go Server
GO_SERVER_PORT=8080
DATABASE_URL=postgres://user:pass@localhost:5432/emergent

# Zitadel (same as NestJS)
ZITADEL_DOMAIN=auth.dev.emergent-company.ai
ZITADEL_API_CLIENT_ID=xxx
ZITADEL_API_CLIENT_SECRET=xxx

# Vite (add to .env)
GO_API_ORIGIN=http://localhost:8080
```

---

## Risk Mitigations

| Risk                       | Mitigation                                       |
| -------------------------- | ------------------------------------------------ |
| Token validation mismatch  | Use identical zitadel-go introspection as NestJS |
| User profile sync issues   | Test upsert logic thoroughly with existing users |
| Database schema drift      | Bun models tested against live DB in CI          |
| Frontend routing confusion | Clear `/api` vs `/api/v2` separation             |
| Rollback needed            | NestJS always available as fallback              |
