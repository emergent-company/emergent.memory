package agents

import (
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"unicode"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
)

// WorkspaceToolDeps holds dependencies for building workspace tools.
type WorkspaceToolDeps struct {
	Provider        sandbox.Provider
	ProviderID      string // provider-specific workspace/container ID
	WorkspaceID     string // our internal workspace UUID
	Config          *sandbox.AgentSandboxConfig
	Logger          *slog.Logger
	CheckoutService *sandbox.CheckoutService // optional; enables credential-aware git clone
	SessionEnv      map[string]string        // per-session env vars (e.g. MEMORY_API_KEY for warm containers)
}

// BuildWorkspaceTools creates ADK tool.Tool wrappers for workspace tools.
// Only tools allowed by the workspace config are included.
// Returns nil if no tools are configured or all tools are filtered out.
func BuildWorkspaceTools(deps WorkspaceToolDeps) ([]tool.Tool, error) {
	if deps.Provider == nil || deps.ProviderID == "" {
		return nil, nil
	}

	// Define all available workspace tool builders
	builders := map[string]func(WorkspaceToolDeps) (tool.Tool, error){
		"bash":       buildBashTool,
		"read":       buildReadTool,
		"write":      buildWriteTool,
		"edit":       buildEditTool,
		"glob":       buildGlobTool,
		"grep":       buildGrepTool,
		"git":        buildGitTool,
		"run_python": buildRunPythonTool,
		"run_go":     buildRunGoTool,
	}

	var tools []tool.Tool
	for _, name := range sandbox.ValidToolNames {
		// Check if tool is allowed by workspace config
		if deps.Config != nil && !deps.Config.IsToolAllowed(name) {
			deps.Logger.Debug("workspace tool filtered by config",
				slog.String("tool", name),
				slog.String("workspace_id", deps.WorkspaceID),
			)
			continue
		}

		builder, ok := builders[name]
		if !ok {
			continue
		}

		t, err := builder(deps)
		if err != nil {
			deps.Logger.Warn("failed to build workspace tool, skipping",
				slog.String("tool", name),
				slog.String("error", err.Error()),
			)
			continue
		}
		tools = append(tools, t)
	}

	if len(tools) > 0 {
		deps.Logger.Info("workspace tools resolved",
			slog.String("workspace_id", deps.WorkspaceID),
			slog.Int("count", len(tools)),
		)
	}

	return tools, nil
}

// --- Individual tool builders ---

const (
	defaultBashTimeoutMs   = 120000 // 2 minutes
	defaultPythonTimeoutMs = 60000  // 1 minute
	defaultGoTimeoutMs     = 60000  // 1 minute
	workspaceDir           = "/workspace"
	pythonScriptPath       = "/tmp/_agent_run.py"
	goScriptPath           = "/tmp/_agent_run.go"
)

func buildBashTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID
	sessionEnv := deps.SessionEnv

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_bash",
			Description: `Execute a bash command inside the sandboxed workspace container.

IMPORTANT: This tool is for terminal operations (git, npm, build tools, package managers, etc.).
DO NOT use it for file operations — use the dedicated workspace tools instead:
- Reading files: workspace_read (NOT cat/head/tail)
- Writing files: workspace_write (NOT echo > or cat <<EOF)
- Editing files: workspace_edit (NOT sed/awk)
- Finding files: workspace_glob (NOT find)
- Searching content: workspace_grep (NOT grep/rg)

Usage notes:
- Use the workdir parameter to set the working directory instead of running "cd <dir> && <command>".
- Default working directory is /workspace. Default timeout is 120000ms (2 minutes).
- For sequential commands that depend on each other, chain with &&.
- For long-running operations, consider whether a timeout increase is needed.
- Avoid using echo/printf for file creation; use workspace_write instead.`,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			command, _ := args["command"].(string)
			if command == "" {
				return map[string]any{"error": "command is required"}, nil
			}

			workdir, _ := args["workdir"].(string)

			timeoutMs := defaultBashTimeoutMs
			if t, ok := args["timeout_ms"].(float64); ok && t > 0 {
				timeoutMs = int(t)
			}

			result, err := provider.Exec(ctx, providerID, &sandbox.ExecRequest{
				Command:   command,
				Workdir:   workdir,
				TimeoutMs: timeoutMs,
				Env:       sessionEnv,
			})
			if err != nil {
				// Timeout errors may still return partial output
				if result != nil {
					return map[string]any{
						"stdout":      result.Stdout,
						"stderr":      result.Stderr + "\n" + err.Error(),
						"exit_code":   result.ExitCode,
						"duration_ms": result.DurationMs,
						"truncated":   result.Truncated,
					}, nil
				}
				return map[string]any{"error": fmt.Sprintf("command execution failed: %s", err.Error())}, nil
			}

			return map[string]any{
				"stdout":      result.Stdout,
				"stderr":      result.Stderr,
				"exit_code":   result.ExitCode,
				"duration_ms": result.DurationMs,
				"truncated":   result.Truncated,
			}, nil
		},
	)
}

func buildRunPythonTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID
	sessionEnv := deps.SessionEnv

	return functiontool.New(
		functiontool.Config{
			Name: "run_python",
			Description: `Execute a Python script in one step inside the sandboxed workspace.

Use this instead of workspace_write + workspace_bash for all Python SDK tasks.
Pass the full script as the "code" parameter — no file write step needed.

The sandbox has the emergent Python SDK pre-installed. Use Client.from_env() to connect:

    from emergent import Client
    client = Client.from_env()

SDK return types (all methods return plain dicts — use dict access):
  client.projects.list()                -> list[dict]  keys: id, name, orgId
  client.projects.get(id)               -> dict        keys: id, name, orgId
  client.projects.create(payload)       -> dict
  client.projects.update(id, payload)   -> dict
  client.projects.delete(id)            -> None

  client.graph.list_objects(type, ...)  -> dict        keys: data (list), cursor, total
  client.graph.create_object(payload)   -> dict        keys: id, entity_id, type, properties, ...
  client.graph.update_object(id, pay)   -> dict        (new version — id changes after update)
  client.graph.delete_object(id)        -> None
  client.graph.hybrid_search(payload)   -> dict        keys: data (list[{object, score}])
  client.graph.bulk_create_objects(items)  -> dict     keys: items, errors

  client.agents.list()                  -> list[dict]  keys: id, name, status, ...
  client.agent_definitions.list()       -> list[dict]  keys: id, name, systemPrompt, ...
  client.schemas.list()                 -> list[dict]
  client.search.hybrid(payload)         -> dict

Always print() every result — empty stdout means no output was produced, not that the call succeeded.
Attribute access (obj.name) raises AttributeError — always use dict access (obj['name']).

Returns structured output: {"stdout": "...", "stderr": "...", "exit_code": N, "duration_ms": N}.
A non-zero exit_code means the script raised an exception — check stderr for the traceback.`,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			code, _ := args["code"].(string)
			if code == "" {
				return map[string]any{"error": "code is required"}, nil
			}

			timeoutMs := defaultPythonTimeoutMs
			if t, ok := args["timeout_ms"].(float64); ok && t > 0 {
				timeoutMs = int(t)
			}

			// Write the script to a temp file, then execute it.
			// Use printf to avoid shell interpretation of the code content.
			// We write via workspace_write semantics using the provider's WriteFile.
			writeErr := provider.WriteFile(ctx, providerID, &sandbox.FileWriteRequest{
				FilePath: pythonScriptPath,
				Content:  code,
			})
			if writeErr != nil {
				return map[string]any{"error": fmt.Sprintf("failed to write script: %s", writeErr.Error())}, nil
			}

			result, err := provider.Exec(ctx, providerID, &sandbox.ExecRequest{
				Command:   fmt.Sprintf("python3 %s", pythonScriptPath),
				TimeoutMs: timeoutMs,
				Env:       sessionEnv,
			})
			if err != nil {
				if result != nil {
					return map[string]any{
						"stdout":      result.Stdout,
						"stderr":      result.Stderr + "\n" + err.Error(),
						"exit_code":   result.ExitCode,
						"duration_ms": result.DurationMs,
					}, nil
				}
				return map[string]any{"error": fmt.Sprintf("execution failed: %s", err.Error())}, nil
			}

			return map[string]any{
				"stdout":      result.Stdout,
				"stderr":      result.Stderr,
				"exit_code":   result.ExitCode,
				"duration_ms": result.DurationMs,
			}, nil
		},
	)
}

func buildRunGoTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID
	sessionEnv := deps.SessionEnv

	return functiontool.New(
		functiontool.Config{
			Name: "run_go",
			Description: `Execute a Go program in one step inside the sandboxed workspace.

Use this instead of workspace_write + workspace_bash for all Go SDK tasks.
Pass the full Go source as the "code" parameter — no file write step needed.

The sandbox has the Emergent Go SDK pre-installed. Use sdk.NewFromEnv() to connect:

    package main

    import (
        "context"
        "fmt"
        "log"

        "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
    )

    func main() {
        client, err := sdk.NewFromEnv()
        if err != nil {
            log.Fatal(err)
        }
        ctx := context.Background()
        projects, err := client.Projects.List(ctx, nil)
        if err != nil {
            log.Fatal(err)
        }
        for _, p := range projects {
            fmt.Printf("%s  %s\n", p.ID, p.Name)
        }
    }

The code must define package main with a main() function.
Credentials are injected automatically via MEMORY_API_KEY / MEMORY_API_URL.

Returns structured output: {"stdout": "...", "stderr": "...", "exit_code": N, "duration_ms": N}.
A non-zero exit_code means the program failed — check stderr for the error.`,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			code, _ := args["code"].(string)
			if code == "" {
				return map[string]any{"error": "code is required"}, nil
			}

			timeoutMs := defaultGoTimeoutMs
			if t, ok := args["timeout_ms"].(float64); ok && t > 0 {
				timeoutMs = int(t)
			}

			// Write the Go source to a temp file.
			writeErr := provider.WriteFile(ctx, providerID, &sandbox.FileWriteRequest{
				FilePath: goScriptPath,
				Content:  code,
			})
			if writeErr != nil {
				return map[string]any{"error": fmt.Sprintf("failed to write script: %s", writeErr.Error())}, nil
			}

			// Copy the sdk-template module, inject the script, and run it.
			// The template module already has the SDK as a local replace directive
			// and all dependencies cached, so no network access is needed.
			runCmd := fmt.Sprintf(
				`set -e
cp -r /sdk-template /tmp/_agent_gomod
cp %s /tmp/_agent_gomod/main.go
cd /tmp/_agent_gomod
go run .`,
				goScriptPath,
			)

			result, err := provider.Exec(ctx, providerID, &sandbox.ExecRequest{
				Command:   runCmd,
				TimeoutMs: timeoutMs,
				Env:       sessionEnv,
			})
			if err != nil {
				if result != nil {
					return map[string]any{
						"stdout":      result.Stdout,
						"stderr":      result.Stderr + "\n" + err.Error(),
						"exit_code":   result.ExitCode,
						"duration_ms": result.DurationMs,
					}, nil
				}
				return map[string]any{"error": fmt.Sprintf("execution failed: %s", err.Error())}, nil
			}

			return map[string]any{
				"stdout":      result.Stdout,
				"stderr":      result.Stderr,
				"exit_code":   result.ExitCode,
				"duration_ms": result.DurationMs,
			}, nil
		},
	)
}

func buildReadTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_read",
			Description: `Read a file or directory from the workspace container. If the path does not exist, an error is returned.

Usage:
- file_path should be an absolute path inside the container, e.g. /workspace/src/main.go.
- By default this tool returns up to 2000 lines from the start of the file.
- offset is the line number to start from (1-indexed). Use it to read later sections of large files.
- limit sets the maximum number of lines to return.
- Use workspace_grep to find specific content in large files.
- If you are unsure of the correct file path, use workspace_glob to look up filenames by glob pattern.
- Contents are returned with each line prefixed by its line number as "<line>: <content>".
- Any line longer than 2000 characters is truncated.
- Call this tool in parallel when you know there are multiple files you want to read.
- Avoid tiny repeated slices (30-line chunks). If you need more context, read a larger window.`,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			filePath, _ := args["file_path"].(string)
			if filePath == "" {
				filePath, _ = args["path"].(string)
			}
			if filePath == "" {
				return map[string]any{"error": "file_path is required"}, nil
			}

			offset := 0
			if o, ok := args["offset"].(float64); ok {
				offset = int(o)
			}
			limit := 0
			if l, ok := args["limit"].(float64); ok {
				limit = int(l)
			}

			result, err := provider.ReadFile(ctx, providerID, &sandbox.FileReadRequest{
				FilePath: filePath,
				Offset:   offset,
				Limit:    limit,
			})
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					return map[string]any{"error": fmt.Sprintf("file not found: %s", filePath)}, nil
				}
				return map[string]any{"error": fmt.Sprintf("file read failed: %s", err.Error())}, nil
			}

			return map[string]any{
				"content":     result.Content,
				"is_dir":      result.IsDir,
				"total_lines": result.TotalLines,
				"file_size":   result.FileSize,
				"is_binary":   result.IsBinary,
			}, nil
		},
	)
}

func buildWriteTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_write",
			Description: `Write (or overwrite) a file in the workspace container.

Usage:
- This tool will overwrite the existing file if there is one at the provided path.
- If writing to an existing file, you MUST use workspace_read first to read its current contents.
- Parent directories are created automatically.
- ALWAYS prefer editing existing files with workspace_edit. NEVER write new files unless explicitly required.
- NEVER proactively create documentation files (*.md) or README files unless explicitly requested.
- file_path should be an absolute path, e.g. /workspace/src/main.go.`,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			filePath, _ := args["file_path"].(string)
			if filePath == "" {
				// Fallback: accept "path" as alias for "file_path"
				filePath, _ = args["path"].(string)
			}
			if filePath == "" {
				return map[string]any{"error": "file_path is required"}, nil
			}
			content, _ := args["content"].(string)

			err := provider.WriteFile(ctx, providerID, &sandbox.FileWriteRequest{
				FilePath: filePath,
				Content:  content,
			})
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("file write failed: %s", err.Error())}, nil
			}

			return map[string]any{
				"success":   true,
				"file_path": filePath,
			}, nil
		},
	)
}

func buildEditTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_edit",
			Description: `Performs exact string replacements in files inside the workspace container.

Usage:
- You must use workspace_read at least once before editing a file. This tool will fail if you have not read the file.
- When providing old_string from workspace_read output, preserve the exact indentation as it appears AFTER the line number prefix (e.g. "1: "). Never include any part of the line number prefix in old_string or new_string.
- ALWAYS prefer editing existing files. NEVER write new files unless explicitly required.
- The edit will FAIL if old_string is not found in the file content.
- The edit will FAIL if old_string is found multiple times and replace_all is false — provide more surrounding lines in old_string to make it unique.
- Use replace_all to replace every occurrence (useful for renaming a variable across a file).`,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			filePath, _ := args["file_path"].(string)
			if filePath == "" {
				filePath, _ = args["path"].(string)
			}
			if filePath == "" {
				return map[string]any{"error": "file_path is required"}, nil
			}
			oldString, _ := args["old_string"].(string)
			if oldString == "" {
				return map[string]any{"error": "old_string is required"}, nil
			}
			newString, _ := args["new_string"].(string)
			replaceAll, _ := args["replace_all"].(bool)

			// Read current file content via provider
			readResult, err := provider.ReadFile(ctx, providerID, &sandbox.FileReadRequest{
				FilePath: filePath,
			})
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					return map[string]any{"error": fmt.Sprintf("file not found: %s", filePath)}, nil
				}
				return map[string]any{"error": fmt.Sprintf("failed to read file for editing: %s", err.Error())}, nil
			}
			if readResult.IsBinary {
				return map[string]any{"error": "cannot edit binary files"}, nil
			}

			content := readResult.Content

			// Apply fuzzy replacement
			newContent, replacements, editErr := applyFuzzyEdit(content, oldString, newString, replaceAll)
			if editErr != nil {
				return map[string]any{"error": editErr.Error()}, nil
			}

			// Calculate lines changed
			oldLines := strings.Count(oldString, "\n") + 1
			newLines := strings.Count(newString, "\n") + 1
			linesChanged := oldLines
			if newLines > oldLines {
				linesChanged = newLines
			}

			// Write back
			err = provider.WriteFile(ctx, providerID, &sandbox.FileWriteRequest{
				FilePath: filePath,
				Content:  newContent,
			})
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("failed to write edited file: %s", err.Error())}, nil
			}

			return map[string]any{
				"success":       true,
				"lines_changed": linesChanged * replacements,
				"replacements":  replacements,
			}, nil
		},
	)
}

func buildGlobTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_glob",
			Description: `Fast file pattern matching tool that works with any codebase size inside the workspace container.

- Supports glob patterns like "**/*.go" or "src/**/*.ts"
- Returns matching file paths sorted by modification time
- Use this tool when you need to find files by name patterns
- Optionally specify a path to restrict the search to a subdirectory
- When you are doing an open-ended search that may require multiple rounds of globbing and grepping, prefer workspace_bash with find or a combined approach
- You can call multiple tools in a single response; it is always better to speculatively perform multiple searches in parallel`,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			pattern, _ := args["pattern"].(string)
			if pattern == "" {
				return map[string]any{"error": "pattern is required"}, nil
			}
			path, _ := args["path"].(string)

			result, err := provider.ListFiles(ctx, providerID, &sandbox.FileListRequest{
				Pattern: pattern,
				Path:    path,
			})
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("glob search failed: %s", err.Error())}, nil
			}

			paths := make([]string, 0, len(result.Files))
			for _, f := range result.Files {
				paths = append(paths, f.Path)
			}

			return map[string]any{
				"matches": paths,
				"count":   len(paths),
			}, nil
		},
	)
}

func buildGrepTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_grep",
			Description: `Fast content search tool for the workspace container.

- Searches file contents using regular expressions
- Supports full regex syntax (e.g. "log.*Error", "func\s+\w+", etc.)
- Filter files by glob pattern with the include parameter (e.g. "*.go", "*.{ts,tsx}")
- Returns file paths and line numbers with at least one match, sorted by modification time
- Use this tool when you need to find files containing specific patterns
- If you need to count matches or do more complex filtering, use workspace_bash with rg (ripgrep) directly`,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			pattern, _ := args["pattern"].(string)
			if pattern == "" {
				return map[string]any{"error": "pattern is required"}, nil
			}

			searchPath, _ := args["path"].(string)
			if searchPath == "" {
				searchPath = workspaceDir
			}
			include, _ := args["include"].(string)

			// Build grep command
			cmd := fmt.Sprintf("grep -rnE %q %q", pattern, searchPath)
			if include != "" {
				cmd = fmt.Sprintf("grep -rnE --include=%q %q %q", include, pattern, searchPath)
			}
			cmd += " 2>/dev/null || true"

			result, err := provider.Exec(ctx, providerID, &sandbox.ExecRequest{
				Command:   cmd,
				TimeoutMs: 30000, // 30s timeout for grep
			})
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("grep execution failed: %s", err.Error())}, nil
			}

			matches := parseWorkspaceGrepOutput(result.Stdout)

			return map[string]any{
				"matches": matches,
				"count":   len(matches),
			}, nil
		},
	)
}

func buildGitTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_git",
			Description: "Execute structured git operations in the workspace container. " +
				"Supported actions: status, diff, commit, checkout, clone. " +
				"For commit, provide a message and optionally a list of files to stage. " +
				"For clone, provide a url (https only) and optionally a path (destination directory) and branch. " +
				"Push/pull are not available (credential management is server-side).",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			action, _ := args["action"].(string)
			if action == "" {
				return map[string]any{"error": "action is required"}, nil
			}

			var cmd string
			switch action {
			case "status":
				cmd = "git status --porcelain"
			case "diff":
				cmd = "git diff && echo '---STAGED---' && git diff --staged"
			case "commit":
				message, _ := args["message"].(string)
				if message == "" {
					return map[string]any{"error": "message is required for commit action"}, nil
				}
				filesRaw, _ := args["files"].([]any)
				if len(filesRaw) > 0 {
					fileArgs := make([]string, 0, len(filesRaw))
					for _, f := range filesRaw {
						if s, ok := f.(string); ok && s != "" {
							fileArgs = append(fileArgs, fmt.Sprintf("%q", s))
						}
					}
					cmd = fmt.Sprintf("git add %s && git commit -m %q", strings.Join(fileArgs, " "), message)
				} else {
					cmd = fmt.Sprintf("git add -A && git commit -m %q", message)
				}
			case "checkout":
				branch, _ := args["branch"].(string)
				if branch == "" {
					return map[string]any{"error": "branch is required for checkout action"}, nil
				}
				// Validate branch name to prevent shell injection
				if match, _ := regexp.MatchString(`^[a-zA-Z0-9_\-\./]+$`, branch); !match {
					return map[string]any{"error": "invalid branch name: must contain only alphanumeric characters, dots, slashes, hyphens and underscores"}, nil
				}
				cmd = fmt.Sprintf("git checkout -b %q 2>/dev/null || git checkout %q", branch, branch)
			case "clone":
				return executeGitClone(ctx, args, deps)
			default:
				return map[string]any{
					"error": fmt.Sprintf("unsupported git action: %s (supported: status, diff, commit, checkout, clone)", action),
				}, nil
			}

			result, err := provider.Exec(ctx, providerID, &sandbox.ExecRequest{
				Command:   cmd,
				TimeoutMs: 60000, // 60s timeout for git operations
			})
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("git operation failed: %s", err.Error())}, nil
			}

			output := result.Stdout
			if result.Stderr != "" {
				output += "\n" + result.Stderr
			}

			return map[string]any{
				"output": strings.TrimSpace(output),
			}, nil
		},
	)
}

// executeGitClone handles the "clone" action for workspace_git.
// It validates the URL and destination path, then delegates to CheckoutService
// (credential-aware) if available, or falls back to a plain git clone.
func executeGitClone(ctx tool.Context, args map[string]any, deps WorkspaceToolDeps) (map[string]any, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return map[string]any{"error": "url is required for clone action"}, nil
	}
	if !strings.HasPrefix(url, "https://") {
		return map[string]any{"error": "url must start with https:// (SSH URLs are not supported)"}, nil
	}

	branch, _ := args["branch"].(string)

	// Resolve destination path: default to /workspace/<repo-name>
	destPath, _ := args["path"].(string)
	if destPath == "" {
		// Strip trailing .git and take the last path segment
		repoName := url
		repoName = strings.TrimSuffix(repoName, ".git")
		if idx := strings.LastIndex(repoName, "/"); idx >= 0 {
			repoName = repoName[idx+1:]
		}
		if repoName == "" {
			repoName = "repo"
		}
		destPath = "/workspace/" + repoName
	}

	// Validate destination path: no shell metacharacters, no path traversal
	if match, _ := regexp.MatchString(`^[a-zA-Z0-9_\-\./]+$`, destPath); !match {
		return map[string]any{"error": "invalid path: must contain only alphanumeric characters, dots, slashes, hyphens and underscores"}, nil
	}
	if strings.Contains(destPath, "..") {
		return map[string]any{"error": "invalid path: path traversal (..) is not allowed"}, nil
	}

	// Prefer credential-aware CheckoutService if available
	if deps.CheckoutService != nil {
		if err := deps.CheckoutService.CloneRepository(ctx, deps.Provider, deps.ProviderID, url, branch, destPath); err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		return map[string]any{"cloned_to": destPath}, nil
	}

	// Fallback: plain git clone (public repos only)
	cmd := fmt.Sprintf("git clone --depth 1 %q %q", url, destPath)
	if branch != "" {
		cmd = fmt.Sprintf("git clone --depth 1 -b %q %q %q", branch, url, destPath)
	}

	result, err := deps.Provider.Exec(ctx, deps.ProviderID, &sandbox.ExecRequest{
		Command:   cmd,
		TimeoutMs: 120000, // 2 minute timeout for clones
	})
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("git clone failed: %s", err.Error())}, nil
	}
	if result.ExitCode != 0 {
		errMsg := strings.TrimSpace(result.Stderr)
		if errMsg == "" {
			errMsg = strings.TrimSpace(result.Stdout)
		}
		return map[string]any{"error": fmt.Sprintf("git clone failed (exit %d): %s", result.ExitCode, errMsg)}, nil
	}

	return map[string]any{"cloned_to": destPath}, nil
}

// parseWorkspaceGrepOutput parses grep -rn output into structured matches.
func parseWorkspaceGrepOutput(output string) []map[string]any {
	var matches []map[string]any
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Format: filepath:linenum:content
		firstColon := strings.Index(line, ":")
		if firstColon < 0 {
			continue
		}
		rest := line[firstColon+1:]
		secondColon := strings.Index(rest, ":")
		if secondColon < 0 {
			continue
		}

		filePath := line[:firstColon]
		lineNumStr := rest[:secondColon]
		content := rest[secondColon+1:]

		// Parse line number
		lineNum := 0
		fmt.Sscanf(lineNumStr, "%d", &lineNum)
		if lineNum <= 0 {
			continue
		}

		matches = append(matches, map[string]any{
			"file_path":   filePath,
			"line_number": lineNum,
			"line":        content,
		})
	}
	return matches
}

// =============================================================================
// Fuzzy edit — 9-strategy replacement engine (ported from OpenCode edit.ts)
// =============================================================================

// applyFuzzyEdit attempts to replace oldString with newString in content using
// a cascade of increasingly flexible matching strategies. Returns the updated
// content, the number of replacements made, and any error.
func applyFuzzyEdit(content, oldString, newString string, replaceAll bool) (string, int, error) {
	// Strategy 9 (replaceAll path) bypasses the uniqueness check
	if replaceAll {
		result, n := multiOccurrenceReplacer(content, oldString, newString)
		if n == 0 {
			return "", 0, fmt.Errorf("Could not find old_string in file content")
		}
		return result, n, nil
	}

	type replacer func(content, find, replace string) (string, bool)

	strategies := []replacer{
		simpleReplacer,
		lineTrimmedReplacer,
		blockAnchorReplacer,
		whitespaceNormalizedReplacer,
		indentationFlexibleReplacer,
		escapeNormalizedReplacer,
		trimmedBoundaryReplacer,
		contextAwareReplacer,
	}

	for _, strategy := range strategies {
		result, ok := strategy(content, oldString, newString)
		if ok {
			return result, 1, nil
		}
	}

	// Distinguish "not found at all" vs "multiple matches"
	if strings.Count(content, oldString) > 1 {
		return "", 0, fmt.Errorf("Found multiple matches for old_string. Provide more surrounding lines in old_string to identify the correct match.")
	}
	return "", 0, fmt.Errorf("Could not find old_string in file content")
}

// --- Strategy 1: exact match ---

func simpleReplacer(content, find, replace string) (string, bool) {
	idx := strings.Index(content, find)
	if idx < 0 {
		return "", false
	}
	// Ensure unique
	if strings.Index(content[idx+1:], find) >= 0 {
		return "", false
	}
	return content[:idx] + replace + content[idx+len(find):], true
}

// --- Strategy 2: line-by-line trim comparison ---

func lineTrimmedReplacer(content, find, replace string) (string, bool) {
	findLines := strings.Split(find, "\n")
	contentLines := strings.Split(content, "\n")

	// Trim each find-line for comparison
	trimmedFind := make([]string, len(findLines))
	for i, l := range findLines {
		trimmedFind[i] = strings.TrimSpace(l)
	}

	n := len(findLines)
	matchStart := -1
	matchCount := 0

	for i := 0; i <= len(contentLines)-n; i++ {
		matched := true
		for j := 0; j < n; j++ {
			if strings.TrimSpace(contentLines[i+j]) != trimmedFind[j] {
				matched = false
				break
			}
		}
		if matched {
			matchCount++
			if matchCount > 1 {
				return "", false // ambiguous
			}
			matchStart = i
		}
	}

	if matchStart < 0 {
		return "", false
	}

	// Reconstruct: preserve original indentation of first matched line, apply delta to replace lines
	origLine := contentLines[matchStart]
	origIndent := leadingWhitespace(origLine)
	findIndent := leadingWhitespace(findLines[0])

	replaceLines := strings.Split(replace, "\n")
	adjusted := adjustIndent(replaceLines, origIndent, findIndent)

	result := append(contentLines[:matchStart], append(adjusted, contentLines[matchStart+n:]...)...)
	return strings.Join(result, "\n"), true
}

// --- Strategy 3: block anchor (first+last line + Levenshtein on middle) ---

func blockAnchorReplacer(content, find, replace string) (string, bool) {
	findLines := strings.Split(find, "\n")
	if len(findLines) < 2 {
		return "", false
	}

	firstFind := strings.TrimSpace(findLines[0])
	lastFind := strings.TrimSpace(findLines[len(findLines)-1])
	contentLines := strings.Split(content, "\n")
	n := len(findLines)

	type candidate struct{ start int }
	var candidates []candidate

	for i := 0; i <= len(contentLines)-n; i++ {
		if strings.TrimSpace(contentLines[i]) == firstFind &&
			strings.TrimSpace(contentLines[i+n-1]) == lastFind {
			candidates = append(candidates, candidate{i})
		}
	}

	if len(candidates) == 0 {
		return "", false
	}

	// Pick best candidate by Levenshtein similarity on middle lines
	threshold := 0.0
	if len(candidates) > 1 {
		threshold = 0.3
	}

	bestIdx := -1
	bestScore := -1.0

	for _, c := range candidates {
		score := middleLinesSimilarity(contentLines[c.start:c.start+n], findLines)
		if score >= threshold && score > bestScore {
			bestScore = score
			bestIdx = c.start
		}
	}

	if bestIdx < 0 {
		return "", false
	}

	origIndent := leadingWhitespace(contentLines[bestIdx])
	findIndent := leadingWhitespace(findLines[0])
	replaceLines := strings.Split(replace, "\n")
	adjusted := adjustIndent(replaceLines, origIndent, findIndent)

	result := append(contentLines[:bestIdx], append(adjusted, contentLines[bestIdx+n:]...)...)
	return strings.Join(result, "\n"), true
}

// --- Strategy 4: whitespace normalization ---

func whitespaceNormalizedReplacer(content, find, replace string) (string, bool) {
	normalizeWS := func(s string) string {
		// Collapse runs of whitespace (including newlines) to a single space
		fields := strings.FieldsFunc(s, unicode.IsSpace)
		return strings.Join(fields, " ")
	}

	normContent := normalizeWS(content)
	normFind := normalizeWS(find)

	idx := strings.Index(normContent, normFind)
	if idx < 0 || strings.Index(normContent[idx+1:], normFind) >= 0 {
		return "", false
	}

	// Fall back to simple replacer on the original content
	return simpleReplacer(content, find, replace)
}

// --- Strategy 5: indentation-flexible ---

func indentationFlexibleReplacer(content, find, replace string) (string, bool) {
	findLines := strings.Split(find, "\n")
	minIndent := minCommonIndent(findLines)

	stripped := make([]string, len(findLines))
	for i, l := range findLines {
		if len(l) >= minIndent {
			stripped[i] = l[minIndent:]
		} else {
			stripped[i] = strings.TrimLeft(l, " \t")
		}
	}
	strippedFind := strings.Join(stripped, "\n")

	contentLines := strings.Split(content, "\n")
	contentMinIndent := minCommonIndent(contentLines)
	strippedContentLines := make([]string, len(contentLines))
	for i, l := range contentLines {
		if len(l) >= contentMinIndent {
			strippedContentLines[i] = l[contentMinIndent:]
		} else {
			strippedContentLines[i] = strings.TrimLeft(l, " \t")
		}
	}
	strippedContent := strings.Join(strippedContentLines, "\n")

	idx := strings.Index(strippedContent, strippedFind)
	if idx < 0 || strings.Index(strippedContent[idx+1:], strippedFind) >= 0 {
		return "", false
	}

	return simpleReplacer(content, find, replace)
}

// --- Strategy 6: escape normalization ---

func escapeNormalizedReplacer(content, find, replace string) (string, bool) {
	unescape := func(s string) string {
		s = strings.ReplaceAll(s, `\n`, "\n")
		s = strings.ReplaceAll(s, `\t`, "\t")
		s = strings.ReplaceAll(s, `\r`, "\r")
		s = strings.ReplaceAll(s, `\\`, "\\")
		return s
	}

	unescapedFind := unescape(find)
	if unescapedFind == find {
		return "", false // nothing changed; don't repeat simpleReplacer
	}

	return simpleReplacer(content, unescapedFind, replace)
}

// --- Strategy 7: trimmed boundary ---

func trimmedBoundaryReplacer(content, find, replace string) (string, bool) {
	trimmedFind := strings.TrimSpace(find)
	if trimmedFind == find {
		return "", false
	}
	return simpleReplacer(content, trimmedFind, replace)
}

// --- Strategy 8: context-aware (anchor + 50% middle heuristic) ---

func contextAwareReplacer(content, find, replace string) (string, bool) {
	findLines := strings.Split(find, "\n")
	if len(findLines) < 3 {
		return "", false
	}

	firstFind := strings.TrimSpace(findLines[0])
	lastFind := strings.TrimSpace(findLines[len(findLines)-1])
	contentLines := strings.Split(content, "\n")
	n := len(findLines)

	matchStart := -1
	matchCount := 0

	for i := 0; i <= len(contentLines)-n; i++ {
		if strings.TrimSpace(contentLines[i]) != firstFind {
			continue
		}
		if strings.TrimSpace(contentLines[i+n-1]) != lastFind {
			continue
		}
		// Require at least 50% of middle lines to match
		middle := findLines[1 : n-1]
		matched := 0
		for j, ml := range middle {
			if strings.TrimSpace(contentLines[i+1+j]) == strings.TrimSpace(ml) {
				matched++
			}
		}
		if len(middle) == 0 || float64(matched)/float64(len(middle)) >= 0.5 {
			matchCount++
			if matchCount > 1 {
				return "", false
			}
			matchStart = i
		}
	}

	if matchStart < 0 {
		return "", false
	}

	origIndent := leadingWhitespace(contentLines[matchStart])
	findIndent := leadingWhitespace(findLines[0])
	replaceLines := strings.Split(replace, "\n")
	adjusted := adjustIndent(replaceLines, origIndent, findIndent)

	result := append(contentLines[:matchStart], append(adjusted, contentLines[matchStart+n:]...)...)
	return strings.Join(result, "\n"), true
}

// --- Strategy 9: multi-occurrence (replace_all=true) ---

func multiOccurrenceReplacer(content, find, replace string) (string, int) {
	count := strings.Count(content, find)
	if count == 0 {
		return content, 0
	}
	return strings.ReplaceAll(content, find, replace), count
}

// =============================================================================
// Helpers
// =============================================================================

// leadingWhitespace returns the leading whitespace characters of a string.
func leadingWhitespace(s string) string {
	for i, r := range s {
		if !unicode.IsSpace(r) {
			return s[:i]
		}
	}
	return s // all whitespace
}

// minCommonIndent returns the minimum indentation width across non-empty lines.
func minCommonIndent(lines []string) int {
	min := math.MaxInt32
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		if indent < min {
			min = indent
		}
	}
	if min == math.MaxInt32 {
		return 0
	}
	return min
}

// adjustIndent re-indents replaceLines so their base indent matches origIndent,
// given that findIndent was the base indent of the old block.
func adjustIndent(replaceLines []string, origIndent, findIndent string) []string {
	result := make([]string, len(replaceLines))
	for i, line := range replaceLines {
		if i == 0 {
			// First line: swap findIndent prefix for origIndent
			if strings.HasPrefix(line, findIndent) {
				result[i] = origIndent + line[len(findIndent):]
			} else {
				result[i] = origIndent + strings.TrimLeft(line, " \t")
			}
		} else {
			result[i] = line
		}
	}
	return result
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// similarity returns a 0.0–1.0 score between two strings (1.0 = identical).
func similarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	maxLen := len([]rune(a))
	if l := len([]rune(b)); l > maxLen {
		maxLen = l
	}
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

// middleLinesSimilarity computes average similarity for the middle lines of a
// candidate block against the find-block's middle lines.
func middleLinesSimilarity(candidateLines, findLines []string) float64 {
	n := len(findLines)
	if n <= 2 {
		return 1.0 // no middle lines; anchors matched
	}
	middle := findLines[1 : n-1]
	total := 0.0
	for i, fl := range middle {
		cl := ""
		if i+1 < len(candidateLines) {
			cl = candidateLines[i+1]
		}
		total += similarity(strings.TrimSpace(cl), strings.TrimSpace(fl))
	}
	return total / float64(len(middle))
}
