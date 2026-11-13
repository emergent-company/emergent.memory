package main

// Import go-daisy's css package so `go mod vendor` includes its directory,
// which carries the three stylesheets the gateway's Tailwind build @imports
// from the stable vendor path (see webui/css/app.css, import order matters):
//
//   - tokens.css — the semantic text-tone ladder and `.eyebrow`
//   - custom.css — sidebar/layout/topbar/alpine/view-transition rules, and the
//     line ladder; it @applies tokens.css's tones, so it must come second
//   - theme-memory.css — the Memory brand theme (daisyUI `dark`, the Tailwind
//     radius alias, the `--color-muted-*` scale); imported last so its 50%
//     `--color-muted` beats tokens.css's own 60% step
//
// The package also publishes the per-package daisyUI registry for reference.
import _ "github.com/emergent-company/go-daisy/components/css"
