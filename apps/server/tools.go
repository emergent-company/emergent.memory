//go:build tools

// Package server is the Memory API server.
//
// This file pins the swagger generator's *library* as a direct dependency of
// the module, which is what both tidy gates require:
//
//   - the required `ci` job generates apps/server/docs/swagger/docs.go before it
//     runs scripts/preflight/go-mod-tidy.sh, and that generated file imports
//     github.com/swaggo/swag, so a module satisfying only the generated state
//     wants swag in the direct require block;
//   - a fresh checkout (the server image build runs `go mod tidy -diff` before
//     compiling) has no docs.go, so without an unconditional importer tidy would
//     move -- or drop -- the requirement.
//
// A //go:build tools file is part of the module graph for `go mod tidy` but is
// never compiled into the binary, so this satisfies both gates without the
// former `tool` directive (which records an indirect requirement and therefore
// fails the ci job). The pin is still read by the four places that shell out to
// `go list -m github.com/swaggo/swag`.
package server

import _ "github.com/swaggo/swag"
