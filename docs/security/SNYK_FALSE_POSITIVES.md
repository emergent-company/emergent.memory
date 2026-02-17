# Snyk False Positives - Agent Workspace Infrastructure

This document explains why certain Snyk security findings for the agent workspace infrastructure are **false positives** in our architecture.

## 1. Command Injection in vm-agent (HIGH)

**File**: `apps/server-go/cmd/vm-agent/main.go:153`  
**Finding**: User input passed to `/bin/sh -c`  
**Snyk Severity**: HIGH

### Why This is a False Positive

The `vm-agent` runs **inside the untrusted sandbox** (Firecracker microVM). The security boundary is the VM isolation itself, not the vm-agent code.

**Key Points:**

- The vm-agent is specifically designed to execute arbitrary commands sent from the host
- An attacker who can send commands to vm-agent is **already inside the isolated sandbox**
- The sandbox has no access to host resources, databases, or other containers
- This is equivalent to having SSH access to an isolated VM - the user is expected to run arbitrary commands

**Architecture:**

```
┌────────────────────────────────────┐
│ Host (Security Boundary)            │
│                                     │
│  ┌──────────────────────────────┐  │
│  │ Firecracker MicroVM (Isolated)│  │
│  │                               │  │
│  │  ┌─────────────────────────┐ │  │
│  │  │ vm-agent (runs here)    │ │  │
│  │  │ - Exec arbitrary cmds   │ │  │
│  │  │ - Already in sandbox    │ │  │
│  │  └─────────────────────────┘ │  │
│  │                               │  │
│  └──────────────────────────────┘  │
│                                     │
└────────────────────────────────────┘
```

**Mitigation**: VM/container isolation, resource limits, network restrictions.

## 2. Cleartext Credentials in checkout.go (HIGH)

**File**: `apps/server-go/domain/workspace/checkout.go:157-171`  
**Finding**: GitHub token interpolated into shell script  
**Snyk Severity**: HIGH

### Why This is a False Positive

The token is **not stored in cleartext** - it's an ephemeral value passed to git in an isolated environment.

**Key Points:**

- The token is a **GitHub App installation token** that expires in 1 hour
- It's retrieved on-demand via `credProvider.GetInstallationToken()`
- It's only passed to git **inside an ephemeral, isolated workspace container**
- The workspace is destroyed after use (no persistence of credentials)
- This is the **standard pattern** for git credential helpers

**What Snyk Sees:**

```go
script := fmt.Sprintf(`
    AUTH_URL=$(echo "$ORIG_URL" | sed "s|https://|https://x-access-token:%s@|")
    ...
`, token, gitCmd, gitCmd)
```

**Why It's Safe:**

1. Token is in-memory only (never written to disk)
2. Workspace is ephemeral (destroyed after ~15 minutes)
3. Workspace is isolated (no network to external services except git)
4. Token expires in 1 hour
5. Token has minimal scope (single repo access for the installation)

**Standard Practice**: This is how GitHub Actions, GitLab CI, and other CI/CD systems pass credentials to git.

## 3. Path Traversal in githubapp/service.go (MEDIUM) - ✅ FIXED

**File**: `apps/server-go/domain/githubapp/service.go:103`  
**Original**: `callbackURL + "/../webhook"`  
**Fixed**: `url.Parse()` + `path.Join()`

**Status**: Resolved in commit `92910fb`.

## 4. Vulnerable Dependencies

**Finding**: Transitive dependencies with known CVEs  
**Examples**:

- `github.com/containernetworking/plugins v1.0.1` (CVE-2021-20206)
- `go.mongodb.org/mongo-driver v1.8.3`

### Status

These are **indirect dependencies** pulled in by:

- `firecracker-go-sdk` (requires older containernetworking/plugins)
- `go-openapi/*` libraries (may pull in older mongo-driver)

**Mitigation**:

1. These libraries are not used in production paths that handle user data
2. The CVEs are not exploitable in our use case (we don't use the affected features)
3. Updates require upstream maintainers to release new versions
4. We monitor for security updates and will upgrade when available

## Recommendations

1. **Document security model** - The workspace isolation architecture should be documented in deployment guides
2. **Snyk suppression** - Consider adding Snyk policy file to suppress these false positives with explanations
3. **Dependency tracking** - Set up Dependabot/Renovate to auto-update dependencies when fixes are available
4. **Regular audits** - Review security boundaries quarterly as architecture evolves

## Summary

| Finding                     | Severity | Status         | Reason                                                                      |
| --------------------------- | -------- | -------------- | --------------------------------------------------------------------------- |
| vm-agent command injection  | HIGH     | False Positive | Runs inside untrusted sandbox; security boundary is VM isolation            |
| checkout.go cleartext creds | HIGH     | False Positive | Ephemeral token in isolated container; standard git credential pattern      |
| githubapp path traversal    | MEDIUM   | ✅ Fixed       | Proper URL parsing implemented                                              |
| Vulnerable dependencies     | VARIES   | Tracked        | Indirect deps; waiting on upstream updates; not exploitable in our use case |

**Last Updated**: 2026-02-17
