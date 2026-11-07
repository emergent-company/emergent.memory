# Example Blueprints

Reference blueprints you can install into any Memory project. Each is a self-contained directory that installs schemas, agents, and seed data.

## Available examples

### [multi-agent](./multi-agent/)

A complete hierarchical multi-agent orchestration system. Installs a schema pack for work lifecycle tracking (WorkPackages, Tasks, AcceptanceCriteria, FeedbackSignals), 11 agent definitions across three tiers (orchestrator, pool managers, leaf agents), and pre-wired agent pools and KPIs.

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/multi-agent --project <slug>
```

### [code-memory-blueprint](./code-memory-blueprint/)

Installs the `code-structure` schema pack — maps the structural and architectural layer of any software codebase. Works for any language or framework (Go, TypeScript, Python, Swift, Rust, etc.).

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint --project <slug>
```

### [blueprint-architect](./blueprint-architect/)

A dedicated authoring agent that designs and emits Memory blueprints — object types, relationship types, agent definitions, and seed data — for a project, registers the schema live, and produces a reusable blueprint directory.

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/blueprint-architect --project <slug>
```

### [security-review](./security-review/)

An agent-only blueprint: a queue-backed security code review agent bound to the
`security-review` work queue. It does not create the queue — see its README for
the prerequisite.

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/security-review --project <slug>
```

### [webpage-feedback](./webpage-feedback/)

Installs the `webpage-feedback` schema pack (Issue and IssueGroup), the
`issue-triager` agent and the `triage-issues` skill for triaging reports
captured by [emergent.feedback](https://github.com/emergent-company/emergent.feedback).

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/webpage-feedback --project <slug>
```

### [test-agents](./test-agents/)

Agent definitions used by the end-to-end domain-classification tests. Not a
general-purpose blueprint.

## Built-in blueprints

The packs every deployment ships live in
`apps/server/domain/blueprints/builtin_packs/` and are installed by name, not
from this directory. Do not add an example pack with the same name as a
built-in one: `TestExamplePackNamesDoNotCollideWithBuiltins` rejects that,
because two copies of a pack are how they drift apart.

## Using blueprints

```bash
# Preview what a blueprint will install (no changes made)
memory blueprints inspect <source>

# Validate a blueprint offline
memory blueprints validate <source>

# Install
memory blueprints install <source> --project <slug>
```

Sources can be a local directory path or a GitHub URL (`github.com/org/repo/path`).

## Authoring conventions

A pack's schema is what most projects' types are made of, so the property
definitions are worth a little care. In a `schemas/*.yaml` (or `packs/*.yaml`)
file, each property takes the JSON-Schema keywords the registry understands:

```yaml
properties:
  tokensUsed:
    type: integer
    title: Tokens used        # the label the UI shows, key stays visible beneath it
    description: Total tokens consumed across the execution tree
  homepage:
    type: url                 # absolute http(s) URL, validated on write
    title: Homepage
  content_hash:
    type: sha256              # exactly 64 hex characters, stored lower-case
    title: Content hash
  last_used:
    type: date                # RFC 3339, validated on write
    title: Last used
  contact_email:
    type: email               # one bare address, stored lower-case
    title: Contact email
  home_country:
    type: country             # ISO 3166-1, stored as the two-letter code
    title: Home country
  latitude:
    type: number              # minimum/maximum are enforced on write
    minimum: -90
    maximum: 90
    title: Latitude
```

- **Label every key a reader cannot scan at a glance** — any `snake_case` key or
  one with an internal capital. `event_time` → `Event time`, `tokensUsed` →
  `Tokens used`. A label that merely repeats its key (`title: Status` on
  `status`) is dead weight: the renderer shows the label and hides an
  identical key. `TestShippedPackPropertyLabels` enforces this for every shipped
  pack.
- **Use `url` and the digest types (`md5`, `sha1`, `sha256`, `sha512`) for
  values that are URLs or digests**, not plain `string`: they are validated on
  write. Use `url` only for absolute `http`/`https` values — a route
  (`/dashboard`), a host name or an env-var key stays `string`. These types have
  no "empty" value: leave an unknown one out (or send `null`), never `""`.
- **Use the semantic types for values with a fixed shape** rather than `string`:
  `email`, `phone`, `country` (ISO 3166-1), `currency` (ISO 4217) and `time`
  (24-hour `HH:MM`) are validated and normalized on write, and the editor
  renders a matching input. `format: email` on a `string` property is the same
  validation under the JSON-Schema spelling.
- **Bound a `number`/`integer` where the domain has a range** — `minimum` and
  `maximum` are enforced on write, so latitude is `-90..90` and a 1–5 rating
  cannot store `9`. A bound of `0` is real, not "unset".
- **Use `date` for timestamps you are adding** rather than a `string` documented
  as ISO 8601, and `number`/`integer`/`boolean` rather than strings — both are
  validated on write. A few older packs still carry string timestamps;
  converting those is tracked separately.
- **Re-applying a blueprint is how an existing project picks up pack changes.**
  Applying refreshes the pack's definitions from the manifest in place — for a
  built-in pack too, which is otherwise created once on first apply and never
  reseeded — so no version bump is needed for a label, description or type
  change. Changing content under an unchanged version does move the pack's
  checksum, which is what the blueprint's "update available" drift signal reads.
