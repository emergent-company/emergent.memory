# Change: Extract Standalone Static Website from Admin Landing Page

## Why

The current landing page is embedded within the React admin SPA (`apps/admin/src/pages/landing`), which creates several problems:

- **Heavy bundle**: Requires entire React/Vite build infrastructure for static marketing content
- **Deployment complexity**: Cannot deploy marketing site independently from admin dashboard
- **Performance overhead**: React runtime and SPA overhead for pages that don't need client-side reactivity
- **SEO limitations**: Single-page app architecture makes search engine indexing more difficult
- **No public documentation path**: Need independent website structure to add docs/help content in future

We want to port the existing React landing page components to a standalone static website using **Go templates (html/template)** with **componentized architecture** matching the current React structure.

## What Changes

- Create new static website application using Go template components (no new tech stack beyond what we have)
- Port existing React landing page components (`Hero`, `Features`, `CTA`, `Footer`, `Topbar`, etc.) to Go template equivalents
- Implement component composition pattern matching React structure
- Generate pure HTML/CSS/minimal JS static output
- Enable independent deployment from admin dashboard
- Prepare structure for future documentation pages
- Maintain existing design system (Tailwind CSS, DaisyUI components)

**This is NOT a breaking change** - the admin landing page will remain functional until we redirect traffic to the new static site.

## Impact

**Affected specs:**

- `landing-page` (MODIFIED - same content, different rendering approach)
- `static-website-generation` (NEW capability - Go template component system)

**Affected code:**

- **NEW**: `apps/website/` - Standalone static site generator
  - `templates/components/` - Go template components (Hero, Features, CTA, etc.)
  - `templates/layouts/` - Page layouts (base, landing, product pages)
  - `templates/pages/` - Page content (index, personal-assistant, product-framework)
  - `static/` - CSS, minimal JS, images
  - `main.go` - Static site generator CLI
- **REFERENCE** (no changes): `apps/admin/src/pages/landing/` - Source of truth for component patterns during port

**Architecture decisions:**

- **Go html/template** for templating (already in our stack via server-go)
- **Component composition**: Nested Go templates mirroring React component tree
- **Tailwind CSS + DaisyUI**: Same design system as admin (copy utility classes)
- **Minimal JavaScript**: Only for interactive elements (theme toggle, mobile menu)
- **Build output**: Static HTML files ready for CDN/static hosting

**Future work enabled:**

- Add `/docs` section for documentation
- Add `/help` section for user guides
- Add `/api-docs` for developer portal
- Deploy marketing site to different domain/CDN than admin app
