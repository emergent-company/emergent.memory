package agents

import (
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/emergent-company/emergent/domain/workspace"
)

// WorkspaceToolDeps holds dependencies for building workspace tools.
type WorkspaceToolDeps struct {
	Provider    workspace.Provider
	ProviderID  string // provider-specific workspace/container ID
	WorkspaceID string // our internal workspace UUID
	Config      *workspace.AgentWorkspaceConfig
	Logger      *slog.Logger
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
		"bash":  buildBashTool,
		"read":  buildReadTool,
		"write": buildWriteTool,
		"edit":  buildEditTool,
		"glob":  buildGlobTool,
		"grep":  buildGrepTool,
		"git":   buildGitTool,
	}

	var tools []tool.Tool
	for _, name := range workspace.ValidToolNames {
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
	defaultBashTimeoutMs = 120000 // 2 minutes
	workspaceDir         = "/workspace"
)

func buildBashTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_bash",
			Description: "Execute a bash command inside the sandboxed workspace container. " +
				"Use this for running build commands, tests, installing packages, or any shell operation. " +
				"The working directory defaults to /workspace. Commands have a 2-minute timeout by default.",
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

			result, err := provider.Exec(ctx, providerID, &workspace.ExecRequest{
				Command:   command,
				Workdir:   workdir,
				TimeoutMs: timeoutMs,
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

func buildReadTool(deps WorkspaceToolDeps) (tool.Tool, error) {
	provider := deps.Provider
	providerID := deps.ProviderID

	return functiontool.New(
		functiontool.Config{
			Name: "workspace_read",
			Description: "Read a file or directory listing from the workspace container. " +
				"Returns file content with line numbers, or directory entries. " +
				"Use offset and limit for paginated reading of large files.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			filePath, _ := args["file_path"].(string)
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

			result, err := provider.ReadFile(ctx, providerID, &workspace.FileReadRequest{
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
			Description: "Write content to a file in the workspace container. " +
				"Creates the file if it doesn't exist. Parent directories are auto-created. " +
				"Overwrites existing content completely.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			filePath, _ := args["file_path"].(string)
			if filePath == "" {
				return map[string]any{"error": "file_path is required"}, nil
			}
			content, _ := args["content"].(string)

			err := provider.WriteFile(ctx, providerID, &workspace.FileWriteRequest{
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
			Description: "Perform string-replacement editing on a file in the workspace container. " +
				"Provide old_string (the exact text to find) and new_string (the replacement). " +
				"By default replaces only the first match. Set replace_all to true for global replacement. " +
				"If multiple matches exist and replace_all is false, the operation fails — provide more context in old_string.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			filePath, _ := args["file_path"].(string)
			if filePath == "" {
				return map[string]any{"error": "file_path is required"}, nil
			}
			oldString, _ := args["old_string"].(string)
			if oldString == "" {
				return map[string]any{"error": "old_string is required"}, nil
			}
			newString, _ := args["new_string"].(string)
			replaceAll, _ := args["replace_all"].(bool)

			// Read current file content
			readResult, err := provider.Exec(ctx, providerID, &workspace.ExecRequest{
				Command: fmt.Sprintf("cat %q", filePath),
			})
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("failed to read file for editing: %s", err.Error())}, nil
			}
			if readResult.ExitCode != 0 {
				if strings.Contains(readResult.Stderr, "No such file") {
					return map[string]any{"error": fmt.Sprintf("file not found: %s", filePath)}, nil
				}
				return map[string]any{"error": fmt.Sprintf("failed to read file: %s", readResult.Stderr)}, nil
			}

			content := readResult.Stdout

			// Count occurrences
			count := strings.Count(content, oldString)
			if count == 0 {
				return map[string]any{"error": "old_string not found in file content"}, nil
			}
			if count > 1 && !replaceAll {
				return map[string]any{
					"error": fmt.Sprintf("Found %d matches for old_string. Provide more surrounding context to identify the correct match, or set replace_all to true.", count),
				}, nil
			}

			// Perform replacement
			var newContent string
			var replacements int
			if replaceAll {
				newContent = strings.ReplaceAll(content, oldString, newString)
				replacements = count
			} else {
				newContent = strings.Replace(content, oldString, newString, 1)
				replacements = 1
			}

			// Calculate lines changed
			oldLines := strings.Count(oldString, "\n") + 1
			newLines := strings.Count(newString, "\n") + 1
			linesChanged := oldLines
			if newLines > oldLines {
				linesChanged = newLines
			}

			// Write back
			err = provider.WriteFile(ctx, providerID, &workspace.FileWriteRequest{
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
			Description: "Find files matching a glob pattern in the workspace container. " +
				"Returns matching file paths sorted by modification time. " +
				"Supports standard glob syntax: *, ?, []. " +
				"Optionally specify a base path to search within.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			pattern, _ := args["pattern"].(string)
			if pattern == "" {
				return map[string]any{"error": "pattern is required"}, nil
			}
			path, _ := args["path"].(string)

			result, err := provider.ListFiles(ctx, providerID, &workspace.FileListRequest{
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
			Description: "Search file contents with a regex pattern inside the workspace container. " +
				"Returns matching file paths, line numbers, and line content. " +
				"Supports extended regex syntax. Use 'include' to filter by file extension (e.g. '*.go').",
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

			result, err := provider.Exec(ctx, providerID, &workspace.ExecRequest{
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
				"Supported actions: status, diff, commit, checkout. " +
				"For commit, provide a message and optionally a list of files to stage. " +
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
				cmd = fmt.Sprintf("git checkout -b %q 2>/dev/null || git checkout %q", branch, branch)
			default:
				return map[string]any{
					"error": fmt.Sprintf("unsupported git action: %s (supported: status, diff, commit, checkout)", action),
				}, nil
			}

			result, err := provider.Exec(ctx, providerID, &workspace.ExecRequest{
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
