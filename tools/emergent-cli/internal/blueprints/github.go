package blueprints

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// FetchGitHubRepo downloads a GitHub repository archive and extracts it to a
// temporary directory, returning the path of that directory and a cleanup
// function the caller should defer.
//
// rawURL must be of the form:
//
//	https://github.com/<org>/<repo>[#<ref>]
//
// If no fragment (#ref) is provided, "HEAD" is used.
// token is optional; when non-empty it is sent as "Authorization: token <tok>".
func FetchGitHubRepo(rawURL, token string) (localDir string, cleanup func(), err error) {
	org, repo, ref, parseErr := parseGitHubURL(rawURL)
	if parseErr != nil {
		return "", func() {}, parseErr
	}

	archiveURL := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", org, repo, ref)

	req, err := http.NewRequest(http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", func() {}, fmt.Errorf("create request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", func() {}, fmt.Errorf("download archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if token == "" {
			return "", func() {}, fmt.Errorf("repository not found or requires authentication — set MEMORY_GITHUB_TOKEN or pass --token")
		}
		return "", func() {}, fmt.Errorf("repository not found: %s", rawURL)
	}
	if resp.StatusCode != http.StatusOK {
		return "", func() {}, fmt.Errorf("unexpected HTTP %d from codeload.github.com", resp.StatusCode)
	}

	// Extract to a temp directory.
	tmpDir, err := os.MkdirTemp("", "memory-blueprints-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir: %w", err)
	}

	cleanupFn := func() { os.RemoveAll(tmpDir) }

	if err := extractTarGz(resp.Body, tmpDir); err != nil {
		cleanupFn()
		return "", func() {}, fmt.Errorf("extract archive: %w", err)
	}

	// The archive contains a single top-level directory named <repo>-<ref>/.
	// Strip it so callers find packs/ and agents/ directly at tmpDir.
	topLevel, err := findTopLevelDir(tmpDir)
	if err != nil {
		cleanupFn()
		return "", func() {}, fmt.Errorf("locate archive root: %w", err)
	}

	return topLevel, cleanupFn, nil
}

// IsGitHubURL returns true when src looks like a GitHub repository URL.
func IsGitHubURL(src string) bool {
	return strings.HasPrefix(src, "https://github.com/")
}

// ──────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────

// parseGitHubURL extracts org, repo, and ref from a GitHub URL.
// Fragment (#ref) is used as the ref; defaults to "HEAD".
func parseGitHubURL(rawURL string) (org, repo, ref string, err error) {
	// Strip scheme
	s := strings.TrimPrefix(rawURL, "https://github.com/")

	// Separate fragment
	if idx := strings.Index(s, "#"); idx != -1 {
		ref = s[idx+1:]
		s = s[:idx]
	}
	if ref == "" {
		ref = "HEAD"
	}

	// Remove trailing .git if present
	s = strings.TrimSuffix(s, ".git")

	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("invalid GitHub URL %q: expected https://github.com/<org>/<repo>[#ref]", rawURL)
	}

	return parts[0], parts[1], ref, nil
}

// extractTarGz reads a gzipped tar stream from r and writes its contents
// under destDir, stripping path traversal attempts.
func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		// Guard against path traversal
		target := filepath.Join(destDir, filepath.Clean("/"+hdr.Name))
		if !strings.HasPrefix(target, destDir+string(os.PathSeparator)) && target != destDir {
			continue // skip suspicious paths
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent: %w", err)
			}
			f, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec
				f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			f.Close()
		}
	}

	return nil
}

// findTopLevelDir looks for a single subdirectory inside dir and returns its
// path.  If there is exactly one subdirectory it is returned; otherwise dir
// itself is returned (archive may not have a top-level wrapper).
func findTopLevelDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(dir, e.Name()))
		}
	}

	if len(dirs) == 1 {
		return dirs[0], nil
	}
	return dir, nil
}
