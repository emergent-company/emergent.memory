package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestMain swaps the provider probe client for a plain one so existing
// provider tests can reach their httptest (loopback) servers. The SSRF guard
// itself is covered by constructing newSSRFSafeHTTPClient directly.
func TestMain(m *testing.M) {
	providerProbeHTTPClient = &http.Client{Timeout: 10 * time.Second}
	os.Exit(m.Run())
}

func TestIsDialablePublicIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", false},
		{"::1", false},
		{"10.0.0.1", false},
		{"172.16.5.4", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false},
		{"100.100.100.200", false},
		{"fc00::1", false},
		{"0.0.0.0", false},
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
	}
	for _, tc := range tests {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("bad test ip %q", tc.ip)
		}
		if got := isDialablePublicIP(ip); got != tc.want {
			t.Errorf("isDialablePublicIP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestSSRFSafeClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newSSRFSafeHTTPClient(2 * time.Second)
	resp, err := client.Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected loopback request to be blocked, got success")
	}
}
