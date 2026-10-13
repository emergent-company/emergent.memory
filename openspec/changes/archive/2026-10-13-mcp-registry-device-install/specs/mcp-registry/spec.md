## MODIFIED Requirements

### Requirement: Registry install resolves from the embedded catalog

Memory SHALL resolve the server definition for a registry install from the
embedded catalog by the entry's registry name, instead of fetching it from the
official registry. An install of a name absent from the catalog SHALL report
not-found without any network call.

An install SHALL select its path from the requested target:

- **server** (the default) installs an outbound server on the shared host,
  keeping the remote-only transport policy (prefer `streamable-http` over `sse`;
  reject an entry that offers only stdio packages) and the existing SSRF guard
  on remote URLs.
- **device** installs the entry's local package on a connected connector: the
  first locally-runnable package (npm → `npx`, pypi → `uvx`, in registry order)
  is translated into a stdio command carrying the package's runtime and package
  arguments rendered in order and the package's own version pinned over the
  entry's, targeted at the requested relay instance, and pushed over the relay.
  A device install of an entry with no locally-runnable package SHALL be rejected
  before any write.

Both paths SHALL declare the selected transport's required-or-secret credential
keys as empty placeholders (recording the secret ones as secret keys) and SHALL
create the server disabled when any key is required, so a server that cannot
authenticate is never enabled. Both paths SHALL persist the entry's registry name
and repository URL.

#### Scenario: Install a catalog entry

- **WHEN** a user installs a registry entry that exists in the catalog and offers a remote transport
- **THEN** the definition is resolved from the catalog and a project MCP server is created for that URL

#### Scenario: Install of an unknown name

- **WHEN** a user attempts to install a registry name absent from the catalog
- **THEN** no server is created and the API reports not-found without contacting the upstream registry

#### Scenario: Outbound install of a stdio-only entry is rejected

- **WHEN** a server-targeted install names an entry whose only packages are unsupported for local running
- **THEN** the install is rejected before any write

#### Scenario: Device install translates an npm package

- **WHEN** a device-targeted install names an entry whose first locally-runnable package is an npm package
- **THEN** the created server is a stdio server with command `npx`, arguments `-y`, the pinned package identifier and the package's rendered arguments, target `device`, and the requested relay instance

#### Scenario: Device install declares required env keys

- **WHEN** a device-targeted install's selected package declares a required-or-secret environment variable
- **THEN** the created server carries that key as an empty placeholder (listed as a secret key when the registry marks it secret) and is created disabled with the key recorded in its required configuration

#### Scenario: Device install of a remote-only entry is rejected

- **WHEN** a device-targeted install names an entry with no locally-runnable package
- **THEN** the install is rejected before any write
