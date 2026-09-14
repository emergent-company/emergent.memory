package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeTarGz builds a gzip-compressed tar archive containing one regular file.
func makeTarGz(t *testing.T, name string, content []byte) []byte {
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

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestLatestPicksHighestConnectorRelease(t *testing.T) {
	name, err := assetName("0.10.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, _ *http.Request) {
		// Deliberately out of order, with a non-connector tag and a newer
		// non-connector release that must be ignored.
		_, _ = fmt.Fprintf(w, `[
			{"tag_name":"connector-v0.2.0","assets":[]},
			{"tag_name":"v9.9.9","assets":[]},
			{"tag_name":"connector-v0.10.0","assets":[{"name":%q,"browser_download_url":%q}]},
			{"tag_name":"connector-v0.9.0","assets":[]}
		]`, name, "")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// The asset's browser_download_url is filled in once the server URL is known.
	listURL := srv.URL + "/releases/download/connector-v0.10.0/" + name
	svc := &Client{HTTP: srv.Client(), APIBase: srv.URL, DownloadBase: srv.URL + "/releases/download"}
	rel, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Version != "0.10.0" {
		t.Errorf("Version = %q, want 0.10.0", rel.Version)
	}
	if rel.Tag != "connector-v0.10.0" {
		t.Errorf("Tag = %q, want connector-v0.10.0", rel.Tag)
	}
	if rel.AssetName != name {
		t.Errorf("AssetName = %q, want %q", rel.AssetName, name)
	}
	// Asset URL is constructed from DownloadBase because the JSON asset URL
	// was empty; this also exercises the constructed-URL fallback.
	if rel.AssetURL != listURL {
		t.Errorf("AssetURL = %q, want %q", rel.AssetURL, listURL)
	}
}

func TestLatestUsesAPIProvidedAssetURL(t *testing.T) {
	name, err := assetName("0.3.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"tag_name":"connector-v0.3.0","assets":[{"name":%q,"browser_download_url":%q}]}]`,
			name, srv.URL+"/cdn/"+name)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	svc := &Client{HTTP: srv.Client(), APIBase: srv.URL, DownloadBase: srv.URL + "/releases/download"}
	rel, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if want := srv.URL + "/cdn/" + name; rel.AssetURL != want {
		t.Errorf("AssetURL = %q, want %q", rel.AssetURL, want)
	}
}

func TestLatestNoConnectorRelease(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc := &Client{HTTP: srv.Client(), APIBase: srv.URL}
	if _, err := svc.Latest(context.Background()); err == nil {
		t.Fatal("Latest = nil error, want no-release error")
	}
}

func TestFetchBinaryVerifiesChecksum(t *testing.T) {
	content := []byte("binary-payload")
	tarball := makeTarGz(t, BinaryName, content)

	mux := http.NewServeMux()
	mux.HandleFunc("/asset.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tarball)
	})
	mux.HandleFunc("/asset.tar.gz.sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  asset.tar.gz\n", sha256Hex(tarball))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc := &Client{HTTP: srv.Client()}
	rel := &Release{Version: "0.1.0", AssetName: "asset.tar.gz", AssetURL: srv.URL + "/asset.tar.gz"}
	got, err := svc.FetchBinary(context.Background(), rel)
	if err != nil {
		t.Fatalf("FetchBinary: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("binary = %q, want %q", got, content)
	}
}

func TestFetchBinaryChecksumMismatch(t *testing.T) {
	tarball := makeTarGz(t, BinaryName, []byte("payload"))

	mux := http.NewServeMux()
	mux.HandleFunc("/asset.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tarball)
	})
	mux.HandleFunc("/asset.tar.gz.sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  asset.tar.gz\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc := &Client{HTTP: srv.Client()}
	rel := &Release{Version: "0.1.0", AssetName: "asset.tar.gz", AssetURL: srv.URL + "/asset.tar.gz"}
	_, err := svc.FetchBinary(context.Background(), rel)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
}

func TestFetchBinaryMissingChecksumIsSkipped(t *testing.T) {
	content := []byte("payload")
	tarball := makeTarGz(t, BinaryName, content)

	// No .sha256 handler: the checksum request 404s and verification is skipped.
	mux := http.NewServeMux()
	mux.HandleFunc("/asset.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tarball)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc := &Client{HTTP: srv.Client()}
	rel := &Release{Version: "0.1.0", AssetName: "asset.tar.gz", AssetURL: srv.URL + "/asset.tar.gz"}
	got, err := svc.FetchBinary(context.Background(), rel)
	if err != nil {
		t.Fatalf("FetchBinary: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("binary = %q, want %q", got, content)
	}
}

func TestReleaseForBuildsPinnedURL(t *testing.T) {
	name, err := assetName("0.4.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}
	svc := &Client{DownloadBase: "https://mirror.example/releases/download"}
	rel, err := svc.ReleaseFor("v0.4.0")
	if err != nil {
		t.Fatalf("ReleaseFor: %v", err)
	}
	if rel.Version != "0.4.0" {
		t.Errorf("Version = %q, want 0.4.0", rel.Version)
	}
	wantURL := "https://mirror.example/releases/download/connector-v0.4.0/" + name
	if rel.AssetURL != wantURL {
		t.Errorf("AssetURL = %q, want %q", rel.AssetURL, wantURL)
	}
	if _, err := svc.ReleaseFor("not-a-version"); err == nil {
		t.Error("ReleaseFor(not-a-version) = nil error, want invalid-version error")
	}
}

func TestReplaceOverInstalledBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, BinaryName)
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	newContent := []byte("new-binary")
	if err := Replace(target, newContent); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(got, newContent) {
		t.Errorf("target = %q, want %q", got, newContent)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestInstallReplacesExecutable(t *testing.T) {
	content := []byte("installed-binary")
	tarball := makeTarGz(t, BinaryName, content)

	mux := http.NewServeMux()
	mux.HandleFunc("/asset.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tarball)
	})
	mux.HandleFunc("/asset.tar.gz.sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s\n", sha256Hex(tarball))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, BinaryName)
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	prev := Executable
	Executable = func() (string, error) { return target, nil }
	defer func() { Executable = prev }()

	svc := &Client{HTTP: srv.Client()}
	rel := &Release{Version: "0.9.0", AssetName: "asset.tar.gz", AssetURL: srv.URL + "/asset.tar.gz"}
	replaced, err := svc.Install(context.Background(), rel)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if replaced != target {
		t.Errorf("Install returned %q, want %q", replaced, target)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("target = %q, want %q", got, content)
	}
	// No temp files left behind in the target directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("stray temp file left behind: %s", e.Name())
		}
	}
}
