## Why

The Linux connector delivered by `add-linux-connector` connects and relays, but registers **no local tools**, so it offers the hub nothing Linux-specific. This change adds Linux platform tools — including write tools — so the connector is useful as an MCP node, the counterpart to the macOS connector's Notes/Reminders tools.

Approval is already solved above the connector: a Memory **agent** carries a tool policy (`tools` allowlist, `bannedTools`, per-tool `toolPolicies` with `confirm`/`disabled`, and a `defaultToolPolicy` of `allow`/`ask`/`deny`) that the executor enforces, pausing a run for human confirmation when required. The connector therefore exposes tools with stable names and enforces filesystem **scope**; it does not implement its own approval loop.

## What Changes

- **Add Linux platform tool providers** implemented with `internal/tools/linux`, registered through the existing tool registry and served over the existing relay (no protocol or gateway change).
- **Tool set:**
  - **Host information** — read-only; enabled by default.
  - **Filesystem** — `read`, `list`, `write`, and `delete`, scoped to explicitly configured root paths with **per-root capabilities**. Deletes go to the **XDG trash** by default (recoverable); permanent deletion is an explicit opt-in.
- **Stable tool names** so agent tool policies (`toolPolicies`, `defaultToolPolicy`) can target and gate them; renaming a tool is a breaking policy change.
- **Scope is connector-enforced; approval is agent-enforced.** The connector confines all filesystem operations to configured roots (fail-closed), and the agent's policy decides whether each tool runs silently, asks for confirmation, or is blocked. No connector-side approval UI.
- **Configuration:** per-provider enable flags; per-root capabilities (`read` / `write` / `delete`); read/write size bounds; and an explicit `allow_permanent_delete` opt-in. Invalid or missing scope, or a root with no capability, grants no access rather than widening it.
- **Report the effective tool set** in `status` / `status --json`.
- **Replace the "no local tools" note** when tools are enabled, keeping the zero-tools case clean.
- **Deferred:** shell command execution (it could later use the same agent-policy gate, but is out of scope here).

## Capabilities

### New Capabilities
- `linux-connector-tools`: Linux platform tool providers exposed by the connector daemon — host information and scoped filesystem read/list/write/delete — with stable names, fail-closed scope, and policy that is owned by the agent.

### Modified Capabilities
<!-- None here: `linux-connector` is introduced by `add-linux-connector` and is not yet archived into openspec/specs/, so it cannot be modified from this change. The "no local tools" availability wording in that capability is superseded once tools exist; reconcile it when `add-linux-connector` is archived. -->

## Impact

- **`connector/` Go module**: new `internal/tools/linux` providers, config schema additions (enable flags, scope, size bounds), and tool-list reporting in status.
- **Depends on `add-linux-connector`** for the daemon, systemd packaging, and tool-registry/relay integration.
- **Policy integration**: relies on the existing Memory agent tool-policy fields (`tools`, `bannedTools`, `toolPolicies`, `defaultToolPolicy`). No new policy surface is added; the connector is not the approval authority.
- **Security**: scope is fail-closed (symlink-safe, separator-aware containment) and **per-root** (`read`/`write`/`delete`); deletes are recoverable via the XDG trash and never silently unlinked; permanent deletion requires explicit opt-in. Execution is gated by agent policy. The recommended agent policy requires confirmation for mutating tools (`toolPolicies` `confirm` on `linux-fs-write`/`linux-fs-delete`, `defaultToolPolicy: "ask"`), because an `allow` default lets writes run silently; this recommendation ships in the connector docs.
- **No gateway/server changes** and no MCP relay protocol changes.
