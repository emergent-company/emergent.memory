# Landing Page Capability - Delta Spec

## MODIFIED Requirements

### Requirement: Product Hierarchy Landing Page

The landing page SHALL present Emergent as a platform with multiple product offerings, positioning Emergent Core as the foundation technology and showcasing specialized products built on top. The page SHALL be rendered as static HTML using the static website generation system.

#### Scenario: Landing page shows Core positioning

- **GIVEN** a user visits the main landing page at `/` or `/landing`
- **WHEN** the page loads
- **THEN** the hero section SHALL explain Emergent Core as the platform foundation
- **AND** the page SHALL include a "Products Built on Emergent Core" section
- **AND** the products section SHALL display cards for Emergent Personal Assistant and Emergent Product Framework
- **AND** each product card SHALL include: product name, tagline, 2-3 key benefits, and a link to the product page
- **AND** the page SHALL be served as pre-rendered static HTML (no client-side JavaScript framework required)

#### Scenario: Navigation to product pages

- **GIVEN** a user is viewing the landing page
- **WHEN** the user clicks on a product card or navigation link
- **THEN** the system SHALL navigate to the corresponding product page (`/personal-assistant` or `/product-framework`)
- **AND** the browser history SHALL update with the new URL
- **AND** the navigation SHALL be a standard HTML link (no client-side routing required)

#### Scenario: Core platform features section

- **GIVEN** a user is viewing the landing page
- **WHEN** they scroll to the features section
- **THEN** the page SHALL display Core platform capabilities (knowledge graph, semantic embeddings, AI chat, MCP integration, configurable template packs)
- **AND** features SHALL emphasize platform extensibility and foundation capabilities
- **AND** the section SHALL be rendered from content data (YAML) into HTML via Go templates

## ADDED Requirements

### Requirement: Static HTML Rendering

The landing page and all product pages SHALL be generated as static HTML files using the static website generation system, not rendered client-side by React.

#### Scenario: Build-time page generation

- **GIVEN** the static site generator runs
- **WHEN** processing the landing page template and content
- **THEN** the system SHALL generate `dist/index.html` with fully rendered HTML
- **AND** all component templates (Hero, Features, CTA, Footer) SHALL be composed into the final HTML
- **AND** the HTML SHALL be minified and optimized for production
- **AND** the HTML SHALL include all necessary metadata for SEO

#### Scenario: Asset references in static HTML

- **GIVEN** a static HTML page is generated
- **WHEN** the HTML references CSS, JavaScript, or images
- **THEN** all asset URLs SHALL use absolute paths from site root (e.g., `/assets/css/style.min.css`)
- **AND** asset URLs SHALL include content hashes for cache busting (e.g., `style.abc123.min.css`)
- **AND** all asset files SHALL exist in the `dist/assets/` directory

#### Scenario: Content updates trigger rebuild

- **GIVEN** content data is updated (e.g., YAML file edited)
- **WHEN** the static site generator runs
- **THEN** the affected pages SHALL be regenerated with new content
- **AND** unchanged pages SHALL not be rebuilt (incremental build optimization)
- **AND** the build SHALL complete in \u003c5 seconds for single page update

### Requirement: Landing Page Performance

The static landing page SHALL load quickly and achieve high performance scores on standard benchmarks.

#### Scenario: Fast initial load

- **GIVEN** a user visits the landing page for the first time
- **WHEN** the page begins loading
- **THEN** the First Contentful Paint SHALL occur within 1.5 seconds
- **AND** the Largest Contentful Paint SHALL occur within 2.5 seconds
- **AND** the page SHALL be interactive within 3 seconds (Time to Interactive)

#### Scenario: Lighthouse performance score

- **GIVEN** the landing page is tested with Google Lighthouse
- **WHEN** running on a standard connection (4G)
- **THEN** the Performance score SHALL be ≥90
- **AND** the Accessibility score SHALL be ≥90
- **AND** the Best Practices score SHALL be ≥90
- **AND** the SEO score SHALL be ≥90

#### Scenario: Asset size budgets

- **GIVEN** the landing page is analyzed
- **WHEN** examining total page weight
- **THEN** the HTML file SHALL be \u003c50KB (uncompressed)
- **AND** the CSS file SHALL be \u003c30KB (gzipped)
- **AND** the JavaScript file SHALL be \u003c5KB (gzipped)
- **AND** total page weight (excluding large images) SHALL be \u003c200KB
