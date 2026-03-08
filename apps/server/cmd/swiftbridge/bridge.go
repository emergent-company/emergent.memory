// bridge.go — CGO export layer.
//
// This file contains ONLY the //export functions and CGO preamble.
// All business logic lives in bridge_internal.go (CGO-free) so that
// unit tests can run on any platform without a C compiler.
//
// Build this package as a C static archive:
//
//	CGO_ENABLED=1 go build -buildmode=c-archive -o libEmergentGoCore.a ./cmd/swiftbridge
//
// Go will also write the corresponding C header (libEmergentGoCore.h).
package main

// #include <stdlib.h>
// #include <stdint.h>
//
// // Callback type for async operations.
// // operationID: identifies the in-flight request.
// // jsonResult:  standard {"result":...} or {"error":"..."} envelope.
// // ctx:         opaque Swift pointer returned unmodified.
// typedef void (*ResultCallback)(uint64_t operationID, const char* jsonResult, void* ctx);
//
// // Callback type for log messages forwarded from the Go runtime.
// // level: 0=debug 1=info 2=warn 3=error
// typedef void (*LogCallback)(int32_t level, const char* message);
//
// // C shims — Go cannot call a C function pointer variable directly, so we
// // forward via a static C function.
// static void invokeResultCallback(ResultCallback cb, uint64_t opID,
//                                  const char* json, void* ctx) {
//   if (cb) cb(opID, json, ctx);
// }
// static void invokeLogCallback(LogCallback cb, int32_t level, const char* msg) {
//   if (cb) cb(level, msg);
// }
import "C"

import (
	"context"
	"fmt"
	"unsafe"
)

// cLogCB is the C-level log callback registered by Swift.
// It is set via RegisterLogCallback and called through bridgeLog.
var cLogCB C.LogCallback

func init() {
	// Wire the CGO log callback into the platform-agnostic bridgeLog function.
	logCallbackMu.Lock()
	logCallbackFn = func(level int, msg string) {
		cb := cLogCB // read inside closure (safe: set before any goroutine calls)
		if cb == nil {
			return
		}
		cMsg := C.CString(msg)
		C.invokeLogCallback(cb, C.int32_t(level), cMsg)
		C.free(unsafe.Pointer(cMsg))
	}
	logCallbackMu.Unlock()
}

// ---------------------------------------------------------------------------
// Memory management
// ---------------------------------------------------------------------------

// FreeString releases a C string that was heap-allocated by the Go bridge.
// Swift MUST call this for every *C.char returned by any bridge function.
//
//export FreeString
func FreeString(s *C.char) {
	C.free(unsafe.Pointer(s))
}

// FreeClient releases all Go-side state associated with the given handle.
//
//export FreeClient
func FreeClient(handle C.uint32_t) {
	goFreeClient(uint32(handle))
}

// ---------------------------------------------------------------------------
// Client lifecycle
// ---------------------------------------------------------------------------

// CreateClient initialises an Emergent SDK client from a JSON configuration
// string and returns a numeric handle to the live client state.
//
// Input JSON:
//
//	{"server_url":"…","auth_mode":"apikey","api_key":"…","org_id":"…","project_id":"…"}
//
// On success: {"result":{"handle":<uint32>}}
// On failure: {"error":"<message>"}
//
// The caller MUST call FreeClient(handle) when done, and FreeString on the
// returned *C.char.
//
//export CreateClient
func CreateClient(configJSON *C.char) *C.char {
	result := goCreateClient(C.GoString(configJSON))
	return C.CString(result)
}

// ---------------------------------------------------------------------------
// Synchronous POC endpoint
// ---------------------------------------------------------------------------

// Ping echoes the provided message, validating the CGO boundary and JSON
// serialisation before the full async API is used.
//
// Input JSON:  {"message":"hello"}
// Output JSON: {"result":{"echo":"hello","timestamp":"<RFC3339>"}}
//
//export Ping
func Ping(handle C.uint32_t, requestJSON *C.char) *C.char {
	result := goPing(uint32(handle), C.GoString(requestJSON))
	return C.CString(result)
}

// ---------------------------------------------------------------------------
// Log callback
// ---------------------------------------------------------------------------

// RegisterLogCallback registers a C function pointer that the bridge will
// invoke for every internal log message.
//
//export RegisterLogCallback
func RegisterLogCallback(cb C.LogCallback) {
	logCallbackMu.Lock()
	cLogCB = cb
	logCallbackMu.Unlock()
}

// ---------------------------------------------------------------------------
// Task cancellation
// ---------------------------------------------------------------------------

// CancelOperation signals cancellation for the given async operation ID.
// If the operation has already completed this is a no-op.
//
//export CancelOperation
func CancelOperation(operationID C.uint64_t) {
	goCancel(uint64(operationID))
}

// ---------------------------------------------------------------------------
// Async API
// ---------------------------------------------------------------------------

// HealthCheck performs an async health check against the Emergent server.
//
// Returns the operation ID. The callback receives:
//
//	(operationID, jsonResult, ctx)
//
// Memory contract: the jsonResult pointer passed to the callback is
// Go-allocated. Ownership transfers to the callback caller — Swift MUST
// call FreeString(jsonResult) to release it. Go does NOT free it after
// the callback returns.
//
//export HealthCheck
func HealthCheck(handle C.uint32_t, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goHealthCheck(uint32(handle), func(id uint64, result string) {
		cResult := C.CString(result)
		// Ownership transfers to Swift — do NOT C.free here.
		// Swift will call FreeString(jsonResult) inside the callback.
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// Search performs an async semantic search.
//
// Input JSON: {"query":"…","limit":10}
//
// See HealthCheck for the callback memory contract.
//
//export Search
func Search(handle C.uint32_t, requestJSON *C.char, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goSearch(uint32(handle), C.GoString(requestJSON), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// Chat sends a message to the AI chat endpoint and returns the full response.
//
// Input JSON: {"message":"…","conversation_id":"…"}
//
// See HealthCheck for the callback memory contract.
//
//export Chat
func Chat(handle C.uint32_t, requestJSON *C.char, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goChat(uint32(handle), C.GoString(requestJSON), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// ListDocuments retrieves a paginated list of documents.
//
// Input JSON: {"limit":20,"cursor":"…"}
//
// See HealthCheck for the callback memory contract.
//
//export ListDocuments
func ListDocuments(handle C.uint32_t, requestJSON *C.char, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goListDocuments(uint32(handle), C.GoString(requestJSON), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// SetContext updates the default org/project context for all subsequent calls
// made through this client handle. This is a synchronous convenience wrapper.
//
// Input JSON: {"org_id":"…","project_id":"…"}
//
//export SetContext
func SetContext(handle C.uint32_t, requestJSON *C.char) *C.char {
	result := goSetContext(uint32(handle), C.GoString(requestJSON))
	return C.CString(result)
}

// GetProjects returns the list of projects accessible by the authenticated user.
//
// Input JSON: {"limit":50,"include_stats":false}
//
// See HealthCheck for the callback memory contract.
//
//export GetProjects
func GetProjects(handle C.uint32_t, requestJSON *C.char, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goGetProjects(uint32(handle), C.GoString(requestJSON), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// SearchObjects performs a hybrid (lexical + vector) search over graph objects
// in the project bound to this client handle.
//
// Input JSON: {"query":"…","limit":10,"lexical_weight":0.5,"vector_weight":0.5}
//
// See HealthCheck for the callback memory contract.
//
//export SearchObjects
func SearchObjects(handle C.uint32_t, requestJSON *C.char, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goSearchObjects(uint32(handle), C.GoString(requestJSON), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// GetProjectStats returns a single project with aggregate statistics.
//
// Input JSON: {"project_id":"…"}
//
// See HealthCheck for the callback memory contract.
//
//export GetProjectStats
func GetProjectStats(handle C.uint32_t, requestJSON *C.char, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goGetProjectStats(uint32(handle), C.GoString(requestJSON), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// GetAccountStats returns aggregate task counts across all projects for the
// authenticated user.
//
// See HealthCheck for the callback memory contract.
//
//export GetAccountStats
func GetAccountStats(handle C.uint32_t, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goGetAccountStats(uint32(handle), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// GetWorkers returns active and queued tasks for a project (the "workers" view).
//
// Input JSON: {"project_id":"…","limit":50}
//
// See HealthCheck for the callback memory contract.
//
//export GetWorkers
func GetWorkers(handle C.uint32_t, requestJSON *C.char, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goGetWorkers(uint32(handle), C.GoString(requestJSON), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// GetUserProfile returns the profile of the currently authenticated user.
//
// See HealthCheck for the callback memory contract.
//
//export GetUserProfile
func GetUserProfile(handle C.uint32_t, cb C.ResultCallback, ctx unsafe.Pointer) C.uint64_t {
	opID := goGetUserProfile(uint32(handle), func(id uint64, result string) {
		cResult := C.CString(result)
		C.invokeResultCallback(cb, C.uint64_t(id), cResult, ctx)
	})
	return C.uint64_t(opID)
}

// main is required for c-archive packages but is never called.
func main() {}

// Ensure context and fmt are imported (used for WithCancel/Sprintf in bridge_internal.go).
var _ = context.Background
var _ = fmt.Sprintf
