# CLI Tool Specification - Delta

## REMOVED Requirements

### Requirement: Bash wrapper script for service control

**Reason**: Duplicate functionality - both `emergent-ctl.sh` bash script and `emergent-cli ctl` Go subcommand provide identical Docker service management commands (`start`, `stop`, `restart`, `status`, `logs`, `health`, `shell`). The bash script wraps `docker compose` commands, and the Go implementation does the same. This creates maintenance burden and user confusion about which tool to use.

**Migration**: Use `emergent-cli ctl <command>` instead of `emergent-ctl <command>`. All functionality is preserved in the Go implementation.

**Breaking Change**: Yes - users with existing installations must update scripts and muscle memory to use `emergent-cli ctl` instead of `emergent-ctl`.

Examples:

```bash
# Old (removed)
emergent-ctl start
emergent-ctl logs -f server
emergent-ctl health

# New (use instead)
emergent-cli ctl start
emergent-cli ctl logs -f server
emergent-cli ctl health
```

**Removed Files**:

- `deploy/minimal/emergent-ctl.sh` - Bash script providing service control wrapper
- Installer references to `emergent-ctl.sh` in `deploy/minimal/install-online.sh` (line 406)

**Updated Files**:

- `deploy/minimal/emergent-auth.sh` - Update references from `emergent-ctl restart` to `emergent-cli ctl restart`
- Documentation files - Replace all `emergent-ctl` command examples with `emergent-cli ctl`
