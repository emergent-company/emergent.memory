package main

// Import go-daisy's css package so `go mod vendor` includes its directory,
// which carries components/css/custom.css — the source file the gateway's
// Tailwind build @imports (via the stable vendor path) to get go-daisy's
// custom component CSS (sidebar/layout/alpine/view-transition rules). The
// package also publishes the per-package daisyUI registry for reference.
import _ "github.com/emergent-company/go-daisy/components/css"
