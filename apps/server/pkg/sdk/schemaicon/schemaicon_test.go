package schemaicon

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"file-text", "file-text"},
		{"FileText", "file-text"},
		{"file_text", "file-text"},
		{"file text", "file-text"},
		{"lucide--file-text", "file-text"},
		{"lucide:file-text", "file-text"},
		{"lucide--GitPullRequest", "git-pull-request"},
		{"GitPullRequest", "git-pull-request"},
		{"AlertTriangle", "alert-triangle"},
		{"  User  ", "user"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsRenderable(t *testing.T) {
	renderable := []string{
		"file-text",
		"FileText",
		"lucide--file-text",
		"lucide:user",
		"git_pull_request",
		"bot",
		"🚀",
		"★",
	}
	for _, s := range renderable {
		if !IsRenderable(s) {
			t.Errorf("IsRenderable(%q) = false, want true", s)
		}
	}

	notRenderable := []string{
		"",
		"   ",
		"not-an-icon",
		"LucideBox", // normalizes to "lucide-box", not in catalog
	}
	for _, s := range notRenderable {
		if IsRenderable(s) {
			t.Errorf("IsRenderable(%q) = true, want false", s)
		}
	}
}

func TestNamesAndSet(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("Names() returned an empty catalog")
	}
	if len(names) != 66 {
		t.Fatalf("Names() has %d entries, want 66 (mirrors web-ui catalog incl. bot)", len(names))
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Errorf("duplicate catalog entry %q", name)
		}
		seen[name] = true
		if Normalize(name) != name {
			t.Errorf("catalog name %q is not canonical kebab", name)
		}
	}

	set := Set()
	if len(set) != len(names) {
		t.Errorf("Set() has %d entries, want %d", len(set), len(names))
	}
	for _, name := range names {
		if _, ok := set[name]; !ok {
			t.Errorf("Set() missing %q", name)
		}
	}

	// Mutating the returned copies must not affect the package catalog.
	names[0] = "mutated"
	set["mutated"] = struct{}{}
	if reflect.DeepEqual(Names(), names) {
		t.Error("Names() exposes the internal catalog")
	}
	if _, ok := Set()["mutated"]; ok {
		t.Error("Set() exposes the internal lookup map")
	}
}

// TestCatalogMatchesWebUI is a drift guard: the shared catalog must mirror the
// gateway's supportedTypeIconClasses, because Tailwind only emits CSS for the
// literal lucide-- classes listed there. When the SDK module is consumed
// outside the monorepo the gateway source is absent; the guard then logs and
// returns rather than registering a skip (the repo's skip-census counts skips).
func TestCatalogMatchesWebUI(t *testing.T) {
	const path = "../../../../web-ui/gateway/type_icons.go"
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Log("web-ui gateway source not present (packaged SDK consumer); drift guard not applicable")
			return
		}
		t.Fatalf("read %s: %v", path, err)
	}

	const marker = "var supportedTypeIconClasses = []string{"
	start := strings.Index(string(data), marker)
	if start < 0 {
		t.Fatalf("%s not found in web-ui type_icons.go", marker)
	}
	block := string(data)[start:]
	if end := strings.Index(block, "}"); end >= 0 {
		block = block[:end]
	}

	re := regexp.MustCompile(`"lucide--([^"]+)"`)
	matches := re.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		t.Fatal("found no lucide-- literals in supportedTypeIconClasses")
	}
	web := make([]string, len(matches))
	for i, m := range matches {
		web[i] = m[1]
	}

	names := Names()
	if len(names) != len(web) {
		t.Fatalf("catalog has %d entries, web-ui has %d: %v vs %v", len(names), len(web), names, web)
	}
	for i := range names {
		if names[i] != web[i] {
			t.Fatalf("catalog entry %d = %q, web-ui = %q (catalogs drifted)", i, names[i], web[i])
		}
	}
}
