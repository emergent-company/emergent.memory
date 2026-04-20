#!/usr/bin/env node
/**
 * NestJS route extractor for codebase CLI.
 * Reads controller files, outputs newline-delimited JSON per route.
 *
 * Output contract (one JSON object per line):
 *   {"method":"GET","path":"/meeting/:meetingId","handler":"getMeeting","domain":"meeting","file":"apps/api/src/meeting/meeting.controller.ts","auth_required":true}
 *
 * Usage:
 *   node scripts/extract-routes.js [--glob PATTERN] [--domain-segment N]
 */

'use strict';

const fs = require('fs');
const path = require('path');

// ---------------------------------------------------------------------------
// Args
// ---------------------------------------------------------------------------
const args = process.argv.slice(2);
function getArg(flag, def) {
  const i = args.indexOf(flag);
  return i >= 0 && args[i + 1] ? args[i + 1] : def;
}

const globPattern = getArg('--glob', 'apps/*/src/**/*.controller.ts');
const domainSegment = parseInt(getArg('--domain-segment', '3'), 10);

// ---------------------------------------------------------------------------
// Recursive file finder (no external deps)
// ---------------------------------------------------------------------------
function matchesPattern(relPath, pattern) {
  const escaped = pattern
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*\*/g, '\x00')
    .replace(/\*/g, '[^/]*')
    .replace(/\x00/g, '.*')
    .replace(/\?/g, '[^/]');
  const re = new RegExp('^' + escaped + '$');
  return re.test(relPath);
}

function walkDir(dir, repoRoot, pattern, results) {
  let entries;
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    if (entry.name === 'node_modules' || entry.name === '.git' || entry.name === 'dist') continue;
    const fullPath = path.join(dir, entry.name);
    const relPath = path.relative(repoRoot, fullPath).replace(/\\/g, '/');
    if (entry.isDirectory()) {
      walkDir(fullPath, repoRoot, pattern, results);
    } else if (entry.isFile() && matchesPattern(relPath, pattern)) {
      results.push(fullPath);
    }
  }
}

// ---------------------------------------------------------------------------
// Auth decorator detection
// ---------------------------------------------------------------------------
const AUTH_DECORATORS = [
  '@Authenticate',
  '@AuthenticateOrAppToken',
  '@AuthenticateOrAppTokenWithPermissions',
  '@UseGuards',
];
const PUBLIC_DECORATOR = '@Public()';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function extractDomain(relFile, segment) {
  const parts = relFile.replace(/\\/g, '/').split('/');
  return parts[segment] ?? path.basename(path.dirname(relFile));
}

function stripQuotes(s) {
  return s.trim().replace(/^['"`]|['"`]$/g, '');
}

function normalizePath(controllerPath, routePath) {
  const base = controllerPath.startsWith('/') ? controllerPath : '/' + controllerPath;
  if (!routePath) return base.replace(/\/$/, '') || '/';
  const full = base.replace(/\/$/, '') + '/' + routePath.replace(/^\//, '');
  return full.replace(/\/+/g, '/');
}

function cleanPath(p) {
  return p.replace(/\/+/g, '/').replace(/\/$/, '') || '/';
}

// ---------------------------------------------------------------------------
// Parse a single controller file
// ---------------------------------------------------------------------------
function parseController(filePath, repoRoot) {
  const src = fs.readFileSync(filePath, 'utf8');
  const relFile = path.relative(repoRoot, filePath).replace(/\\/g, '/');
  const domain = extractDomain(relFile, domainSegment);

  // Extract @Controller('prefix')
  const controllerMatch = src.match(/@Controller\(([^)]*)\)/);
  let controllerPrefix = '';
  if (controllerMatch) {
    const inner = controllerMatch[1].trim();
    if (inner) {
      controllerPrefix = stripQuotes(inner);
    }
  }

  const routes = [];
  const lines = src.split('\n');

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Look for HTTP method decorator
    const httpMatch = line.match(/@(Get|Post|Put|Delete|Patch|Options|Head|All)\(([^)]*)\)/);
    if (!httpMatch) continue;

    const httpVerb = httpMatch[1].toUpperCase();
    const routeArg = stripQuotes(httpMatch[2]);

    // Scan surrounding lines for auth decorators
    const windowStart = Math.max(0, i - 8);
    const windowEnd = Math.min(lines.length - 1, i + 8);
    const windowText = lines.slice(windowStart, windowEnd + 1).join('\n');

    const hasPublic = windowText.includes(PUBLIC_DECORATOR);
    // Conservative default: auth_required=true unless @Public() is present
    const auth_required = !hasPublic;

    // Find handler method name — look forward for method signature
    let handler = '';
    for (let j = i + 1; j <= Math.min(lines.length - 1, i + 6); j++) {
      const trimmed = lines[j].trim();
      if (trimmed.startsWith('@') || trimmed.startsWith('//') || trimmed === '') continue;
      const methodMatch = trimmed.match(/^(?:async\s+)?(\w+)\s*\(/);
      if (methodMatch) {
        handler = methodMatch[1];
        break;
      }
    }

    if (!handler) continue;

    const fullPath = cleanPath(normalizePath(controllerPrefix, routeArg));

    routes.push({ method: httpVerb, path: fullPath, handler, domain, file: relFile, auth_required });
  }

  return routes;
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
const repoRoot = process.cwd();
const files = [];
walkDir(repoRoot, repoRoot, globPattern, files);

if (files.length === 0) {
  process.stderr.write(`extract-routes: no files matched glob "${globPattern}"\n`);
  process.exit(0);
}

process.stderr.write(`extract-routes: scanning ${files.length} controller files\n`);

for (const file of files) {
  try {
    const routes = parseController(file, repoRoot);
    for (const r of routes) {
      process.stdout.write(JSON.stringify(r) + '\n');
    }
  } catch (err) {
    process.stderr.write(`extract-routes: warn: ${file}: ${err}\n`);
  }
}
