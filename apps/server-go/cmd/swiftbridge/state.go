// state.go holds all mutable global state for the bridge in a CGO-free file.
// Placing state here means bridge_test.go and bridge_internal.go can be
// compiled and tested on any platform (Linux CI) without a C compiler.
package main

import (
	"sync"
	"sync/atomic"

	sdk "github.com/emergent-company/emergent.memory/apps/server-go/pkg/sdk"
)

// ---------------------------------------------------------------------------
// Client registry
// ---------------------------------------------------------------------------

var (
	clientsMu sync.RWMutex
	clients   = make(map[uint32]*sdk.Client)
	nextID    uint32 = 1
)

// ---------------------------------------------------------------------------
// Cancellation registry
// ---------------------------------------------------------------------------

var (
	cancelsMu sync.Mutex
	cancels   = make(map[uint64]func())
	nextOpID  uint64 = 1
)

// registerCancel stores a cancel function and returns an operation ID.
func registerCancel(cancel func()) uint64 {
	opID := atomic.AddUint64(&nextOpID, 1) - 1
	cancelsMu.Lock()
	cancels[opID] = cancel
	cancelsMu.Unlock()
	return opID
}

// deregisterCancel removes and returns the cancel func (if any).
func deregisterCancel(opID uint64) func() {
	cancelsMu.Lock()
	cancel, ok := cancels[opID]
	if ok {
		delete(cancels, opID)
	}
	cancelsMu.Unlock()
	if ok {
		return cancel
	}
	return nil
}

// ---------------------------------------------------------------------------
// Log callback (stored as an opaque interface to avoid CGO dependency here)
// ---------------------------------------------------------------------------

var (
	logCallbackMu sync.RWMutex
	// logCallbackFn is set by the CGO layer (bridge.go) and called via
	// bridgeLog. Typed as a plain Go func to avoid a CGO import in this file.
	logCallbackFn func(level int, msg string)
)

// bridgeLog routes a log message through the registered log handler (if any).
// level: 0=debug, 1=info, 2=warn, 3=error
func bridgeLog(level int, msg string) {
	logCallbackMu.RLock()
	fn := logCallbackFn
	logCallbackMu.RUnlock()
	if fn != nil {
		fn(level, msg)
	}
}

// ---------------------------------------------------------------------------
// JSON envelope
// ---------------------------------------------------------------------------

// envelope is the standard JSON wrapper returned across the CGO boundary.
type envelope struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}
