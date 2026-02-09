import path from 'node:path';

import type {
  ApplicationProcessProfile,
  EnvironmentProfileId,
} from './types.js';
import { getRequiredEnvVar, getEnvVarWithDefault } from './env-validation.js';

const WORKSPACE_NAMESPACE = 'workspace-cli';

const DEFAULT_RESTART_POLICY = {
  maxRestarts: 5,
  minUptimeSec: 60,
  sleepBetweenMs: 5000,
  expBackoffInitialMs: 5000,
  expBackoffMaxMs: 120000,
} as const;

const DEFAULT_LOG_ROOT = 'logs';

function buildLogConfig(serviceId: string, subdir?: string) {
  const dir = subdir ? path.join(DEFAULT_LOG_ROOT, subdir) : DEFAULT_LOG_ROOT;
  return {
    outFile: path.join(dir, `${serviceId}.out.log`),
    errorFile: path.join(dir, `${serviceId}.error.log`),
  } as const;
}

// Required environment variables - will throw if not set
function getAdminPort(): string {
  return getRequiredEnvVar('ADMIN_PORT');
}

function getServerPort(): string {
  return getRequiredEnvVar('SERVER_PORT');
}

// Determine which server implementation to use
function getServerImplementation(): 'nodejs' | 'go' {
  const impl = getEnvVarWithDefault('SERVER_IMPLEMENTATION', 'go');
  if (impl !== 'nodejs' && impl !== 'go') {
    throw new Error(
      `Invalid SERVER_IMPLEMENTATION: ${impl}. Must be 'nodejs' or 'go'`
    );
  }
  return impl;
}

// Lazy-loaded profiles to avoid reading env vars at module load time
// This allows loadEnvironmentVariables() to run before these are accessed
let _applicationProfiles: readonly ApplicationProcessProfile[] | null = null;

// Node.js server configuration
function getNodeJsServerProfile(): ApplicationProcessProfile {
  return {
    processId: 'server',
    entryPoint: 'npm',
    args: ['run', 'start:dev'],
    cwd: 'apps/server',
    envProfile: 'development',
    restartPolicy: DEFAULT_RESTART_POLICY,
    logs: buildLogConfig('server', 'server'),
    healthCheck: {
      url: `http://localhost:${getServerPort()}/health`,
      timeoutMs: 180000, // 180s (3 min) - SWC compilation + NestJS init can take 2.5+ minutes on cold start
    },
    dependencies: [],
    namespace: WORKSPACE_NAMESPACE,
    defaultEnabled: true,
    setupCommands: ['npm install'],
    exposedPorts: [getServerPort()],
    environmentOverrides: {
      staging: {
        NODE_ENV: 'staging',
      },
      production: {
        NODE_ENV: 'production',
      },
    },
  };
}

// Go server configuration (uses air for hot reload)
function getGoServerProfile(): ApplicationProcessProfile {
  return {
    processId: 'server',
    entryPoint: '/root/go/bin/air',
    args: [],
    cwd: 'apps/server-go',
    envProfile: 'development',
    restartPolicy: DEFAULT_RESTART_POLICY,
    logs: buildLogConfig('server', 'server'),
    healthCheck: {
      url: `http://localhost:${getServerPort()}/health`,
      timeoutMs: 60000, // 60s - air needs time to build on first start
    },
    dependencies: [],
    namespace: WORKSPACE_NAMESPACE,
    defaultEnabled: true,
    setupCommands: ['/usr/local/go/bin/go build ./...'],
    exposedPorts: [getServerPort()],
    environmentOverrides: {
      staging: {
        GO_ENV: 'staging',
      },
      production: {
        GO_ENV: 'production',
      },
    },
  };
}

function getApplicationProfiles(): readonly ApplicationProcessProfile[] {
  if (_applicationProfiles === null) {
    const serverImpl = getServerImplementation();
    const serverProfile =
      serverImpl === 'go' ? getGoServerProfile() : getNodeJsServerProfile();

    _applicationProfiles = [
      {
        processId: 'admin',
        entryPoint: 'npm',
        args: ['run', 'dev'],
        cwd: 'apps/admin',
        envProfile: 'development',
        restartPolicy: DEFAULT_RESTART_POLICY,
        logs: buildLogConfig('admin', 'admin'),
        healthCheck: {
          url: `http://localhost:${getAdminPort()}/__workspace_health`,
          timeoutMs: 60000, // 60s - Vite can take 18s+ on cold start with dependency re-optimization
        },
        dependencies: [],
        namespace: WORKSPACE_NAMESPACE,
        defaultEnabled: true,
        setupCommands: ['npm install'],
        exposedPorts: [getAdminPort()],
        environmentOverrides: {
          staging: {
            VITE_APP_ENV: 'staging',
          },
          production: {
            VITE_APP_ENV: 'production',
          },
        },
      },
      serverProfile,
    ] satisfies readonly ApplicationProcessProfile[];
  }
  return _applicationProfiles;
}

export function listApplicationProcesses(): readonly ApplicationProcessProfile[] {
  return getApplicationProfiles();
}

export function getApplicationProcess(
  processId: string
): ApplicationProcessProfile {
  const profile = getApplicationProfiles().find(
    (item) => item.processId === processId
  );

  if (!profile) {
    throw new Error(`Unknown application process: ${processId}`);
  }

  return profile;
}

export function listDefaultApplicationProcesses(): readonly ApplicationProcessProfile[] {
  return getApplicationProfiles().filter((profile) => profile.defaultEnabled);
}

export function resolveEnvironmentOverrides(
  processId: string,
  environment: EnvironmentProfileId
): Readonly<Record<string, string>> {
  const profile = getApplicationProcess(processId);
  const overrides = profile.environmentOverrides?.[environment];
  return overrides ?? {};
}

export function getCurrentServerImplementation(): 'nodejs' | 'go' {
  return getServerImplementation();
}
