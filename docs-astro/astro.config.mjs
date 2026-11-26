import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import icon from 'astro-icon';

// The Memory docs site (Astro + Starlight) — the only docs system; the MkDocs
// Material build that published to GitHub Pages is retired.
//
// Served at the root of its own hostname (docs.memory.emergent-company.ai), so
// `base` is `/` and every internal link is root-relative. `DOCS_SITE` only
// moves the *canonical* origin — what `site` feeds (canonical tags, sitemap,
// Open Graph) — so the dev deployment can name itself
// (https://docs.dev.memory.emergent-company.ai) without the link graph moving.
const site = process.env.DOCS_SITE || 'https://docs.memory.emergent-company.ai';

export default defineConfig({
  site,
  base: '/',
  integrations: [
    // Iconify `lucide` set — the icon set the product UI draws (the gateway
    // renders `lucide--<name>` classes). No runtime JS: SVGs are inlined at
    // build time by `<Icon name="lucide:..."/>`.
    icon(),
    starlight({
      title: 'Memory',
      description:
        'Official documentation for the Memory platform, its Go and Python SDKs, and the Memory CLI',
      logo: {
        src: './src/assets/monogram.svg',
        alt: 'Memory',
      },
      favicon: '/favicon.svg',
      customCss: ['./src/styles/brand.css'],
      // Dark is the product's only theme, but a reader whose OS is light should
      // not be forced into it. `auto` is the default; the topbar switch flips
      // light <-> dark from there (see src/components/ThemeToggle.astro).
      components: {
        ThemeSelect: './src/components/ThemeToggle.astro',
      },
      // Pre-paint guard (mirrors the gateway's foucBackgroundGuard): paint the
      // theme's own page colour before CSS lands, so there is no white flash.
      // `ThemeProvider` sets `data-theme` on <html> before paint; the
      // `prefers-color-scheme` twin covers the very first load.
      head: [
        {
          tag: 'style',
          content: [
            "html{background-color:#0e1017}",
            "@media (prefers-color-scheme: light){html{background-color:#fbfbfd}}",
            "html[data-theme='light']{background-color:#fbfbfd}",
            "html[data-theme='dark']{background-color:#0e1017}",
          ].join(''),
        },
      ],
      // Frame styling mirrors app.css `.memory-md pre`: base-200 body, hairline
      // edge, --radius-box (0.5rem), 0.8375rem code at 1.6 line-height. The
      // colour slots are two themes so syntax highlighting follows the page
      // theme; the surface colours live in brand.css (--ec-frm-*) because they
      // have to track `--mem-*` per theme.
      expressiveCode: {
        themes: ['github-dark-default', 'github-light-default'],
        styleOverrides: {
          borderRadius: '0.5rem',
          codeFontSize: '0.8375rem',
          codeLineHeight: '1.6',
          uiFontSize: '0.75rem',
        },
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/emergent-company/emergent.memory',
        },
      ],
      // Starlight appends the page's path **relative to the project root**
      // (`src/content/docs/…`), so the base is the project root, not the
      // content directory: naming `…/docs-astro/src/content/docs/` here doubles
      // it.
      editLink: {
        baseUrl:
          'https://github.com/emergent-company/emergent.memory/edit/dev/docs-astro/',
      },
      sidebar: [
        {
          label: 'Getting Started',
          items: [
            { label: 'Start here', slug: 'user-guide/getting-started' },
            { label: 'Self-hosted', slug: 'user-guide/getting-started/self-hosted' },
            { label: 'Memory Cloud', slug: 'user-guide/getting-started/memory-cloud' },
          ],
        },
        {
          label: 'User Guide',
          items: [
            { label: 'Overview', slug: 'user-guide' },
            { label: 'Knowledge Graph', slug: 'user-guide/knowledge-graph' },
            { label: 'Documents', slug: 'user-guide/documents' },
            { label: 'Chat', slug: 'user-guide/chat' },
            { label: 'Agents', slug: 'user-guide/agents' },
            { label: 'Blueprints', slug: 'user-guide/blueprints' },
            { label: 'Branches', slug: 'user-guide/branches' },
            { label: 'Data Sources', slug: 'user-guide/datasources' },
            { label: 'API Tokens', slug: 'user-guide/api-tokens' },
            { label: 'Backups', slug: 'user-guide/backups' },
            { label: 'Notifications', slug: 'user-guide/notifications' },
            { label: 'Tasks', slug: 'user-guide/tasks' },
            { label: 'Integrations', slug: 'user-guide/integrations' },
            { label: 'Model Setup', slug: 'user-guide/model-setup' },
          ],
        },
        {
          label: 'Tutorials',
          items: [
            { label: 'Deploy on Railway', slug: 'tutorials/deploy-to-railway' },
            { label: 'Install a blueprint', slug: 'tutorials/install-a-blueprint' },
            { label: 'Configure an MCP share', slug: 'tutorials/configure-mcp-share' },
          ],
        },
        {
          label: 'Developer Guide',
          items: [
            { label: 'Overview', slug: 'developer-guide' },
            { label: 'Provider Setup', slug: 'developer-guide/provider-setup' },
            { label: 'Deploy Portal', slug: 'developer-guide/deploy-portal' },
            { label: 'MCP Servers', slug: 'developer-guide/mcp-servers' },
            { label: 'Extraction Pipeline', slug: 'developer-guide/extraction' },
            { label: 'Email Setup', slug: 'developer-guide/email-setup' },
            { label: 'Health & Ops', slug: 'developer-guide/health-ops' },
            { label: 'Security Scopes', slug: 'developer-guide/security-scopes' },
            { label: 'Background Scheduler', slug: 'developer-guide/scheduler' },
            { label: 'CLI Reference', slug: 'developer-guide/cli-reference' },
            { label: 'CLI Authentication', slug: 'developer-guide/cli-auth' },
            { label: 'Service Accounts', slug: 'developer-guide/service-accounts' },
            { label: 'Sandbox', slug: 'developer-guide/sandbox' },
            { label: 'GitHub Issue Coding', slug: 'developer-guide/github-issue-coding' },
            { label: 'MCP Relay', slug: 'developer-guide/mcp-relay' },
            { label: 'A2A and A2UI', slug: 'developer-guide/a2a-protocol' },
            { label: 'Schema', slug: 'developer-guide/schema' },
            { label: 'Schema Registry', slug: 'developer-guide/schema-registry' },
          ],
        },
        {
          label: 'SDKs',
          items: [
            {
              // One group per language. The Go SDK is the only one with pages
              // of its own (guides + the generated per-package reference), and
              // the reference stays collapsed under its own label so 34 package
              // pages do not push the rest of the sidebar off screen.
              label: 'Go SDK',
              items: [
                { label: 'Overview', slug: 'go-sdk' },
                { label: 'Authentication', slug: 'go-sdk/authentication' },
                { label: 'Multi-Tenancy', slug: 'go-sdk/multi-tenancy' },
                { label: 'Error Handling', slug: 'go-sdk/error-handling' },
                { label: 'Streaming', slug: 'go-sdk/streaming' },
                { label: 'Graph ID Model', slug: 'go-sdk/graph-id-model' },
                { label: 'Changelog', slug: 'go-sdk/changelog' },
                { label: 'Reference', items: [{ autogenerate: { directory: 'go-sdk/reference' } }] },
              ],
            },
            { label: 'Python SDK', slug: 'python-sdk' },
            { label: 'Swift SDK', slug: 'swift-sdk' },
          ],
        },
        { label: 'CLI', slug: 'cli' },
        { label: 'API Reference', slug: 'api-reference' },
      ],
    }),
  ],
});
