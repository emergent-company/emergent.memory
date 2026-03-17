package autoupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DownloadAndInstall finds the correct asset for the current OS/arch,
// downloads it, and replaces the running binary.
// Returns the path to the newly installed binary.
func DownloadAndInstall(release *Release) (string, error) {
	url, name, err := FindAsset(release.Assets)
	if err != nil {
		return "", err
	}
	return InstallUpdate(url, name)
}

// FindAsset selects the release asset that matches the current OS and
// architecture. Returns the download URL, asset name, and any error.
func FindAsset(assets []Asset) (string, string, error) {
	osName := runtime.GOOS
	archName := runtime.GOARCH

	target := fmt.Sprintf("memory-cli-%s-%s", osName, archName)
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

// InstallUpdate downloads the archive at url, extracts the binary, and
// atomically replaces the currently running executable.
// Returns the path of the installed binary.
func InstallUpdate(url, filename string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}

	tmpFile, err := os.CreateTemp("", "memory-update-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return "", err
	}
	tmpFile.Close()

	var binaryData []byte
	if strings.HasSuffix(filename, ".zip") {
		binaryData, err = extractZip(tmpFile.Name())
	} else {
		binaryData, err = extractTarGz(tmpFile.Name())
	}
	if err != nil {
		return "", fmt.Errorf("extraction failed: %w", err)
	}

	// Capture current executable path BEFORE replacing it.
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

	return currentExec, nil
}

func extractTarGz(path string) ([]byte, error) {
	f, err := os.Open(path)
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
		if header.Typeflag == tar.TypeReg && isBinaryEntry(header.Name) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary not found in archive")
}

func extractZip(path string) ([]byte, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		if isBinaryEntry(f.Name) {
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

// isBinaryEntry returns true when the archive entry name looks like the
// memory CLI binary.
func isBinaryEntry(name string) bool {
	base := filepath.Base(name)
	return base == "memory" || base == "memory.exe" ||
		base == "emergent" || base == "emergent.exe" ||
		strings.HasPrefix(base, "memory-cli-") ||
		strings.HasPrefix(base, "emergent-cli-")
}
