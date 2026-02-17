package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	// DefaultSetupCommandTimeout is the per-command timeout for setup commands (5 minutes).
	DefaultSetupCommandTimeout = 5 * time.Minute
)

// SetupExecutor runs setup commands inside a workspace after repository checkout.
type SetupExecutor struct {
	orchestrator *Orchestrator
	log          *slog.Logger
}

// NewSetupExecutor creates a new setup command executor.
func NewSetupExecutor(orchestrator *Orchestrator, log *slog.Logger) *SetupExecutor {
	return &SetupExecutor{
		orchestrator: orchestrator,
		log:          log.With("component", "workspace-setup"),
	}
}

// RunSetupCommands executes setup commands sequentially inside a workspace.
// Commands run in /workspace directory. If a command fails, remaining commands are skipped
// with a warning. Returns the number of commands executed successfully.
func (e *SetupExecutor) RunSetupCommands(ctx context.Context, ws *AgentWorkspace, commands []string) (int, error) {
	if len(commands) == 0 {
		return 0, nil
	}

	provider, err := e.orchestrator.GetProvider(ws.Provider)
	if err != nil {
		return 0, fmt.Errorf("provider %q not available: %w", ws.Provider, err)
	}

	e.log.Info("running setup commands",
		"workspace_id", ws.ID,
		"command_count", len(commands),
	)

	for i, cmd := range commands {
		cmdCtx, cancel := context.WithTimeout(ctx, DefaultSetupCommandTimeout)

		e.log.Info("executing setup command",
			"workspace_id", ws.ID,
			"command_index", i+1,
			"command_total", len(commands),
			"command", cmd,
		)

		result, err := provider.Exec(cmdCtx, ws.ProviderWorkspaceID, &ExecRequest{
			Command:   cmd,
			Workdir:   "/workspace",
			TimeoutMs: int(DefaultSetupCommandTimeout.Milliseconds()),
		})
		cancel()

		if err != nil {
			e.log.Warn("setup command execution failed, skipping remaining commands",
				"workspace_id", ws.ID,
				"command_index", i+1,
				"command", cmd,
				"error", err,
			)
			return i, fmt.Errorf("setup command %d failed: %w", i+1, err)
		}

		if result.ExitCode != 0 {
			e.log.Warn("setup command returned non-zero exit code, skipping remaining commands",
				"workspace_id", ws.ID,
				"command_index", i+1,
				"command", cmd,
				"exit_code", result.ExitCode,
				"stderr", truncateOutput(result.Stderr, 500),
			)
			return i, fmt.Errorf("setup command %d exited with code %d: %s", i+1, result.ExitCode, truncateOutput(result.Stderr, 200))
		}

		e.log.Info("setup command completed",
			"workspace_id", ws.ID,
			"command_index", i+1,
			"duration_ms", result.DurationMs,
		)
	}

	e.log.Info("all setup commands completed successfully",
		"workspace_id", ws.ID,
		"command_count", len(commands),
	)

	return len(commands), nil
}

// truncateOutput truncates a string to maxLen characters, adding "..." if truncated.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
