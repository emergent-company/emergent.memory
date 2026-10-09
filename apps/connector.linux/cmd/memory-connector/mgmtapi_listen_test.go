package main

import (
	"net"
	"testing"
	"time"
)

// TestListenManagementAPIBindsFreePort asserts the helper returns a usable
// loopback listener when the port is available.
func TestListenManagementAPIBindsFreePort(t *testing.T) {
	ln, err := listenManagementAPI("127.0.0.1:0", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("listenManagementAPI on a free port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	if got := ln.Addr().(*net.TCPAddr).IP.String(); got != "127.0.0.1" {
		t.Errorf("bound host = %q, want 127.0.0.1", got)
	}
}

// TestListenManagementAPIRetriesTransientBind asserts a port released shortly
// after the first failed attempt is still bound (the restart case: a
// predecessor holds the port for a moment).
func TestListenManagementAPIRetriesTransientBind(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold port: %v", err)
	}
	addr := held.Addr().String()
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = held.Close()
	}()

	ln, err := listenManagementAPI(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("expected retry to succeed once the port was released: %v", err)
	}
	_ = ln.Close()
}

// TestListenManagementAPIFailsAfterWindow asserts a port held for the whole
// window yields an error (so the caller can fail fast) and that the helper
// waits for the window rather than returning immediately.
func TestListenManagementAPIFailsAfterWindow(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold port: %v", err)
	}
	defer func() { _ = held.Close() }()

	const window = 150 * time.Millisecond
	start := time.Now()
	ln, err := listenManagementAPI(held.Addr().String(), window)
	elapsed := time.Since(start)
	if err == nil {
		_ = ln.Close()
		t.Fatal("expected an error for a permanently held port")
	}
	if elapsed < window {
		t.Fatalf("returned after %s, want at least the %s retry window", elapsed, window)
	}
}
