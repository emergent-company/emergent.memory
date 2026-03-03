## Context

The `emergent-company/emergent` monorepo contains a fully-implemented Go SDK (`apps/server-go/pkg/sdk/`) at v0.8.0 with 30 service clients covering the complete Emergent API surface. The companion Mac app (`emergent-company/emergent.memory.mac`) contains a Swift API layer (`Emergent/Core/EmergentAPIClient.swift`, `Emergent/Core/Models.swift`) that represents the current public-facing Swift integration point, while a formal `EmergentSwiftSDK/` package is planned but unimplemented pending upstream CGO work.

Neither SDK has public-facing documentation beyond the Go SDK README and CHANGELOG. There is no GitHub Pages site, no documentation tooling, and no LLM-consumable reference. The existing `docs/` tree is large (~35k files) internal content not wired to any site generator. `docs/public/` exists as a small three-file stub with an `index.json` catalogue — useful context but not connected to any output pipeline.

The goal is to introduce a MkDocs Material documentation site at `docs/site/`, deployed automatically to GitHub Pages, covering both SDKs fully, plus `docs/llms*.md` files for LLM consumption.

## Goals / Non-Goals

**Goals:**
- Full per-package Go SDK reference (30 clients + auth/errors/graphutil/testutil)
- Six narrative guides for the Go SDK (quickstart, auth, multi-tenancy, error handling, streaming/SSE, graph ID model)
- Swift Core layer reference (15 `EmergentAPIClient` methods + 16 public model types + errors)
- MkDocs Material site under `docs/site/` with `mkdocs.yml` at repo root
- GitHub Actions workflow auto-deploying to `gh-pages` on `main` push; build-only check on PRs
- Three LLM reference files (`docs/llms.md`, `docs/llms-go-sdk.md`, `docs/llms-swift-sdk.md`) in plain markdown
- `docs/requirements.txt` declaring Python/MkDocs dependencies
- Update `apps/server-go/pkg/sdk/README.md` to link to the published site

**Non-Goals:**
- Auto-generating docs from godoc/pkgsite (manual authored markdown only — avoids toolchain complexity)
- Improving inline godoc comments in Go source (stretch task, tracked separately)
- Hosting the formal `EmergentSwiftSDK/` Swift Package (not implemented; documented as roadmap)
- Including internal `docs/` content in the public site
- Versioned docs (single `latest` version for now)
- Migrating `docs/public/` content into the MkDocs site (left as-is; separate concern)
- Dark mode toggle, analytics, or custom domain setup

## Decisions

### Decision 1: `docs/site/` as MkDocs `docs_dir`, not the repo root `docs/`

**Choice:** Set `docs_dir: docs/site` in `mkdocs.yml`, keeping MkDocs source files separate from the existing large internal `docs/` tree.

**Rationale:** The existing `docs/` directory contains ~35,000 internal files (architecture, bugs, deployment, etc.) that must not be published publicly. Pointing MkDocs at `docs/site/` is the cleanest separation without restructuring anything. Alternative considered: a top-level `site-docs/` directory — rejected because `docs/site/` is more discoverable and fits the existing `docs/` grouping. Alternative considered: `docs_dir: docs/public` (reuse the existing stub) — rejected because `docs/public/` has an incompatible `index.json`-catalogue structure and different purpose.

### Decision 2: MkDocs Material over Docusaurus or pkgsite

**Choice:** MkDocs Material (`pip install mkdocs-material`).

**Rationale:** MkDocs Material is the dominant choice for Go/SDK documentation sites, requires no Node.js or React build pipeline, produces fast static HTML, has excellent search out of the box, and `mkdocs gh-deploy` is a single command. Docusaurus is better suited for versioned docs with React components — overkill here. `pkgsite` only serves godoc-style content; it cannot host narrative guides or Swift docs in the same site.

### Decision 3: Authored markdown, not generated from godoc

**Choice:** All reference pages are hand-authored markdown files.

**Rationale:** Godoc comment coverage is currently sparse across the 30 service clients. Auto-generation via `pkgsite` or `gomarkdoc` would produce skeleton pages with missing content. Authored markdown allows control over examples, callouts, cross-links, and the dual-ID model explanation. The trade-off is maintenance burden when the SDK evolves — mitigated by the `CHANGELOG.md` update convention already in place.

**Alternative considered:** `gomarkdoc` to generate per-package markdown, then patch it with narrative content. Rejected because the generated output requires significant post-processing and re-generation on every SDK release, coupling the docs build to Go toolchain availability in the Pages CI.

### Decision 4: Swift docs hosted in the server repo, not the Mac app repo

**Choice:** `docs/site/swift-sdk/` lives in `emergent-company/emergent` (this repo), not in `emergent-company/emergent.memory.mac`.

**Rationale:** A single GitHub Pages deployment point is simpler. The Swift source is stable enough to document in authored markdown that does not auto-sync. Cross-referencing source file paths (e.g., `Emergent/Core/Models.swift`) is sufficient for developers to navigate to the implementation. When the formal `EmergentSwiftSDK/` is built, the docs can be updated in place. Alternative considered: hosting Swift docs in the Mac repo with a separate Pages deploy — rejected due to complexity of two Pages deployments and split discovery.

### Decision 5: `docs/llms.md` follows llms.txt conventions (plain markdown, no directives)

**Choice:** Three flat markdown files (`docs/llms.md`, `docs/llms-go-sdk.md`, `docs/llms-swift-sdk.md`) with no YAML front matter, MkDocs admonitions, or HTML.

**Rationale:** The `llms.txt` convention (popularised by Anthropic's and other documentation sites) specifies that LLM-consumable docs should be raw, directive-free markdown that loads into a context window without preprocessing. These files are NOT part of the MkDocs `docs/site/` tree — they live at `docs/` root, so they are not rendered by MkDocs (avoids nav/theme injection) but remain version-controlled and easily accessible via the GitHub raw URL.

### Decision 6: GitHub Actions deployment uses `mkdocs gh-deploy --force`

**Choice:** Deploy via `mkdocs gh-deploy --force` to the `gh-pages` branch using the `GITHUB_TOKEN` provided automatically in Actions.

**Rationale:** `mkdocs gh-deploy` handles branch creation, push, and force-update in one command. No separate `peaceiris/actions-gh-pages` action needed. The workflow uses `permissions: contents: write` to allow the token to push to `gh-pages`. Alternative considered: building to `./site` and using `actions/deploy-pages` + `actions/upload-pages-artifact` — more steps, no benefit for this use case.

### Decision 7: `mkdocs.yml` at repo root, not `docs/mkdocs.yml`

**Choice:** `mkdocs.yml` at the repository root.

**Rationale:** MkDocs defaults to looking for `mkdocs.yml` at the working directory, which is the repo root in all CI environments. Placing it at `docs/mkdocs.yml` requires `--config-file docs/mkdocs.yml` on every command, making local dev and CI more error-prone. Repo root placement is the standard convention.

### Decision 8: Nav structure — tabs at top level, packages under Reference

**Choice:** Top nav: Home | Go SDK | Swift SDK. Under Go SDK: guides flat, then "Reference" group with 30+ sub-pages. Under Swift SDK: Overview, API Client, Models, Errors, Roadmap.

**Rationale:** MkDocs Material's navigation tabs mode makes the top-level sections visually distinct. Grouping all 30 package reference pages under a "Reference" collapsible section keeps the sidebar scannable — without grouping, 30 flat items would be overwhelming. The Swift SDK is small enough (5 pages) to remain flat under its tab.

## Risks / Trade-offs

**[Risk: Docs drift from SDK reality as API evolves]** → Mitigation: Add a note in `CONTRIBUTING.md` (or the docs site itself) that SDK changes require updating the corresponding `docs/site/go-sdk/reference/<package>.md` page. Consider a CI lint step that warns when `apps/server-go/pkg/sdk/**` changes without a corresponding `docs/site/**` change.

**[Risk: MkDocs build fails in CI if `docs/site/` nav entries don't match actual files]** → Mitigation: Use `--strict` on PRs to catch missing file references early. The `mkdocs.yml` nav will be kept explicit (not auto-generated) so any missing page is immediately a build error.

**[Risk: GitHub Pages not enabled for the repo]** → Mitigation: Document the one-time manual step (Settings → Pages → Source: `gh-pages` branch) in `docs/site/index.md` and in the workflow file comments. The workflow will silently succeed but pages won't be live until this step is done.

**[Risk: `docs/site/` MkDocs output conflicts with internal `docs/` content]** → Mitigation: `mkdocs build` outputs to `./site/` (default), which is already in `.gitignore` patterns. The MkDocs `docs_dir: docs/site` setting means it reads from `docs/site/` but writes to `./site/` — no collision with `docs/` internal files.

**[Risk: Python/MkDocs version pinning causes CI failures over time]** → Mitigation: Pin `mkdocs-material` to a specific minor version in `docs/requirements.txt` (e.g., `mkdocs-material==9.5.x`). Use `pip install --upgrade` only on deliberate upgrades.

**[Risk: Swift docs become stale if `EmergentAPIClient.swift` changes]** → Mitigation: Swift source changes infrequently; the roadmap note in the Swift docs section sets expectations. When the formal Swift SDK ships, the docs section will be replaced wholesale.

## Migration Plan

1. **Branch:** Create `sdk-documentation` branch from `main`
2. **Scaffold:** Add `mkdocs.yml`, `docs/requirements.txt`, `docs/site/` tree with placeholder pages
3. **Go SDK guides:** Author all 6 narrative guides under `docs/site/go-sdk/`
4. **Go SDK reference:** Author one page per package under `docs/site/go-sdk/reference/` (30 + auth + errors + graphutil = 33 pages)
5. **Swift SDK:** Author 5 pages under `docs/site/swift-sdk/`
6. **LLM files:** Author `docs/llms.md`, `docs/llms-go-sdk.md`, `docs/llms-swift-sdk.md`
7. **CI workflow:** Add `.github/workflows/docs.yml`
8. **README update:** Add docs link to `apps/server-go/pkg/sdk/README.md`
9. **Local verify:** Run `mkdocs build --strict` locally; confirm zero errors
10. **PR:** Open PR; CI runs `mkdocs build --strict`; review docs pages via `mkdocs serve`
11. **Merge to main:** Triggers auto-deploy to `gh-pages`
12. **Manual step:** Enable GitHub Pages in repo Settings → Pages → `gh-pages` branch (done once by repo admin)

**Rollback:** Delete the `gh-pages` branch and remove the workflow file. No production system is affected — this is pure documentation infrastructure.

## Open Questions

- **GitHub Pages URL:** Will the site be at `https://emergent-company.github.io/emergent/` (default) or a custom domain? If custom domain, a `CNAME` file is needed in `docs/site/`. No action needed now; can be added post-launch.
- **MkDocs plugins:** Should `mkdocs-autorefs` be included for cross-page `[Foo][]` link resolution? Useful for the reference pages but adds a pip dependency. Decision deferred to implementation — include if it reduces link maintenance burden.
- **`docs/public/` integration:** The existing `docs/public/` stub (3 files + `index.json`) could be surface-linked from the docs site's home page. Deferred — low priority, no requirement for it in the specs.
- **Monorepo tag path for `go get`:** The SDK uses sub-module tags (`apps/server-go/pkg/sdk/vX.Y.Z`). The docs should clarify the `go get` path precisely. Confirm the exact install command matches what's in the CHANGELOG before publishing.
