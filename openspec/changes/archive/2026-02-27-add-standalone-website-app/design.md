# Design: Standalone Static Website with Go Gomponents

## Context

We're extracting the React landing page (currently in `apps/admin/src/pages/landing`) into a **standalone static website** that:

- Uses **Go + Gomponents** for type-safe HTML component generation
- Maintains **component-based architecture** matching React patterns
- Outputs **pure HTML/CSS/minimal JS** for CDN deployment
- Enables **independent versioning and deployment** from admin app
- Prepares foundation for documentation and public content pages

**Stakeholders:**

- Marketing: Need fast, SEO-optimized landing pages
- Product: Want to add docs/help content without bloating admin SPA
- Engineering: Want to avoid maintaining React build for static content
- Operations: Want simple CDN deployment (no Node.js runtime)

**Constraints:**

- Must maintain exact visual design from current landing page
- Must work without JavaScript (progressive enhancement)
- Must be deployable to static CDN (no server runtime)
- Should reuse Tailwind/daisyUI design system
- Build time acceptable (one-time generation, not hot path)

## Goals / Non-Goals

**Goals:**

- ✅ Static HTML generation using Go + Gomponents
- ✅ Component composition pattern (mimic React structure)
- ✅ Minimal JavaScript (only for theme toggle, mobile menu)
- ✅ SEO-optimized output (proper meta tags, sitemap, structured data)
- ✅ Fast page loads (\u003c2s, Lighthouse \u003e90)
- ✅ Responsive design (mobile-first)
- ✅ Easy content updates for non-developers
- ✅ Foundation for docs/help sections

**Non-Goals:**

- ❌ Client-side reactivity (use static HTML)
- ❌ Complex JavaScript framework (no React, Vue, etc.)
- ❌ Database-driven content (static file generation)
- ❌ Server-side rendering at request time (pre-generate at build)
- ❌ Replacing admin app landing page immediately (coexist during transition)

## Decisions

### Decision 1: Go Gomponents for Type-Safe HTML Generation

**Choice:** Use Gomponents library (`github.com/maragudk/gomponents`) for composable, type-safe HTML component generation.

**Why:**

- Type-safe HTML in pure Go (no template strings)
- Component composition similar to React/JSX
- Compile-time error checking (invalid HTML caught early)
- No separate template files to maintain
- Auto-escaping prevents XSS
- Already using Go in our stack

**Alternatives considered:**

1. **Go html/template**
   - ❌ String-based templates (no type safety)
   - ❌ Runtime errors for typos
   - ❌ Separate .html files to maintain
2. **Static site generator (Hugo, 11ty, Astro)**
   - ❌ New toolchain to learn/maintain
   - ❌ Additional npm dependencies
   - ❌ Separate language/config from main codebase
3. **Server-side React (Next.js, Gatsby)**
   - ❌ Heavy build toolchain
   - ❌ Large dependency tree
   - ❌ Overkill for static content

**Implementation pattern:**

```go
// Component structure
internal/components/
├── layout.go         // Base HTML layout
├── hero.go           // Hero section component
├── features.go       // Features grid component
├── cta.go            // Call-to-action component
└── footer.go         // Footer component

// Example: Hero component (similar to React)
package components

import (
    . "github.com/maragudk/gomponents"
    . "github.com/maragudk/gomponents/html"
)

func Hero() Node {
    return Div(
        Class("relative z-2 overflow-hidden lg:h-screen"),
        ID("hero"),

        // Grainy texture background
        Div(Class("absolute inset-0 -z-1 opacity-20 grainy")),

        // Content container
        Div(
            Class("container flex items-center justify-center pt-20"),
            Div(
                Class("w-100 text-center"),
                P(
                    Class("text-2xl font-extrabold"),
                    Text("Systems That Learn,"),
                    Br(),
                    Span(
                        Class("bg-clip-text text-transparent"),
                        Text("Adapt, and Evolve"),
                    ),
                ),
            ),
        ),
    )
}

// Usage in page
func LandingPage() Node {
    return Layout(
        PageConfig{Title: "Emergent - Adaptive Systems for AI"},
        Hero(),
        Features(),
        CTA(),
        Footer(),
    )
}
```

### Decision 2: Tailwind CSS Build Strategy (Hybrid Approach)

**Choice:** Use Tailwind CSS standalone binary + pre-built CSS committed to repository.

**Why:**

- **No Node.js/npm at build/deploy time** - Standalone binary is native executable
- **AI-agent friendly** - Pre-built CSS means agents don't need CSS tooling
- **Fast deployment** - Just build Go binary, CSS already exists
- **Optional rebuilds** - Developers can rebuild CSS when styles change

**Build approach:**

1. **One-time setup** (dev machine):

   ```bash
   # Download Tailwind standalone binary
   curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64
   chmod +x tailwindcss-linux-x64
   mv tailwindcss-linux-x64 bin/tailwindcss

   # For daisyUI support, one-time npm install
   cd styles && npm install -D daisyui
   ```

2. **CSS build** (when styles change):

   ```bash
   # Build CSS with Tailwind + daisyUI
   ./bin/tailwindcss -i styles/input.css -o static/styles.css --minify

   # Or if using npm for daisyUI plugin:
   cd styles && npx tailwindcss -i input.css -o ../static/styles.css --minify
   ```

3. **Commit CSS** to repository:

   ```bash
   git add static/styles.css
   git commit -m "Update compiled CSS"
   ```

4. **Build/deploy** (no CSS build needed):
   ```bash
   go build -o bin/server cmd/server/main.go
   ./bin/server  # CSS already in static/
   ```

**Directory structure:**

```
apps/website/
├── bin/
│   └── tailwindcss          # Standalone binary (optional, gitignored)
├── styles/
│   ├── input.css            # Source CSS with @tailwind directives
│   ├── themes/              # daisyUI theme files copied from admin
│   ├── package.json         # Only for daisyUI plugin (optional)
│   └── node_modules/        # Gitignored
├── static/
│   └── styles.css           # ✓ COMMITTED (pre-built, ~20-30KB gzipped)
└── Makefile
```

**Makefile targets:**

```makefile
# Build Go binary only (CSS already exists)
build:
	go build -o bin/server cmd/server/main.go

# Rebuild CSS (optional - when styles change)
css-rebuild:
	@echo "Rebuilding CSS (requires Tailwind binary or npm)"
	@./bin/tailwindcss -i styles/input.css -o static/styles.css --minify
	@echo "⚠️  Don't forget to commit static/styles.css"

# Run server
run:
	go run cmd/server/main.go
```

**Alternatives considered:**

1. **Always build CSS at deploy time**

   - ❌ Requires npm/node in CI/CD
   - ❌ Slower builds
   - ❌ AI agents need CSS tooling

2. **Use CDN for Tailwind**

   - ❌ Larger bundle (full framework, not purged)
   - ❌ External dependency at runtime
   - ❌ Can't use daisyUI easily

3. **Write custom CSS**
   - ❌ Loses design system consistency with admin
   - ❌ More code to maintain

### Decision 3: Minimal JavaScript Strategy

**Choice:** Use vanilla JavaScript for essential interactivity only (theme toggle, mobile menu, topbar scroll behavior).

**Why:**

- Progressive enhancement: pages work without JS
- Fast page loads (no framework overhead)
- Easy to understand and maintain
- Reduces bundle size (\u003c5KB for all JS)

**Scope of JS:**

1. **Theme toggle** (~1KB)

   - Read/write `localStorage`
   - Toggle `data-theme` attribute
   - Respect `prefers-color-scheme`

2. **Mobile menu** (~1KB)

   - Toggle drawer open/close
   - Lock body scroll when open

3. **Topbar scroll behavior** (~1KB)

   - Show/hide navbar on scroll
   - Transparent → solid background on scroll

4. **Smooth scroll** (~500B)
   - Anchor link smooth scrolling

**Implementation:**

```javascript
// theme.js (~1KB minified)
const theme = localStorage.getItem('theme') ||
  (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
document.documentElement.setAttribute('data-theme', theme);

// mobile-menu.js (~1KB minified)
document.getElementById('menu-toggle').addEventListener('click', () =\u003e {
  document.getElementById('drawer').classList.toggle('open');
});
```

### Decision 4: Build-Time Asset Optimization

**Choice:** Generate fully optimized static assets at build time (CSS purge, image optimization, JS minification).

**Why:**

- Zero runtime overhead
- Maximum CDN cacheability
- Deterministic builds
- Easy to audit output

**Build pipeline:**

```bash
# Build workflow
1. Tailwind CSS → PurgeCSS → Minify → style.min.css (~20KB)
2. Component templates → Go template compiler → HTML files
3. Images → ImageMagick/Sharp → WebP + fallbacks
4. JS → Terser → Minified bundles
5. All → Output to dist/ → Ready for CDN
```

**Tools:**

- **CSS**: `tailwindcss` CLI → PurgeCSS → `cssnano`
- **Images**: `sharp` or `imagemagick` (WebP conversion, resizing)
- **JS**: `terser` or `esbuild` (minification)
- **HTML**: Go template rendering (no post-processing needed)

### Decision 5: Content Data Format

**Choice:** Use YAML frontmatter + Markdown for page content, Go structs for component data.

**Why:**

- Non-developers can edit content
- Version-controlled (Git)
- Type-safe in Go
- Supports i18n in future

**Example:**

```yaml
# content/pages/index.yaml
hero:
  title: 'Systems That Learn, Adapt, and Evolve'
  subtitle: 'Build AI applications on adaptive infrastructure'
  cta:
    primary:
      text: 'Explore Core'
      link: '/emergent-core'
    secondary:
      text: 'Learn More'
      link: '#features'

features:
  - title: 'Knowledge Graph Foundation'
    description: 'Build on semantic relationships'
    icon: 'lucide--network'
  # ... more features
```

```go
// Go structs
type PageData struct {
    Hero     HeroData     `yaml:"hero"`
    Features []FeatureData `yaml:"features"`
}

type HeroData struct {
    Title    string    `yaml:"title"`
    Subtitle string    `yaml:"subtitle"`
    CTA      CTAData   `yaml:"cta"`
}
```

### Decision 6: Deployment Strategy

**Choice:** Output static files ready for any CDN/static host (Cloudflare Pages, Netlify, S3+CloudFront, Vercel, GitHub Pages).

**Why:**

- Flexibility: not locked to specific platform
- Performance: serve from edge locations
- Cost: static hosting is cheap/free
- Reliability: no server to crash

**Output structure:**

```
dist/
├── index.html
├── personal-assistant/
│   └── index.html
├── product-framework/
│   └── index.html
├── assets/
│   ├── css/
│   │   └── style.min.css
│   ├── js/
│   │   └── app.min.js
│   └── images/
│       └── *.webp
├── sitemap.xml
└── robots.txt
```

**Deployment options documented:**

1. **Cloudflare Pages**: `npx wrangler pages publish dist`
2. **Netlify**: Drag-and-drop `dist/` or CLI deploy
3. **AWS S3 + CloudFront**: `aws s3 sync dist/ s3://bucket`
4. **Vercel**: `vercel --prod dist/`
5. **Self-hosted**: Nginx/Caddy serving `dist/`

## Risks / Trade-offs

### Risk 1: Divergence from Admin Landing Page Design

**Risk:** Static site design drifts from admin landing page over time.

**Mitigation:**

- During port: Do pixel-perfect comparison (visual regression testing)
- After launch: Admin landing page redirects to static site (single source of truth)
- Design system: Share Tailwind config between projects
- Documentation: Clear guidelines on when to update which site

### Risk 2: Content Update Workflow Complexity

**Risk:** Non-developers struggle to update content (need to edit YAML, rebuild, deploy).

**Mitigation:**

- Phase 1: Accept manual YAML editing (target audience: developers)
- Phase 2: Create simple CMS or form-based editor (future work)
- Documentation: Step-by-step content update guide with screenshots
- Automation: CI/CD auto-deploys on Git push to `main`

**Trade-off:** Simplicity vs. ease-of-use. We choose simplicity initially.

### Risk 3: Component Re-implementation Effort

**Risk:** Porting React components to Go templates is time-consuming and error-prone.

**Mitigation:**

- Start with simple components (Footer, CTA)
- Build reusable patterns first (card, button, section)
- Test each component in isolation
- Visual diff tool: Compare rendered output to React version
- Incremental approach: Port one page at a time

**Effort estimate:** ~2-3 days for core components + 1 day per page.

### Risk 4: JavaScript Bugs in Minimal Implementation

**Risk:** Custom JS for theme toggle/mobile menu has bugs across browsers.

**Mitigation:**

- Use well-tested patterns (copy from daisyUI examples)
- Progressive enhancement: site works without JS
- Browser testing: Automate with Playwright
- Fallbacks: Ensure graceful degradation

## Migration Plan

### Phase 1: Parallel Development (Week 1-2)

1. Build static site generator
2. Port landing page components
3. Deploy to staging domain (`website.dev.emergent-company.ai`)
4. Visual regression testing vs. admin landing page
5. Performance benchmarking (Lighthouse, WebPageTest)

### Phase 2: Coexistence (Week 3)

1. Static site live on staging
2. Admin landing page still at `/` and `/landing` (production)
3. User testing on staging
4. SEO crawl testing
5. Analytics setup (if needed)

### Phase 3: Cutover (Week 4)

1. Deploy static site to `www.emergent-company.ai` or `emergent-company.ai`
2. Redirect admin landing routes (`/`, `/landing`) to static site
3. Monitor for issues (404s, broken links, slow loads)
4. Update admin app to remove landing page code (cleanup)

### Rollback Plan

If static site has critical issues:

1. Remove DNS/redirect to static site
2. Restore admin landing page routes
3. Fix static site issues offline
4. Re-deploy when ready

**Rollback time:** \u003c5 minutes (DNS change or route restore)

## Open Questions

1. **Domain/subdomain strategy:**

   - Option A: `emergent-company.ai` (root domain, replace everything)
   - Option B: `www.emergent-company.ai` (www subdomain, admin stays at root)
   - Option C: `marketing.emergent-company.ai` (separate subdomain)
   - **Decision needed:** Which domain should host the static site?

2. **Analytics integration:**

   - Do we need Google Analytics, Plausible, or other tracking?
   - What metrics matter for marketing pages?
   - **Decision needed:** Analytics requirements

3. **A/B testing capability:**

   - Will marketing want to A/B test landing page variants?
   - If yes, need edge logic (Cloudflare Workers, Vercel Edge Functions)
   - **Decision needed:** A/B testing in scope?

4. **Content update frequency:**

   - How often will content change (daily, weekly, monthly)?
   - Who will make updates (devs, marketing, product)?
   - Informs whether we need CMS or Git workflow is fine
   - **Decision needed:** Update workflow requirements

5. **Internationalization (i18n):**
   - Will we need multi-language support?
   - If yes, Go templates support this well (separate content files per locale)
   - **Decision needed:** i18n in initial scope or future work?

## Next Steps

1. **Approval:** Get sign-off on Go template approach and architecture
2. **Domain decision:** Finalize where static site will be hosted
3. **Kick off:** Start with Phase 1 (parallel development)
4. **First milestone:** Landing page component port complete + visual regression passing
5. **Second milestone:** Product pages complete + SEO validation
6. **Launch:** Cut over to static site, monitor, cleanup admin landing code
