# landing-page Specification

## Purpose
TBD - created by archiving change add-emergent-product-hierarchy. Update Purpose after archive.
## Requirements
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

### Requirement: Personal Assistant Product Page

The system SHALL provide a dedicated product page for Emergent Personal Assistant at `/personal-assistant` that explains the product's value proposition, features, and use cases for personal life management.

#### Scenario: Personal Assistant page structure

- **GIVEN** a user navigates to `/personal-assistant`
- **WHEN** the page loads
- **THEN** the page SHALL display:
  - Hero section with value proposition ("Reclaim your cognitive bandwidth—your life's invisible project manager")
  - Problem statement ("administrative siege" - 19,656 life admin tasks over lifetime causing burnout)
  - Solution overview (cognitive prosthetic for executive function)
  - Key features section (6-8 features drawn from value proposition)
  - Use cases section (3-5 scenarios with problem/solution/outcome/value delivered)
  - How it works section (agentic architecture, local-first privacy)
  - Trust & privacy section (data sovereignty, local processing)
  - Getting started CTA

#### Scenario: Personal Assistant features display

- **GIVEN** a user is viewing the Personal Assistant product page
- **WHEN** they view the features section
- **THEN** the page SHALL list features including:
  - **Restore Executive Function**: External prefrontal cortex that scaffolds task initiation and reduces paralysis
  - **Eliminate Financial Waste**: Prevents $500-1000/year in subscription waste, late fees, missed warranty claims
  - **Prevent Relationship Damage**: Never forget important dates; maintain social bonds without mental load
  - **Semantic Document Search**: Find any document in seconds by asking questions ("When does warranty expire?")
  - **Proactive Monitoring**: Background surveillance of expirations, renewals, and obligations (doesn't wait for prompts)
  - **Email Paralysis Breaker**: Draft generation to overcome communication anxiety and Wall of Awful
  - **Privacy-First Architecture**: Local processing; sensitive data never leaves your device
  - **Subscription Defense**: Automated cancellation of forgotten subscriptions; fights dark patterns
- **AND** each feature SHALL include a feature → value mapping explaining the user benefit

#### Scenario: Personal Assistant use cases

- **GIVEN** a user is viewing the Personal Assistant product page
- **WHEN** they view the use cases section
- **THEN** the page SHALL display 3-5 scenarios drawn from research such as:
  - **The Forgotten Car Insurance Renewal**: How AI discovers expiration, gathers comparison data, and presents top 3 options → $630 saved, zero cognitive load
  - **The Wall of Awful Email Inbox**: How AI drafts responses to break communication paralysis → job opportunity captured, friendship preserved
  - **The Subscription Graveyard**: How AI detects unused recurring charges and handles cancellation → $1,007/year recovered
  - **Mom's 70th Birthday**: How AI provides 2-week notice with gift ideas and relationship context → thoughtful gift, relationship strengthened
  - **The Lost Vaccine Record Crisis**: How AI searches emails/documents to compile proof → $300 saved, enrollment deadline met
- **AND** each use case SHALL follow the format: User profile → Problem (with quantified impact) → With Personal Assistant (step-by-step) → Value Delivered (time, money, relationships saved)

#### Scenario: Data privacy and cognitive prosthetic explanation

- **GIVEN** a user is viewing the Personal Assistant product page
- **WHEN** they look for privacy information and product positioning
- **THEN** the page SHALL clearly state:
  - **Privacy Architecture**: "Your sensitive data never leaves your device. Personal Assistant runs locally using on-device processing and embedded vector search."
  - **Data Sovereignty**: "You maintain physical control of bank statements, medical records, and private documents. No cloud upload required."
  - **Cognitive Prosthetic Framing**: "Personal Assistant is not a chatbot—it's an external executive function that restores cognitive bandwidth by fighting the 'administrative siege' of modern life."
  - **Proactive vs. Reactive**: "Unlike assistants that wait for commands, Personal Assistant monitors your life 24/7 and discovers tasks autonomously."
- **AND** the page SHALL mention local-first architecture (LanceDB, on-device NPU/CPU processing)
- **AND** the page SHALL explain the research foundation (e.g., "Based on research into the 19,656 life admin tasks burdening individuals over a lifetime")

### Requirement: Product Framework Product Page

The system SHALL provide a dedicated product page for Emergent Product Framework at `/product-framework` that explains the product's value for product strategy, planning, and definition work.

#### Scenario: Product Framework page structure

- **GIVEN** a user navigates to `/product-framework`
- **WHEN** the page loads
- **THEN** the page SHALL display:
  - Hero section with value proposition ("Build better products with strategic clarity")
  - Problem statement (product strategy is complex and disconnected)
  - Solution overview (AI-powered framework for product definition)
  - Key features section (4-6 features)
  - Use cases section (2-3 scenarios)
  - How it works section
  - Getting started CTA

#### Scenario: Product Framework features display

- **GIVEN** a user is viewing the Product Framework product page
- **WHEN** they view the features section
- **THEN** the page SHALL list features including:
  - Strategic planning tools and frameworks
  - Value proposition development
  - Go-to-market strategy and tactics
  - Product roadmap visualization
  - AI-powered insights and recommendations
  - Living product definition ("product bible")

#### Scenario: Product Framework use cases

- **GIVEN** a user is viewing the Product Framework product page
- **WHEN** they view the use cases section
- **THEN** the page SHALL display 2-3 scenarios such as:
  - Solo founder building product strategy from scratch
  - Product leader maintaining product roadmap and vision
  - Team aligning on product definition and strategy
- **AND** each use case SHALL explain the problem, solution, and outcome

#### Scenario: Product bible explanation

- **GIVEN** a user is viewing the Product Framework product page
- **WHEN** they read about the "product bible" feature
- **THEN** the page SHALL explain that Product Framework creates a comprehensive, living product definition
- **AND** the page SHALL mention generating artifacts like marketing materials, presentations, and documentation from the product definition

### Requirement: Product Page Navigation

The system SHALL provide consistent navigation between the main landing page and product pages.

#### Scenario: Top navigation with product links

- **GIVEN** a user is on any public page (landing, personal-assistant, product-framework)
- **WHEN** they view the top navigation bar
- **THEN** the navigation SHALL include:
  - "Emergent" or "Core" link to landing page
  - "Products" dropdown or section with links to Personal Assistant and Product Framework
  - "Get Started" CTA button

#### Scenario: Footer navigation

- **GIVEN** a user scrolls to the footer on any public page
- **WHEN** they view the footer links
- **THEN** the footer SHALL include links to:
  - All product pages (Core, Personal Assistant, Product Framework)
  - Documentation
  - API/Developer access
  - Company information

#### Scenario: Breadcrumb navigation

- **GIVEN** a user is on a product page (`/personal-assistant` or `/product-framework`)
- **WHEN** they want to navigate back
- **THEN** the page MAY include breadcrumb navigation showing: Home > [Product Name]

### Requirement: Responsive Product Pages

All product pages SHALL be fully responsive and accessible on mobile, tablet, and desktop devices.

#### Scenario: Mobile-optimized layout

- **GIVEN** a user accesses any product page on a mobile device (viewport width < 768px)
- **WHEN** the page loads
- **THEN** the layout SHALL adapt to single-column display
- **AND** images and cards SHALL resize appropriately
- **AND** navigation SHALL collapse into a mobile menu
- **AND** text SHALL remain readable without horizontal scrolling

#### Scenario: Tablet and desktop layouts

- **GIVEN** a user accesses any product page on tablet (768px-1024px) or desktop (>1024px)
- **WHEN** the page loads
- **THEN** the layout SHALL use multi-column grids for features and use cases
- **AND** hero sections SHALL use optimal aspect ratios
- **AND** whitespace SHALL be appropriate for readability

### Requirement: Product Page Performance

Product pages SHALL load quickly and provide a smooth user experience.

#### Scenario: Initial page load

- **GIVEN** a user navigates to any product page
- **WHEN** the page begins loading
- **THEN** the page SHALL display visible content within 2 seconds (First Contentful Paint)
- **AND** the page SHALL be fully interactive within 4 seconds (Time to Interactive)
- **AND** images SHALL use lazy loading for below-the-fold content

#### Scenario: Navigation between product pages

- **GIVEN** a user is on one product page
- **WHEN** they navigate to another product page
- **THEN** the navigation SHALL be near-instantaneous (client-side routing)
- **AND** the page SHALL not require full page reload

### Requirement: Landing Page Product Branding

The landing page SHALL represent the "Emergent" product with accurate branding, messaging, and visual identity.

#### Scenario: User visits landing page

- **WHEN** a user navigates to `/` or `/landing`
- **THEN** the page displays "Emergent" branding (logo, product name)
- **AND** all references to template content (Scalo, generic dashboards) are removed

#### Scenario: Logo displays correctly

- **WHEN** the landing page renders
- **THEN** the Emergent logo appears in the topbar
- **AND** the logo works in both light and dark themes
- **AND** the logo is accessible (alt text, proper ARIA labels)

### Requirement: Clear Value Proposition

The landing page SHALL communicate Emergent's core value: transforming documents into AI-ready knowledge through intelligent database processing.

#### Scenario: Hero section conveys purpose

- **WHEN** a user views the hero section
- **THEN** the primary headline clearly states the product's main benefit
- **AND** the supporting text explains how Emergent works (semantic embeddings, graph relationships, MCP)
- **AND** the language is accessible to both technical and non-technical audiences

#### Scenario: Call-to-action guides users

- **WHEN** a user wants to try the product
- **THEN** a prominent "Open Dashboard" button is visible
- **AND** clicking the button navigates to `/admin`
- **AND** secondary actions (docs, GitHub) are available but less prominent

### Requirement: Product-Specific Features

The landing page SHALL highlight Emergent's key capabilities: document ingestion, semantic embeddings, knowledge graph, schema-aware chat, hybrid search, and multi-tenant projects.

#### Scenario: Features section displays core capabilities

- **WHEN** a user scrolls to the features section
- **THEN** exactly 6 feature cards are displayed
- **AND** each card has an icon, title, and brief description
- **AND** features focus on user benefits, not implementation details

#### Scenario: Feature content is accurate

- **WHEN** feature descriptions are rendered
- **THEN** they accurately reflect implemented functionality
- **AND** no placeholder or template content is shown
- **AND** technical terms are explained in user-friendly language

### Requirement: Clean Content Removal

The landing page SHALL NOT contain template-specific content: technology stack badges, e-commerce/CRM showcase, "Buy Now" buttons, testimonials, or bundle offers.

#### Scenario: Template content is removed

- **WHEN** the landing page renders
- **THEN** no technology stack logos (React, Next.js, Tailwind, etc.) are visible
- **AND** no e-commerce/CRM/dashboard screenshots are shown
- **AND** no "Buy Now" or purchase-related buttons exist
- **AND** no references to template marketplace (Scalo, daisyUI store) appear

#### Scenario: Showcase section is product-focused

- **WHEN** the showcase section renders (if present)
- **THEN** it displays Emergent product screenshots OR architecture diagram OR is removed entirely
- **AND** no generic admin dashboard templates are shown

### Requirement: SEO and Accessibility

The landing page SHALL include proper meta tags, semantic HTML, and accessibility features for Emergent branding.

#### Scenario: Meta tags reflect product

- **WHEN** the page HTML is rendered
- **THEN** the page title is "Emergent - AI-Ready Knowledge Management"
- **AND** meta description accurately describes Emergent's purpose
- **AND** Open Graph tags use Emergent branding and description

#### Scenario: Accessibility standards met

- **WHEN** the page is evaluated for accessibility
- **THEN** all images have appropriate alt text
- **AND** heading hierarchy is semantic (h1, h2, h3)
- **AND** keyboard navigation works for all interactive elements
- **AND** ARIA labels are present where needed

### Requirement: Responsive Design Maintained

The landing page SHALL maintain responsive design and theme compatibility (light/dark mode) after rebranding.

#### Scenario: Mobile layout works correctly

- **WHEN** the page is viewed on mobile devices (< 768px)
- **THEN** all content is readable and properly formatted
- **AND** navigation is accessible via mobile menu
- **AND** images and sections stack appropriately

#### Scenario: Theme switching works

- **WHEN** a user toggles between light and dark themes
- **THEN** all text remains readable
- **AND** images/logos display appropriate variants
- **AND** color contrast meets accessibility standards (WCAG AA)

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

