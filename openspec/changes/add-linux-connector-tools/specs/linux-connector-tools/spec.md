## Purpose

Exposes Linux platform tools — host information and per-root-scoped filesystem read/list/write/delete — through the connector daemon's existing tool registry and relay, with stable tool names, fail-closed path scope, recoverable (trash) deletes, and execution gated by the calling agent's tool policy.

## ADDED Requirements

### Requirement: Register configured Linux tool providers

The daemon SHALL register the Linux tool providers that are enabled in configuration through the same tool registry the other platforms use, SHALL NOT register disabled providers, and SHALL start successfully when no providers are enabled.

#### Scenario: Enabled providers are registered
- **WHEN** the daemon starts with one or more Linux tool providers enabled
- **THEN** those tools appear in the registered tool set served over the relay

#### Scenario: Disabled providers are absent
- **WHEN** a Linux tool provider is disabled or unconfigured
- **THEN** it is not registered and cannot be invoked through the relay

#### Scenario: Start with no tools
- **WHEN** no Linux tool providers are enabled
- **THEN** the daemon still connects and serves an empty tool set without error

### Requirement: Per-root capabilities

Each configured filesystem root SHALL declare its own capabilities (`read`, `write`, `delete`), the connector SHALL permit an operation only when the root that contains the target grants that capability, and a root SHALL grant no access until a capability is set.

#### Scenario: Capability granted by the matched root
- **WHEN** an operation targets a path inside a root that grants the capability
- **THEN** the operation is permitted (subject to the other requirements)

#### Scenario: Capability not granted
- **WHEN** an operation targets a path inside a root that does not grant that capability
- **THEN** the connector denies the operation with a clear error

#### Scenario: Missing capabilities are inert
- **WHEN** a root is configured without any capability
- **THEN** no operation on that root is permitted

#### Scenario: Invalid root configuration disables the provider
- **WHEN** the filesystem provider is enabled with invalid or empty roots
- **THEN** the provider is disabled and the reason is reported, rather than falling back to unrestricted access

### Requirement: Scoped read-only filesystem access

The filesystem read and list tools SHALL operate only within a root that grants them, SHALL reject paths that resolve outside every root (including via symlinks or `..`), and SHALL enforce a maximum read size with truncation reported.

#### Scenario: Read within a permitted root
- **WHEN** a request reads a file inside a root that grants read
- **THEN** the tool returns up to the configured size limit

#### Scenario: List within a permitted root
- **WHEN** a request lists a directory inside a root that grants read
- **THEN** the tool returns the directory entries

#### Scenario: Path outside allowed roots is denied
- **WHEN** a request targets a path that resolves outside every root
- **THEN** the tool denies the request with a clear error and returns no content

#### Scenario: Symlink escape is denied
- **WHEN** a path inside a root is a symlink that resolves outside the allowed roots
- **THEN** the tool denies the request

#### Scenario: Oversized read is bounded
- **WHEN** a requested file exceeds the maximum read size
- **THEN** the tool returns at most the configured limit and reports truncation

### Requirement: Scoped write access

The filesystem write tool SHALL operate only within a root that grants write, SHALL reject targets and parent directories that resolve outside a permitted root, SHALL write atomically, and SHALL bound write size.

#### Scenario: Write within a permitted root
- **WHEN** a write targets a path inside a root that grants write
- **THEN** the tool writes the content atomically and reports success

#### Scenario: Write outside allowed roots is denied
- **WHEN** a write targets a path or parent that resolves outside every permitted root
- **THEN** the tool denies the request and writes nothing

#### Scenario: Write without the capability is denied
- **WHEN** a write targets a root that does not grant write
- **THEN** the tool denies the request and writes nothing

#### Scenario: Oversized write is rejected
- **WHEN** a write exceeds the maximum write size
- **THEN** the tool rejects the write and leaves the existing file unchanged

### Requirement: Scoped delete via the desktop trash

The filesystem delete tool SHALL operate only within a root that grants delete, SHALL move the target to the XDG Trash rather than unlinking it, SHALL never silently fall back to permanent deletion, and SHALL reject targets outside a permitted root.

#### Scenario: Delete within a permitted root moves to trash
- **WHEN** a delete targets a path inside a root that grants delete
- **THEN** the target is moved into the trash `files/` directory with a matching atomic `info/` entry recording its original path

#### Scenario: Delete of a directory is recoverable as a unit
- **WHEN** a delete targets a directory inside a permitted root
- **THEN** the whole directory is moved to trash in one operation rather than being recursively unlinked

#### Scenario: Delete outside allowed roots is denied
- **WHEN** a delete targets a path that resolves outside every root, or a root that does not grant delete
- **THEN** the tool denies the request and removes nothing

#### Scenario: Trash unavailable does not erase
- **WHEN** the target cannot be trashed (for example across filesystems, on a read-only mount, or on an unsupported mount) and permanent deletion is not enabled
- **THEN** the connector returns an error and the target is left intact

#### Scenario: Permanent deletion requires explicit opt-in
- **WHEN** permanent deletion is enabled in configuration and trashing is impossible
- **THEN** the connector may permanently delete the target; otherwise it never does

#### Scenario: Symlink is trashed, not its target
- **WHEN** the delete target is a symbolic link
- **THEN** the link itself is trashed and the link target is left untouched

### Requirement: Host and system information

The connector SHALL provide a read-only host information tool reporting basic system facts (hostname, operating system and kernel version, uptime, disk usage, and memory usage) and SHALL NOT expose secret material such as environment variables or credential files.

#### Scenario: Report host facts
- **WHEN** the host information tool is invoked
- **THEN** it returns hostname, OS/kernel, uptime, disk, and memory information

#### Scenario: No secrets exposed
- **WHEN** the host information tool runs
- **THEN** its output contains no environment values or credential contents

### Requirement: Stable tool names for policy targeting

The connector SHALL expose stable, documented, namespaced tool names (for example `linux-host-info`, `linux-fs-list`, `linux-fs-read`, `linux-fs-write`, `linux-fs-delete`) so agent tool policies can target them, and renaming a tool SHALL be treated as a breaking change.

#### Scenario: Policy can target a tool by name
- **WHEN** an agent defines a tool policy for a Linux tool
- **THEN** the policy key matches the exact registered tool name

### Requirement: Policy is enforced by the agent, not the connector

The connector SHALL execute a tool call that is within configured scope without performing its own approval step, and SHALL return a normal error for calls outside scope. Whether a call runs silently, requires confirmation, or is blocked SHALL be determined upstream by the agent's tool policy, not by the connector.

#### Scenario: In-scope call executes without a connector prompt
- **WHEN** an agent invokes an in-scope Linux tool
- **THEN** the connector executes it and returns the result, without any connector-side approval prompt

#### Scenario: Out-of-scope call returns an error
- **WHEN** an agent invokes a Linux tool with a path outside scope
- **THEN** the connector returns an error result and the connector does not prompt for approval

### Requirement: Report the effective tool set

Status SHALL report the effective Linux tool set — which providers and capabilities are registered and which are disabled with the reason — in both human-readable and machine-readable output.

#### Scenario: Status lists effective tools
- **WHEN** status is requested
- **THEN** it lists the registered Linux tools and any disabled providers with reasons

### Requirement: Log mutating tool calls

The connector SHALL emit a structured log record for each write and delete invocation containing the tool name, the matched root, the resolved target path, and the outcome, and SHALL NOT log file contents or secret material.

#### Scenario: Successful mutation is logged
- **WHEN** a write or delete succeeds
- **THEN** a log record is emitted with the tool name, matched root, target path, and a success outcome

#### Scenario: Denied mutation is logged
- **WHEN** a write or delete is denied because the path or capability is not permitted
- **THEN** a log record is emitted with the tool name, matched root (if any), target path, and a denied outcome

#### Scenario: Contents are never logged
- **WHEN** any mutating tool call is logged
- **THEN** the record contains no file contents and no secret material

### Requirement: Least privilege by default

The connector SHALL NOT enable filesystem tools by default: only read-only host information is enabled by default, and the filesystem provider requires explicitly configured roots with explicit per-root capabilities.

#### Scenario: Defaults are read-only
- **WHEN** a user enables Linux tool support without further configuration
- **THEN** only host information is registered and no filesystem access is available

#### Scenario: Filesystem requires explicit roots
- **WHEN** configuration provides no roots
- **THEN** filesystem read, list, write, and delete tools are not registered

#### Scenario: No capability is implicit
- **WHEN** a root is configured without an explicit capability
- **THEN** no filesystem operation on that root is permitted
