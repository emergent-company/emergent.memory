## ADDED Requirements

### Requirement: Firecracker microVM creation

The system SHALL create isolated Firecracker microVMs for agent workspaces using KVM hardware virtualization.

#### Scenario: Create microVM with default configuration

- **WHEN** the Firecracker provider receives a workspace creation request with default resource limits
- **THEN** the system creates a Firecracker microVM with 2 vCPUs, 4GB RAM, a 10GB root filesystem backed by a block device, and a network interface via TAP device, achieving startup in under 125ms

#### Scenario: Create microVM with custom resource limits

- **WHEN** the provider receives a request with `resource_limits = {"cpu": "4", "memory": "8G", "disk": "20G"}`
- **THEN** the system creates a microVM with 4 vCPUs, 8GB RAM, and a 20GB root filesystem

#### Scenario: KVM not available

- **WHEN** the Firecracker provider attempts to create a microVM but `/dev/kvm` is not accessible
- **THEN** the provider returns an error "KVM not available" and the orchestrator falls back to the next provider

### Requirement: Firecracker microVM destruction

The system SHALL cleanly destroy Firecracker microVMs and release all associated resources.

#### Scenario: Destroy running microVM

- **WHEN** a destroy request is received for a running Firecracker microVM
- **THEN** the system sends a shutdown request via the Firecracker API, waits up to 5 seconds for clean shutdown, then terminates the process and releases the block device, TAP device, and any allocated memory

#### Scenario: Destroy stopped microVM

- **WHEN** a destroy request is received for a stopped microVM
- **THEN** the system removes the block device file and network configuration and deletes the VM metadata

### Requirement: Firecracker command execution

The system SHALL execute commands inside Firecracker microVMs via the Firecracker API or SSH agent.

#### Scenario: Execute bash command via internal agent

- **WHEN** a bash tool request targets a Firecracker workspace
- **THEN** the system communicates with a lightweight agent process running inside the microVM (started as part of the VM image) to execute the command, and returns stdout, stderr, and exit code

#### Scenario: File operations via internal agent

- **WHEN** a read, write, edit, glob, or grep tool request targets a Firecracker workspace
- **THEN** the system routes the operation through the internal agent which executes the file operation on the microVM's filesystem

### Requirement: Firecracker networking

The system SHALL configure networking for Firecracker microVMs using TAP devices and iptables.

#### Scenario: Workspace has network access

- **WHEN** a Firecracker microVM is created with default network settings
- **THEN** the system creates a TAP device, configures a virtual network interface inside the VM, sets up NAT via iptables on the host for outbound internet access, and assigns a unique IP from a private subnet

#### Scenario: Isolated network mode

- **WHEN** a microVM is created with network isolation enabled
- **THEN** the system creates a TAP device with no iptables NAT rules, the VM can only communicate with the host (for agent communication) but NOT with the internet

### Requirement: Firecracker block device management

The system SHALL manage persistent block devices for Firecracker microVM filesystems.

#### Scenario: Create block device for new workspace

- **WHEN** a new Firecracker workspace is created
- **THEN** the system creates a sparse file of the specified disk size, formats it with ext4, and attaches it as the root block device for the microVM

#### Scenario: Copy-on-write for warm pool

- **WHEN** a warm pool workspace is assigned to an agent
- **THEN** the system uses copy-on-write (via `cp --reflink=auto` on supported filesystems or thin provisioning) to create a fast clone from the base image, avoiding full disk copy

#### Scenario: Block device persists across stop/resume

- **WHEN** a Firecracker workspace is stopped and later resumed
- **THEN** the system reattaches the same block device file to the new microVM, preserving all filesystem state

### Requirement: Firecracker manager service

The system SHALL include a Firecracker manager service deployable as a Docker container with KVM access.

#### Scenario: Manager starts successfully

- **WHEN** the Firecracker manager container starts with access to `/dev/kvm`
- **THEN** it validates KVM availability, initializes the Firecracker binary, sets up the networking bridge, and reports healthy status

#### Scenario: Manager health check

- **WHEN** a health check request is made to the Firecracker manager
- **THEN** it reports: KVM availability, number of active VMs, warm pool size, available resources (CPU/memory/disk), and any errors

#### Scenario: Manager handles concurrent VM requests

- **WHEN** multiple workspace creation requests arrive simultaneously
- **THEN** the manager creates VMs concurrently (up to the configured maximum) without resource conflicts (each gets unique IP, TAP device, block device)
