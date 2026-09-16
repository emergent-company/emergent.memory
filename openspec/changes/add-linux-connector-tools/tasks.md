## 1. Provider scaffolding

- [ ] 1.1 Add an `internal/tools/linux` provider package implementing the existing tool registry interface (name, tool list, invoke) and wire registration into the daemon; verify a unit test registers a provider and invokes a tool through the registry

## 2. Host information tool

- [ ] 2.1 Implement the host information provider (hostname, OS/kernel, uptime, disk, memory) as `linux-host-info`; verify unit tests assert each field is populated and that output contains no environment values or credential contents

## 3. Scoping, read, and list

- [ ] 3.1 Implement the path-scoping helper that resolves a target (absolute + `EvalSymlinks`) and returns the matched root plus its capabilities, using separator-aware containment (not naive prefix); verify unit tests cover inside/outside roots, `..` traversal, symlink escape, prefix confusion (`/data` vs `/database`), and capability-not-granted denial
- [ ] 3.2 Implement `linux-fs-read` and `linux-fs-list` gated on the matched root's read capability, with a maximum read size and truncation reporting; verify unit tests cover read within a permitted root, denial outside/without capability, oversized-file truncation, symlink escape, and directory listing

## 4. Write access

- [ ] 4.1 Implement `linux-fs-write` gated on the matched root's write capability (atomic temp + rename, bounded by `max_write_bytes`, scope-checked for target and parent); verify unit tests cover in-root write, atomicity (no partial file on failure), out-of-root denial, capability denial, oversized-write rejection, and parent-symlink escape

## 5. Delete via the XDG trash

- [ ] 5.1 Implement the XDG home-trash mechanics under `$XDG_DATA_HOME/Trash` (default `~/.local/share/Trash`): auto-create `files/`+`info/`, create the `info/<name>.trashinfo` entry atomically (`O_CREAT|O_EXCL`) with `[Trash Info]`, percent-encoded absolute `Path=`, and `DeletionDate=YYYY-MM-DDThh:mm:ss` local time (no tz offset), then move the payload; verify unit tests against a temp `XDG_DATA_HOME` including collision handling and long-basename (`NAME_MAX`) safety
- [ ] 5.2 Implement cross-filesystem trashing (`EXDEV`): use `$topdir/.Trash/$uid` only when it is a non-symlink directory with the sticky bit, else create `$topdir/.Trash-$uid` (0700); verify unit tests cover both paths and the error when neither is usable
- [ ] 5.3 Implement `linux-fs-delete` gated on the matched root's delete capability, trashing directories as a unit (single rename, no recursive unlink) and trashing symlinks as links (`lstat`, never the target); verify unit tests cover in-root delete, directory-as-unit, symlink-not-target, out-of-root/capability denial, and that a trash failure leaves the target intact (no silent unlink)
- [ ] 5.4 Gate permanent deletion behind `allow_permanent_delete`; verify unit tests assert permanent delete is impossible when the flag is off and only used as a fallback when on

## 6. Configuration, per-root capabilities, and defaults

- [ ] 6.1 Parse the `tools.linux` config section (per-provider enabled flag, `roots` with `read`/`write`/`delete` capabilities, `max_read_bytes`, `max_write_bytes`, `allow_permanent_delete`); verify unit tests cover valid config, roots without capabilities, missing roots, and invalid values
- [ ] 6.2 Enforce fail-closed defaults (host info only; filesystem requires roots; a root with no capability is inert; invalid config disables the provider and is reported); verify unit tests assert each default

## 7. Policy integration and tool naming

- [ ] 7.1 Expose stable, documented, namespaced tool names (`linux-host-info`, `linux-fs-list`, `linux-fs-read`, `linux-fs-write`, `linux-fs-delete`); verify a unit test asserts the exact registered names and that the README documents them for agent policy targeting
- [ ] 7.2 Confirm the connector performs no approval of its own: verify a unit test asserts an in-scope call executes and returns a result, and an out-of-scope call returns an error result, with no connector-side prompt
- [ ] 7.3 Ship the recommended agent tool-policy example (`confirm` on `linux-fs-write`/`linux-fs-delete`, `defaultToolPolicy: "ask"`, full `tools` list); verify the snippet's tool names exactly match the registered names

## 8. Status and observability

- [ ] 8.1 Report the effective Linux tool set (registered tools, per-root capabilities, and disabled providers with reasons) in text and `status --json`; verify unit tests plus a golden JSON fixture
- [ ] 8.2 Emit a structured log record for each write and delete invocation (tool name, matched root, target path, outcome) and never log contents; verify unit tests assert one record per mutating call, a denied outcome on rejection, and no content in the record

## 9. Verification and docs

- [ ] 9.1 Run `go build ./...`, `go test ./...`, and `task lint` in `connector/`; verify all pass
- [ ] 9.2 End-to-end smoke: enable filesystem with one read/write/trash root and one read-only root, start the daemon, confirm read/list/write succeed where permitted, a delete lands in the trash (verifiable via the `.trashinfo`), and out-of-root or unpermitted operations are denied; record result
- [ ] 9.3 Update `connector/README.md` (Linux tools, stable names, per-root capabilities, trash behavior and limitations, agent-policy gating, recommended confirm/ask policy) and verify `openspec validate add-linux-connector-tools --strict` passes
