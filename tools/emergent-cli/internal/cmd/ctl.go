package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/installer"
	"github.com/spf13/cobra"
)

var ctlFlags struct {
	dir    string
	follow bool
	lines  int
}

var ctlCmd = &cobra.Command{
	Use:   "ctl",
	Short: "Control Emergent services",
	Long: `Control and manage Emergent standalone services.

This command provides service management capabilities similar to emergent-ctl:
  - start/stop/restart services
  - view service status and logs
  - check server health
  - open shell in server container

Examples:
  emergent ctl start
  emergent ctl stop
  emergent ctl status
  emergent ctl logs -f
  emergent ctl logs server
  emergent ctl health`,
}

var ctlStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start all services",
	RunE:  runCtlStart,
}

var ctlStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all services",
	RunE:  runCtlStop,
}

var ctlRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart all services",
	RunE:  runCtlRestart,
}

var ctlStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show service status",
	RunE:  runCtlStatus,
}

var ctlLogsCmd = &cobra.Command{
	Use:   "logs [service]",
	Short: "Show service logs",
	Long: `Show logs from Emergent services.

Examples:
  emergent ctl logs           # Show recent logs from all services
  emergent ctl logs -f        # Follow logs in real-time
  emergent ctl logs server    # Show logs from server only
  emergent ctl logs -n 50     # Show last 50 lines`,
	RunE: runCtlLogs,
}

var ctlHealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check server health",
	RunE:  runCtlHealth,
}

var ctlShellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open shell in server container",
	RunE:  runCtlShell,
}

var ctlPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull latest Docker images",
	RunE:  runCtlPull,
}

func init() {
	homeDir, _ := os.UserHomeDir()
	defaultDir := filepath.Join(homeDir, ".emergent")

	ctlCmd.PersistentFlags().StringVar(&ctlFlags.dir, "dir", defaultDir, "Installation directory")

	ctlLogsCmd.Flags().BoolVarP(&ctlFlags.follow, "follow", "f", false, "Follow log output")
	ctlLogsCmd.Flags().IntVarP(&ctlFlags.lines, "lines", "n", 100, "Number of lines to show")

	ctlCmd.AddCommand(ctlStartCmd)
	ctlCmd.AddCommand(ctlStopCmd)
	ctlCmd.AddCommand(ctlRestartCmd)
	ctlCmd.AddCommand(ctlStatusCmd)
	ctlCmd.AddCommand(ctlLogsCmd)
	ctlCmd.AddCommand(ctlHealthCmd)
	ctlCmd.AddCommand(ctlShellCmd)
	ctlCmd.AddCommand(ctlPullCmd)

	rootCmd.AddCommand(ctlCmd)
}

func getDockerManager() (*installer.DockerManager, error) {
	cfg := installer.Config{InstallDir: ctlFlags.dir}
	inst := installer.New(cfg)

	if !inst.IsInstalled() {
		return nil, fmt.Errorf("no installation found at %s. Run 'emergent install' first", ctlFlags.dir)
	}

	output := &installer.DefaultOutput{}
	return installer.NewDockerManager(ctlFlags.dir, output), nil
}

func runCtlStart(cmd *cobra.Command, args []string) error {
	dm, err := getDockerManager()
	if err != nil {
		return err
	}

	fmt.Println("\033[0;34mStarting Emergent services...\033[0m")
	if err := dm.Up(); err != nil {
		return fmt.Errorf("failed to start services: %w", err)
	}
	fmt.Println("\033[0;32m✓ Services started\033[0m")
	return nil
}

func runCtlStop(cmd *cobra.Command, args []string) error {
	dm, err := getDockerManager()
	if err != nil {
		return err
	}

	fmt.Println("\033[0;34mStopping Emergent services...\033[0m")
	if err := dm.Down(false); err != nil {
		return fmt.Errorf("failed to stop services: %w", err)
	}
	fmt.Println("\033[0;32m✓ Services stopped\033[0m")
	return nil
}

func runCtlRestart(cmd *cobra.Command, args []string) error {
	dm, err := getDockerManager()
	if err != nil {
		return err
	}

	fmt.Println("\033[0;34mRestarting Emergent services...\033[0m")

	composePath := filepath.Join(ctlFlags.dir, "docker", "docker-compose.yml")
	envPath := filepath.Join(ctlFlags.dir, "config", ".env.local")

	execCmd := exec.Command("docker", "compose", "-f", composePath, "--env-file", envPath, "restart")
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		return fmt.Errorf("failed to restart services: %w", err)
	}

	fmt.Println("\033[0;32m✓ Services restarted\033[0m")
	_ = dm
	return nil
}

func runCtlStatus(cmd *cobra.Command, args []string) error {
	_, err := getDockerManager()
	if err != nil {
		return err
	}

	fmt.Println("\033[0;34mEmergent Service Status:\033[0m")

	composePath := filepath.Join(ctlFlags.dir, "docker", "docker-compose.yml")
	envPath := filepath.Join(ctlFlags.dir, "config", ".env.local")

	execCmd := exec.Command("docker", "compose", "-f", composePath, "--env-file", envPath, "ps")
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	return execCmd.Run()
}

func runCtlLogs(cmd *cobra.Command, args []string) error {
	_, err := getDockerManager()
	if err != nil {
		return err
	}

	composePath := filepath.Join(ctlFlags.dir, "docker", "docker-compose.yml")
	envPath := filepath.Join(ctlFlags.dir, "config", ".env.local")

	cmdArgs := []string{"compose", "-f", composePath, "--env-file", envPath, "logs"}

	if ctlFlags.follow {
		cmdArgs = append(cmdArgs, "-f")
	}

	cmdArgs = append(cmdArgs, "--tail", fmt.Sprintf("%d", ctlFlags.lines))

	if len(args) > 0 {
		cmdArgs = append(cmdArgs, args...)
	}

	execCmd := exec.Command("docker", cmdArgs...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	return execCmd.Run()
}

func runCtlHealth(cmd *cobra.Command, args []string) error {
	cfg := installer.Config{InstallDir: ctlFlags.dir}
	inst := installer.New(cfg)

	if !inst.IsInstalled() {
		return fmt.Errorf("no installation found at %s", ctlFlags.dir)
	}

	port := inst.GetServerPort()
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)

	fmt.Println("\033[0;34mChecking server health...\033[0m")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		fmt.Println("\033[0;31m✗ Server is not responding\033[0m")
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("\033[0;31m✗ Server returned status %d\033[0m\n", resp.StatusCode)
		return fmt.Errorf("unhealthy status: %d", resp.StatusCode)
	}

	fmt.Println("\033[0;32m✓ Server is healthy\033[0m")

	var prettyJSON map[string]interface{}
	if err := json.Unmarshal(body, &prettyJSON); err == nil {
		formatted, _ := json.MarshalIndent(prettyJSON, "", "  ")
		fmt.Println(string(formatted))
	} else {
		fmt.Println(strings.TrimSpace(string(body)))
	}

	return nil
}

func runCtlShell(cmd *cobra.Command, args []string) error {
	_, err := getDockerManager()
	if err != nil {
		return err
	}

	fmt.Println("\033[0;34mOpening shell in server container...\033[0m")

	execCmd := exec.Command("docker", "exec", "-it", "emergent-server", "sh")
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	return execCmd.Run()
}

func runCtlPull(cmd *cobra.Command, args []string) error {
	dm, err := getDockerManager()
	if err != nil {
		return err
	}

	fmt.Println("\033[0;34mPulling latest Docker images...\033[0m")
	if err := dm.Pull(); err != nil {
		return fmt.Errorf("failed to pull images: %w", err)
	}
	fmt.Println("\033[0;32m✓ Images updated\033[0m")
	return nil
}
