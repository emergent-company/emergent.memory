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
	"path/filepath"
	"runtime"
	"strings"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/installer"
	"github.com/spf13/cobra"
)

var upgradeFlags struct {
	dir   string
	force bool
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the CLI to the latest version",
	Long: `Checks for the latest release on GitHub and upgrades the CLI binary if a newer version is available.

Use 'emergent upgrade server' to upgrade the standalone server installation.

Examples:
  emergent upgrade              # Upgrade CLI binary
  emergent upgrade server       # Upgrade standalone server (pull latest images)`,
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

	upgradeServerCmd.Flags().StringVar(&upgradeFlags.dir, "dir", defaultDir, "Installation directory")
	upgradeServerCmd.Flags().BoolVarP(&upgradeFlags.force, "force", "f", false, "Force upgrade without confirmation")

	upgradeCmd.AddCommand(upgradeServerCmd)
	rootCmd.AddCommand(upgradeCmd)
}

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
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

	return inst.Upgrade()
}

func runUpgrade(cmd *cobra.Command, args []string) {
	fmt.Println("Checking for updates...")

	release, err := getLatestRelease()
	if err != nil {
		fmt.Printf("Error checking for updates: %v\n", err)
		os.Exit(1)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "cli-")
	latestVersion = strings.TrimPrefix(latestVersion, "v")
	currentVersion := strings.TrimPrefix(Version, "cli-")
	currentVersion = strings.TrimPrefix(currentVersion, "v")

	if Version == "dev" && !upgradeFlags.force {
		fmt.Println("You are running a development version. Upgrade skipped.")
		fmt.Printf("Latest release: %s\n", release.TagName)
		fmt.Println("Use --force to upgrade anyway.")
		return
	}

	if latestVersion == currentVersion && !upgradeFlags.force {
		fmt.Printf("You are already using the latest version: %s\n", Version)
		return
	}

	if Version == "dev" {
		fmt.Printf("Forcing upgrade from dev version to %s\n", release.TagName)
	} else if latestVersion == currentVersion {
		fmt.Printf("Forcing reinstall of %s\n", release.TagName)
	} else {
		fmt.Printf("New version available: %s (Current: %s)\n", release.TagName, Version)
	}

	if !upgradeFlags.force {
		fmt.Print("Do you want to upgrade? [y/N]: ")

		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" {
			fmt.Println("Upgrade canceled.")
			return
		}
	}

	assetURL, assetName, err := findAsset(release.Assets)
	if err != nil {
		fmt.Printf("Error finding compatible asset: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Downloading %s...\n", assetName)
	if err := installUpdate(assetURL, assetName); err != nil {
		fmt.Printf("Upgrade failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully upgraded to %s\n", release.TagName)
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

	return &release, nil
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

func installUpdate(url, filename string) error {
	tmpFile, err := os.CreateTemp("", "emergent-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return err
	}
	tmpFile.Close()

	tmpDir, err := os.MkdirTemp("", "emergent-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	var binaryData []byte

	if strings.HasSuffix(filename, ".zip") {
		binaryData, err = extractZip(tmpFile.Name())
	} else {
		binaryData, err = extractTarGz(tmpFile.Name())
	}
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	currentExec, err := os.Executable()
	if err != nil {
		return err
	}

	currentExec, err = filepath.EvalSymlinks(currentExec)
	if err != nil {
		return err
	}

	newExecPath := currentExec + ".new"
	if err := os.WriteFile(newExecPath, binaryData, 0755); err != nil {
		return err
	}

	oldExecPath := currentExec + ".old"
	os.Remove(oldExecPath)

	if err := os.Rename(currentExec, oldExecPath); err != nil {
		return fmt.Errorf("failed to move current binary: %w", err)
	}

	if err := os.Rename(newExecPath, currentExec); err != nil {
		_ = os.Rename(oldExecPath, currentExec)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	_ = os.Remove(oldExecPath)

	return nil
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
