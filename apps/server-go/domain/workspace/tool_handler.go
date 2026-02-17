package workspace

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

const (
	defaultBashTimeoutMs = 120000 // 2 minutes
)

// getWorkspaceAndProvider validates the workspace ID, checks status, and returns the workspace + provider.
// It also updates last_used_at asynchronously.
func (h *Handler) getWorkspaceAndProvider(c echo.Context) (*WorkspaceResponse, Provider, error) {
	user := auth.GetUser(c)
	if user == nil {
		return nil, nil, apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return nil, nil, apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, nil, apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	ws, err := h.svc.GetByID(c.Request().Context(), id)
	if err != nil {
		return nil, nil, err
	}

	// Workspace must be ready for tool operations
	if ws.Status != StatusReady {
		return nil, nil, apperror.NewBadRequest(
			fmt.Sprintf("workspace is not ready (current status: %s)", ws.Status),
		)
	}

	provider, err := h.orchestrator.GetProvider(ws.Provider)
	if err != nil {
		return nil, nil, apperror.ErrInternal.WithMessage(
			fmt.Sprintf("provider %q not available", ws.Provider),
		)
	}

	// Touch last_used_at asynchronously (fire-and-forget)
	go func() {
		_ = h.svc.TouchLastUsed(c.Request().Context(), id)
	}()

	return ws, provider, nil
}

// BashTool handles POST /api/v1/agent/workspaces/:id/bash
// @Summary      Execute bash command
// @Description  Executes a command inside the workspace container
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body BashRequest true "Command to execute"
// @Success      200 {object} BashResponse "Command output"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/v1/agent/workspaces/{id}/bash [post]
// @Security     bearerAuth
func (h *Handler) BashTool(c echo.Context) error {
	ws, provider, err := h.getWorkspaceAndProvider(c)
	if err != nil {
		return err
	}

	var req BashRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.Command == "" {
		return apperror.NewBadRequest("command is required")
	}

	timeoutMs := req.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = defaultBashTimeoutMs
	}

	result, err := provider.Exec(c.Request().Context(), ws.ProviderWorkspaceID, &ExecRequest{
		Command:   req.Command,
		Workdir:   req.Workdir,
		TimeoutMs: timeoutMs,
	})
	if err != nil {
		// Timeout errors still return partial output
		if result != nil {
			return c.JSON(http.StatusOK, &BashResponse{
				Stdout:     result.Stdout,
				Stderr:     result.Stderr + "\n" + err.Error(),
				ExitCode:   result.ExitCode,
				DurationMs: result.DurationMs,
				Truncated:  result.Truncated,
			})
		}
		return apperror.NewInternal("command execution failed", err)
	}

	return c.JSON(http.StatusOK, &BashResponse{
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMs: result.DurationMs,
		Truncated:  result.Truncated,
	})
}

// ReadTool handles POST /api/v1/agent/workspaces/:id/read
// @Summary      Read file
// @Description  Reads file content or directory listing from the workspace
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body ReadRequest true "File read parameters"
// @Success      200 {object} ReadResponse "File content"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "File not found"
// @Router       /api/v1/agent/workspaces/{id}/read [post]
// @Security     bearerAuth
func (h *Handler) ReadTool(c echo.Context) error {
	ws, provider, err := h.getWorkspaceAndProvider(c)
	if err != nil {
		return err
	}

	var req ReadRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.FilePath == "" {
		return apperror.NewBadRequest("file_path is required")
	}

	result, err := provider.ReadFile(c.Request().Context(), ws.ProviderWorkspaceID, &FileReadRequest{
		FilePath: req.FilePath,
		Offset:   req.Offset,
		Limit:    req.Limit,
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return apperror.ErrNotFound.WithMessage(fmt.Sprintf("file not found: %s", req.FilePath))
		}
		return apperror.NewInternal("file read failed", err)
	}

	return c.JSON(http.StatusOK, &ReadResponse{
		Content:    result.Content,
		IsDir:      result.IsDir,
		TotalLines: result.TotalLines,
		FileSize:   result.FileSize,
		IsBinary:   result.IsBinary,
	})
}

// WriteTool handles POST /api/v1/agent/workspaces/:id/write
// @Summary      Write file
// @Description  Writes file content to the workspace, auto-creating parent directories
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body WriteRequest true "File write parameters"
// @Success      200 {object} map[string]any "Write confirmation"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/v1/agent/workspaces/{id}/write [post]
// @Security     bearerAuth
func (h *Handler) WriteTool(c echo.Context) error {
	ws, provider, err := h.getWorkspaceAndProvider(c)
	if err != nil {
		return err
	}

	var req WriteRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.FilePath == "" {
		return apperror.NewBadRequest("file_path is required")
	}

	err = provider.WriteFile(c.Request().Context(), ws.ProviderWorkspaceID, &FileWriteRequest{
		FilePath: req.FilePath,
		Content:  req.Content,
	})
	if err != nil {
		return apperror.NewInternal("file write failed", err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success":   true,
		"file_path": req.FilePath,
	})
}

// EditTool handles POST /api/v1/agent/workspaces/:id/edit
// @Summary      Edit file
// @Description  Performs string replacement editing on a file in the workspace
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body EditRequest true "Edit parameters"
// @Success      200 {object} EditResponse "Edit result"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "File not found"
// @Router       /api/v1/agent/workspaces/{id}/edit [post]
// @Security     bearerAuth
func (h *Handler) EditTool(c echo.Context) error {
	ws, provider, err := h.getWorkspaceAndProvider(c)
	if err != nil {
		return err
	}

	var req EditRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.FilePath == "" {
		return apperror.NewBadRequest("file_path is required")
	}
	if req.OldString == "" {
		return apperror.NewBadRequest("old_string is required")
	}

	ctx := c.Request().Context()

	// Read the current file content (without line numbers)
	readResult, err := provider.Exec(ctx, ws.ProviderWorkspaceID, &ExecRequest{
		Command: fmt.Sprintf("cat %q", req.FilePath),
	})
	if err != nil {
		return apperror.NewInternal("failed to read file for editing", err)
	}
	if readResult.ExitCode != 0 {
		if strings.Contains(readResult.Stderr, "No such file") {
			return apperror.ErrNotFound.WithMessage(fmt.Sprintf("file not found: %s", req.FilePath))
		}
		return apperror.NewBadRequest(fmt.Sprintf("failed to read file: %s", readResult.Stderr))
	}

	content := readResult.Stdout

	// Count occurrences
	count := strings.Count(content, req.OldString)

	if count == 0 {
		return apperror.NewBadRequest("oldString not found in content")
	}

	if count > 1 && !req.ReplaceAll {
		return apperror.NewBadRequest(
			"Found multiple matches for oldString. Provide more surrounding lines in oldString to identify the correct match.",
		)
	}

	// Perform replacement
	var newContent string
	var replacements int
	if req.ReplaceAll {
		newContent = strings.ReplaceAll(content, req.OldString, req.NewString)
		replacements = count
	} else {
		newContent = strings.Replace(content, req.OldString, req.NewString, 1)
		replacements = 1
	}

	// Calculate lines changed
	oldLines := strings.Count(req.OldString, "\n") + 1
	newLines := strings.Count(req.NewString, "\n") + 1
	linesChanged := oldLines
	if newLines > oldLines {
		linesChanged = newLines
	}

	// Write back
	err = provider.WriteFile(ctx, ws.ProviderWorkspaceID, &FileWriteRequest{
		FilePath: req.FilePath,
		Content:  newContent,
	})
	if err != nil {
		return apperror.NewInternal("failed to write edited file", err)
	}

	return c.JSON(http.StatusOK, &EditResponse{
		Success:      true,
		LinesChanged: linesChanged * replacements,
		Replacements: replacements,
	})
}

// GlobTool handles POST /api/v1/agent/workspaces/:id/glob
// @Summary      Glob file search
// @Description  Finds files matching a glob pattern in the workspace, sorted by modification time
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body GlobRequest true "Glob pattern"
// @Success      200 {object} map[string]any "Matching file paths"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/v1/agent/workspaces/{id}/glob [post]
// @Security     bearerAuth
func (h *Handler) GlobTool(c echo.Context) error {
	ws, provider, err := h.getWorkspaceAndProvider(c)
	if err != nil {
		return err
	}

	var req GlobRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.Pattern == "" {
		return apperror.NewBadRequest("pattern is required")
	}

	result, err := provider.ListFiles(c.Request().Context(), ws.ProviderWorkspaceID, &FileListRequest{
		Pattern: req.Pattern,
		Path:    req.Path,
	})
	if err != nil {
		return apperror.NewInternal("glob search failed", err)
	}

	// Convert to path strings for simpler response
	paths := make([]string, 0, len(result.Files))
	for _, f := range result.Files {
		paths = append(paths, f.Path)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"matches": paths,
		"count":   len(paths),
	})
}

// GrepTool handles POST /api/v1/agent/workspaces/:id/grep
// @Summary      Grep content search
// @Description  Searches file contents with regex pattern in the workspace, returns structured matches
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body GrepRequest true "Search pattern"
// @Success      200 {object} GrepResponse "Search results"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/v1/agent/workspaces/{id}/grep [post]
// @Security     bearerAuth
func (h *Handler) GrepTool(c echo.Context) error {
	ws, provider, err := h.getWorkspaceAndProvider(c)
	if err != nil {
		return err
	}

	var req GrepRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.Pattern == "" {
		return apperror.NewBadRequest("pattern is required")
	}

	searchPath := req.Path
	if searchPath == "" {
		searchPath = workspaceDir
	}

	// Build grep command
	// Use grep -rn for recursive search with line numbers
	// Use -E for extended regex support
	cmd := fmt.Sprintf("grep -rnE %q %q", req.Pattern, searchPath)
	if req.Include != "" {
		cmd = fmt.Sprintf("grep -rnE --include=%q %q %q", req.Include, req.Pattern, searchPath)
	}
	// Limit output to prevent overwhelming results
	cmd += " 2>/dev/null || true"

	result, err := provider.Exec(c.Request().Context(), ws.ProviderWorkspaceID, &ExecRequest{
		Command:   cmd,
		TimeoutMs: 30000, // 30s timeout for grep
	})
	if err != nil {
		return apperror.NewInternal("grep execution failed", err)
	}

	// Parse grep output: file:line:content
	matches := parseGrepOutput(result.Stdout)

	return c.JSON(http.StatusOK, &GrepResponse{
		Matches: matches,
	})
}

// parseGrepOutput parses grep -rn output into structured matches.
func parseGrepOutput(output string) []GrepMatch {
	var matches []GrepMatch
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Format: filepath:linenum:content
		// Split at first two colons
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

		lineNum := parseInt(lineNumStr)
		if lineNum <= 0 {
			continue
		}

		matches = append(matches, GrepMatch{
			FilePath:   filePath,
			LineNumber: lineNum,
			Line:       content,
		})
	}
	return matches
}

// GitTool handles POST /api/v1/agent/workspaces/:id/git
// @Summary      Git operations
// @Description  Executes structured git operations (status, diff, commit, push, pull, checkout) without exposing credentials
// @Tags         agent-workspaces
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body GitRequest true "Git operation parameters"
// @Success      200 {object} GitResponse "Git output"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/v1/agent/workspaces/{id}/git [post]
// @Security     bearerAuth
func (h *Handler) GitTool(c echo.Context) error {
	ws, provider, err := h.getWorkspaceAndProvider(c)
	if err != nil {
		return err
	}

	var req GitRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.Action == "" {
		return apperror.NewBadRequest("action is required")
	}

	ctx := c.Request().Context()
	providerID := ws.ProviderWorkspaceID

	var cmd string
	switch req.Action {
	case "status":
		cmd = "git status --porcelain"
	case "diff":
		cmd = "git diff && echo '---STAGED---' && git diff --staged"
	case "commit":
		if req.Message == "" {
			return apperror.NewBadRequest("message is required for commit action")
		}
		// Stage specified files or all changes
		if len(req.Files) > 0 {
			fileArgs := make([]string, len(req.Files))
			for i, f := range req.Files {
				fileArgs[i] = fmt.Sprintf("%q", f)
			}
			cmd = fmt.Sprintf("git add %s && git commit -m %q", strings.Join(fileArgs, " "), req.Message)
		} else {
			cmd = fmt.Sprintf("git add -A && git commit -m %q", req.Message)
		}
	case "push":
		// Push uses server-managed credentials via checkout service
		if h.checkoutSvc != nil {
			result, err := h.checkoutSvc.InjectCredentialsForPush(ctx, provider, providerID, "git push")
			if err != nil {
				return apperror.NewInternal("git push failed", err)
			}
			output := sanitizeGitOutput(result.Stdout + "\n" + result.Stderr)
			return c.JSON(http.StatusOK, &GitResponse{Output: strings.TrimSpace(output)})
		}
		cmd = "git push"
	case "pull":
		// Pull uses server-managed credentials via checkout service
		if h.checkoutSvc != nil {
			result, err := h.checkoutSvc.InjectCredentialsForPush(ctx, provider, providerID, "git pull")
			if err != nil {
				return apperror.NewInternal("git pull failed", err)
			}
			output := sanitizeGitOutput(result.Stdout + "\n" + result.Stderr)
			return c.JSON(http.StatusOK, &GitResponse{Output: strings.TrimSpace(output)})
		}
		cmd = "git pull"
	case "checkout":
		if req.Branch == "" {
			return apperror.NewBadRequest("branch is required for checkout action")
		}
		cmd = fmt.Sprintf("git checkout -b %q 2>/dev/null || git checkout %q", req.Branch, req.Branch)
	default:
		return apperror.NewBadRequest(
			fmt.Sprintf("unsupported git action: %s (supported: status, diff, commit, push, pull, checkout)", req.Action),
		)
	}

	result, err := provider.Exec(ctx, providerID, &ExecRequest{
		Command:   cmd,
		TimeoutMs: 60000, // 60s timeout for git operations
	})
	if err != nil {
		return apperror.NewInternal("git operation failed", err)
	}

	output := result.Stdout
	if result.Stderr != "" {
		output += "\n" + result.Stderr
	}

	// Sanitize output — never expose credentials in response
	output = sanitizeGitOutput(output)

	return c.JSON(http.StatusOK, &GitResponse{
		Output: strings.TrimSpace(output),
	})
}

// sanitizeGitOutput removes potential credential leaks from git output.
func sanitizeGitOutput(output string) string {
	// Remove any URLs that might contain tokens
	lines := strings.Split(output, "\n")
	sanitized := make([]string, 0, len(lines))
	for _, line := range lines {
		// Mask any line containing authentication tokens in URLs
		if strings.Contains(line, "@github.com") && (strings.Contains(line, "https://") || strings.Contains(line, "http://")) {
			// Mask the token portion: https://TOKEN@github.com -> https://***@github.com
			parts := strings.SplitN(line, "@", 2)
			if len(parts) == 2 {
				protoIdx := strings.LastIndex(parts[0], "://")
				if protoIdx >= 0 {
					line = parts[0][:protoIdx+3] + "***@" + parts[1]
				}
			}
		}
		sanitized = append(sanitized, line)
	}
	return strings.Join(sanitized, "\n")
}
