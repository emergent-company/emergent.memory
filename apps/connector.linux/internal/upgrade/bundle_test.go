package upgrade

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsInsideAppBundle(t *testing.T) {
	if IsInsideAppBundle("/usr/local/bin/memory-connector") {
		t.Error("/usr/local/bin/memory-connector reported inside a bundle")
	}

	for _, p := range []string{
		"/Applications/Foo.app/Contents/Resources/memory-connector",
		"/Applications/Foo.app/Contents/MacOS/memory-connector",
		"/tmp/My App.app/Contents/MacOS/memory-connector",
	} {
		if !IsInsideAppBundle(p) {
			t.Errorf("%q not detected inside a bundle", p)
		}
	}

	// A Contents/MacOS ancestor without a .app component is still a bundle.
	if !IsInsideAppBundle("/Volumes/X/Contents/MacOS/memory-connector") {
		t.Error("Contents/MacOS ancestor not detected")
	}
}

func TestIsInsideAppBundleResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	bundleExe := filepath.Join(dir, "Foo.app", "Contents", "Resources", "memory-connector")
	if err := os.MkdirAll(filepath.Dir(bundleExe), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(bundleExe, []byte("x"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(bundleExe, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if !IsInsideAppBundle(link) {
		t.Error("symlink into bundle not detected after resolution")
	}
}
