# Install a blueprint

Install the [personal-memory blueprint](https://github.com/mkucharz/personal-memory-blueprint)
into an empty project from its GitHub URL, then check what arrived: six schema
packs, 23 object types and four agents. The video is one real import, start to
finish, in under thirty seconds.

<video controls preload="metadata" width="1280" poster="../../assets/tutorials/install-a-blueprint/poster.webp">
  <source src="../../assets/tutorials/install-a-blueprint/walkthrough.webm" type="video/webm"/>
  <track kind="captions" src="../../assets/tutorials/install-a-blueprint/walkthrough.vtt" srclang="en" label="English" default/>
  Your browser cannot play this video — <a href="../../assets/tutorials/install-a-blueprint/walkthrough.webm">download it</a>.
</video>

## Before you start

- A Memory project to install into. A blueprint applies to one project; nothing outside it changes.
- A Memory server that can reach GitHub, because the server fetches the repository itself — the browser being online is not enough.
- No AI provider key. The blueprint installs and applies without one; an agent asks for a model the first time it runs.

!!! note "A blueprint is installed from a URL"

    There is no archive to unzip and nothing to upload: the repository URL is the
    whole installer, and the same URL is what a later update is checked against.

## 1. Start from an empty project

Open the project that should hold the blueprint. This walkthrough uses a new,
empty one, which is the honest way to see what the blueprint adds: no object
types of its own, no agents, and a graph with nothing in it.

A blueprint applies to one project, and installing it twice in two projects
gives two independent copies. Start empty when you want to see all of it.

## 2. Open Blueprints

In the project sidebar, open **Blueprints**. The page lists the schema packs this
project has installed, so a fresh project shows an empty installed list and the
way in: **Import**.

## 3. Choose Import

Click **Import**. A blueprint is installed from a repository, not from an uploaded
archive, so the dialog asks for one thing: a URL.

**New from registry** sits next to it for blueprints that are published to a
registry; the personal-memory blueprint is not, so this walkthrough uses the URL.

## 4. Paste the repository URL

Paste the blueprint's repository:

```
https://github.com/mkucharz/personal-memory-blueprint
```

The field truncates the URL once it is wider than the dialog. That is display
only — the whole URL is what gets submitted.

The **server** fetches the repository, so the Memory instance has to be able to
reach GitHub even when your browser can.

## 5. Import, and let it apply

Click **Import**. In one request the server fetches the repository, reads every
pack and agent in it, creates the blueprint, publishes it, and applies it to this
project. For a blueprint this size that is a few seconds, and there is no second
confirmation to accept.

An import into a project that already has some of these object types stops at a
merge review instead: it shows the colliding types and applies nothing until you
confirm the merge.

## 6. Check the installed entry

The gallery comes back with the blueprint in its installed list: **personal-memory
v4.1.1**, a link to the repository it came from, and the packs it contributed.
That version is the blueprint's own, not the app's, and it is what **Check for
updates** compares against the repository.

!!! note "The copy in the project is the one that matters"

    Two projects can carry the same blueprint and drift apart, because what was
    applied lives in each project's own graph. The repository is only where a
    version, a re-check and an update come from.

## 7. See the schema it brought

Open **Schema**. The blueprint's six packs are now the project's type registry:
23 object types over 55 relationship types.

| Pack | Object types |
| --- | --- |
| `personal-memory` | `Person`, `Organization`, `Obligation`, `Task`, `Project`, `Event`, `FinancialTransaction`, `Contact`, `Place`, `Note`, `File`, `Fact`, `Habit` |
| `pm-companies` | `LegalEntity`, `Contract` |
| `pm-money` | `Account` |
| `pm-assets` | `Vehicle`, `Property`, `InsurancePolicy`, `Domain`, `Server` |
| `pm-legal` | `LegalCase` |
| `pm-knowledge` | `Guide` |

Every object type also declares how its own page is laid out — the sections a
`Property` shows, the fields a `Vehicle` previews inline — in the same pack that
declares the type. There is no per-project page configuration to write.

## 8. Meet the agents

Open **Agents**. Four agents came with the blueprint, each one a YAML file in the
repository.

| Agent | Trigger | What it does |
| --- | --- | --- |
| `memory-extractor` | manual | Extracts objects from emails, events and conversations, and gives each a stable key |
| `obligations-watcher` | daily, 05:00 server time | Turns upcoming obligations into tasks, and rolls an obligation forward when its task is done |
| `daily-briefing` | manual | A read-only briefing: events, deadlines, tasks, people, money, habits, notes |
| `personal-kb-agent` | chat | Saves and recalls on request |

None of them pins a model; each inherits the project's default at run time, which
is why the install needed no AI key.

!!! note "Configure a provider before the first run"

    A run needs a model to answer with. Add one under **Project settings →
    Providers**, then run an agent by hand: `daily-briefing` is read-only, so it
    is the least surprising first call.

## Updating, and the CLI

The installed entry keeps its source repository and its version, and its row in
**Blueprints** offers **Check for updates**. An update is applied from the same
row, and the blueprint's version moves when the repository's manifest does.

The `memory` CLI installs the same blueprint without a browser, which is what a
scripted or self-hosted setup uses:

```bash
memory blueprints install https://github.com/mkucharz/personal-memory-blueprint
```

!!! warning "An update adds types; it does not change them"

    Applying an update currently adds object types the project does not have yet
    and leaves existing ones alone
    ([emergent.memory#294](https://github.com/emergent-company/emergent.memory/issues/294)).
    To change a type that already exists, follow the blueprint's `MIGRATION.md`:
    uninstall, install, migrate.

## Next

The blueprint describes the graph; it does not fill it. Point it at a source —
Gmail, Google Calendar, Apple Contacts — or let `memory-extractor` read what you
paste in. The blueprint's
[README](https://github.com/mkucharz/personal-memory-blueprint) lists which types
each source writes, and what each key convention means.
