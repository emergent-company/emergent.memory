# memory-connector

A small, headless Go CLI that registers your machine as an external MCP node on
a Memory project and serves local tools over the project's MCP relay. In v1 the
tool set is Apple Notes and Reminders on macOS, driven by AppleScript
(`osascript`) — no third-party dependencies.

The relay works over one **outbound** WebSocket, so no inbound ports or NAT
configuration are needed. Connected nodes appear under
**Settings → MCP nodes** in Memory and their tools can be attached to agents.

## Build

Go 1.26 (match `gateway/go.mod`). From this directory:

```sh
go build ./...
# binary at ./memory-connector
```

or install onto your PATH:

```sh
go install ./cmd/memory-connector
```

From the repo root, `task connector:build` builds into `connector/dist/` (the
host OS); `task connector:build-linux` cross-compiles `linux/amd64` and
`linux/arm64` binaries into the same directory.

## Help, version, and shell completion

The CLI is built on [cobra](https://github.com/spf13/cobra): every command has
rich `--help` output and shell completion is available for all of them.

```sh
memory-connector --help                  # list commands
memory-connector help                    # same as --help
memory-connector help auth               # help for a command group
memory-connector auth --help             # same, from the command itself
memory-connector auth login --help       # help + flags for a leaf command
memory-connector projects use --help
memory-connector --version               # memory-connector <version>
```

Run with no arguments to print the version, as before.

Every command follows the same exit-code contract: `0` on success, `1` on a
runtime error (bad config, unreachable server, …), and `2` on usage errors
(unknown command or flag, missing required flag).

### Install shell completion

```sh
# bash (current session)
source <(memory-connector completion bash)
# bash (persist; Linux)
memory-connector completion bash > /etc/bash_completion.d/memory-connector
# bash (persist; macOS with Homebrew)
memory-connector completion bash > "$(brew --prefix)/etc/bash_completion.d/memory-connector"

# zsh
memory-connector completion zsh > "${fpath[1]}/_memory-connector"

# fish
memory-connector completion fish > ~/.config/fish/completions/memory-connector.fish

# PowerShell
memory-connector completion powershell | Out-String | Invoke-Expression
```

`memory-connector completion --help` lists the supported shells.

## Install & release

Prebuilt binaries are published as GitHub release assets on
[`emergent-company/memory.web-ui`](https://github.com/emergent-company/memory.web-ui/releases).

### Tag & asset contract

- **Tag scheme:** `connector-vX.Y.Z` (e.g. `connector-v0.1.0`).
- **Asset:** `memory-connector_<VERSION>_<os>_<arch>.tar.gz`, containing the bare
  `memory-connector` binary, plus a matching `.tar.gz.sha256` checksum file.
- **Platforms:** `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.

### Install with `install.sh`

```sh
# newest connector release → $HOME/.local/bin
curl -fsSL https://raw.githubusercontent.com/emergent-company/memory.web-ui/master/connector/install.sh | bash

# pin a version and/or install directory
./connector/install.sh --version 0.1.0 --dir /usr/local/bin

# install the rolling dev build (tag `connector-dev`)
./connector/install.sh --channel dev
```

`install.sh` detects your OS/arch, resolves the newest `connector-v*` release
(`--channel stable`, the default) or the rolling `connector-dev` build
(`--channel dev`), verifies the `.sha256` when present, and installs the
binary. Use `--help` for all flags. `MEMORY_CONNECTOR_BASE_URL` overrides the
asset base URL (for mirrors or local smoke tests); the default is the GitHub
release download URL. `INSTALL_DIR` is the env-var form of `--dir`.

### Cut a release

```sh
git tag connector-v0.1.0
git push origin connector-v0.1.0
```

The `Connector Release` workflow (`.github/workflows/connector-release.yml`)
cross-builds the four platforms, packages the tarballs and checksums, and
publishes them to a GitHub release for the tag.

### Channels

- **stable** — `connector-vX.Y.Z` tags. Immutable, non-prerelease releases.
  This is what `install.sh` (default) and `resolve_version` pick up.
- **dev** — every push to `main` touching `connector/**` publishes a rolling
  prerelease under the fixed tag `connector-dev`, overwriting the previous dev
  build. Version embeds the short commit sha (`0.0.0-dev.<sha>`). Install with
  `install.sh --channel dev`.

Both channels publish a `manifest.json` release asset (app, channel, version,
per-platform asset URLs + sha256) as the seed for a future in-app updater.

## Configure: `init`

Non-interactive; flags capture the server, token, and instance identity:

```sh
memory-connector init \
  --server-url https://memory.emergent-company.ai \
  --token emt_<project-scoped-token> \
  [--project-id <uuid>] \
  [--instance-id <stable-id>]
```

- `server_url`, `token` are required; `token` must be a **project-scoped**
  `emt_…` token (project context comes from the token).
- `project_id` is optional; include it only when you want it recorded.
- `instance_id` defaults to `<hostname>-connector`. Choose a stable,
  machine-unique value if you run several connectors against one project.
- Configuration is written to `~/.config/memory-connector.yml` (mode `0600`)
  **only after** connectivity is verified against
  `GET /api/mcp-relay/sessions`. On failure the file is removed again, so an
  unusable config never persists.
- On Linux, `XDG_CONFIG_HOME` is honored: when set, the default path is
  `$XDG_CONFIG_HOME/memory-connector.yml`. Otherwise
  `~/.config/memory-connector.yml` (also the macOS default).
- Override the path with `--config <path>` (also accepted by `relay`, `daemon`,
  `install`, `uninstall`, and `status`).

```sh
# run over plain HTTP/WS for local development (see "wss vs ws" below)
memory-connector init --server-url http://localhost:8095 --token emt_xxx --project-id dev
```

## Run: `relay`

Foreground process. Connects, registers the instance + tool list, and serves
relayed tool calls until interrupted:

```sh
memory-connector relay
```

`SIGINT`/`SIGTERM` stop it cleanly. Supervision (launchd/brew/systemd) is out
of scope for v1 — run it however you like.

## Run as a daemon: `daemon` (Linux)

Same relay loop as `relay`, plus a single-instance lock so two daemons can't
serve the same config:

```sh
memory-connector daemon
```

It takes an exclusive `flock` on `<config>.lock` (e.g.
`~/.config/memory-connector.yml.lock`, mode `0600`) before connecting. If
another daemon already holds it, the command exits `1` with

```
memory-connector daemon: already running (lock held: <path>)
```

The lock is released on exit, so a crashed or stopped daemon does not leave a
stale lock behind (no pid files).

## Install as a systemd user service (Linux)

```sh
memory-connector install               # writes the unit, enables linger, daemon-reload, enable --now
memory-connector install --no-linger   # skip loginctl enable-linger
memory-connector uninstall             # disable --now (best-effort), remove unit, daemon-reload
```

`install`/`uninstall` are **Linux-only** and always use `systemctl --user` — no
root required. The unit is written to
`$XDG_CONFIG_HOME/systemd/user/memory-connector.service` when
`XDG_CONFIG_HOME` is set, otherwise
`~/.config/systemd/user/memory-connector.service`. `install --bin <path>`
overrides the binary in `ExecStart` (defaults to the running executable). Both
commands are idempotent. On other platforms they fail with
`... only supported on Linux`.

No `XDG_RUNTIME_DIR` setup is required: the connector fills in
`XDG_RUNTIME_DIR` (`/run/user/<uid>`) and `DBUS_SESSION_BUS_ADDRESS` for its own
`systemctl`/`loginctl` calls, so `memory-connector install` works from a fresh
non-login shell (`ssh host 'memory-connector install'`, cron, etc.).

`install` also runs `loginctl enable-linger <uid>` (best-effort — a failure only
warns and the install proceeds) so the user service starts at boot and keeps
running after logout. Pass `--no-linger` to skip it.

One systemd quirk remains, but it is not the connector's: a plain
`ssh host 'systemctl --user …'` or `journalctl --user …` still needs the
caller's own user bus, so use `ssh -t host` (or set `XDG_RUNTIME_DIR` yourself in
that interactive shell). The connector's own `install`/`uninstall`/`status`
commands need nothing.

## Check: `status`

```sh
memory-connector status
```

Prints the local instance id and version, the locally registered tool names
(plus a platform note when Apple tools are unavailable), and a best-effort hub
check against `GET /api/mcp-relay/sessions`: whether this instance id is
currently connected, the hub-registered tool count, and a mismatch warning when
that count differs from the local tool set (restart `relay` to re-register).

Exits `0` even when the instance is not connected or the hub is unreachable —
non-zero only on config problems.

### Machine-readable: `status --json`

`--json` prints the same snapshot as a stable JSON document instead of the
human text (missing config still exits non-zero and emits no JSON):

```sh
memory-connector status --json
```

```json
{
  "schema_version": 1,
  "instance_id": "mbp-1",
  "version": "0.1.0",
  "project": { "id": "9f1c1a4b-9d11-4a2e-9a6d-1234567890ab" },
  "tools": ["notes_search", "reminders_list"],
  "hub_state": "connected",
  "hub_detail": { "hub_tool_count": 2, "local_tool_count": 2, "session_count": 0 }
}
```

`project` is present only when the config records a `project_id`. `hub_state`
is one of `connected`, `not_connected`, `auth_failed`, `unreachable`, or
`missing_config`. `schema_version` is bumped on any breaking shape change.
Exit codes and the human text output are unchanged.

## Self-update: `upgrade`

Upgrade the connector in place from its GitHub releases:

```sh
memory-connector upgrade                    # newest release
memory-connector upgrade --check            # report current + latest only
memory-connector upgrade --force            # reinstall the current version
memory-connector upgrade --version 0.2.0    # pin a specific release
```

`upgrade` resolves the newest `connector-v*` release, downloads the archive for
this OS/architecture, verifies its published SHA-256 when reachable, and
atomically replaces the running executable (a temp file in the target's
directory, `chmod`, then `rename` over the target; the original is kept if the
swap fails). It exits `0` when already up to date, `--check` never downloads,
and a current version that is `dev` or unparseable is treated as older than any
release. Like the other commands: `0` success, `1` runtime error (API,
download, checksum, or replace failure), `2` usage.

### Release contract

Connector releases are published from the shared
[`emergent-company/memory.web-ui`](https://github.com/emergent-company/memory.web-ui/releases)
repo, so the resolver enumerates releases and picks the highest
`connector-v*` semver rather than trusting `/releases/latest`:

- **Tag:** `connector-vX.Y.Z` (e.g. `connector-v0.1.0`).
- **Asset:** `memory-connector_<X.Y.Z>_<os>_<arch>.tar.gz`, containing the bare
  `memory-connector` binary, plus a matching `.tar.gz.sha256`.
- **Platforms:** `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- **Mirrors:** `MEMORY_CONNECTOR_BASE_URL` overrides the asset base (default
  `https://github.com/emergent-company/memory.web-ui/releases/download`) and
  `MEMORY_CONNECTOR_API_BASE_URL` overrides the GitHub API base (default
  `https://api.github.com`).

## Sign in: `auth`

Account sign-in uses the OAuth 2.0 **device authorization** flow. It is
separate from the project-scoped `emt_*` token used by `relay`: OAuth gives an
account identity (email) that later commands use to list projects and mint
tokens.

```sh
memory-connector auth login                 # server from config server_url
memory-connector auth login --server https://api.example.ai
memory-connector auth login --client-id 390138928478289930 --issuer https://<issuer>
memory-connector auth status [--json]
memory-connector auth access-token [--server ...] [--json]
memory-connector auth import --server <url> [--json]   # session JSON on stdin
memory-connector auth list [--json]
memory-connector auth logout
```

`auth login` resolves the server's OIDC issuer via `GET /api/auth/issuer`,
discovers the OAuth endpoints, then prints a URL and a code:

```
To sign in, visit https://<issuer>/device?user_code=ABCD-EFGH
and enter code: ABCD-EFGH
Waiting for authorization...
memory-connector auth: signed in as you@example.com
```

The flow uses the Memory CLI's public OAuth client id
`362800068257972227` and scopes `openid profile email offline_access`
(`offline_access` is required for the refresh token). On success the identity
is confirmed with `GET /api/auth/me`.

`--client-id` overrides that default so a deployment can use its own registered
OAuth client. The built-in id is **prod-only**: in dev it answers
`invalid_client`, so use the dev native client `390138928478289930` there.
`--issuer` uses the given issuer directly, skipping `GET /api/auth/issuer`. The
effective client id is persisted with the session, so later refreshes
(`auth access-token`) use the same client even when the flags are omitted.

`auth status` prints the server, email, issuer, and token expiry; `--json`
emits the same as a stable document. `auth logout` clears the account and is
idempotent (exit `0` even when already signed out).

`auth access-token` prints the stored account's OAuth access token so another
local process (for example a GUI) can call the Memory API as the signed-in
user. It resolves the server from `--server` or the config's `server_url`,
refreshes the session first when the token is expired or within 60 seconds of
expiry, and never reads or prints the refresh token. Text mode prints the raw
token only; `--json` emits
`{"schema_version":1,"server_url":"…","access_token":"…","expires_at":"<RFC3339>"}`.
It exits `1` when not signed in (with the same hint as `projects`) and `2` on
usage errors.

`auth import --server <url>` reads a single session JSON object from **stdin**
(never argv, which `ps` exposes) and stores it, for clients such as the macOS
app that can read sessions the CLI cannot:

```sh
# the app pipes the session; tokens never touch the command line
memory-connector auth import --server https://api.example.ai [--json]
```

The payload is
`{"access_token":"…","refresh_token":"…","expires_at":"<RFC3339, optional>","issuer":"…","client_id":"…","email":"…"}`;
`access_token` is required. The session is stored as-is (no `/api/auth/me`
round-trip; `email` from the payload is trusted) and `--server` becomes the
active account. It prints the `auth status` shape and never echoes the tokens.
Exit codes: `0` success, `1` runtime error, `2` usage.

`auth list [--json]` enumerates the stored servers for a multi-environment UI:

```
memory-connector auth list
https://api.example.ai  you@example.com  active  2030-01-02T03:04:05Z
https://staging.example.ai  -  -  -
```

`--json` emits
`{"schema_version":1,"accounts":[{"server_url":"…","email":"…","issuer":"…","signed_in":true,"active":true,"expires_at":"<RFC3339>"}]}`.
An empty list is exit `0`.

### Native app sign-in: `auth start` / `auth complete`

Native apps (for example the macOS app) can own the browser and custom-scheme
callback while the connector owns PKCE, the token exchange, and storage:

```sh
memory-connector auth start \
  --redirect-uri com.emergent.memory.connector://callback \
  --client-id 390138006318678019 \
  --issuer https://<issuer> [--server https://api.example.ai] [--json]
memory-connector auth complete --login-id <id> --code <code> --state <state> [--server ...] [--json]
memory-connector auth cancel --login-id <id>
```

`auth start` prints the `authorize_url`, `login_id`, and `state` (or emits them
as JSON). `--client-id` overrides the default public client id
(`362800068257972227`) so an app can use its own registered OAuth client
(e.g. the macOS app's `390138006318678019` prod / `390138928478289930` dev).
`--issuer` uses the given issuer directly, skipping
`GET /api/auth/issuer`, and lets `--server` be omitted. The effective client id
is stored in the one-shot pending record, so `auth complete` needs no extra
flag.

Sessions live **in the connector's own store**, not the Memory CLI's
`~/.memory/`: one `0600` file per server under
`~/.config/memory-connector/accounts/`, plus an `active` pointer. The
connector never writes to `~/.memory/credentials.json`
(`account.ImportCLISession` can read it one-way if you want to reuse an
existing CLI sign-in).

## Projects: `projects`

After `auth login`, list and select the project this machine relays for:

```sh
memory-connector projects list [--json]
memory-connector projects use <id-or-name> [--instance-id <id>] [--disabled-tools <a,b>] [--json]
memory-connector projects current [--json]
```

`projects list` calls `GET /api/projects` with the signed-in account's OAuth
access token and marks the active project. `projects use` resolves the project
(by id or name), remembers it as the per-account active project, ensures a
project token exists, and materializes the engine config at the `--config` path
so `relay` can use it.

The connector mints its own least-privilege project token with scopes
`["data:read"]` (matching the macOS app), named `connector-<hostname>`. A token
is **reused** from the local store when one already exists for that
project/server and is only minted when missing; it is stored `0600` under
`~/.config/memory-connector/tokens/`. `auth logout` clears that account's
stored tokens, so the next sign-in mints fresh ones rather than reusing stale
credentials. Project selection is stored per account under
`~/.config/memory-connector/projects/`.

Materializing the config preserves any existing `instance_id` and
`disabled_tools`; a missing `instance_id` defaults to `<hostname>-connector`.
`projects use --instance-id <id>` overrides the written instance id and
`--disabled-tools a,b` replaces the disabled-tool list for that project (pass
an empty `--disabled-tools ""` to clear it); omitting either flag preserves the
existing value. This lets a GUI apply a per-project profile in one call.

## API layer

The connector talks to the Memory API through `internal/memoryapi`, a thin
wrapper over the Memory core Go SDK
(`github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`) that keeps
SDK types out of the rest of the connector. It currently exposes project
listing and API-token create/list/revoke, authenticated with a project-scoped
`emt_*` token. Secrets such as OAuth sessions are kept in
`internal/secretstore`, a 0600 file store with atomic writes.

## Tools

The connector registers a platform-appropriate tool set. On **Linux** it serves:

| Tool | What it does | Inputs |
|------|--------------|--------|
| `linux-host-info` | hostname, OS, kernel, uptime, disk and memory usage | none |
| `linux-fs-list` | list a directory inside an allowed root | `path` (req) |
| `linux-fs-read` | read a file inside an allowed root (bounded) | `path` (req) |
| `linux-fs-write` | atomically write a file inside an allowed root | `path`, `content` (req) |
| `linux-fs-delete` | move a file or directory to the XDG trash | `path` (req) |

On **macOS** the Apple tools are:

| Tool | What it does | Inputs | Result |
|------|--------------|--------|--------|
| `notes_search` | find notes by text in an optional folder | `query` (req), `folder`?, `limit`? (default 20) | `notes`: `[{name, snippet}]` |
| `notes_create` | create a note in the default folder | `title` (req), `body`? | `{title, created}` |
| `reminders_list` | list reminders from an optional list; each reminder carries an addressable `id`, its `list`, and completion state | `list_name`?, `include_completed`? (default false), `include_notes`? (default false) | `reminders`: `[{id, name, list, due_date (RFC3339 or null), completed, notes?}]` |
| `reminders_lists` | enumerate reminder lists so one can be named unambiguously | none | `lists`: `[{id, name, count}]` |
| `reminders_add` | add a reminder to a list (the default list when `list_name` is omitted) | `title` (req), `list_name`?, `due_date`? (RFC3339), `notes`? | `{title, added}` |
| `reminders_update` | change an existing reminder by `id`: title, due date (set/clear), notes (set/clear), priority, completion, or list (move); only provided fields change | `id` (req), `title`?, `due_date`? \| `clear_due`?, `notes`? \| `clear_notes`?, `priority`?, `completed`?, `list_name`? | `{reminder: {id, name, list, due_date, completed, notes?}}` |
| `reminders_delete` | delete an existing reminder by `id` | `id` (req) | `{id, deleted}` |

`reminders_update` and `reminders_delete` require the embedded EventKit helper
(`memory-reminders`), because iOS/macOS reminder identifiers are only
addressable through EventKit — there is no safe AppleScript id-based fallback.
On a host without the embedded helper those two tools return the
platform-unavailable error. The read/add tools prefer the helper when present
and fall back to AppleScript otherwise.

On other platforms the connector still runs but registers no tools; `status`
shows the reason. Tools are never implicitly exposed: the filesystem tools are
scoped to configured roots and per-root capabilities.

### Linux `tools` configuration

Add a `tools` block to the connector config (all fields optional — existing
configs keep working, with host info on and no filesystem roots):

```yaml
tools:
  linux:
    host_info:
      enabled: true                 # absent / nil => enabled
    filesystem:
      enabled: true                 # absent / nil => enabled when roots exist
      roots:
        - { path: /srv/scratch, read: true, write: true, delete: true }
      max_read_bytes: 1048576        # default 1 MiB
      max_write_bytes: 1048576       # default 1 MiB
      allow_permanent_delete: false  # trash by default; permanent only if true
```

`linux-fs-list`/`linux-fs-read` need a root with `read: true`;
`linux-fs-write` needs `write: true`; `linux-fs-delete` needs `delete: true`.
Writes are atomic and bounded by `max_write_bytes`; deletes move paths to the
XDG trash (`$XDG_DATA_HOME/Trash`, default `~/.local/share/Trash`) unless
`allow_permanent_delete` is set. Every write/delete is audited (tool, root,
path, outcome) — file contents are never logged. `status` lists the effective
tools and, when tools are unavailable, a `tools-disabled:` line with reasons.
`disabled_tools` still hides a tool from the hub entirely (calls then fail as
disabled).

### Recommended agent tool policy

When attaching these tools to an agent, keep the full allowlist, ask by default,
and require explicit confirmation for the mutating filesystem tools (design D8):

```json
{
  "defaultToolPolicy": "ask",
  "tools": [
    "linux-host-info",
    "linux-fs-list",
    "linux-fs-read",
    "linux-fs-write",
    "linux-fs-delete"
  ],
  "toolPolicies": {
    "linux-fs-write": { "confirm": true },
    "linux-fs-delete": { "confirm": true }
  }
}
```

The tool names must match the constants exactly. On macOS, list the Apple tool
names instead.

## macOS Automation permission

The first AppleScript run triggers a macOS permission prompt. Grant
**Automation** access for the host process/terminal that runs `memory-connector`
in **System Settings → Privacy & Security → Automation** (Notes and Reminders).

If a tool call fails because permission was denied or revoked, the error
mentions error `-1743` and tells you to grant Automation permission for the
host. After granting, restart the connector (permission checks happen at
script launch).

## `wss` vs `ws`

Use **`wss://`** (or `https://` server URLs) in production — the bearer token
travels in the WebSocket handshake header and must not go over plaintext.
`http://`/`ws://` is allowed for local development only; `init`/`relay` accept
it without complaint, so keep an eye on the scheme you configure.

## Security notes

- The token is stored in `~/.config/memory-connector.yml` with mode `0600`.
- The token is project-scoped: the relay only ever sees the project the token
  belongs to.
- No inbound ports are opened; the relay connection is outbound only.

## Mac app sign-in (Zitadel / OAuth)

The macOS app can sign users in with the instance's Zitadel identity provider:
the app opens the **hosted web login** in the browser and returns via the custom
scheme `com.emergent.memory.connector://callback` (Authorization Code + PKCE,
no client secret). The signed-in user session is used for identity and project
listing; on project selection the app **mints a project-scoped `emt_*` token**
(name `Memory Connector (macOS)`, least-privilege `data:read`) and stores it in
the Keychain, so the headless engine relays independently of the browser
session. A manually pasted token keeps working (Connection → Advanced — API
token fallback).

### Operator setup (Zitadel console, one-time)

1. Create an application of type **Native** (Console → project → Applications).
2. Name it e.g. `Memory Connector`; leave **auth method = none (PKCE)** — no
   client secret is shipped in the app.
3. Add redirect URI `com.emergent.memory.connector://callback` and a matching
   **post-logout redirect URI** for sign-out.
4. Enable **Development mode** for the app: a non-HTTPS custom scheme requires
   it.
5. Recommended: set **Token Type = JWT**; keep refresh tokens enabled.
6. Copy the **Client ID** into the app (Connection → Memory Account); set the
   **Issuer URL** to the Zitadel custom domain (no trailing slash).

### Troubleshooting

- **Issuer mismatch** — the issuer must match discovery exactly; no trailing `/`.
- **Callback never returns** — check the app is registered and Development mode
  is on; the built app declares the scheme via `CFBundleURLTypes`.
- **Signed out unexpectedly** — Zitadel refresh tokens are single-use and
  rotate; the app serializes refreshes and re-prompts sign-in if a rotated token
  is lost.
- **Token-based identity missing** — when only a manual token is configured the
  app shows identity as unavailable (expected); sign-in is required for the
  account and project switcher.

## Layout

```
cmd/memory-connector   init / relay / daemon / auth / projects / install / uninstall / status / upgrade subcommands
internal/config        YAML config, load/save/validate, sessions probe
internal/appletools    AppleScript runner + Notes/Reminders tool handlers
internal/relay         MCP relay wire codec + client (register, dispatch, reconnect)
internal/toolreg       MCP tool registry
internal/memoryapi     thin wrapper over the Memory core Go SDK
internal/secretstore   0600 atomic file store for secrets
internal/status        structured status document (text + JSON)
internal/account       per-server OAuth sessions + device-flow login
internal/project       project selection + per-project token store
internal/supervise     shared engine supervision policy
internal/tools         platform-aware effective tool selection
internal/tools/linux   Linux host-info + root-scoped filesystem tools
internal/upgrade       self-update: release resolution, download, checksum, replace
```

See `NOTICE` for MIT attribution of behavior and Apple-tool concepts derived
from the Diane project.

## Where secrets are stored

The app keeps its secrets in **0600 files** under
`~/.config/memory-connector/` (directory 0700, atomic writes):

| File | Contents |
|------|----------|
| `session.json` | OIDC session (access/refresh/id tokens, expiry, issuer, client id) |
| `projects-tokens.json` | Per-project connector tokens (`[projectId: token]`) |
| `manual-token` | Manual fallback API token (no OIDC) |

Files are identity-independent, so they **persist across rebuilds** and there
are no Keychain access prompts. (The engine already stores its `emt_*` token in
the same 0600 config directory.)

On first run after upgrading, the app migrates any secrets it previously stored
in the login Keychain into these files (one-time, best-effort) and deletes the
legacy Keychain items. Non-secret settings (server URL, instance id, disabled
tools, OIDC issuer/client id) remain in `UserDefaults`.

### Signing (optional)

Automated code signing is no longer required for secrets to work. Ad-hoc builds
are fine; the only rebuild-related prompts that can still re-appear are macOS
**Automation (TCC)** grants for Notes/Reminders, which are keyed to the app's
code identity. If you want those to persist too, you can optionally give builds
a stable identity: run `tools/mac-setup-signing.sh` once, then
`tools/mac-build.sh --install` signs automatically. See the script headers for
the manual Apple Development alternative (`tools/mac-sign.sh`).
