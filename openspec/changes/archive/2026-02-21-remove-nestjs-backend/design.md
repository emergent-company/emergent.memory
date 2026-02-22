## Context

The Emergent workspace has fully migrated its primary backend application from NestJS (`apps/server`) to Go (`apps/server-go`). The NestJS codebase is now completely obsolete, but its files, dependencies, and configuration remnants still exist in the repository. This includes heavy NPM dependencies (`@nestjs/*`, `class-validator`, `typeorm`, etc.), Docker configurations, and Nx workspace targets. Keeping this code around slows down `pnpm install`, pollutes search results, and creates confusion for new developers.

## Goals / Non-Goals

**Goals:**

- Completely remove the `apps/server` directory.
- Remove all Node.js/NestJS specific backend dependencies from the root `package.json` to reduce the `node_modules` footprint.
- Remove the NestJS service from local development orchestrations (e.g., `docker-compose.yml`, `package.json` scripts).
- Update Nx configurations (`nx.json`, workspace `project.json` references) to drop the `server` project.
- Ensure the remaining workspace projects (`admin`, `server-go`, `website`) continue to build, lint, and test successfully.

**Non-Goals:**

- Modifying or refactoring the new Go backend (`server-go`).
- Removing shared libraries in `libs/` that are still consumed by the frontend (`admin`), even if they were originally created for the NestJS backend. (We will only remove libs exclusively used by `apps/server`).

## Decisions

**1. Dependency Removal Strategy**
We will manually remove `@nestjs/*`, `class-validator`, `class-transformer`, `typeorm`, and related testing utilities from `package.json` rather than using `pnpm remove`, to ensure we can do a clean sweep and then run a fresh `pnpm install`. We will verify no frontend project relies on these (e.g., `class-validator` might occasionally be used in shared DTOs, so we must be careful).

**2. Handling Shared DTOs (`libs/`)**
If any `libs/` packages were shared between `admin` and the NestJS `server`, we must ensure they don't break when NestJS decorators (like `@ApiProperty`, `@IsString`) are removed. We may need to strip NestJS/class-validator decorators from shared DTOs if the Go backend doesn't use them and the frontend doesn't need them for validation.

**3. CI/CD and Scripts Update**
We will thoroughly search the `.github/` workflows (or similar CI configs) and root `package.json` scripts for any mentions of `nx run server:` or simply `server`. These will be replaced with `server-go` or removed entirely.

## Risks / Trade-offs

- **Risk:** Frontend (`admin`) might be implicitly relying on a type or utility exported from `apps/server` or a shared library heavily tied to NestJS.
  - **Mitigation:** Run `nx run admin:typecheck` and `nx run admin:lint` immediately after removing the `server` directory and updating `package.json`.
- **Risk:** Removing validation decorators (`class-validator`) from shared DTOs might break frontend form validation if the frontend was relying on them (though typically frontends use Zod or Yup).
  - **Mitigation:** Audit shared libs. If the frontend relies on `class-validator`, we will keep that specific dependency but remove the NestJS ones. Otherwise, we strip them.
- **Risk:** Local development scripts (`workspace:start`) might break if they expect the Node server to spin up.
  - **Mitigation:** Test the developer onboarding flow locally (e.g., `pnpm run workspace:start`, `docker-compose up`) to ensure the Go backend spins up correctly and the admin panel connects to it seamlessly.
