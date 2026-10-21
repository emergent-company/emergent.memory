# Database Migration System

The NestJS-era migration runner this page described (`kb.schema_migrations`,
`npx nx run server:migrate`, alphabetical ordering) is gone. The Go server uses
[Goose](https://pressly.github.io/goose/) with SQL files embedded in the binary,
and the maintained documentation lives next to the code:

- [`apps/server/migrations/README.md`](../../apps/server/migrations/README.md) —
  directory layout, naming, merge order, and how to add a migration.

Day to day, from the product repo root:

```bash
task migrate:status   # show applied and pending migrations
task migrate:up       # apply pending migrations
task migrate:down     # roll back the last migration
```

Migrations are immutable once merged: CI runs `Migration Immutability` and
`Migration Order Guard`, so fix a bad migration with a new one instead of editing
it in place.
