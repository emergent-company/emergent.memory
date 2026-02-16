package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/installer"
	"github.com/spf13/cobra"
)

var upgradeFlags struct {
	dir     string
	force   bool
	cliOnly bool
}

// Package manager detection functions
func isPackageManagerInstalled() (string, bool) {
	// Check if installed via pacman (Arch Linux)
	if isArchLinux() {
		cmd := exec.Command("pacman", "-Qi", "emergent")
		if err := cmd.Run(); err == nil {
			return "pacman", true
		}
	}
	// Future: check for dpkg (Debian/Ubuntu), rpm (Fedora/RHEL), etc.
	return "", false
}

func isArchLinux() bool {
	// Check for Arch-specific files
	if _, err := os.Stat("/etc/arch-release"); err == nil {
		return true
	}
	if _, err := os.Stat("/etc/manjaro-release"); err == nil {
		return true
	}
	// Check if pacman exists
	if _, err := exec.LookPath("pacman"); err == nil {
		return true
	}
	return false
}

func isCLIInPath() bool {
	path, err := exec.LookPath("emergent")
	if err != nil {
		return false
	}
	// Verify it's actually the emergent CLI (not just any executable named emergent)
	cmd := exec.Command(path, "version")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Emergent (CLI and server)",
	Long: `Upgrades Emergent to the latest version.

For standalone installations (Docker-based):
  - Upgrades the CLI binary
  - Pulls the latest versioned Docker images
  - Restarts services with updated images

For CLI-only installations:
  - Upgrades just the CLI binary

Examples:
  emergent upgrade              # Upgrade everything
  emergent upgrade --cli-only   # Upgrade CLI only (skip server)
  emergent upgrade server       # Legacy: upgrade server only`,
	Run: runUpgrade,
}

var upgradeServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Upgrade the standalone server installation",
	Long: `Upgrades the Emergent standalone server installation.

This will:
  - Pull the latest Docker images
  - Restart services with the new images
  - Preserve all existing configuration and data

Examples:
  emergent upgrade server
  emergent upgrade server --dir ~/.emergent`,
	RunE: runUpgradeServer,
}

func init() {
	homeDir, _ := os.UserHomeDir()
	defaultDir := filepath.Join(homeDir, ".emergent")

	upgradeCmd.Flags().BoolVarP(&upgradeFlags.force, "force", "f", false, "Force upgrade even for dev versions")
	upgradeCmd.Flags().BoolVar(&upgradeFlags.cliOnly, "cli-only", false, "Only upgrade CLI, skip server")
	upgradeCmd.Flags().StringVar(&upgradeFlags.dir, "dir", defaultDir, "Installation directory")

	upgradeServerCmd.Flags().StringVar(&upgradeFlags.dir, "dir", defaultDir, "Installation directory")
	upgradeServerCmd.Flags().BoolVarP(&upgradeFlags.force, "force", "f", false, "Force upgrade without confirmation")

	upgradeCmd.AddCommand(upgradeServerCmd)
	rootCmd.AddCommand(upgradeCmd)
}

type Release struct {
	TagName     string  `json:"tag_name"`
	Assets      []Asset `json:"assets"`
	ImagesReady bool
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runUpgradeServer(cmd *cobra.Command, args []string) error {
	cfg := installer.Config{
		InstallDir: upgradeFlags.dir,
		Verbose:    true,
	}

	inst := installer.New(cfg)

	if !inst.IsInstalled() {
		return fmt.Errorf("no installation found at %s. Run 'emergent install' first", upgradeFlags.dir)
	}

	// Check if Docker images are ready before allowing server upgrade
	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("error checking for updates: %w", err)
	}

	if !release.ImagesReady && !upgradeFlags.force {
		displayLatest := strings.TrimPrefix(release.TagName, "v")
		fmt.Println()
		fmt.Println("⚠️  Docker images for this release are still being built")
		fmt.Printf("Latest release: %s\n", displayLatest)
		fmt.Println()
		fmt.Println("Please wait a few minutes and try again.")
		fmt.Println("Use --force to skip this check.")
		fmt.Println()
		return nil
	}

	cfg.ServerPort = inst.GetServerPort()

	if !upgradeFlags.force {
		fmt.Print("Upgrade server installation? [y/N]: ")
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" {
			fmt.Println("Upgrade canceled.")
			return nil
		}
	}

	return inst.Upgrade(release.TagName)
}

func runUpgrade(cmd *cobra.Command, args []string) {
	fmt.Println("Checking for updates...")

	// Check if installed via package manager
	pkgMgr, installedViaPkgMgr := isPackageManagerInstalled()
	if installedViaPkgMgr {
		fmt.Println()
		fmt.Printf("⚠️  Emergent is installed via %s package manager\n", pkgMgr)
		fmt.Println()
		fmt.Println("To upgrade, use your system package manager:")
		switch pkgMgr {
		case "pacman":
			fmt.Println("  sudo pacman -Syu emergent")
		}
		fmt.Println()
		fmt.Println("The 'emergent upgrade' command is for standalone installations only.")
		fmt.Println()
		return
	}

	// Check if CLI is in PATH (for standalone installations)
	if !isCLIInPath() {
		fmt.Println()
		fmt.Println("⚠️  Warning: emergent CLI is not in your PATH")
		fmt.Println()
		currentExec, err := os.Executable()
		if err == nil {
			execDir := filepath.Dir(currentExec)
			fmt.Printf("Add to PATH: export PATH=\"%s:$PATH\"\n", execDir)
			fmt.Println()
		}
	}

	release, err := getLatestRelease()
	if err != nil {
		fmt.Printf("Error checking for updates: %v\n", err)
		os.Exit(1)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "cli-")
	latestVersion = strings.TrimPrefix(latestVersion, "v")
	currentVersion := strings.TrimPrefix(Version, "cli-")
	currentVersion = strings.TrimPrefix(currentVersion, "v")

	// Check if server is installed
	cfg := installer.Config{
		InstallDir: upgradeFlags.dir,
		Verbose:    true,
	}
	inst := installer.New(cfg)
	serverInstalled := inst.IsInstalled()

	if Version == "dev" && !upgradeFlags.force {
		displayLatest := strings.TrimPrefix(release.TagName, "v")
		fmt.Println("You are running a development version. Upgrade skipped.")
		fmt.Printf("Latest release: %s\n", displayLatest)
		fmt.Println("Use --force to upgrade anyway.")
		return
	}

	cliNeedsUpgrade := latestVersion != currentVersion || upgradeFlags.force

	if !cliNeedsUpgrade && !serverInstalled {
		displayCurrent := strings.TrimPrefix(Version, "v")
		fmt.Printf("You are already using the latest version: %s\n", displayCurrent)
		return
	}

	if serverInstalled && !upgradeFlags.cliOnly && !release.ImagesReady {
		displayLatest := strings.TrimPrefix(release.TagName, "v")
		fmt.Println()
		fmt.Println("⚠️  Warning: Docker images for this release are still being built")
		fmt.Printf("Latest release: %s\n", displayLatest)
		fmt.Println()
		fmt.Println("The CLI can be upgraded now, but server upgrade should wait until")
		fmt.Println("Docker images are available (usually takes 5-10 minutes after release).")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  1. Wait a few minutes and try again")
		fmt.Println("  2. Upgrade CLI only now with: emergent upgrade --cli-only")
		fmt.Println()
		return
	}

	// Show what will be upgraded (normalize display - strip "v" prefix for consistency)
	fmt.Println()
	displayCurrent := strings.TrimPrefix(Version, "v")
	displayLatest := strings.TrimPrefix(release.TagName, "v")
	fmt.Printf("Current CLI version: %s\n", displayCurrent)
	fmt.Printf("Latest version: %s\n", displayLatest)
	if serverInstalled && !upgradeFlags.cliOnly {
		fmt.Printf("Server installation: %s\n", upgradeFlags.dir)
	}
	fmt.Println()

	if cliNeedsUpgrade {
		if Version == "dev" {
			fmt.Printf("Will upgrade CLI from dev version to %s\n", displayLatest)
		} else if latestVersion == currentVersion {
			fmt.Printf("Will reinstall CLI %s\n", displayLatest)
		} else {
			fmt.Printf("Will upgrade CLI: %s → %s\n", displayCurrent, displayLatest)
		}
	} else {
		fmt.Println("CLI is up to date")
	}

	if serverInstalled && !upgradeFlags.cliOnly {
		fmt.Println("Will upgrade server: pull latest images and restart")
	}
	fmt.Println()

	if !upgradeFlags.force {
		fmt.Print("Proceed with upgrade? [y/N]: ")
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" {
			fmt.Println("Upgrade canceled.")
			return
		}
	}

	// Upgrade CLI
	if cliNeedsUpgrade {
		assetURL, assetName, err := findAsset(release.Assets)
		if err != nil {
			fmt.Printf("Error finding compatible asset: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Downloading %s...\n", assetName)
		newBinaryPath, err := installUpdate(assetURL, assetName)
		if err != nil {
			fmt.Printf("CLI upgrade failed: %v\n", err)
			os.Exit(1)
		}
		displayLatest := strings.TrimPrefix(release.TagName, "v")
		fmt.Printf("✓ CLI upgraded to %s\n", displayLatest)

		// If server also needs upgrading, re-exec the new binary so it runs
		// with the updated code (the current process still has old code in memory).
		// We use the path returned by installUpdate() rather than os.Executable()
		// because on Linux /proc/self/exe becomes a dead symlink after the binary
		// is replaced, causing filepath.EvalSymlinks to fail.
		if serverInstalled && !upgradeFlags.cliOnly {
			fmt.Println()
			fmt.Println("Upgrading server with new CLI...")
			reexecArgs := []string{"upgrade", "server", "--dir", upgradeFlags.dir, "--force"}
			execCmd := exec.Command(newBinaryPath, reexecArgs...)
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr
			execCmd.Stdin = os.Stdin
			if err := execCmd.Run(); err != nil {
				fmt.Printf("Server upgrade failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println()
			fmt.Println("✓ Upgrade complete!")
			return
		}
	}

	// Upgrade server if installed and not --cli-only (CLI was already up to date)
	if serverInstalled && !upgradeFlags.cliOnly {
		fmt.Println()
		fmt.Println("Upgrading server...")
		cfg.ServerPort = inst.GetServerPort()
		if err := inst.Upgrade(release.TagName); err != nil {
			fmt.Printf("Server upgrade failed: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println()
	fmt.Println("✓ Upgrade complete!")
}

func getLatestRelease() (*Release, error) {
	resp, err := http.Get("https://api.github.com/repos/emergent-company/emergent/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	release.ImagesReady = checkImagesReady(release.Assets)

	return &release, nil
}

func checkImagesReady(assets []Asset) bool {
	for _, asset := range assets {
		if asset.Name == "images-ready.txt" {
			return true
		}
	}
	return false
}

func findAsset(assets []Asset) (string, string, error) {
	osName := runtime.GOOS
	archName := runtime.GOARCH

	target := fmt.Sprintf("emergent-cli-%s-%s", osName, archName)
	if osName == "windows" {
		target += ".zip"
	} else {
		target += ".tar.gz"
	}

	for _, asset := range assets {
		if asset.Name == target {
			return asset.BrowserDownloadURL, asset.Name, nil
		}
	}

	return "", "", fmt.Errorf("no asset found for %s/%s", osName, archName)
}

// installUpdate downloads and installs the new binary, returning the path
// of the installed binary. The caller can use this path to re-exec the new binary.
func installUpdate(url, filename string) (string, error) {
	tmpFile, err := os.CreateTemp("", "emergent-update-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return "", err
	}
	tmpFile.Close()

	tmpDir, err := os.MkdirTemp("", "emergent-extract-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	var binaryData []byte

	if strings.HasSuffix(filename, ".zip") {
		binaryData, err = extractZip(tmpFile.Name())
	} else {
		binaryData, err = extractTarGz(tmpFile.Name())
	}
	if err != nil {
		return "", fmt.Errorf("extraction failed: %w", err)
	}

	// Resolve the current executable path BEFORE replacing it.
	// On Linux, os.Executable() reads /proc/self/exe which becomes a dead
	// symlink after the binary is replaced, so we must capture it now.
	currentExec, err := os.Executable()
	if err != nil {
		return "", err
	}

	currentExec, err = filepath.EvalSymlinks(currentExec)
	if err != nil {
		return "", err
	}

	newExecPath := currentExec + ".new"
	if err := os.WriteFile(newExecPath, binaryData, 0755); err != nil {
		return "", err
	}

	oldExecPath := currentExec + ".old"
	os.Remove(oldExecPath)

	if err := os.Rename(currentExec, oldExecPath); err != nil {
		return "", fmt.Errorf("failed to move current binary: %w", err)
	}

	if err := os.Rename(newExecPath, currentExec); err != nil {
		_ = os.Rename(oldExecPath, currentExec)
		return "", fmt.Errorf("failed to replace binary: %w", err)
	}

	_ = os.Remove(oldExecPath)

	// Return the path where the new binary was installed
	return currentExec, nil
}

func extractTarGz(filepath string) ([]byte, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if header.Typeflag == tar.TypeReg {
			baseName := header.Name
			if strings.Contains(baseName, "/") {
				parts := strings.Split(baseName, "/")
				baseName = parts[len(parts)-1]
			}

			// Match various binary naming patterns
			if baseName == "emergent" || baseName == "emergent.exe" ||
				baseName == "emergent-cli" || baseName == "emergent-cli.exe" ||
				strings.HasPrefix(baseName, "emergent-cli-") {
				return io.ReadAll(tr)
			}
		}
	}
	return nil, fmt.Errorf("binary not found in archive")
}

func extractZip(filepath string) ([]byte, error) {
	r, err := zip.OpenReader(filepath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		baseName := f.Name
		if strings.Contains(baseName, "/") {
			parts := strings.Split(baseName, "/")
			baseName = parts[len(parts)-1]
		}

		if baseName == "emergent" || baseName == "emergent.exe" ||
			baseName == "emergent-cli" || baseName == "emergent-cli.exe" ||
			strings.HasPrefix(baseName, "emergent-cli-") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("binary not found in archive")
}
