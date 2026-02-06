import type { StorybookConfig } from '@storybook/react-vite';
import tailwindcss from '@tailwindcss/vite';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const config: StorybookConfig = {
  framework: {
    name: '@storybook/react-vite',
    options: {},
  },
  staticDirs: ['../public'],
  stories: ['../src/**/*.stories.@(ts|tsx)'],
  addons: [
    '@storybook/addon-a11y',
    '@storybook/addon-links',
    '@storybook/addon-docs',
  ],
  docs: {},
  core: {
    disableTelemetry: true,
  },
  viteFinal: async (baseConfig) => {
    // Add Tailwind CSS plugin for proper CSS processing (including @iconify/tailwind4)
    baseConfig.plugins = [...(baseConfig.plugins || []), tailwindcss()];
    baseConfig.resolve = {
      ...(baseConfig.resolve ?? {}),
      alias: {
        ...(baseConfig.resolve?.alias ?? {}),
        // Resolve to admin app's src regardless of invocation CWD
        '@': path.resolve(__dirname, '../src'),
      },
    };
    // Allow external hostnames for dev server access
    baseConfig.server = {
      ...(baseConfig.server ?? {}),
      allowedHosts: ['localhost', 'storybook.dev.emergent-company.ai'],
      hmr: {
        // When accessed via external proxy, use the proxy's host/port for WebSocket
        clientPort: 443,
      },
    };
    return baseConfig;
  },
};

export default config;
