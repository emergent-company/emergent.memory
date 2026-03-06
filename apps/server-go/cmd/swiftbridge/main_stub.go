//go:build !cgo

// main_stub.go provides a main() stub when CGO is disabled (e.g., for
// running unit tests on Linux CI without a C compiler).
// The real main() is in bridge.go and is only active when building the
// c-archive with CGO_ENABLED=1.
package main

func main() {}
