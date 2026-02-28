# workspace-tool-interface Specification

## Purpose
TBD - created by archiving change agent-workspace-infrastructure. Update Purpose after archive.
## Requirements
### Requirement: Bash command execution

The system SHALL execute bash commands within a workspace container and return structured results. This is the primary tool for arbitrary command execution.

#### Scenario: Successful command execution

- **WHEN** a bash request is sent with `command = "ls -la /workspace"` and `workdir = "/workspace"`
- **THEN** the system executes the command inside the workspace container, returns `stdout`, `stderr`, `exit_code`, and `duration_ms` as structured JSON

#### Scenario: Command with non-zero exit code

- **WHEN** a bash request is sent with a command that fails (e.g., `npm test` with failing tests)
- **THEN** the system returns `exit_code` reflecting the actual exit code, captures both `stdout` and `stderr`, and does NOT treat non-zero exit as an API error (200 response with exit_code in body)

#### Scenario: Command timeout

- **WHEN** a bash request is sent with `timeout_ms = 30000` and the command runs for more than 30 seconds
- **THEN** the system sends SIGTERM to the process, waits 5 seconds, sends SIGKILL if still running, and returns an error response indicating timeout with any partial stdout/stderr captured

#### Scenario: Command with custom working directory

- **WHEN** a bash request is sent with `workdir = "/workspace/src"`
- **THEN** the command executes with the current working directory set to `/workspace/src`

#### Scenario: Long-running command output

- **WHEN** a bash command produces more than 50KB of stdout
- **THEN** the system truncates the output to 50KB, indicates truncation in the response, and provides information about the total output size

### Requirement: File read

The system SHALL read file contents from workspace containers with offset/limit support for large files.

#### Scenario: Read entire small file

- **WHEN** a read request is sent with `file_path = "/workspace/README.md"` and no offset/limit
- **THEN** the system returns the file content with line numbers prefixed (e.g., `1: # README\n2: \n3: Content...`)

#### Scenario: Read file with offset and limit

- **WHEN** a read request is sent with `file_path`, `offset = 100`, and `limit = 50`
- **THEN** the system returns lines 100-149 of the file with correct line number prefixes

#### Scenario: Read non-existent file

- **WHEN** a read request is sent for a file path that does not exist in the workspace
- **THEN** the system returns a clear error indicating the file was not found

#### Scenario: Read directory listing

- **WHEN** a read request is sent for a path that is a directory
- **THEN** the system returns a listing of directory entries with trailing `/` for subdirectories

#### Scenario: Read binary file

- **WHEN** a read request is sent for a binary file (image, compiled binary)
- **THEN** the system returns metadata about the file (size, type) but does NOT return binary content inline

### Requirement: File write

The system SHALL write file contents to workspace containers, creating directories as needed.

#### Scenario: Write new file

- **WHEN** a write request is sent with `file_path = "/workspace/new-file.ts"` and `content = "export const x = 1;"`
- **THEN** the system creates the file with the specified content and returns success

#### Scenario: Overwrite existing file

- **WHEN** a write request is sent for an existing file path
- **THEN** the system replaces the entire file content with the new content

#### Scenario: Write with auto-create directories

- **WHEN** a write request is sent with `file_path = "/workspace/src/new/deep/file.ts"` and intermediate directories do not exist
- **THEN** the system creates all necessary parent directories and then writes the file

### Requirement: File edit (string replacement)

The system SHALL support targeted string replacement within files, matching OpenCode's edit tool behavior.

#### Scenario: Successful string replacement

- **WHEN** an edit request is sent with `file_path`, `old_string = "const port = 3000"`, and `new_string = "const port = 8080"`
- **THEN** the system finds the exact `old_string` in the file, replaces it with `new_string`, and returns success with the number of lines changed

#### Scenario: Old string not found

- **WHEN** an edit request is sent with an `old_string` that does not exist in the file
- **THEN** the system returns an error "oldString not found in content"

#### Scenario: Multiple matches for old string

- **WHEN** an edit request is sent with an `old_string` that appears multiple times in the file and `replace_all` is not set
- **THEN** the system returns an error "Found multiple matches for oldString" instructing the caller to provide more context or use `replace_all`

#### Scenario: Replace all occurrences

- **WHEN** an edit request is sent with `replace_all = true` and `old_string` appears multiple times
- **THEN** the system replaces ALL occurrences and returns the total count of replacements

### Requirement: File glob (pattern search)

The system SHALL find files matching glob patterns within workspace containers.

#### Scenario: Find files by extension

- **WHEN** a glob request is sent with `pattern = "**/*.ts"` and `path = "/workspace/src"`
- **THEN** the system returns a list of all TypeScript files under `/workspace/src`, sorted by modification time

#### Scenario: No matches found

- **WHEN** a glob request is sent with a pattern that matches no files
- **THEN** the system returns an empty list (not an error)

#### Scenario: Glob with specific directory

- **WHEN** a glob request is sent with `path = "/workspace/tests"`
- **THEN** the search is scoped to only the `/workspace/tests` directory and its subdirectories

### Requirement: Content grep (regex search)

The system SHALL search file contents using regular expressions within workspace containers.

#### Scenario: Search for pattern across files

- **WHEN** a grep request is sent with `pattern = "TODO"` and `path = "/workspace/src"`
- **THEN** the system returns a list of files containing the pattern, with file paths and line numbers for each match

#### Scenario: Search with file type filter

- **WHEN** a grep request is sent with `pattern = "import"`, `path = "/workspace"`, and `include = "*.ts"`
- **THEN** the system only searches TypeScript files and returns matches from those files

#### Scenario: Regex pattern search

- **WHEN** a grep request is sent with `pattern = "function\\s+\\w+"`
- **THEN** the system interprets the pattern as a regular expression and returns matching lines

### Requirement: Git operations

The system SHALL provide structured git operations within workspace containers without exposing raw git credentials.

#### Scenario: Git status

- **WHEN** a git request is sent with `action = "status"`
- **THEN** the system returns structured information about changed files, staged files, and untracked files

#### Scenario: Git commit

- **WHEN** a git request is sent with `action = "commit"`, `message = "fix: update port"`, and `files = ["src/server.ts"]`
- **THEN** the system stages the specified files and creates a commit with the given message

#### Scenario: Git diff

- **WHEN** a git request is sent with `action = "diff"`
- **THEN** the system returns the diff output showing staged and unstaged changes

#### Scenario: Git push

- **WHEN** a git request is sent with `action = "push"` and the workspace has a configured remote
- **THEN** the system pushes commits to the remote using a short-lived GitHub App installation token (agent never sees the credential), and commits are attributed to the `emergent-app[bot]` identity

#### Scenario: Git branch operations

- **WHEN** a git request is sent with `action = "checkout"` and `branch = "feature/new-thing"`
- **THEN** the system creates and checks out the new branch

### Requirement: Tool operation audit logging

The system SHALL log every tool operation for security audit and debugging.

#### Scenario: Successful operation logged

- **WHEN** any tool operation (bash, read, write, edit, glob, grep, git) completes successfully
- **THEN** the system logs the operation type, workspace ID, agent session ID, timestamp, duration, and a summary of the request (file paths, commands) but NOT file contents or command output

#### Scenario: Failed operation logged

- **WHEN** any tool operation fails
- **THEN** the system logs the failure with error details, including the same metadata as successful operations

### Requirement: Provider-agnostic tool interface

The system SHALL execute all tools identically regardless of the underlying provider (Firecracker, E2B, or gVisor).

#### Scenario: Same tool behavior across providers

- **WHEN** a bash command `echo "hello"` is executed in a Firecracker workspace, an E2B workspace, and a gVisor workspace
- **THEN** all three return identical structured responses with `stdout = "hello\n"`, `stderr = ""`, `exit_code = 0`

#### Scenario: Provider abstraction for file operations

- **WHEN** a read request is made to a Firecracker workspace (block device filesystem) vs. a gVisor workspace (Docker volume filesystem)
- **THEN** both return the same formatted file content with line numbers, regardless of underlying storage mechanism

