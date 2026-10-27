// Package installer provides installation, upgrade, and uninstallation logic for Memory standalone deployments.
package installer

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/envfile"
)

const (
	// envFileMode is the only mode this CLI accepts for .env.local. The file
	// holds provider API keys and, on a standalone install, the account-level
	// STANDALONE_API_KEY, so it must not be readable by other users.
	envFileMode os.FileMode = 0o600
	// configDirMode is the only mode this CLI accepts for <install>/config.
	// Its only content is .env.local, so group/world traversal buys nothing.
	configDirMode os.FileMode = 0o700
)

// Config holds installation configuration
type Config struct {
	// InstallDir is the base directory for installation (default: ~/.memory)
	InstallDir string
	// ServerPort is the port for the server (default: 3002)
	ServerPort int
	// LLM Provider configuration
	GoogleAPIKey string
	// OpenAI configuration
	OpenAIBaseURL string // kept for backward compat; maps to OPENAI_API_KEY template
	LLMModel      string
	// SkipStart skips starting services after installation
	SkipStart bool
	// Force overwrites existing installation
	Force bool
	// Verbose enables detailed output
	Verbose bool
}

// Installer handles Memory installation operations
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

// IsInstalled checks if Memory is already installed
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
	envDir := filepath.Join(i.config.InstallDir, "config")
	for _, dir := range []string{
		filepath.Join(i.config.InstallDir, "bin"),
		envDir,
		filepath.Join(i.config.InstallDir, "docker"),
	} {
		// The env directory is the only one that holds credentials, so it is the
		// only one created 0700; bin/ and docker/ hold executables and templates.
		mode := os.FileMode(0o755)
		if dir == envDir {
			mode = configDirMode
		}
		if err := os.MkdirAll(dir, mode); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// MkdirAll applies mode only when it creates a directory, so an install
	// created by an older CLI keeps its 0755 config directory. Tighten it when
	// it is ours to tighten: a directory owned by another user (root-installed
	// install, shared mount) or a filesystem that cannot express the mode is
	// tolerated (see tightenTolerated).
	if err := tightenExistingDir(envDir, configDirMode); err != nil {
		return err
	}

	return nil
}

// tightenExistingDir forces dir to mode when it is more permissive than that and
// we can chmod it. Best-effort by design: failing to tighten a directory we do
// not own must not fail the install or upgrade. A missing directory is a no-op:
// there is nothing to tighten, and an existing install that has no config/
// directory should not fail the upgrade over it.
func tightenExistingDir(dir string, mode os.FileMode) error {
	_, _, err := tightenExistingDirReporting(dir, mode)
	return err
}

// tightenExistingDirReporting is tightenExistingDir plus the mode the directory
// had and whether it was changed, so the upgrade path can say when it narrowed a
// directory the user pointed at explicitly instead of doing it silently.
func tightenExistingDirReporting(dir string, mode os.FileMode) (oldMode os.FileMode, changed bool, err error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to stat %s: %w", dir, err)
	}
	oldMode = info.Mode().Perm()
	if oldMode&0o077 == 0 {
		return oldMode, false, nil
	}
	if err := chmodPath(dir, mode); err != nil {
		if tightenTolerated(err) {
			return oldMode, false, nil
		}
		return oldMode, false, fmt.Errorf("failed to tighten permissions on %s: %w", dir, err)
	}
	return oldMode, true, nil
}

// chmodPath is os.Chmod. Tests swap it to exercise tightenTolerated without a
// real read-only mount or a root-owned directory.
var chmodPath = os.Chmod

// tightenTolerated reports whether a failed chmod on a directory we may not
// control is best-effort rather than fatal.
//
// A tighter directory mode is best-effort on a filesystem that cannot express
// it: the credentials live in the 0600 file inside, so failing an install or
// upgrade over the directory mode would break something that is otherwise fine.
//
//   - EPERM: we do not own the directory (root-installed or shared install).
//   - EROFS: the filesystem is mounted read-only.
//   - ENOTSUP: the filesystem does not implement chmod (some network mounts).
//     ENOTSUP and EOPNOTSUPP are the same value on Linux (95), so one constant
//     covers both deliberately — writing the pair would be dead code.
//
// EACCES is deliberately NOT tolerated: it means we lack permission on a
// directory we are supposed to own (or an ACL denies us), which is worth
// surfacing instead of leaving a permissive directory behind and reporting a
// successful install.
func tightenTolerated(err error) bool {
	return errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EROFS) ||
		errors.Is(err, syscall.ENOTSUP)
}

// tightenConfigDir best-effort tightens <installDir>/config to configDirMode.
// It warns instead of failing: an upgrade that is otherwise fine must not fail
// because a directory mode could not be applied (see tightenTolerated).
//
// Unlike item 1's default-path rule this runs for any --dir, so an explicit
// installation directory is narrowed too. That is safe (the only file inside is
// .env.local, already 0600, so 0700 removes listing and creation, not read
// access) but it is not silent: a change that actually lands is reported.
func (i *Installer) tightenConfigDir() {
	dir := filepath.Join(i.config.InstallDir, "config")
	oldMode, changed, err := tightenExistingDirReporting(dir, configDirMode)
	if err != nil {
		i.output.Warn("Could not tighten permissions on %s: %v", dir, err)
		return
	}
	if changed {
		i.output.Info("Tightened permissions on %s from %04o to %04o", dir, oldMode, configDirMode)
	}
}

// tightenExistingFile forces path to mode when it already exists. os.WriteFile
// applies its perm argument only when it creates the file, so without this an
// existing over-permissive file keeps its mode while gaining new secrets.
// Tightening before the write keeps the new value from ever being briefly
// readable at the old mode.
func tightenExistingFile(path string, mode os.FileMode) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("failed to tighten permissions on %s: %w", path, err)
	}
	return nil
}

// GenerateEnvFile creates the .env.local configuration file
func (i *Installer) GenerateEnvFile() (string, error) {
	postgresPassword, err := GenerateSecret(32)
	if err != nil {
		return "", err
	}

	objectStoreSecret, err := GenerateSecret(32)
	if err != nil {
		return "", err
	}

	apiKey, err := GenerateSecret(32)
	if err != nil {
		return "", err
	}

	llmEncryptionKey, err := GenerateSecret(32)
	if err != nil {
		return "", err
	}

	// The provider settings are left empty and filled in by UpdateAll below:
	// their values come from flags or the environment and can contain '#', '$',
	// quotes or surrounding whitespace, which raw interpolation would let the
	// server misread (a " #" truncates the value) or make the file unparseable
	// from the first boot. That is the creation-time half of #2148.
	envContent := fmt.Sprintf(`POSTGRES_USER=emergent
POSTGRES_PASSWORD=%s
POSTGRES_DB=emergent
POSTGRES_PORT=15432

OBJECT_STORE_ACCESS_KEY=emergent
OBJECT_STORE_SECRET_KEY=%s
OBJECT_STORE_API_PORT=19000
STORAGE_REGION=us-east-1

STANDALONE_MODE=true
STANDALONE_API_KEY=%s
STANDALONE_USER_EMAIL=admin@localhost
STANDALONE_ORG_NAME=My Organization
STANDALONE_PROJECT_NAME=Default Project

XBERG_PORT=18000
SERVER_PORT=%d

GOOGLE_API_KEY=
OPENAI_API_KEY=
OPENAI_MODEL=
EMBEDDING_DIMENSION=768
XBERG_LOG_LEVEL=info

LLM_ENCRYPTION_KEY=%s
`, postgresPassword, objectStoreSecret, apiKey, i.config.ServerPort, llmEncryptionKey)

	envPath := filepath.Join(i.config.InstallDir, "config", ".env.local")
	if err := tightenExistingFile(envPath, envFileMode); err != nil {
		return "", err
	}
	if err := os.WriteFile(envPath, []byte(envContent), envFileMode); err != nil {
		return "", fmt.Errorf("failed to write env file: %w", err)
	}

	// Empty values are written too, so the file keeps the full key set the
	// template has always carried and the server sees "" rather than a missing
	// key (the same shape as an unset flag before this change).
	if err := envfile.UpdateAll(envPath, map[string]string{
		"GOOGLE_API_KEY": i.config.GoogleAPIKey,
		"OPENAI_API_KEY": i.config.OpenAIBaseURL,
		"OPENAI_MODEL":   i.config.LLMModel,
	}, envFileMode); err != nil {
		return "", fmt.Errorf("failed to write provider settings: %w", err)
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

// WriteTempoConfig writes the Grafana Tempo configuration file.
func (i *Installer) WriteTempoConfig() error {
	tempoDir := filepath.Join(i.config.InstallDir, "docker", "tempo")
	if err := os.MkdirAll(tempoDir, 0755); err != nil {
		return fmt.Errorf("failed to create tempo directory: %w", err)
	}
	tempoPath := filepath.Join(tempoDir, "tempo.yaml")
	if err := os.WriteFile(tempoPath, []byte(GetTempoConfigTemplate()), 0644); err != nil {
		return fmt.Errorf("failed to write tempo.yaml: %w", err)
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
	fmt.Printf("%sMemory Standalone Installer%s\n", colorBold, colorReset)
	fmt.Println("=============================")
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
		i.output.Info("Use --force to overwrite or run 'memory upgrade' to update")
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
	if err := i.WriteTempoConfig(); err != nil {
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
		i.printCompletionMessage(apiKey, false, i.config.GoogleAPIKey != "" || i.config.OpenAIBaseURL != "")
		// Prompt for LLM provider if not provided via flag
		if i.config.GoogleAPIKey == "" && i.config.OpenAIBaseURL == "" {
			i.PromptLLMProvider()
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
		i.output.Warn("Health check timeout - check logs with: docker logs memory-server")
	} else {
		i.output.Success("Server is healthy!")
	}

	// Build sandbox images required by the agent (non-fatal if Docker unavailable).
	i.output.Step("Building sandbox images...")
	if err := docker.BuildSandboxImages(GetWorkspaceBaseDockerfile(), WorkspaceBaseImage, gitTokenSecretEnv()); err != nil {
		i.output.Warn("Could not build workspace base sandbox image (non-fatal): %v", err)
	} else {
		i.output.Success("Workspace base sandbox image ready")
	}
	if err := docker.BuildSandboxImages(GetPythonSDKDockerfile(), "emergent-memory-python-sdk:latest", ""); err != nil {
		i.output.Warn("Could not build Python SDK sandbox image (non-fatal): %v", err)
	} else {
		i.output.Success("Python SDK sandbox image ready")
	}
	if err := docker.BuildSandboxImages(GetGoSDKDockerfile(), "emergent-memory-go-sdk:latest", ""); err != nil {
		i.output.Warn("Could not build Go SDK sandbox image (non-fatal): %v", err)
	} else {
		i.output.Success("Go SDK sandbox image ready")
	}

	i.printCompletionMessage(apiKey, true, i.config.GoogleAPIKey != "" || i.config.OpenAIBaseURL != "")

	// Prompt for LLM provider if not provided via flag
	if i.config.GoogleAPIKey == "" && i.config.OpenAIBaseURL == "" {
		i.PromptLLMProvider()
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

	// An install created by an older CLI keeps a 0755 config directory:
	// MkdirAll only applies the mode on create and Install bails out when a
	// standalone install already exists, so Upgrade is the only path that ever
	// sees one (#2119). Idempotent, and it warns rather than fails: the file
	// inside is already 0600, so the directory mode hides a filename, not a
	// secret, and must not break an otherwise fine upgrade.
	i.tightenConfigDir()

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

	// Regenerate tempo/tempo.yaml — ensures Tempo config is always present and up to date.
	if err := i.WriteTempoConfig(); err != nil {
		i.output.Warn("Could not update tempo/tempo.yaml: %v", err)
	}

	docker := NewDockerManager(i.config.InstallDir, i.output)

	// --- PostgreSQL major-version upgrade ---
	// Detect the current on-disk PostgreSQL major version from the data volume.
	// If it is older than what the new template expects, run pg_upgrade in-place
	// before bringing up the new containers. This is fully transparent to the user.
	i.output.Step("Checking PostgreSQL data volume version...")
	currentPgVersion, pgVolumeName, err := detectPostgresVersion(composePath)
	if err != nil {
		i.output.Warn("Could not detect PostgreSQL version: %v — skipping automatic upgrade", err)
	} else if currentPgVersion > 0 && currentPgVersion < PostgresMajorVersion {
		i.output.Info("Detected PostgreSQL %d on disk (volume: %s), target is PostgreSQL %d", currentPgVersion, pgVolumeName, PostgresMajorVersion)
		i.output.Step("Performing automatic PostgreSQL major-version upgrade...")
		fmt.Println()
		upgraded, upgradeErr := i.RunPostgresUpgrade(docker, pgVolumeName)
		if upgradeErr != nil {
			// Put the previous compose file back only when the volume is still on
			// the old major. A failure after the upgrade step leaves 18 data that
			// a pre-18 image refuses to open (it rejects settings it does not know,
			// e.g. autovacuum_worker_slots), so restoring the old file there would
			// turn one error into a crash loop.
			if !upgraded {
				if restoreErr := copyFile(backupPath, composePath); restoreErr == nil {
					i.output.Warn("Restored docker-compose.yml from backup after failed pg_upgrade")
				}
			}
			return fmt.Errorf("PostgreSQL upgrade failed: %w", upgradeErr)
		}
		fmt.Println()
	} else if currentPgVersion == PostgresMajorVersion {
		i.output.Success("PostgreSQL %d already up to date (volume: %s)", currentPgVersion, pgVolumeName)
	} else {
		i.output.Info("No existing PostgreSQL data volume found (checked: %s) — fresh database will be created", pgVolumeName)
	}
	// --- end PostgreSQL upgrade ---

	// Warn about secrets the existing .env.local lacks before any container is
	// recreated: an incomplete file is how a migrated install ends up with a
	// server that cannot encrypt credentials (#2004).
	i.output.Step("Checking installation secrets...")
	i.checkEnvSecrets()

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

	// Record the running image's collation version on each database (see
	// refreshCollationVersions). The major upgrade rebuilt every index inside
	// the upgrade image, so a mismatch left behind by the distro move would be
	// logged on every connection. Done on every upgrade rather than only when
	// this run migrated the volume: it is a no-op when the versions agree, and
	// that is exactly what heals the case where the upgrade container failed
	// after moving the data and the user re-ran `memory upgrade`.
	i.output.Step("Refreshing collation versions...")
	if err := i.refreshCollationVersions(docker); err != nil {
		i.output.Warn("Could not refresh collation versions: %v", err)
		i.output.Warn("The database is usable; silence the warning with `ALTER DATABASE <name> REFRESH COLLATION VERSION`.")
	} else {
		i.output.Success("Collation versions refreshed")
	}

	// Build sandbox images required by the agent (non-fatal if Docker unavailable).
	i.output.Step("Building sandbox images...")
	if err := docker.BuildSandboxImages(GetWorkspaceBaseDockerfile(), WorkspaceBaseImage, gitTokenSecretEnv()); err != nil {
		i.output.Warn("Could not build workspace base sandbox image (non-fatal): %v", err)
	} else {
		i.output.Success("Workspace base sandbox image ready")
	}
	if err := docker.BuildSandboxImages(GetPythonSDKDockerfile(), "emergent-memory-python-sdk:latest", ""); err != nil {
		i.output.Warn("Could not build Python SDK sandbox image (non-fatal): %v", err)
	} else {
		i.output.Success("Python SDK sandbox image ready")
	}
	if err := docker.BuildSandboxImages(GetGoSDKDockerfile(), "emergent-memory-go-sdk:latest", ""); err != nil {
		i.output.Warn("Could not build Go SDK sandbox image (non-fatal): %v", err)
	} else {
		i.output.Success("Go SDK sandbox image ready")
	}

	if version != "" && version != "unknown" {
		i.SaveInstalledVersion(version)
	}

	fmt.Println()
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s  ✓ Memory Server Upgrade Complete!%s\n", colorGreen, colorBold, colorReset)
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
	return NormalizeVersion(string(content))
}

// SaveInstalledVersion records the installed server version in
// <installDir>/version using the canonical form (bare semver, no "v" or
// "cli-" prefix) so a version recorded by one CLI build compares equal to the
// image tag read back by `memory server doctor` (#2004).
func (i *Installer) SaveInstalledVersion(version string) {
	versionPath := filepath.Join(i.config.InstallDir, "version")
	_ = os.WriteFile(versionPath, []byte(NormalizeVersion(version)+"\n"), 0644)
}

// NormalizeVersion returns the canonical form of a server version string: the
// bare semver with any "cli-" and "v" prefix removed.
//
// The release workflow strips the leading "v" from the ldflags Version while
// the container image tag keeps it, so `0.87.4` and `v0.87.4` denote the same
// server and must compare equal. Both formats resolve; neither is "wrong".
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "cli-")
	v = strings.TrimPrefix(v, "v")
	return v
}

// requiredEnvKeys are the secrets the regenerated compose template interpolates
// that have no safe default: missing ones either break authentication
// (STANDALONE_API_KEY), disable credential encryption (LLM_ENCRYPTION_KEY) or
// silently fall back to a well-known placeholder (POSTGRES_PASSWORD,
// OBJECT_STORE_ACCESS_KEY, OBJECT_STORE_SECRET_KEY).
var requiredEnvKeys = []string{
	"POSTGRES_PASSWORD",
	"STANDALONE_API_KEY",
	"LLM_ENCRYPTION_KEY",
	"OBJECT_STORE_ACCESS_KEY",
	"OBJECT_STORE_SECRET_KEY",
}

// checkEnvSecrets warns, before containers are recreated, about secrets the
// existing .env.local does not define. Installs created before a key was
// introduced (or before the MinIO→SeaweedFS rename) keep their old file — the
// upgrade never rewrites user secrets — so the first sign of the gap used to be
// a server log line after the restart (#2004). Never fails the upgrade: the
// stack may legitimately be partially configured.
func (i *Installer) checkEnvSecrets() {
	envPath := i.GetEnvPath()
	content, err := os.ReadFile(envPath)
	if err != nil {
		i.output.Warn("Could not read %s: %v", envPath, err)
		return
	}

	for _, key := range missingEnvKeys(string(content), requiredEnvKeys) {
		i.output.Warn("%s is not set in %s — containers will start without it (or with the compose default)", key, envPath)
	}

	if hasLegacyMinioKeys(string(content)) && !envKeySet(string(content), "OBJECT_STORE_SECRET_KEY") {
		i.output.Info("Found legacy MINIO_ROOT_USER/MINIO_ROOT_PASSWORD keys; SeaweedFS reads OBJECT_STORE_ACCESS_KEY/OBJECT_STORE_SECRET_KEY — see deploy/self-hosted/UPGRADING_OBJECT_STORE.md")
	}
}

// missingEnvKeys returns the subset of required that content does not define
// with a non-empty value.
func missingEnvKeys(content string, required []string) []string {
	defined := envfile.ReadAll([]byte(content))

	var missing []string
	for _, key := range required {
		if defined[key] == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

// envKeySet reports whether content defines key with a non-empty value.
func envKeySet(content, key string) bool {
	v, ok := envfile.Read([]byte(content), key)
	return ok && v != ""
}

// hasLegacyMinioKeys reports whether content still carries the MinIO
// credentials used before the object store moved to SeaweedFS.
func hasLegacyMinioKeys(content string) bool {
	return envKeySet(content, "MINIO_ROOT_USER") || envKeySet(content, "MINIO_ROOT_PASSWORD")
}

// Uninstall removes a Memory installation
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
	fmt.Printf("%s%s  ✓ Memory Uninstalled%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Println()

	if keepData {
		fmt.Printf("%sNote:%s Docker volumes were preserved. To remove them manually:\n", colorYellow, colorReset)
		fmt.Println("  docker volume rm docker_postgres_data docker_object_store_data docker_memory_cli_config")
	}

	return nil
}

// PromptLLMProvider interactively asks the user to configure an LLM provider (Google AI or OpenAI)
func (i *Installer) PromptLLMProvider() {
	fmt.Println()
	fmt.Printf("%s%sLLM Provider Setup (optional)%s\n", colorCyan, colorBold, colorReset)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("An LLM provider enables AI-powered features including:")
	fmt.Println("  - Semantic search with text embeddings")
	fmt.Println("  - AI-powered document analysis")
	fmt.Println("  - Intelligent entity extraction")
	fmt.Println()
	fmt.Println("Choose a provider to configure:")
	fmt.Println("  1. Google AI (Gemini) — requires an API key")
	fmt.Println("  2. OpenAI API — requires an API key")
	fmt.Println("  3. Skip for now")
	fmt.Println()
	fmt.Print("Choice [1-3]: ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		i.PromptGoogleAI()
	case "2":
		i.PromptOpenAICompatible()
	default:
		fmt.Println()
		i.output.Warn("Skipped. You can configure a provider later with the 'memory provider' command.")
	}
}

// PromptGoogleAI interactively asks the user for a Google API key and saves it
func (i *Installer) PromptGoogleAI() {
	fmt.Println()
	fmt.Printf("%s%sGoogle AI Setup%s\n", colorCyan, colorBold, colorReset)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("To get a Google API key:")
	fmt.Println("  1. Go to https://aistudio.google.com/apikey")
	fmt.Println("  2. Click 'Create API Key'")
	fmt.Println("  3. Copy the generated key")
	fmt.Println()
	fmt.Print("Enter your Google API key: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		i.output.Warn("No key provided. Skipping Google AI setup.")
		return
	}

	if err := i.updateEnv(map[string]string{"GOOGLE_API_KEY": input}); err != nil {
		i.output.Error("Could not save Google API key: %v", err)
		return
	}

	i.output.Success("Google AI configured successfully")
}

// PromptOpenAICompatible interactively asks the user for OpenAI settings and saves them
func (i *Installer) PromptOpenAICompatible() {
	fmt.Println()
	fmt.Printf("%s%sOpenAI API Setup%s\n", colorCyan, colorBold, colorReset)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("Configure settings for OpenAI or a compatible provider.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("API Key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		i.output.Warn("API key is required. Skipping OpenAI setup.")
		return
	}

	fmt.Print("Base URL (leave blank for https://api.openai.com/v1): ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)

	fmt.Print("Model Name (e.g., openai/gpt-4o): ")
	model, _ := reader.ReadString('\n')
	model = strings.TrimSpace(model)

	updates := map[string]string{
		"OPENAI_API_KEY": apiKey,
	}
	if baseURL != "" {
		updates["OPENAI_BASE_URL"] = baseURL
	}
	if model != "" {
		updates["OPENAI_MODEL"] = model
	}

	if err := i.updateEnv(updates); err != nil {
		i.output.Error("Could not save OpenAI settings: %v", err)
		return
	}

	i.output.Success("OpenAI API configured successfully")
}

// updateEnv updates one or more variables in the .env.local file.
//
// It goes through apps/cli/internal/envfile like every other env-file edit in
// this CLI: the quoting fallback, the round-trip guard and the atomic replace are
// shared, instead of a second raw writer that could corrupt a key the user
// pasted into the wizard (#2148). UpdateAll keeps the old all-or-nothing
// property: a value that cannot be represented refuses the whole update with the
// file byte-identical, rather than writing the keys before it.
func (i *Installer) updateEnv(updates map[string]string) error {
	return envfile.UpdateAll(i.GetEnvPath(), updates, envFileMode)
}

func (i *Installer) printCompletionMessage(apiKey string, servicesStarted bool, providerConfigured bool) {
	fmt.Println()
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s  ✓ Memory Installation Complete!%s\n", colorGreen, colorBold, colorReset)
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorGreen, colorBold, colorReset)
	fmt.Println()

	fmt.Printf("Server URL: http://localhost:%d\n", i.config.ServerPort)
	fmt.Printf("API Key: %s\n", apiKey)
	fmt.Println()

	fmt.Printf("%s%sMCP Configuration%s\n", colorCyan, colorBold, colorReset)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("To connect your AI agent to Memory, add this configuration:")
	fmt.Println()
	fmt.Printf("%sFor Claude Desktop:%s\n", colorBold, colorReset)
	fmt.Println("  File: ~/.config/claude/claude_desktop_config.json (macOS/Linux)")
	fmt.Println("  File: %APPDATA%\\Claude\\claude_desktop_config.json (Windows)")
	fmt.Println()
	fmt.Printf("%sConfiguration (copy this JSON):%s\n", colorBold, colorReset)
	fmt.Println()

	binPath := filepath.Join(i.config.InstallDir, "bin", "memory")
	fmt.Printf(`{
  "mcpServers": {
    "memory": {
      "command": "%s",
      "args": ["mcp"],
      "env": {
        "MEMORY_SERVER_URL": "http://localhost:%d",
        "MEMORY_ACCOUNT_API_KEY": "%s"
      }
    }
  }
}
`, strings.ReplaceAll(binPath, "\\", "\\\\"), i.config.ServerPort, apiKey)

	fmt.Println()
	fmt.Printf("%sNote:%s Restart your AI agent (Claude Desktop/VS Code) after adding this config.\n", colorYellow, colorReset)
	fmt.Println()
	fmt.Printf("%s📚 Documentation:%s https://github.com/emergent-company/emergent.memory\n", colorCyan, colorReset)
	fmt.Println()

	if !providerConfigured {
		fmt.Printf("%s%sLLM Provider Setup (required for AI features)%s\n", colorYellow, colorBold, colorReset)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println()
		fmt.Println("No LLM provider was configured during install.")
		fmt.Println("AI features (embeddings, extraction, chat) will not work until you add one.")
		fmt.Println()
		fmt.Println("To configure a provider, run:")
		fmt.Println("  memory provider configure google --api-key <your-key>")
		fmt.Println("  OR")
		fmt.Println("  memory provider configure openai-compatible --base-url <url> --api-key <key> --generative-model <model>")
		fmt.Println()
	}
}

// GetEnvPath returns the path to the .env.local file
func (i *Installer) GetEnvPath() string {
	return filepath.Join(i.config.InstallDir, "config", ".env.local")
}

// GetServerPort reads the server port from the existing .env.local file.
// envfile owns the grammar for these files (#2148, #2174): a value it quoted —
// or one a human wrote with `export`, spaces around the separator, or a trailing
// comment — has to resolve to the same port here, or the CLI configures and
// health-checks a URL the server is not listening on (#2178).
func (i *Installer) GetServerPort() int {
	content, err := os.ReadFile(i.GetEnvPath())
	if err != nil {
		return 3002 // default
	}

	raw, ok := envfile.Read(content, "SERVER_PORT")
	if !ok {
		return 3002
	}
	if port, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && port > 0 {
		return port
	}

	return 3002
}
