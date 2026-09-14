package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/emergent-company/memory.web-ui/connector/internal/upgrade"
)

// useUpgradeClient swaps the command's client factory for the test and restores
// it afterwards.
func useUpgradeClient(t *testing.T, c *upgrade.Client) {
	t.Helper()
	prev := newUpgradeClient
	newUpgradeClient = func() *upgrade.Client { return c }
	t.Cleanup(func() { newUpgradeClient = prev })
}

// setVersion overrides the reported CLI version for the duration of a test.
func setVersion(t *testing.T, v string) {
	t.Helper()
	prev := version
	version = v
	t.Cleanup(func() { version = prev })
}

// upgradeReleaseServer serves the releases API and counts archive downloads.
func upgradeReleaseServer(t *testing.T, tag string, tarball []byte, sha256 string) (*httptest.Server, *int32) {
	t.Helper()
	var downloads int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+upgrade.Repo+"/releases", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"tag_name":%q}]`, tag)
	})
	mux.HandleFunc("/releases/download/"+tag+"/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			atomic.AddInt32(&downloads, 1)
			_, _ = w.Write(tarball)
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = fmt.Fprintf(w, "%s\n", sha256)
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &downloads
}

func TestUpgradeAlreadyUpToDateSkipsDownload(t *testing.T) {
	srv, downloads := upgradeReleaseServer(t, "connector-v0.1.0", nil, "")
	useUpgradeClient(t, &upgrade.Client{
		HTTP:         srv.Client(),
		APIBase:      srv.URL,
		DownloadBase: srv.URL + "/releases/download",
	})
	setVersion(t, "0.1.0")

	out, errOut, err := executeRoot("upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v (stderr=%s)", err, errOut)
	}
	if !strings.Contains(out, "already up to date") {
		t.Errorf("stdout = %q, want already-up-to-date message", out)
	}
	if got := atomic.LoadInt32(downloads); got != 0 {
		t.Errorf("downloads = %d, want 0", got)
	}
}

func TestUpgradeCheckReportsWithoutDownloading(t *testing.T) {
	srv, downloads := upgradeReleaseServer(t, "connector-v0.2.0", nil, "")
	useUpgradeClient(t, &upgrade.Client{
		HTTP:         srv.Client(),
		APIBase:      srv.URL,
		DownloadBase: srv.URL + "/releases/download",
	})
	setVersion(t, "0.1.0")

	out, errOut, err := executeRoot("upgrade", "--check")
	if err != nil {
		t.Fatalf("upgrade --check: %v (stderr=%s)", err, errOut)
	}
	if !strings.Contains(out, "0.1.0") || !strings.Contains(out, "0.2.0") {
		t.Errorf("stdout = %q, want current and latest versions", out)
	}
	if !strings.Contains(out, "newer version is available") {
		t.Errorf("stdout = %q, want availability message", out)
	}
	if got := atomic.LoadInt32(downloads); got != 0 {
		t.Errorf("downloads = %d, want 0 for --check", got)
	}
}

func TestUpgradeDownloadsAndReplaces(t *testing.T) {
	content := []byte("new-installed-binary")
	tarball := makeTarGzForTest(t, upgrade.BinaryName, content)
	sha := sha256HexForTest(tarball)

	name, err := upgradeAssetNameForTest("0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	tag := "connector-v0.2.0"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+upgrade.Repo+"/releases", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"tag_name":%q}]`, tag)
	})
	mux.HandleFunc("/releases/download/"+tag+"/"+name, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tarball)
	})
	mux.HandleFunc("/releases/download/"+tag+"/"+name+".sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s\n", sha)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	target := filepath.Join(dir, upgrade.BinaryName)
	if err := os.WriteFile(target, []byte("old-installed-binary"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	prevExec := upgrade.Executable
	upgrade.Executable = func() (string, error) { return target, nil }
	t.Cleanup(func() { upgrade.Executable = prevExec })

	useUpgradeClient(t, &upgrade.Client{
		HTTP:         srv.Client(),
		APIBase:      srv.URL,
		DownloadBase: srv.URL + "/releases/download",
	})
	setVersion(t, "0.1.0")

	out, errOut, err := executeRoot("upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v (stderr=%s)", err, errOut)
	}
	if !strings.Contains(out, "0.1.0 -> 0.2.0") {
		t.Errorf("stdout = %q, want old -> new message", out)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("target = %q, want %q", got, content)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("replaced binary is not executable: mode %v", info.Mode().Perm())
	}
}

func TestUpgradeHelpListsFlags(t *testing.T) {
	out, _, err := executeRoot("upgrade", "--help")
	if err != nil {
		t.Fatalf("upgrade --help: %v", err)
	}
	for _, flagName := range []string{"--check", "--force", "--version"} {
		if !strings.Contains(out, flagName) {
			t.Errorf("upgrade --help missing flag %q:\n%s", flagName, out)
		}
	}
}

// makeTarGzForTest builds a gzip-compressed tar archive with one regular file.
func makeTarGzForTest(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func sha256HexForTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// upgradeAssetNameForTest mirrors the internal archive naming for the current
// test platform.
func upgradeAssetNameForTest(version string) (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64":
		return fmt.Sprintf("%s_%s_%s_%s.tar.gz", upgrade.BinaryName, version, runtime.GOOS, runtime.GOARCH), nil
	default:
		return "", fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}
