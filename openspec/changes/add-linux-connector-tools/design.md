## Context

See `proposal.md — Why`. Prerequisites: `add-linux-connector` (daemon, systemd unit, and the shared tool registry / relay). That change deliberately registers no Linux tools; this one adds them.

Approval is already a platform concern: a Memory agent definition carries `tools` (allowlist), `bannedTools`, per-tool `toolPolicies` (`confirm`, `disabled`) and a `defaultToolPolicy` of `allow` / `ask` / `deny`, enforced by the executor (a `confirm`/`ask` pauses the run for human approval). The connector is a tool provider, not an approval authority.

Deletion has a platform convention too: the **XDG Trash specification** — not a syscall — so deletes are recoverable by default.

## Goals / Non-Goals

**Goals:**

- Linux tool providers plugged into the existing registry/relay with no protocol change, including write and delete tools.
- Fail-closed, per-root scope; recoverable deletes; stable tool names the agent policy can target.
- Effective tool set visible in status.

**Non-Goals:**

- Connector-side approval prompts or a permission engine (that is the agent's `toolPolicies` / `defaultToolPolicy`).
- Shell command execution (deferred).
- Trash management tools (list/restore/empty) — left to desktop trash tooling.
- Sandboxing/containers, and macOS/gateway changes.

## Decisions

### D1 — Reuse the existing tool provider interface and registry

Each capability is a provider implementing the same interface the registry consumes (`name`, tool list, invoke). No relay-protocol or gateway change; mirrors how the macOS `appletools` providers register.

### D2 — Tool set: host information (default) + filesystem read/list/write/delete (opt-in)

- **Host information** is enabled by default: hostname, OS/kernel, uptime, disk, memory. No secrets.
- **Filesystem** (`linux-fs-list`, `linux-fs-read`, `linux-fs-write`, `linux-fs-delete`) is enabled only when roots are configured.
- **Why write tools are acceptable:** gating is owned by the agent's tool policy (`allow`/`ask`/`deny`, per-tool `confirm`/`disabled`), and the connector bounds blast radius per root.

### D3 — Fail-closed path scoping (read and write)

The filesystem provider resolves every requested path (absolute + `EvalSymlinks`) and verifies containment in an allowed root with a separator-aware prefix check (not a naive string prefix), rejecting escapes via symlinks or `..`. Write targets and their parents are re-checked the same way. Roots are resolved identically at load time.

- **Why:** naive prefix checks (`/data` matching `/database`) and symlink escapes are the classic traversal bugs.
- **Trade-off:** resolving symlinks rejects some legitimate symlinked layouts; acceptable for a security boundary.

### D4 — Per-root capabilities

Each configured root declares its own capabilities (`read`, `write`, `delete`); the scope check returns the **matched root and its capabilities**, and each operation is permitted only if the matched root grants it. A root with no capabilities is inert; no roots configured disables the filesystem provider.

- **Why:** grants read-only access to sensitive trees and read/write/delete to scratch trees, instead of an all-or-nothing global toggle; blast radius is per root.
- **Alternative considered:** a single global `writes` toggle — rejected as too coarse.

### D5 — Bounded, safe I/O

`max_read_bytes` bounds reads (truncation reported). Writes are atomic (temp file in the target directory, then rename) and bounded by `max_write_bytes`; writes never silently succeed partially.

### D6 — Conservative defaults

Host information is enabled by default; the filesystem provider requires explicit roots with explicit per-root capabilities. Nothing broad is ever enabled implicitly: a root grants no access until a capability is set.

### D7 — Effective tool set in status

Status (`status` / `status --json`) lists registered Linux tools and disabled providers (with reasons, e.g. missing roots/capabilities), replacing the "no tools available" note when tools are enabled.

### D8 — Policy authority is the agent; the connector implements no approval loop

The connector exposes tools and enforces scope. Whether a call runs silently, pauses for confirmation, or is blocked is decided by the agent definition:

- `tools` — allowlist; a tool absent from it is not offered to the agent.
- `bannedTools` — hard block.
- `toolPolicies[name]` — `{confirm, message, disabled}` for specific tools.
- `defaultToolPolicy` — `allow` / `ask` / `deny` fallback.

A scope denial from the connector is returned as a normal tool error; the connector does not prompt.

#### Recommended agent policy for the Linux tools

Mutating tools SHOULD require confirmation. Recommended definition fragment:

```json
{
  "tools": [
    "linux-host-info",
    "linux-fs-list",
    "linux-fs-read",
    "linux-fs-write",
    "linux-fs-delete"
  ],
  "defaultToolPolicy": "ask",
  "toolPolicies": {
    "linux-fs-write":  { "confirm": true, "message": "Write to disk? {args_json}" },
    "linux-fs-delete": { "confirm": true, "message": "Delete? {args_json}" }
  }
}
```

- `defaultToolPolicy: "ask"` means any unlisted tool pauses for approval.
- Per-tool `confirm: true` makes write/delete pause even under an `allow` default.
- To hard-block instead of asking, add the tool to `bannedTools` or set `"disabled": true` in its `toolPolicies` entry.
- A definition that omits `linux-fs-write` / `linux-fs-delete` from `tools` cannot call them at all.

### D9 — Stable tool naming for policy targeting

Tool names are stable and namespaced (`linux-host-info`, `linux-fs-list`, `linux-fs-read`, `linux-fs-write`, `linux-fs-delete`). Agent policy keys reference these exact names, so renaming is a breaking change and requires a deprecation note.

### D10 — Delete uses the desktop trash by default; permanent delete is opt-in

`linux-fs-delete` moves the target to the **XDG Trash** by default, so deletion is recoverable:

- Home trash is `$XDG_DATA_HOME/Trash` (default `~/.local/share/Trash`), with `files/` and `info/`; the `info/<name>.trashinfo` entry (`[Trash Info]`, percent-encoded absolute `Path=`, `DeletionDate=YYYY-MM-DDThh:mm:ss` local time, **no** tz offset) is created atomically (`O_CREAT|O_EXCL`) before the payload is moved.
- Same filesystem: `rename()` into `files/`. Cross-filesystem (`EXDEV`): use `$topdir/.Trash/$uid` (only if a non-symlink directory with the sticky bit) else `$topdir/.Trash-$uid` (0700), creating it if needed; if neither is usable, **error**.
- Deleting a directory moves the whole tree via a single rename (no recursive walk), preserving recoverability.
- **Permanent delete requires explicit opt-in** (`allow_permanent_delete: true`). The connector SHALL NOT silently `unlink` when trashing fails; on failure it returns an error unless permanent fallback is explicitly enabled.
- Trashing a symlink trashes the link itself (use `lstat`), never the target. Long basenames are handled (truncate/probe) to avoid `ENAMETOOLONG`. `/`, mount points, and the Trash directory itself cannot be trashed.

- **Why:** recoverable deletes are a much safer default for an agent-invoked tool; the spec forbids silent erasure when the trash is unavailable.
- **Alternative considered:** permanent `unlink` only — rejected (unrecoverable); `gio`/`trash-cli` shell-out — rejected (external runtime dependency breaks the single-static-binary promise).

### D11 — Implement the trash spec in-process

The connector implements the XDG Trash mechanics directly rather than shelling out. No complete, maintained, importable Go library exists (`Bios-Marcel/wastebasket/v2` lacks cross-device handling; `gdu`'s is not a stable API), and a headless daemon should not depend on `gio`/`trash-cli` being installed.

- **Why:** keeps the standalone-binary guarantee and makes the cross-device policy explicit.
- **Optional:** if trashing fails and permanent fallback is not enabled, the connector may probe `gio trash` / `trash-put` as a last resort before erroring.

### D12 — Log mutating tool calls

Each write and delete invocation emits one structured log record (to the daemon log / journald) containing the tool name, the matched root, the resolved target path, and the outcome (success, denied, or error). File contents are never logged, and read-only tools are not logged by default to avoid noise.

- **Why:** an agent-invoked mutating tool should be auditable even though the connector does not prompt; the systemd journal is the natural sink with rotation already handled.
- **Alternative considered:** a separate connector-side audit file — rejected; duplicates journald/rotation and adds a new artifact to manage.

Config sketch (connector YAML):

```yaml
tools:
  linux:
    host_info: { enabled: true }
    filesystem:
      enabled: true
      roots:
        - { path: /srv/scratch, read: true, write: true, delete: trash }
        - { path: /etc/app,    read: true, write: false, delete: false }
      max_read_bytes: 1048576
      max_write_bytes: 1048576
      allow_permanent_delete: false   # deletes fall back to unlink only when true
```

## Risks / Trade-offs

- **[Agent policy set to `allow` runs writes silently]** → Documented trust decision; per-root caps still bound the blast radius; ship the recommended `ask`/`confirm` policy for mutating tools.
- **[Path traversal / symlink TOCTOU]** → Resolve and containment-check call-time (including write targets and parents); fail closed on any resolution error.
- **[Trash unavailable (cross-device, read-only, network/removable, root-squash)]** → Return an error; only fall back to permanent delete when `allow_permanent_delete: true`. Never silent `unlink`.
- **[Trash growth]** → Trashed items accumulate under `$XDG_DATA_HOME/Trash`; documented, user-managed; no connector-side auto-empty.
- **[Sensitive files inside allowed roots]** → Roots and their capabilities are user responsibility; host-info excludes env/credentials.
- **[Large or binary I/O]** → `max_read_bytes` / `max_write_bytes` bounds + truncation/atomicity reporting.

## Migration Plan

- Purely additive config; with no `tools.linux` section the daemon keeps the current no-tools behavior.
- When enabled, status/tools output lists tools; the `linux-connector` capability's "no tools available" wording is superseded and reconciled when `add-linux-connector` is archived.
- Adding/renaming tools is a policy-visible change: document names so agents can target them.

## Open Questions

- Whether shell execution is ever exposed (currently a non-goal); if so, whether it rides the same agent-policy gate and with what command allowlist.
