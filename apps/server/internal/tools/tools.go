//go:build tools

// Package tools pins build-time tools that this module needs but no committed
// source compiles. `go mod tidy` reads imports regardless of build
// constraints, so the blank import below is what keeps the requirement in
// go.mod; nothing here is ever built.
package tools

import (
	// swag generates the API spec: `task server:swagger`, apps/server/Makefile,
	// and both Dockerfiles resolve its version with
	// `go list -m github.com/swaggo/swag` and then run `swag init`.
	//
	// The generated docs/swagger package is the only committed-code-adjacent
	// importer of this module, and it is gitignored, so a tidy run on a clean
	// tree drops the requirement. deploy/self-hosted/Dockerfile.server runs
	// `go mod tidy -diff` before generating the docs, which is exactly that
	// clean tree — the shipped image build failed on it. Keep this import.
	_ "github.com/swaggo/swag"
)
