package webui

import (
	"strings"
	"testing"
)

func TestVersionStableAndNonEmpty(t *testing.T) {
	a := version()
	if a == "" {
		t.Fatal("version() returned empty hash")
	}
	if b := version(); a != b {
		t.Fatalf("version() not stable: %q vs %q", a, b)
	}
}

func TestAssetPathCacheBusts(t *testing.T) {
	u := AssetPath("/js/chat.js")
	if !strings.HasPrefix(u, "/assets/js/chat.js?v=") {
		t.Fatalf("AssetPath = %q, want /assets/js/chat.js?v=<hash>", u)
	}
	if h := strings.TrimPrefix(u, "/assets/js/chat.js?v="); h != version() {
		t.Fatalf("AssetPath hash %q != version() %q", h, version())
	}
}
