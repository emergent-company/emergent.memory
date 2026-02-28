# Implementation Tasks: Standalone Website App

## 1. Project Setup

- [ ] 1.1 Create `apps/website/` directory structure
- [ ] 1.2 Set up Go module (`go.mod`) for website generator
- [ ] 1.3 Create `static/` directories (css, js, images, fonts)
- [ ] 1.4 Add Nx project configuration for website app
- [ ] 1.5 Create `.gitignore` for build artifacts
- [ ] 1.6 Set up build output directory (`dist/` or `public/`)

## 2. Template Infrastructure

- [ ] 2.1 Create base layout template (`templates/layouts/base.html`)
- [ ] 2.2 Implement Go template component loading system
- [ ] 2.3 Create component registry/loader pattern
- [ ] 2.4 Set up partial template inclusion mechanism
- [ ] 2.5 Implement data passing between layouts and components

## 3. Design System Port

- [ ] 3.1 Extract Tailwind config from admin app
- [ ] 3.2 Create minimal Tailwind CSS build for website
- [ ] 3.3 Copy daisyUI theme configuration
- [ ] 3.4 Port custom CSS classes (grainy texture, animations, gradients)
- [ ] 3.5 Extract and minify only used utility classes
- [ ] 3.6 Set up PostCSS/build pipeline for CSS

## 4. Component Implementation (Port from React)

- [ ] 4.1 Port `Topbar` component
  - [ ] Navigation structure
  - [ ] Mobile drawer menu
  - [ ] Scroll behavior logic
  - [ ] Logo component
  - [ ] Theme toggle (minimal JS)
- [ ] 4.2 Port `Hero` component
  - [ ] Gradient text animations
  - [ ] Background effects (grainy texture)
  - [ ] 3D graph background (assess if needed)
  - [ ] CTA buttons
- [ ] 4.3 Port `Features` component
  - [ ] Feature cards layout
  - [ ] Icons integration (Iconify or inline SVG)
  - [ ] Responsive grid
- [ ] 4.4 Port `CTA` component
  - [ ] Call-to-action section
  - [ ] Button styles
- [ ] 4.5 Port `Footer` component
  - [ ] Links structure
  - [ ] Multi-column layout
  - [ ] Copyright info
  - [ ] Social links (if any)

## 5. Page Templates

- [ ] 5.1 Create landing page (`templates/pages/index.html`)
  - [ ] Compose Hero + Features + CTA + Footer
  - [ ] Wire up navigation
  - [ ] Set metadata (title, description, OG tags)
- [ ] 5.2 Create Personal Assistant product page (`templates/pages/personal-assistant.html`)
  - [ ] Hero section with value proposition
  - [ ] Features section (8 features from spec)
  - [ ] Use cases section (3-5 scenarios)
  - [ ] Privacy \u0026 trust section
  - [ ] Getting started CTA
- [ ] 5.3 Create Product Framework page (`templates/pages/product-framework.html`)
  - [ ] Hero section
  - [ ] Features section (6 features)
  - [ ] Use cases section (2-3 scenarios)
  - [ ] Product bible explanation
  - [ ] Getting started CTA

## 6. Static Asset Management

- [ ] 6.1 Copy/optimize images from admin app
- [ ] 6.2 Create favicon set
- [ ] 6.3 Add social media preview images (OG, Twitter Card)
- [ ] 6.4 Set up font loading (if custom fonts used)
- [ ] 6.5 Optimize image formats (WebP with fallbacks)

## 7. Minimal JavaScript

- [ ] 7.1 Theme toggle functionality
  - [ ] LocalStorage persistence
  - [ ] System preference detection
  - [ ] Toggle UI interaction
- [ ] 7.2 Mobile menu toggle
  - [ ] Drawer open/close
  - [ ] Body scroll lock when open
- [ ] 7.3 Smooth scroll to anchors (if needed)
- [ ] 7.4 Topbar scroll behavior (hide on scroll down)
- [ ] 7.5 Bundle and minify JS

## 8. Build System

- [ ] 8.1 Create Go static site generator (`main.go`)
  - [ ] Load all templates
  - [ ] Parse template data
  - [ ] Render pages to HTML
  - [ ] Copy static assets
  - [ ] Generate sitemap.xml
- [ ] 8.2 Add CLI flags (output dir, watch mode, serve mode)
- [ ] 8.3 Implement file watching for development
- [ ] 8.4 Add local dev server (optional: `http.ListenAndServe`)
- [ ] 8.5 Create build script (`npm run website:build`)
- [ ] 8.6 Create dev script (`npm run website:dev`)

## 9. SEO \u0026 Metadata

- [ ] 9.1 Add proper `<head>` metadata to each page
  - [ ] Title, description
  - [ ] Open Graph tags (og:title, og:image, og:description)
  - [ ] Twitter Card tags
  - [ ] Canonical URLs
- [ ] 9.2 Generate `sitemap.xml`
- [ ] 9.3 Create `robots.txt`
- [ ] 9.4 Add structured data (JSON-LD) for rich snippets

## 10. Deployment Preparation

- [ ] 10.1 Create deployment documentation
- [ ] 10.2 Add Dockerfile for serving static files (optional: Nginx/Caddy)
- [ ] 10.3 Configure cache headers for static assets
- [ ] 10.4 Set up CDN integration guide (Cloudflare, etc.)
- [ ] 10.5 Document DNS/domain setup for website subdomain

## 11. Quality Assurance

- [ ] 11.1 Visual regression testing (compare with admin landing page)
- [ ] 11.2 Mobile responsiveness testing (iOS, Android)
- [ ] 11.3 Browser compatibility testing (Chrome, Safari, Firefox, Edge)
- [ ] 11.4 Accessibility audit (WCAG 2.1 AA)
  - [ ] Keyboard navigation
  - [ ] Screen reader testing
  - [ ] Color contrast checks
- [ ] 11.5 Performance testing
  - [ ] Lighthouse score (\u003e90 for all metrics)
  - [ ] Page load time (\u003c2s)
  - [ ] Asset size audit
- [ ] 11.6 SEO validation
  - [ ] Meta tags present
  - [ ] Structured data valid
  - [ ] Sitemap accessible

## 12. Documentation

- [ ] 12.1 Create README.md for website app
  - [ ] How to build
  - [ ] How to run locally
  - [ ] How to add new pages
  - [ ] Component architecture explanation
- [ ] 12.2 Document template syntax and conventions
- [ ] 12.3 Add code examples for common patterns
- [ ] 12.4 Create deployment guide
- [ ] 12.5 Document how to update content (for non-devs)

## 13. Future-Proofing

- [ ] 13.1 Create placeholder structure for `/docs` section
- [ ] 13.2 Create placeholder structure for `/help` section
- [ ] 13.3 Document how to add new sections/pages
- [ ] 13.4 Plan content management strategy (Markdown? YAML frontmatter?)
