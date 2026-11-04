package upgrade

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"connector-v0.1.0", "v0.1.0", 0},
		{"0.2.0", "0.1.9", 1},
		{"0.1.0", "0.2.0", -1},
		{"0.10.0", "0.9.0", 1}, // numeric, not lexicographic
		{"0.9.0", "0.10.0", -1},
		{"1.0.0", "0.99.99", 1},
		{"0.1", "0.1.0", 0},
		{"0.1.0-rc.1", "0.1.0", -1}, // prerelease is older
		{"0.1.0", "0.1.0-rc.1", 1},
		{"0.1.0-rc.1", "0.1.0-rc.2", -1},
		{"dev", "0.1.0", -1}, // unknown current is always older
		{"0.1.0", "dev", 1},  // unknown candidate never wins
		{"", "0.1.0", -1},
		{"dev", "", 0}, // both unparseable
	}
	for _, tc := range cases {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"connector-v0.2.0": "0.2.0",
		"v0.2.0":           "0.2.0",
		"0.2.0":            "0.2.0",
		"  v1.2.3 ":        "1.2.3",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssetNamePlatforms(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "memory-connector_0.2.0_linux_amd64.tar.gz"},
		{"linux", "arm64", "memory-connector_0.2.0_linux_arm64.tar.gz"},
		{"darwin", "amd64", "memory-connector_0.2.0_darwin_amd64.tar.gz"},
		{"darwin", "arm64", "memory-connector_0.2.0_darwin_arm64.tar.gz"},
	}
	for _, tc := range cases {
		got, err := assetName("0.2.0", tc.goos, tc.goarch)
		if err != nil {
			t.Errorf("assetName(%s/%s): %v", tc.goos, tc.goarch, err)
		}
		if got != tc.want {
			t.Errorf("assetName(%s/%s) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
	if _, err := assetName("0.2.0", "windows", "amd64"); err == nil {
		t.Error("assetName(windows/amd64) = nil error, want unsupported-platform error")
	}
}
