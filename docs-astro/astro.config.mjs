import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import icon from 'astro-icon';

// POC: Astro/Starlight port of docs/site (MkDocs Material).
// Published at https://emergent-company.github.io/emergent.memory/
export default defineConfig({
  site: 'https://emergent-company.github.io',
  base: '/emergent.memory',
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
      // Dark-only, like the product UI: no theme switcher.
      components: {
        ThemeSelect: './src/components/ThemeSelect.astro',
      },
      // Pre-paint guard (mirrors the gateway's foucBackgroundGuard).
      head: [
        {
          tag: 'style',
          content: 'html{color-scheme:dark;background-color:#0e1017}',
        },
      ],
      // The product UI is dark-only; keep code blocks dark in both slots.
      // Frame styling mirrors app.css `.memory-md pre`: base-200 body,
      // hairline edge, --radius-box (0.5rem), 0.8375rem code at 1.6 line-height.
      expressiveCode: {
        themes: ['github-dark-default'],
        styleOverrides: {
          borderRadius: '0.5rem',
          borderColor: 'oklch(1 0 0 / 0.07)',
          codeBackground: 'oklch(0.175 0.014 274)',
          codeFontSize: '0.8375rem',
          codeLineHeight: '1.6',
          uiFontSize: '0.75rem',
          frames: {
            editorBackground: 'oklch(0.215 0.016 274)',
            terminalBackground: 'oklch(0.175 0.014 274)',
            editorActiveTabBackground: 'oklch(0.175 0.014 274)',
            editorTabBarBackground: 'oklch(0.215 0.016 274)',
            tooltipSuccessBackground: 'oklch(0.71 0.15 152)',
          },
        },
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/emergent-company/emergent.memory',
        },
      ],
      editLink: {
        baseUrl:
          'https://github.com/emergent-company/emergent.memory/edit/dev/docs/site/',
      },
      sidebar: [
        {
          label: 'User Guide',
          items: [
            { label: 'Overview', slug: 'user-guide' },
            { label: 'Getting Started', slug: 'user-guide/getting-started' },
            { label: 'Knowledge Graph', slug: 'user-guide/knowledge-graph' },
            { label: 'Agents', slug: 'user-guide/agents' },
            { label: 'Branches', slug: 'user-guide/branches' },
            { label: 'Data Sources', slug: 'user-guide/datasources' },
          ],
        },
        {
          label: 'Developer Guide',
          items: [
            { label: 'Overview', slug: 'developer-guide' },
            { label: 'MCP Servers', slug: 'developer-guide/mcp-servers' },
            { label: 'CLI Reference', slug: 'developer-guide/cli-reference' },
            { label: 'Schema', slug: 'developer-guide/schema' },
            { label: 'Security Scopes', slug: 'developer-guide/security-scopes' },
          ],
        },
        {
          label: 'SDKs',
          items: [
            { label: 'Go SDK', slug: 'go-sdk' },
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
