// The brand tokens are a Go package so a Go consumer (today the CLI's lipgloss
// styling, tomorrow anything else) can import resolved values instead of
// hard-coding hex. design-system/build.py generates brand.go from the token file; this
// module exists so the generated file is actually compiled by `go vet` and by
// the brand preflight rather than sitting in a directory no module owns.
module github.com/emergent-company/emergent.memory/design-system

go 1.25.0
