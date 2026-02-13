// Package installer provides installation, upgrade, and uninstallation logic for Emergent standalone deployments.
package installer

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config holds installation configuration
type Config struct {
	// InstallDir is the base directory for installation (default: ~/.emergent)
	InstallDir string
	// ServerPort is the port for the server (default: 3002)
	ServerPort int
	// GoogleAPIKey is the optional Google API key for embeddings
	GoogleAPIKey string
	// SkipStart skips starting services after installation
	SkipStart bool
	// Force overwrites existing installation
	Force bool
	// Verbose enables detailed output
	Verbose bool
}

// Installer handles Emergent installation operations
type Installer struct {
	config Config
	output OutputWriter
}

// OutputWriter handles formatted output
type OutputWriter interface {
	Info(format string, args ...interface{})
	Success(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
	Step(format string, args ...interface{})
}

// DefaultOutput implements OutputWriter with colored console output
type DefaultOutput struct {
	Verbose bool
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorGreen  = "\033[0;32m"
	colorRed    = "\033[0;31m"
	colorYellow = "\033[0;33m"
	colorCyan   = "\033[0;36m"
)

func (o *DefaultOutput) Info(format string, args ...interface{}) {
	fmt.Printf(colorCyan+format+colorReset+"\n", args...)
}

func (o *DefaultOutput) Success(format string, args ...interface{}) {
	fmt.Printf(colorGreen+"✓ "+format+colorReset+"\n", args...)
}

func (o *DefaultOutput) Warn(format string, args ...interface{}) {
	fmt.Printf(colorYellow+"⚠ "+format+colorReset+"\n", args...)
}

func (o *DefaultOutput) Error(format string, args ...interface{}) {
	fmt.Printf(colorRed+"✗ "+format+colorReset+"\n", args...)
}

func (o *DefaultOutput) Step(format string, args ...interface{}) {
	fmt.Printf(colorCyan+format+colorReset+"\n", args...)
}

// New creates a new Installer with the given configuration
func New(cfg Config) *Installer {
	return &Installer{
		config: cfg,
		output: &DefaultOutput{Verbose: cfg.Verbose},
	}
}

// SetOutput sets a custom output writer
func (i *Installer) SetOutput(o OutputWriter) {
	i.output = o
}

// CheckPrerequisites verifies Docker and Docker Compose are installed
func (i *Installer) CheckPrerequisites() error {
	// Check Docker
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker is not installed. Install from https://docs.docker.com/get-docker/")
	}

	// Check Docker Compose v2
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker Compose v2 is not installed. Install from https://docs.docker.com/compose/install/")
	}

	return nil
}

// IsInstalled checks if Emergent is already installed
func (i *Installer) IsInstalled() bool {
	composePath := filepath.Join(i.config.InstallDir, "docker", "docker-compose.yml")
	_, err := os.Stat(composePath)
	return err == nil
}

// GenerateSecret generates a cryptographically secure random hex string
func GenerateSecret(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CreateDirectories creates the installation directory structure
func (i *Installer) CreateDirectories() error {
	dirs := []string{
		filepath.Join(i.config.InstallDir, "bin"),
		filepath.Join(i.config.InstallDir, "config"),
		filepath.Join(i.config.InstallDir, "docker"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// GenerateEnvFile creates the .env.local configuration file
func (i *Installer) GenerateEnvFile() (string, error) {
	postgresPassword, err := GenerateSecret(32)
	if err != nil {
		return "", err
	}

	minioPassword, err := GenerateSecret(32)
	if err != nil {
		return "", err
	}

	apiKey, err := GenerateSecret(32)
	if err != nil {
		return "", err
	}

	envContent := fmt.Sprintf(`POSTGRES_USER=emergent
POSTGRES_PASSWORD=%s
POSTGRES_DB=emergent
POSTGRES_PORT=15432

MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=%s
MINIO_API_PORT=19000

STANDALONE_MODE=true
STANDALONE_API_KEY=%s
STANDALONE_USER_EMAIL=admin@localhost
STANDALONE_ORG_NAME=My Organization
STANDALONE_PROJECT_NAME=Default Project

KREUZBERG_PORT=18000
SERVER_PORT=%d

GOOGLE_API_KEY=%s
EMBEDDING_DIMENSION=768
KREUZBERG_LOG_LEVEL=info
`, postgresPassword, minioPassword, apiKey, i.config.ServerPort, i.config.GoogleAPIKey)

	envPath := filepath.Join(i.config.InstallDir, "config", ".env.local")
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return "", fmt.Errorf("failed to write env file: %w", err)
	}

	return apiKey, nil
}

// WriteDockerCompose writes the docker-compose.yml file
func (i *Installer) WriteDockerCompose() error {
	composePath := filepath.Join(i.config.InstallDir, "docker", "docker-compose.yml")
	content := GetDockerComposeTemplate()
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}
	return nil
}

// WriteInitSQL writes the init.sql file for PostgreSQL initialization
func (i *Installer) WriteInitSQL() error {
	initPath := filepath.Join(i.config.InstallDir, "docker", "init.sql")
	content := GetInitSQLTemplate()
	if err := os.WriteFile(initPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write init.sql: %w", err)
	}
	return nil
}

// WriteConfigYAML writes the CLI config.yaml file
func (i *Installer) WriteConfigYAML(apiKey string) error {
	configContent := fmt.Sprintf(`server_url: http://localhost:%d
api_key: %s
`, i.config.ServerPort, apiKey)

	configPath := filepath.Join(i.config.InstallDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return fmt.Errorf("failed to write config.yaml: %w", err)
	}
	return nil
}

// Install performs a fresh installation
func (i *Installer) Install() error {
	fmt.Printf("%sEmergent Standalone Installer%s\n", colorBold, colorReset)
	fmt.Println("==============================")
	fmt.Println()

	// Check prerequisites
	i.output.Step("Checking prerequisites...")
	if err := i.CheckPrerequisites(); err != nil {
		return err
	}
	i.output.Success("Docker and Docker Compose installed")

	// Check for existing installation
	if i.IsInstalled() && !i.config.Force {
		i.output.Info("Existing installation detected at %s", i.config.InstallDir)
		i.output.Info("Use --force to overwrite or run 'emergent upgrade' to update")
		return fmt.Errorf("installation already exists")
	}

	// Create directories
	i.output.Step("Creating installation directories...")
	if err := i.CreateDirectories(); err != nil {
		return err
	}
	i.output.Success("Directories created")

	// Generate configuration
	i.output.Step("Generating secure configuration...")
	apiKey, err := i.GenerateEnvFile()
	if err != nil {
		return err
	}
	i.output.Success("Configuration created")

	// Write docker-compose.yml
	i.output.Step("Writing Docker Compose configuration...")
	if err := i.WriteDockerCompose(); err != nil {
		return err
	}
	if err := i.WriteInitSQL(); err != nil {
		return err
	}
	i.output.Success("Docker configuration created")

	// Write CLI config
	if err := i.WriteConfigYAML(apiKey); err != nil {
		return err
	}
	i.output.Success("CLI configuration created at %s/config.yaml", i.config.InstallDir)

	if i.config.SkipStart {
		i.output.Info("Skipping service start (--skip-start)")
		i.printCompletionMessage(apiKey, false)
		// Prompt for Google API key if not provided via flag
		if i.config.GoogleAPIKey == "" {
			i.PromptGoogleAPIKey()
		}
		return nil
	}

	// Start services
	docker := NewDockerManager(i.config.InstallDir, i.output)

	i.output.Step("Pulling Docker images...")
	if err := docker.Pull(); err != nil {
		return err
	}
	i.output.Success("Images pulled")

	i.output.Step("Starting services...")
	if err := docker.Up(); err != nil {
		return err
	}
	i.output.Success("Services started")

	i.output.Step("Waiting for services to become healthy...")
	if err := docker.WaitForHealth(i.config.ServerPort, 120); err != nil {
		i.output.Warn("Health check timeout - check logs with: docker logs emergent-server")
	} else {
		i.output.Success("Server is healthy!")
	}

	i.printCompletionMessage(apiKey, true)

	// Prompt for Google API key if not provided via flag
	if i.config.GoogleAPIKey == "" {
		i.PromptGoogleAPIKey()
	}

	return nil
}

// Upgrade performs an upgrade of an existing installation.
// The version parameter should be the target version tag (e.g., "v0.7.3").
// If empty, falls back to "latest".
//
// On every upgrade, the docker-compose.yml and init.sql are fully regenerated
// from the embedded templates. This ensures upgrades always pick up new services,
// env vars, config changes, and corrected image names — regardless of what the
// user's existing compose file looks like. The .env.local (user secrets) is
// never touched.
func (i *Installer) Upgrade(version string) error {
	if !i.IsInstalled() {
		return fmt.Errorf("no existing installation found at %s", i.config.InstallDir)
	}

	i.output.Info("Upgrading server installation at %s", i.config.InstallDir)
	fmt.Println()

	currentVersion := i.GetInstalledVersion()
	i.output.Info("Current version: %s", currentVersion)

	// Determine image tag
	imageTag := "latest"
	if version != "" && version != "unknown" {
		imageTag = strings.TrimPrefix(version, "v")
		i.output.Info("Target version: %s", version)
	} else {
		i.output.Warn("Version not specified, using :latest tag")
	}
	fmt.Println()

	// Back up and regenerate docker-compose.yml from the embedded template.
	// This is the core of the hardened upgrade: we always write the latest template
	// rather than trying to regex-patch the old file. This guarantees new services,
	// env vars, image names, and config are always correct.
	composePath := filepath.Join(i.config.InstallDir, "docker", "docker-compose.yml")
	backupPath := composePath + ".bak"
	if err := copyFile(composePath, backupPath); err != nil {
		i.output.Warn("Could not backup docker-compose.yml: %v", err)
	} else {
		i.output.Info("Backed up docker-compose.yml to docker-compose.yml.bak")
	}

	composeContent := GetDockerComposeTemplateWithVersion(imageTag)
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}
	i.output.Success("Regenerated docker-compose.yml with image tag %s", imageTag)

	// Regenerate init.sql — only affects new database instances, but keeps it
	// in sync with the current version's requirements.
	initPath := filepath.Join(i.config.InstallDir, "docker", "init.sql")
	if err := os.WriteFile(initPath, []byte(GetInitSQLTemplate()), 0644); err != nil {
		i.output.Warn("Could not update init.sql: %v", err)
	}

	docker := NewDockerManager(i.config.InstallDir, i.output)

	i.output.Step("Pulling Docker images...")
	if err := docker.Pull(); err != nil {
		// Restore backup on pull failure so the user isn't left with a compose
		// file referencing an image that doesn't exist
		if restoreErr := copyFile(backupPath, composePath); restoreErr == nil {
			i.output.Warn("Restored docker-compose.yml from backup after pull failure")
		}
		return err
	}
	i.output.Success("Images updated")

	i.output.Step("Restarting services...")
	if err := docker.Up(); err != nil {
		return err
	}
	i.output.Success("Services restarted")

	i.output.Step("Waiting for services to become healthy...")
	if err := docker.WaitForHealth(i.config.ServerPort, 60); err != nil {
		i.output.Warn("Health check timeout - services may still be starting")
	} else {
		i.output.Success("Server is healthy!")
	}

	if version != "" && version != "unknown" {
		i.SaveInstalledVersion(version)
	}

	fmt.Println()
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s  ✓ Emergent Server Upgrade Complete!%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Println()

	return nil
}

// copyFile copies src to dst, preserving content. Used for backup/restore.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func (i *Installer) GetInstalledVersion() string {
	versionPath := filepath.Join(i.config.InstallDir, "version")
	content, err := os.ReadFile(versionPath)
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(content))
}

func (i *Installer) SaveInstalledVersion(version string) {
	versionPath := filepath.Join(i.config.InstallDir, "version")
	_ = os.WriteFile(versionPath, []byte(version+"\n"), 0644)
}

// Uninstall removes an Emergent installation
func (i *Installer) Uninstall(keepData bool) error {
	if !i.IsInstalled() {
		return fmt.Errorf("no installation found at %s", i.config.InstallDir)
	}

	docker := NewDockerManager(i.config.InstallDir, i.output)

	// Stop services — pass !keepData to remove volumes via docker compose down -v
	i.output.Step("Stopping services...")
	if err := docker.Down(!keepData); err != nil {
		i.output.Warn("Failed to stop services: %v", err)
	} else {
		i.output.Success("Services stopped")
	}

	if !keepData {
		i.output.Step("Removing Docker volumes...")
		if err := docker.RemoveVolumes(); err != nil {
			i.output.Warn("Failed to remove some volumes: %v", err)
		} else {
			i.output.Success("Volumes removed")
		}
	} else {
		i.output.Info("Keeping data volumes (--keep-data)")
	}

	i.output.Step("Removing installation directory...")
	if err := os.RemoveAll(i.config.InstallDir); err != nil {
		return fmt.Errorf("failed to remove installation directory: %w", err)
	}
	i.output.Success("Installation directory removed")

	fmt.Println()
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s  ✓ Emergent Uninstalled%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Println()

	if keepData {
		fmt.Printf("%sNote:%s Docker volumes were preserved. To remove them manually:\n", colorYellow, colorReset)
		fmt.Println("  docker volume rm docker_postgres_data docker_minio_data docker_emergent_cli_config")
	}

	return nil
}

// PromptGoogleAPIKey interactively asks the user for a Google API key and saves it
func (i *Installer) PromptGoogleAPIKey() {
	fmt.Println()
	fmt.Printf("%s%sGoogle API Key Setup (optional)%s\n", colorCyan, colorBold, colorReset)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("A Google API key enables AI-powered features including:")
	fmt.Println("  - Semantic search with text embeddings")
	fmt.Println("  - AI-powered document analysis")
	fmt.Println("  - Intelligent entity extraction")
	fmt.Println()
	fmt.Println("To get a Google API key:")
	fmt.Println("  1. Go to https://aistudio.google.com/apikey")
	fmt.Println("  2. Click 'Create API Key'")
	fmt.Println("  3. Copy the generated key")
	fmt.Println()
	fmt.Print("Enter your Google API key (press Enter to skip): ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		fmt.Println()
		i.output.Warn("Skipped. You can set it later with: emergent config set google_api_key YOUR_KEY")
		return
	}

	// Update the .env.local file
	envPath := i.GetEnvPath()
	content, err := os.ReadFile(envPath)
	if err != nil {
		i.output.Warn("Could not read config file: %v", err)
		return
	}

	lines := strings.Split(string(content), "\n")
	found := false
	for idx, line := range lines {
		if strings.HasPrefix(line, "GOOGLE_API_KEY=") {
			lines[idx] = "GOOGLE_API_KEY=" + input
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, "GOOGLE_API_KEY="+input)
	}

	if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		i.output.Warn("Could not save Google API key: %v", err)
		return
	}

	i.output.Success("Google API key saved to configuration")
}

func (i *Installer) printCompletionMessage(apiKey string, servicesStarted bool) {
	fmt.Println()
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s  ✓ Emergent Installation Complete!%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Println()

	fmt.Printf("Server URL: http://localhost:%d\n", i.config.ServerPort)
	fmt.Printf("API Key: %s\n", apiKey)
	fmt.Println()

	fmt.Printf("%s%sMCP Configuration%s\n", colorCyan, colorBold, colorReset)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("To connect your AI agent to Emergent, add this configuration:")
	fmt.Println()
	fmt.Printf("%sFor Claude Desktop:%s\n", colorBold, colorReset)
	fmt.Println("  File: ~/.config/claude/claude_desktop_config.json (macOS/Linux)")
	fmt.Println("  File: %APPDATA%\\Claude\\claude_desktop_config.json (Windows)")
	fmt.Println()
	fmt.Printf("%sConfiguration (copy this JSON):%s\n", colorBold, colorReset)
	fmt.Println()

	binPath := filepath.Join(i.config.InstallDir, "bin", "emergent")
	fmt.Printf(`{
  "mcpServers": {
    "emergent": {
      "command": "%s",
      "args": ["mcp"],
      "env": {
        "EMERGENT_SERVER_URL": "http://localhost:%d",
        "EMERGENT_API_KEY": "%s"
      }
    }
  }
}
`, strings.ReplaceAll(binPath, "\\", "\\\\"), i.config.ServerPort, apiKey)

	fmt.Println()
	fmt.Printf("%sNote:%s Restart your AI agent (Claude Desktop/VS Code) after adding this config.\n", colorYellow, colorReset)
	fmt.Println()
	fmt.Printf("%s📚 Documentation:%s https://github.com/emergent-company/emergent\n", colorCyan, colorReset)
	fmt.Println()
}

// GetEnvPath returns the path to the .env.local file
func (i *Installer) GetEnvPath() string {
	return filepath.Join(i.config.InstallDir, "config", ".env.local")
}

// GetServerPort reads the server port from the existing .env.local file
func (i *Installer) GetServerPort() int {
	envPath := i.GetEnvPath()
	content, err := os.ReadFile(envPath)
	if err != nil {
		return 3002 // default
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "SERVER_PORT=") {
			var port int
			if _, err := fmt.Sscanf(line, "SERVER_PORT=%d", &port); err == nil && port > 0 {
				return port
			}
		}
	}

	return 3002
}
