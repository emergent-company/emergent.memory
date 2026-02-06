# Static Website Generation Capability - Delta Spec

## ADDED Requirements

### Requirement: Go Template Component System

The system SHALL provide a component-based template system using Go html/template that enables reusable UI components with composition and data passing.

#### Scenario: Component registration and loading

- **GIVEN** the static site generator starts
- **WHEN** templates are loaded from `templates/components/`
- **THEN** each `.html` file SHALL be parsed as a named template
- **AND** components SHALL be accessible via `{{template "ComponentName" .}}`
- **AND** template parsing errors SHALL halt the build with clear error messages

#### Scenario: Component composition

- **GIVEN** a page template wants to use multiple components
- **WHEN** the page template includes `{{template "Topbar" .}}` and `{{template "Hero" .Data.Hero}}`
- **THEN** the system SHALL render the Topbar component with full context
- **AND** the system SHALL render the Hero component with scoped data (`.Data.Hero`)
- **AND** nested components SHALL have access to passed data only

#### Scenario: Layout composition

- **GIVEN** a page template uses a base layout
- **WHEN** the page defines `{{define "content"}}...{{end}}`
- **THEN** the base layout SHALL wrap the content in HTML structure (head, body)
- **AND** the base layout SHALL include metadata (title, description, OG tags)
- **AND** the page content SHALL be injected into the `{{template "content" .}}` block

#### Scenario: Type-safe data passing

- **GIVEN** a component expects specific data structure (Go struct)
- **WHEN** the component is rendered with data
- **THEN** the template SHALL access struct fields via `.FieldName`
- **AND** missing fields SHALL cause build-time errors (strict mode)
- **AND** data SHALL be auto-escaped for HTML safety

### Requirement: Static Asset Optimization

The system SHALL optimize all static assets (CSS, JavaScript, images) at build time for maximum performance and minimal file sizes.

#### Scenario: CSS optimization pipeline

- **GIVEN** Tailwind CSS utility classes are used in templates
- **WHEN** the build process runs
- **THEN** Tailwind SHALL generate CSS with only used utilities (PurgeCSS)
- **AND** the CSS SHALL be minified and concatenated into `style.min.css`
- **AND** unused daisyUI components SHALL be excluded from output
- **AND** the final CSS bundle SHALL be \u003c30KB gzipped

#### Scenario: JavaScript bundling and minification

- **GIVEN** JavaScript files exist in `src/js/` (theme.js, mobile-menu.js, topbar.js)
- **WHEN** the build process runs
- **THEN** all JS files SHALL be concatenated into `app.min.js`
- **AND** the JS SHALL be minified with Terser or esbuild
- **AND** the final JS bundle SHALL be \u003c5KB gzipped
- **AND** critical JS (theme) SHALL be inlined in HTML head

#### Scenario: Image optimization

- **GIVEN** images exist in `static/images/`
- **WHEN** the build process runs
- **THEN** images SHALL be converted to WebP format with fallbacks
- **AND** images SHALL be resized to multiple breakpoints (mobile, tablet, desktop)
- **AND** responsive `srcset` attributes SHALL be generated automatically
- **AND** images SHALL use lazy loading (`loading="lazy"`) for below-fold content

#### Scenario: Build output structure

- **GIVEN** the static site generator completes a build
- **WHEN** examining the `dist/` output directory
- **THEN** the structure SHALL be:
  ```
  dist/
  ├── index.html
  ├── personal-assistant/
  │   └── index.html
  ├── product-framework/
  │   └── index.html
  ├── assets/
  │   ├── css/style.min.css (with content hash)
  │   ├── js/app.min.js (with content hash)
  │   └── images/*.webp
  ├── sitemap.xml
  ├── robots.txt
  └── favicon.ico
  ```
- **AND** all HTML files SHALL be minified (whitespace removed)
- **AND** asset URLs SHALL include content hashes for cache busting

### Requirement: Content-Driven Page Generation

The system SHALL generate pages from structured content data (YAML) combined with Go templates, enabling non-developers to update content without code changes.

#### Scenario: YAML content structure

- **GIVEN** a page content file exists at `content/pages/index.yaml`
- **WHEN** the file contains structured data:
  ```yaml
  hero:
    title: 'Systems That Learn'
    subtitle: 'Build on adaptive infrastructure'
    cta:
      primary: { text: 'Get Started', link: '/docs' }
  ```
- **THEN** the Go struct SHALL parse this YAML into typed fields
- **AND** the template SHALL access data via `.Hero.Title`, `.Hero.CTA.Primary.Text`
- **AND** YAML parsing errors SHALL halt the build with line numbers

#### Scenario: Dynamic page generation from content

- **GIVEN** multiple YAML files exist in `content/pages/` (index.yaml, personal-assistant.yaml, product-framework.yaml)
- **WHEN** the build process runs
- **THEN** the system SHALL generate one HTML file per YAML file
- **AND** each HTML file SHALL use the appropriate template (matching filename or specified in YAML frontmatter)
- **AND** all pages SHALL inherit base layout unless overridden

#### Scenario: Content validation

- **GIVEN** a YAML content file has invalid structure (missing required fields, wrong types)
- **WHEN** the build process attempts to parse it
- **THEN** the build SHALL fail with a validation error
- **AND** the error message SHALL specify the file, field, and expected type
- **AND** the build SHALL not produce partial output

### Requirement: SEO Optimization

The system SHALL generate SEO-optimized HTML with proper metadata, structured data, and sitemaps to maximize search engine visibility.

#### Scenario: Page metadata generation

- **GIVEN** a page is generated
- **WHEN** the HTML is rendered
- **THEN** the `\u003chead\u003e` SHALL include:
  - `\u003ctitle\u003e` unique to the page
  - `\u003cmeta name="description"\u003e` unique to the page
  - `\u003cmeta property="og:title"\u003e`, `og:description`, `og:image`, `og:url` (Open Graph)
  - `\u003cmeta name="twitter:card"\u003e`, `twitter:title`, `twitter:description`, `twitter:image` (Twitter Card)
  - `\u003clink rel="canonical"\u003e` pointing to the page URL
- **AND** metadata SHALL be sourced from YAML content (`meta.title`, `meta.description`)
- **AND** missing metadata SHALL use sensible defaults

#### Scenario: Sitemap generation

- **GIVEN** all pages have been generated
- **WHEN** the build completes
- **THEN** a `sitemap.xml` file SHALL be created in `dist/`
- **AND** the sitemap SHALL include all page URLs with `\u003clastmod\u003e` timestamps
- **AND** the sitemap SHALL be valid XML conforming to sitemaps.org schema
- **AND** the sitemap SHALL be referenced in `robots.txt`

#### Scenario: Structured data (JSON-LD)

- **GIVEN** a page represents a product or organization
- **WHEN** the page is rendered
- **THEN** the HTML SHALL include a `\u003cscript type="application/ld+json"\u003e` block
- **AND** the structured data SHALL use schema.org types (Organization, Product, WebPage)
- **AND** structured data SHALL be validated against Google's Rich Results Test

#### Scenario: robots.txt generation

- **GIVEN** the build completes
- **WHEN** examining the output
- **THEN** a `robots.txt` file SHALL exist at `dist/robots.txt`
- **AND** the file SHALL allow all crawlers (`User-agent: * / Allow: /`)
- **AND** the file SHALL reference the sitemap (`Sitemap: https://example.com/sitemap.xml`)

### Requirement: Development Workflow

The system SHALL provide a fast development workflow with live reloading and file watching for rapid iteration on templates and content.

#### Scenario: Local development server

- **GIVEN** a developer runs `npm run website:dev`
- **WHEN** the command starts
- **THEN** the system SHALL start an HTTP server on `http://localhost:8080`
- **AND** the system SHALL serve files from `dist/` (generated on start)
- **AND** the terminal SHALL display the local URL and build status

#### Scenario: File watching and auto-rebuild

- **GIVEN** the development server is running
- **WHEN** a developer edits a template file (`.html`)
- **THEN** the system SHALL detect the change within 500ms
- **AND** the system SHALL rebuild only affected pages
- **AND** the terminal SHALL log the rebuild time (e.g., "Rebuilt in 234ms")
- **AND** the browser SHALL refresh automatically (if using live reload extension)

#### Scenario: Content file changes

- **GIVEN** the development server is running
- **WHEN** a developer edits a content file (`content/pages/index.yaml`)
- **THEN** the system SHALL detect the change
- **AND** the system SHALL re-parse YAML and rebuild the affected page
- **AND** YAML errors SHALL be displayed in terminal with line numbers

#### Scenario: CSS/JS changes

- **GIVEN** the development server is running
- **WHEN** a developer edits `src/css/custom.css` or `src/js/theme.js`
- **THEN** the system SHALL rebuild the asset bundles
- **AND** the browser SHALL refresh to load new assets
- **AND** the rebuild SHALL complete within 1 second

### Requirement: Production Build

The system SHALL create optimized, production-ready static files suitable for deployment to any CDN or static hosting platform.

#### Scenario: Production build command

- **GIVEN** a developer runs `npm run website:build`
- **WHEN** the command executes
- **THEN** the system SHALL:
  1. Clean the `dist/` directory
  2. Parse all content YAML files
  3. Render all page templates
  4. Optimize and bundle CSS
  5. Optimize and bundle JavaScript
  6. Optimize images
  7. Generate sitemap.xml
  8. Generate robots.txt
  9. Copy static assets
- **AND** the build SHALL complete in \u003c30 seconds for typical site (\u003c20 pages)
- **AND** the terminal SHALL report success with file count and total size

#### Scenario: Build output validation

- **GIVEN** the production build completes successfully
- **WHEN** examining the `dist/` directory
- **THEN** all HTML files SHALL be valid HTML5 (pass W3C validator)
- **AND** all asset URLs SHALL resolve (no broken links)
- **AND** all images SHALL be optimized (\u003c200KB each)
- **AND** total bundle size SHALL be \u003c500KB (excluding large images)

#### Scenario: Deployment to CDN

- **GIVEN** the `dist/` directory contains production-ready files
- **WHEN** deploying to a CDN (Cloudflare Pages, Netlify, Vercel, S3+CloudFront)
- **THEN** the deployment SHALL succeed with a simple upload/sync command
- **AND** the static site SHALL be accessible via the configured domain
- **AND** all pages SHALL load in \u003c2 seconds (First Contentful Paint)
- **AND** Lighthouse scores SHALL be \u003e90 for Performance, Accessibility, Best Practices, SEO

### Requirement: Progressive Enhancement JavaScript

The system SHALL use minimal JavaScript for essential interactivity while ensuring the site functions without JavaScript enabled.

#### Scenario: Theme toggle functionality

- **GIVEN** the site is loaded
- **WHEN** a user clicks the theme toggle button
- **THEN** the page SHALL switch between light and dark themes instantly
- **AND** the theme preference SHALL be saved to `localStorage`
- **AND** the page SHALL respect system preference (`prefers-color-scheme`) on first visit
- **AND** the theme SHALL persist across page navigations
- **AND** the theme SHALL apply before content renders (no flash of unstyled content)

#### Scenario: Mobile navigation menu

- **GIVEN** the site is viewed on mobile (\u003c768px width)
- **WHEN** a user taps the menu button
- **THEN** the navigation drawer SHALL slide open from the left or top
- **AND** the body scroll SHALL be locked (prevent scrolling behind drawer)
- **WHEN** the user taps outside the drawer or the close button
- **THEN** the drawer SHALL close
- **AND** body scroll SHALL be restored

#### Scenario: Topbar scroll behavior

- **GIVEN** the user scrolls down the page
- **WHEN** the scroll position exceeds 500px
- **THEN** the topbar SHALL slide up and hide (improve reading experience)
- **WHEN** the user scrolls up
- **THEN** the topbar SHALL slide back down and become visible

#### Scenario: No-JavaScript fallback

- **GIVEN** a user has JavaScript disabled
- **WHEN** the site loads
- **THEN** all content SHALL be readable and navigable
- **AND** the theme SHALL default to light mode or system preference (via CSS media query)
- **AND** the mobile menu SHALL be accessible via `:target` pseudo-class or always visible
- **AND** all links and buttons SHALL function normally

### Requirement: Responsive Design

All generated pages SHALL be fully responsive and provide optimal viewing experience across mobile, tablet, and desktop devices.

#### Scenario: Mobile-first layout

- **GIVEN** a page is viewed on mobile (viewport width \u003c768px)
- **WHEN** the page renders
- **THEN** the layout SHALL use single-column design
- **AND** images SHALL resize to fit viewport width
- **AND** text SHALL be readable without zooming (minimum 16px font size)
- **AND** touch targets SHALL be at least 44x44px (WCAG 2.1 AA)
- **AND** no horizontal scrolling SHALL occur

#### Scenario: Tablet layout

- **GIVEN** a page is viewed on tablet (viewport width 768px-1024px)
- **WHEN** the page renders
- **THEN** the layout MAY use two-column grids for feature cards
- **AND** the topbar SHALL display full navigation (no hamburger menu)
- **AND** images SHALL use optimized sizes for tablet resolution

#### Scenario: Desktop layout

- **GIVEN** a page is viewed on desktop (viewport width \u003e1024px)
- **WHEN** the page renders
- **THEN** the layout SHALL use multi-column grids (up to 3 columns)
- **AND** the max content width SHALL be constrained (e.g., 1280px) for readability
- **AND** whitespace SHALL be used generously for breathing room
- **AND** images SHALL use full-resolution assets
