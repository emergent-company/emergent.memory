package main

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

// TestMgmtAPIAddrBindsLoopbackOnly asserts the management API listen address is
// loopback-only (127.0.0.1), never 0.0.0.0 or a hostname, for representative
// ports.
func TestMgmtAPIAddrBindsLoopbackOnly(t *testing.T) {
	for _, port := range []int{1, 8890, 65535} {
		addr := mgmtAPIAddr(port)
		if !strings.HasPrefix(addr, "127.0.0.1:") {
			t.Fatalf("mgmtAPIAddr(%d) = %q, want 127.0.0.1 prefix", port, addr)
		}
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("SplitHostPort(%q): %v", addr, err)
		}
		if host != "127.0.0.1" {
			t.Errorf("host = %q, want 127.0.0.1", host)
		}
		if portStr != strconv.Itoa(port) {
			t.Errorf("port = %q, want %d", portStr, port)
		}
	}
}
