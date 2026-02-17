# Agent Workspace Third-Party Licenses

The agent workspace infrastructure uses the following Apache 2.0 licensed components:

## Runtime & Isolation

### gVisor

- **Project:** Google gVisor
- **License:** Apache License 2.0
- **Copyright:** Copyright 2018 The gVisor Authors
- **Source:** https://github.com/google/gvisor
- **Usage:** OCI runtime (`runsc`) for sandboxed container execution. Provides application-level kernel isolation without requiring hardware virtualization.

### Firecracker

- **Project:** Amazon Firecracker
- **License:** Apache License 2.0
- **Copyright:** Copyright 2018 Amazon.com, Inc. or its affiliates
- **Source:** https://github.com/firecracker-microvm/firecracker
- **Usage:** MicroVM manager for hardware-isolated agent workspaces (requires KVM). Used when KVM is available on the host.

### E2B SDK (Go)

- **Project:** E2B
- **License:** Apache License 2.0
- **Copyright:** Copyright 2023 FoundryLabs, Inc. (E2B)
- **Source:** https://github.com/e2b-dev/e2b
- **Usage:** Managed sandbox API client for cloud-hosted agent workspaces. Used as an alternative to self-hosted providers.

## Go Dependencies

### Docker SDK for Go

- **Project:** Moby (Docker)
- **License:** Apache License 2.0
- **Copyright:** Copyright 2013-2024 Docker, Inc.
- **Source:** https://github.com/moby/moby
- **Package:** `github.com/docker/docker`
- **Usage:** Docker API client for container lifecycle management (create, exec, attach, destroy).

### golang-jwt/jwt

- **Project:** golang-jwt
- **License:** MIT License
- **Copyright:** Copyright 2012 Dave Grijalva; 2021 golang-jwt maintainers
- **Source:** https://github.com/golang-jwt/jwt
- **Package:** `github.com/golang-jwt/jwt/v5`
- **Usage:** JWT signing for GitHub App authentication (RS256).

## License Compliance

All runtime dependencies used by the agent workspace feature are licensed under Apache 2.0 or MIT, both of which are compatible with commercial and closed-source distribution. No AGPL, GPL, or other copyleft-licensed dependencies are used.

### Rejected Dependencies

| Library | License  | Reason for Rejection                                   |
| ------- | -------- | ------------------------------------------------------ |
| Daytona | AGPL-3.0 | Copyleft; incompatible with closed-source distribution |
