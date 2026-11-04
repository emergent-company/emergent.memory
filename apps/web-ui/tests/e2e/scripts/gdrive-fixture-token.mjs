#!/usr/bin/env node
// One-shot Google Drive OAuth token fetcher for the `gdrive-live` e2e fixture.
//
// Produces a REFRESH TOKEN (full drive scope) that scripts/gdrive-fixture-token
// can exchange to create/edit/trash real files. The memory-side OAuth client
// the test configures uses the restricted drive.readonly strategy, but the
// fixture needs write access, so it is provisioned separately against the SAME
// Google account the human later consents with (otherwise the source cannot
// see the fixture folder).
//
// Usage:
//   node scripts/gdrive-fixture-token.mjs --client <clientId> --secret <clientSecret>
//   # or set E2E_GDRIVE_CLIENT_ID / E2E_GDRIVE_CLIENT_SECRET
//
// Flow:
//   1. Build the Google consent URL (access_type=offline + prompt=consent so a
//      refresh token is always returned).
//   2. Print the URL — open it in a browser, sign in with the fixture account.
//   3. Google redirects to http://127.0.0.1:8787?code=... — this script serves
//      that callback, exchanges the code, and prints the refresh token exactly
//      once.
//
// Prereq: the OAuth client must allow the redirect URI
// `http://127.0.0.1:8787` (a Desktop-app client does this by default; for a
// "Web application" client register it under Authorized redirect URIs).

import http from 'node:http';

const args = process.argv.slice(2);
function flag(name) {
  const idx = args.indexOf(name);
  return idx >= 0 && idx + 1 < args.length ? args[idx + 1] : '';
}

const CLIENT_ID = flag('--client') || process.env.E2E_GDRIVE_CLIENT_ID || '';
const CLIENT_SECRET = flag('--secret') || process.env.E2E_GDRIVE_CLIENT_SECRET || '';
const PORT = Number(flag('--port') || process.env.E2E_GDRIVE_TOKEN_PORT || '8787');
const REDIRECT_URI = `http://127.0.0.1:${PORT}`;
const SCOPE = 'https://www.googleapis.com/auth/drive';

if (!CLIENT_ID || !CLIENT_SECRET) {
  console.error('Missing client id/secret. Pass --client/--secret or set');
  console.error('E2E_GDRIVE_CLIENT_ID / E2E_GDRIVE_CLIENT_SECRET.');
  process.exit(2);
}

const authParams = new URLSearchParams({
  client_id: CLIENT_ID,
  redirect_uri: REDIRECT_URI,
  response_type: 'code',
  scope: SCOPE,
  access_type: 'offline',
  prompt: 'consent',
});
const authorizeURL = `https://accounts.google.com/o/oauth2/v2/auth?${authParams.toString()}`;

console.log('');
console.log('Open this URL in a browser and sign in with the fixture Google account:');
console.log('');
console.log(authorizeURL);
console.log('');
console.log(`Waiting for Google to redirect back to ${REDIRECT_URI} ...`);
console.log('');

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, REDIRECT_URI);
  const code = url.searchParams.get('code');
  const err = url.searchParams.get('error');

  if (err) {
    console.error(`Authorization failed: ${err}`);
    res.writeHead(400, { 'Content-Type': 'text/plain' });
    res.end(`Authorization failed: ${err}`);
    server.close(() => process.exit(1));
    return;
  }

  if (!code) {
    res.writeHead(200, { 'Content-Type': 'text/plain' });
    res.end('No authorization code present. Return to the printed URL.');
    return;
  }

  const tokenBody = new URLSearchParams({
    grant_type: 'authorization_code',
    code,
    client_id: CLIENT_ID,
    client_secret: CLIENT_SECRET,
    redirect_uri: REDIRECT_URI,
  });

  try {
    const resp = await fetch('https://oauth2.googleapis.com/token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: tokenBody.toString(),
    });
    const payload = await resp.json();
    if (!resp.ok || !payload.refresh_token) {
      console.error('Token exchange failed.');
      if (payload.error_description) console.error(`  ${payload.error}: ${payload.error_description}`);
      res.writeHead(400, { 'Content-Type': 'text/plain' });
      res.end('Token exchange failed — see the terminal for details.');
      server.close(() => process.exit(1));
      return;
    }
    console.log('');
    console.log('Refresh token (set E2E_GDRIVE_FIXTURE_REFRESH_TOKEN to this):');
    console.log(payload.refresh_token);
    console.log('');
    res.writeHead(200, { 'Content-Type': 'text/plain' });
    res.end('Done. The refresh token is printed in the terminal.');
    server.close(() => process.exit(0));
  } catch (e) {
    console.error(`Token exchange request failed: ${e.message}`);
    res.writeHead(500, { 'Content-Type': 'text/plain' });
    res.end('Token exchange request failed.');
    server.close(() => process.exit(1));
  }
});

server.listen(PORT, '127.0.0.1');
