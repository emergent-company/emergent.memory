# Change Proposal: Standalone Website App

## Summary

I've created a comprehensive OpenSpec change proposal for extracting the current React landing page into a **standalone static website** using **Go template components**.

## What's Included

### 📋 Core Documents

1. **`proposal.md`** - High-level overview

   - **Why**: Current landing page is embedded in admin SPA (performance, deployment, SEO issues)
   - **What**: Port React components to Go templates, generate static HTML
   - **Impact**: New `apps/website/` app, modified landing-page spec

2. **`design.md`** - Technical architecture (200+ lines)

   - Go html/template component system design
   - 5 major architectural decisions with rationale
   - Build pipeline and asset optimization strategy
   - Risk analysis and mitigation plans
   - 3-phase migration plan (parallel dev → coexistence → cutover)
   - 5 open questions for stakeholder decisions

3. **`tasks.md`** - Implementation checklist (13 sections, 80+ tasks)
   - Project setup
   - Template infrastructure
   - Design system port (Tailwind/daisyUI)
   - Component implementation (Topbar, Hero, Features, etc.)
   - Page templates (landing, personal-assistant, product-framework)
   - Build system
   - SEO \u0026 metadata
   - Quality assurance
   - Documentation
   - Future-proofing for docs/help sections

### 📐 Spec Deltas

4. **`specs/static-website-generation/spec.md`** - NEW capability

   - 8 comprehensive requirements with scenarios:
     - Go Template Component System
     - Static Asset Optimization
     - Content-Driven Page Generation
     - SEO Optimization
     - Development Workflow
     - Production Build
     - Progressive Enhancement JavaScript
     - Responsive Design

5. **`specs/landing-page/spec.md`** - MODIFIED capability
   - Updated existing landing page requirements to specify static HTML rendering
   - Added performance requirements (Lighthouse scores, load times)
   - Clarified build-time vs. runtime rendering

## Key Architectural Decisions

### ✅ Go html/template for Components

- **Why**: Already in stack, no new dependencies, type-safe, auto-escaping
- **Pattern**: Nested templates mimicking React component structure
- **Alternative rejected**: Hugo/11ty (new toolchain), Next.js (too heavy)

### ✅ Minimal JavaScript (\u003c5KB total)

- **Scope**: Theme toggle, mobile menu, topbar scroll only
- **Progressive enhancement**: Site works without JS
- **Alternative rejected**: React/SPA (defeats purpose of static site)

### ✅ Build-Time Optimization

- **CSS**: Tailwind → PurgeCSS → Minify → ~20KB gzipped
- **Images**: WebP + responsive srcset + lazy loading
- **HTML**: Minified, content-hashed assets
- **Alternative rejected**: Runtime optimization (slower, more complex)

### ✅ YAML Content + Go Structs

- **Why**: Non-devs can edit, version-controlled, type-safe
- **Format**: Frontmatter + structured data
- **Alternative rejected**: CMS (premature), plain HTML (not DRY)

### ✅ CDN-Ready Static Output

- **Flexibility**: Works on any static host (Cloudflare, Netlify, S3, Vercel)
- **Performance**: Edge serving, no server runtime
- **Alternative rejected**: Server-side rendering (unnecessary complexity)

## Migration Strategy

### Phase 1: Parallel Development (Week 1-2)

- Build static site generator
- Port React components to Go templates
- Deploy to staging (`website.dev.emergent-company.ai`)
- Visual regression testing

### Phase 2: Coexistence (Week 3)

- Static site on staging
- Admin landing page still in production
- User testing and performance validation

### Phase 3: Cutover (Week 4)

- Deploy static site to production domain
- Redirect admin landing routes
- Monitor and cleanup

**Rollback**: \u003c5 minutes (DNS/route restore)

## Validation Status

✅ **OpenSpec validation passed** (`openspec validate add-standalone-website-app --strict`)

All requirements properly formatted with scenarios, all deltas correctly structured.

## Open Questions for Stakeholders

1. **Domain strategy**: Root (`emergent-company.ai`), www subdomain, or separate marketing domain?
2. **Analytics**: Google Analytics, Plausible, or none?
3. **A/B testing**: In scope for initial launch?
4. **Update frequency**: How often will content change?
5. **Internationalization (i18n)**: Multi-language support needed?

## Next Steps

1. **Review this proposal** and provide feedback on architecture decisions
2. **Answer open questions** (domain, analytics, etc.)
3. **Approve to proceed** or request changes
4. **Start implementation** following tasks.md checklist

## Files Created

```
openspec/changes/add-standalone-website-app/
├── proposal.md                                    # Overview
├── design.md                                      # Architecture (200+ lines)
├── tasks.md                                       # Implementation (80+ tasks)
└── specs/
    ├── static-website-generation/spec.md         # NEW capability
    └── landing-page/spec.md                      # MODIFIED capability
```

## Estimated Effort

**Development**: 2-3 weeks (1 developer)

- Week 1: Core infrastructure + component port
- Week 2: Product pages + SEO + optimization
- Week 3: Testing + deployment + migration

**Maintenance**: Low (static site, minimal JS)

## Success Metrics

- **Performance**: Lighthouse \u003e90 all metrics
- **Load time**: \u003c2s First Contentful Paint
- **Bundle size**: \u003c200KB total (excluding large images)
- **Build time**: \u003c30s for full site rebuild
- **Deployment**: \u003c5min from Git push to live

---

**Questions or need clarification?** The design.md has deep technical details, and I can expand on any section.
