package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestServerVersionsMatch pins #2004: `memory server doctor` compared the
// contents of ~/.memory/version byte-for-byte against the container image tag,
// which always carries a "v" prefix. A version file written by a CLI whose
// ldflags stripped the prefix ("0.87.4") therefore always warned about a
// mismatch. Both formats must resolve to the same release.
func TestServerVersionsMatch(t *testing.T) {
	tests := []struct {
		name      string
		installed string
		container string
		want      bool
	}{
		{"bare file vs prefixed tag", "0.87.4", "v0.87.4", true},
		{"both prefixed", "v0.87.4", "v0.87.4", true},
		{"both bare", "0.87.4", "0.87.4", true},
		{"whitespace and newline from the file", " 0.87.4\n", "v0.87.4", true},
		{"different releases", "0.87.3", "v0.87.4", false},
		{"missing installed version", "", "v0.87.4", false},
		{"missing container version", "0.87.4", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serverVersionsMatch(tt.installed, tt.container); got != tt.want {
				t.Errorf("serverVersionsMatch(%q, %q) = %v, want %v",
					tt.installed, tt.container, got, tt.want)
			}
		})
	}
}

// TestGetInstalledVersionFromFileNormalizes covers legacy files that still hold
// the "v" prefix.
func TestGetInstalledVersionFromFileNormalizes(t *testing.T) {
	for _, content := range []string{"0.87.4\n", "v0.87.4\n", "  v0.87.4  \n"} {
		installDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(installDir, "version"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if got := getInstalledVersionFromFile(installDir); got != "0.87.4" {
			t.Errorf("getInstalledVersionFromFile() with %q = %q, want %q", content, got, "0.87.4")
		}
	}
}
