## ADDED Requirements

### Requirement: CLI accepts YAML files for schema commands
The `memory schemas install --file` and `memory schemas create --file` commands SHALL accept `.yaml` and `.yml` files in addition to `.json`. Detection is based on file extension (case-insensitive). The YAML content is converted to JSON internally before sending to the server; the conversion is transparent to the user.

#### Scenario: Install from a YAML file
- **WHEN** user runs `memory schemas install --file schema.yaml`
- **THEN** the CLI reads and parses the YAML file, converts it to JSON, creates the schema, and installs it successfully

#### Scenario: Create from a YAML file
- **WHEN** user runs `memory schemas create --file schema.yml`
- **THEN** the CLI reads and parses the YAML file and creates the schema successfully

#### Scenario: Invalid extension is rejected
- **WHEN** user runs `memory schemas install --file schema.txt`
- **THEN** the CLI returns an error: "unsupported file format: must be .json, .yaml, or .yml"

#### Scenario: YAML parse error is reported clearly
- **WHEN** user supplies a malformed YAML file
- **THEN** the CLI returns an error that includes the YAML parse failure message, not a JSON error
