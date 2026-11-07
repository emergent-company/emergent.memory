// Hand-written — not generated, and it must stay that way.
//
// It carries the go:generate directive for the component gallery's dependency
// graph and adoption findings, which are build output (gitignored; see
// gateway/.gitignore). The directive used to live inside
// devgallery_graph_gen.go and devgallery_adoption_gen.go themselves, which is a
// chicken-and-egg: `go generate ./...` reads directives from source files, so
// the only thing that said "regenerate me" was the files whose absence it was
// supposed to survive. A cold clone therefore had no way back to a compilable
// gateway except knowing the command by hand
// (`go run ./cmd/componentgraph`).
//
// Order matters: the generator walks the generated `*_templ.go` files for render
// call sites, so `templ generate` must run first. Both call sites honour that —
// the `generate` task in gateway/Taskfile.yml and the pair of build paths that
// replicate it (.github/workflows/web-ui.yml, apps/web-ui/Dockerfile).

//go:generate go run ./cmd/componentgraph

package main
